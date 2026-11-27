package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
)

// Swappable for tests.
var (
	sshCommand    = "ssh"
	callTimeout   = 20 * time.Second
	helloTimeout  = 20 * time.Second
	controlDirFor = controlDir
)

// ErrOffline means that the host could not be reached.
var ErrOffline = errors.New("host is offline")

// Remote is a machine reached through the system ssh. Only the host alias is passed to ssh, so
// that ~/.ssh/config applies with its user, port, keys and jump hosts.
type Remote struct {
	name   string
	alias  string
	binary string

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	stderr *bytes.Buffer
	nextID int
}

// NewRemote returns the host called name, reached with `ssh alias`. binary is the path of
// switchboard on that host; "~/" at its start stands for the home directory there.
func NewRemote(name, alias, binary string) *Remote {
	if alias == "" {
		alias = name
	}
	if binary == "" {
		binary = "switchboard"
	}
	return &Remote{name: name, alias: alias, binary: binary}
}

func (r *Remote) Name() string { return r.name }

// controlDir returns a private directory for the ssh control sockets. Its path stays short,
// because Unix socket paths are limited to about 108 bytes.
func controlDir() (string, error) {
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "switchboard")
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		dir = fmt.Sprintf("/tmp/switchboard-%d", os.Getuid())
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, os.Chmod(dir, 0o700)
}

// sshArgs returns the ssh arguments up to and including the host alias. Interactive sessions
// get a terminal and may ask for a password; the connection for requests may not.
func (r *Remote) sshArgs(interactive bool) []string {
	args := []string{"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}
	if interactive {
		args = append([]string{"-t"}, args...)
	} else {
		args = append([]string{"-T", "-o", "BatchMode=yes"}, args...)
	}
	if dir, err := controlDirFor(); err == nil {
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPersist=10m",
			"-o", "ControlPath="+filepath.Join(dir, "%C"))
	}
	return append(args, "--", r.alias)
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// remoteBinary returns the binary path for the remote shell. A leading "~/" becomes $HOME,
// because a quoted tilde is not expanded.
func remoteBinary(b string) string {
	if rest, ok := strings.CutPrefix(b, "~/"); ok {
		return `"$HOME"/` + shellQuote(rest)
	}
	return shellQuote(b)
}

func (r *Remote) AttachCmd(id string) *exec.Cmd {
	args := append(r.sshArgs(true), "screen", "-x", shellQuote(id))
	return exec.Command(sshCommand, args...)
}

// start opens the connection and reads the hello line. r.mu is held.
func (r *Remote) start() error {
	args := append(r.sshArgs(false), remoteBinary(r.binary), "serve", "--stdio")
	cmd := exec.Command(sshCommand, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = &limitedWriter{buf: stderr, max: 4096}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: starting ssh: %w", r.name, err)
	}
	lines := make(chan []byte, 1)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), maxLine)
		for sc.Scan() {
			lines <- append([]byte(nil), sc.Bytes()...)
		}
	}()
	r.cmd, r.stdin, r.lines, r.stderr = cmd, stdin, lines, stderr

	var line []byte
	select {
	case l, ok := <-lines:
		if !ok {
			return r.fail(nil)
		}
		line = l
	case <-time.After(helloTimeout):
		return r.fail(fmt.Errorf("%s: %w: no answer within %s", r.name, ErrOffline, helloTimeout))
	}
	var h hello
	if err := json.Unmarshal(line, &h); err != nil || h.Switchboard != "switchboard" {
		return r.fail(fmt.Errorf("%s: unexpected output instead of switchboard: %q "+
			"(does the login shell print something?)", r.name, shorten(string(line))))
	}
	if h.Protocol != ProtocolVersion {
		return r.fail(fmt.Errorf("%s runs switchboard %s with protocol %d, this one speaks protocol %d; "+
			"install the same version on both machines", r.name, h.Version, h.Protocol, ProtocolVersion))
	}
	return nil
}

// fail closes the connection and returns err, or, if err is nil, an error derived from how ssh
// ended. r.mu is held.
func (r *Remote) fail(err error) error {
	cmd, stderr, lines := r.cmd, r.stderr, r.lines
	r.stdin.Close()
	r.cmd, r.stdin, r.lines, r.stderr = nil, nil, nil, nil
	go func() {
		for range lines {
		}
	}()
	if err != nil {
		cmd.Process.Kill()
		go cmd.Wait()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		waitErr = <-done
	}
	msg := strings.TrimSpace(stderr.String())
	var exit *exec.ExitError
	switch {
	case errors.As(waitErr, &exit) && exit.ExitCode() == 255:
		return fmt.Errorf("%s: %w: %s", r.name, ErrOffline, msg)
	case errors.As(waitErr, &exit) && exit.ExitCode() == 127:
		return fmt.Errorf("%s: switchboard not found as %q (exit 127); set binary for this host "+
			"in the config, for example ~/.local/bin/switchboard", r.name, r.binary)
	case strings.Contains(msg, `unknown command "serve"`):
		return fmt.Errorf("%s: the switchboard there is too old for remote use; update it", r.name)
	case msg != "":
		return fmt.Errorf("%s: connection ended: %s", r.name, msg)
	}
	return fmt.Errorf("%s: connection ended", r.name)
}

func (r *Remote) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		r.fail(errors.New("closed"))
	}
	return nil
}

// call sends req and returns the response. Requests are sent one at a time over the same
// connection; a broken connection is reopened by the next call.
func (r *Remote) call(req request) (response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		if err := r.start(); err != nil {
			return response{}, err
		}
	}
	r.nextID++
	req.ID = r.nextID
	data, err := json.Marshal(req)
	if err != nil {
		return response{}, err
	}
	if _, err := r.stdin.Write(append(data, '\n')); err != nil {
		return response{}, r.fail(nil)
	}
	var line []byte
	select {
	case l, ok := <-r.lines:
		if !ok {
			return response{}, r.fail(nil)
		}
		line = l
	case <-time.After(callTimeout):
		return response{}, r.fail(fmt.Errorf("%s: %w: no answer within %s", r.name, ErrOffline, callTimeout))
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil || resp.ID != req.ID {
		return response{}, r.fail(fmt.Errorf("%s: invalid response %q", r.name, shorten(string(line))))
	}
	if resp.Error != "" {
		return resp, remoteError{msg: resp.Error, code: codes[resp.Code]}
	}
	return resp, nil
}

func (r *Remote) List() ([]sessions.Info, error, error) {
	resp, err := r.call(request{Op: opList})
	if err != nil {
		return nil, nil, err
	}
	var warn error
	if resp.Warn != "" {
		warn = errors.New(resp.Warn)
	}
	for i := range resp.Infos {
		resp.Infos[i].Host = r.name
	}
	return resp.Infos, warn, nil
}

func (r *Remote) Find(query string) (screen.Session, int, error) {
	resp, err := r.call(request{Op: opFind, Query: query})
	if err != nil {
		return screen.Session{}, 0, err
	}
	if resp.Session == nil {
		return screen.Session{}, 0, fmt.Errorf("%s: no session in response", r.name)
	}
	return *resp.Session, resp.Window, nil
}

func (r *Remote) Hardcopy(id string, window int, scrollback bool) ([]string, error) {
	resp, err := r.call(request{Op: opHardcopy, Session: id, Window: window, Scrollback: scrollback})
	return resp.Lines, err
}

func (r *Remote) Stuff(id string, window int, text string, enter bool) error {
	_, err := r.call(request{Op: opStuff, Session: id, Window: window, Text: text, Enter: enter})
	return err
}

func (r *Remote) ReadDialog(id string, window int) (answer.Dialog, error) {
	resp, err := r.call(request{Op: opReadDialog, Session: id, Window: window})
	if err != nil {
		return answer.Dialog{}, err
	}
	if resp.Dialog == nil {
		return answer.Dialog{}, fmt.Errorf("%s: no dialog in response", r.name)
	}
	return *resp.Dialog, nil
}

func (r *Remote) SendChoice(id string, window, n int, label, fingerprint string) error {
	_, err := r.call(request{Op: opSendChoice, Session: id, Window: window, N: n, Label: label, Fingerprint: fingerprint})
	return err
}

func shorten(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// limitedWriter keeps the first max bytes written to it.
type limitedWriter struct {
	mu  sync.Mutex
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
