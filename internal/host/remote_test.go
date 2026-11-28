package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/detect"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

// The test binary doubles as the switchboard on the remote side: the fake ssh below runs it with
// SB_HELPER set, and TestMain then serves a fake host instead of running the tests.
func TestMain(m *testing.M) {
	switch os.Getenv("SB_HELPER") {
	case "serve":
		Serve(os.Stdin, os.Stdout, fakeHost{log: os.Getenv("SB_LOG")}, "test")
		os.Exit(0)
	case "old":
		fmt.Fprintln(os.Stderr, `Error: unknown command "serve" for "switchboard"`)
		os.Exit(1)
	case "proto99":
		fmt.Println(`{"switchboard":"switchboard","protocol":99,"version":"9.9"}`)
		time.Sleep(time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

var updated = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

var fakeInfo = sessions.Info{
	Session:  screen.Session{ID: "42.agent", PID: 42, Name: "agent", State: screen.StateDetached},
	Agent:    &state.Record{Session: "42.agent", Window: "1", Agent: true, Status: state.StatusWaiting, UpdatedAt: updated},
	Detected: &detect.Result{Agent: "claude-code", Status: state.StatusWaiting, Question: "Proceed?", Options: []string{"Yes", "No"}},
	Status:   state.StatusWaiting,
	Message:  "Proceed?",
}

// fakeHost answers with fixed data and appends what is typed to the file log.
type fakeHost struct{ log string }

func (fakeHost) Name() string { return "" }
func (fakeHost) List() ([]sessions.Info, error, error) {
	return []sessions.Info{fakeInfo}, errors.New("cleanup failed"), nil
}
func (fakeHost) Find(q string) (screen.Session, int, error) {
	if q != "agent" {
		return screen.Session{}, 0, fmt.Errorf("no screen session %q found", q)
	}
	return fakeInfo.Session, 1, nil
}
func (fakeHost) Hardcopy(id string, window int, scrollback bool) ([]string, error) {
	return []string{fmt.Sprintf("%s %d %v", id, window, scrollback)}, nil
}
func (h fakeHost) Stuff(id string, window int, text string, enter bool) error {
	f, err := os.OpenFile(h.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s|%d|%s|%v\n", id, window, text, enter)
	return err
}
func (fakeHost) ReadDialog(id string, window int) (answer.Dialog, error) {
	if id == "1.idle" {
		return answer.Dialog{}, answer.ErrNoDialog
	}
	return answer.Dialog{Question: "Proceed?", Options: []string{"Yes", "No"}, Fingerprint: "fp"}, nil
}
func (fakeHost) SendChoice(id string, window, n int, label, fp string) error {
	if fp != "fp" {
		return answer.ErrChanged
	}
	return nil
}
func (fakeHost) AttachCmd(string) *exec.Cmd { return nil }
func (fakeHost) Close() error               { return nil }

// fakeSSH installs an ssh that drops its options and the host and runs the command with sh,
// like ssh does on the remote side. The host "down" fails like an unreachable machine, and
// "chatty" prints a greeting first, like a noisy login shell.
func fakeSSH(t *testing.T, helper string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo call >> "$SB_LOG.ssh"
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    -T|-t) shift ;;
    --) shift; break ;;
    *) break ;;
  esac
done
host=$1; shift
case "$host" in
  down) echo "ssh: connect to host down port 22: Connection refused" >&2; exit 255 ;;
  chatty) echo "Welcome to chatty" ;;
esac
exec sh -c "$*"
`
	path := filepath.Join(dir, "ssh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := sshCommand
	t.Cleanup(func() { sshCommand = orig })
	sshCommand = path
	t.Setenv("SB_HELPER", helper)
	log := filepath.Join(dir, "typed")
	t.Setenv("SB_LOG", log)
	return log
}

func self(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestRemoteRoundTrip(t *testing.T) {
	log := fakeSSH(t, "serve")
	r := NewRemote("box", "", self(t))
	defer r.Close()

	infos, warn, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	want := fakeInfo
	want.Host = "box"
	if len(infos) != 1 || !reflect.DeepEqual(infos[0], want) {
		got, _ := json.Marshal(infos)
		t.Errorf("List() = %s", got)
	}
	if warn == nil || warn.Error() != "cleanup failed" {
		t.Errorf("warn = %v", warn)
	}

	s, window, err := r.Find("agent")
	if err != nil || s.ID != "42.agent" || window != 1 {
		t.Errorf("Find() = %+v, %d, %v", s, window, err)
	}
	if _, _, err := r.Find("nope"); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("Find(nope) err = %v", err)
	}

	lines, err := r.Hardcopy("42.agent", 1, true)
	if err != nil || !reflect.DeepEqual(lines, []string{"42.agent 1 true"}) {
		t.Errorf("Hardcopy() = %q, %v", lines, err)
	}

	text := `a\b ^c $HOME 'q' "d" äöü`
	if err := r.Stuff("42.agent", 1, text, true); err != nil {
		t.Fatal(err)
	}
	typed, _ := os.ReadFile(log)
	if string(typed) != "42.agent|1|"+text+"|true\n" {
		t.Errorf("typed %q", typed)
	}

	d, err := r.ReadDialog("42.agent", 1)
	if err != nil || d.Fingerprint != "fp" || len(d.Options) != 2 {
		t.Errorf("ReadDialog() = %+v, %v", d, err)
	}
	if _, err := r.ReadDialog("1.idle", 0); !errors.Is(err, answer.ErrNoDialog) {
		t.Errorf("ReadDialog(idle) err = %v, want ErrNoDialog", err)
	}
	if err := r.SendChoice("42.agent", 1, 2, "No", "fp"); err != nil {
		t.Error(err)
	}
	if err := r.SendChoice("42.agent", 1, 2, "No", "old"); !errors.Is(err, answer.ErrChanged) {
		t.Errorf("SendChoice(old) err = %v, want ErrChanged", err)
	}
}

func TestRemoteReconnectsAfterClose(t *testing.T) {
	fakeSSH(t, "serve")
	r := NewRemote("box", "", self(t))
	defer r.Close()
	if _, _, err := r.List(); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, _, err := r.List(); err != nil {
		t.Fatalf("after Close: %v", err)
	}
}

func TestRemoteConnectionErrors(t *testing.T) {
	tests := []struct {
		name, helper, alias, binary string
		offline                     bool
		want                        string
	}{
		{"unreachable", "serve", "down", "", true, "Connection refused"},
		{"binary missing", "serve", "box", "/nonexistent/switchboard", false, "set binary"},
		{"too old", "old", "box", "", false, "too old"},
		{"other protocol", "proto99", "box", "", false, "protocol 99"},
		{"chatty login", "serve", "chatty", "", false, "Welcome to chatty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeSSH(t, tt.helper)
			binary := tt.binary
			if binary == "" {
				binary = self(t)
			}
			r := NewRemote("box", tt.alias, binary)
			defer r.Close()
			_, _, err := r.List()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if errors.Is(err, ErrOffline) != tt.offline {
				t.Errorf("offline = %v, want %v", errors.Is(err, ErrOffline), tt.offline)
			}
		})
	}
}

func TestRemoteBinaryAndAttach(t *testing.T) {
	if got := remoteBinary("~/.local/bin/switch board"); got != `"$HOME"/'.local/bin/switch board'` {
		t.Errorf("remoteBinary = %s", got)
	}
	if got := remoteBinary("it's"); got != `'it'\''s'` {
		t.Errorf("remoteBinary = %s", got)
	}
	orig := controlDirFor
	t.Cleanup(func() { controlDirFor = orig })
	controlDirFor = func() (string, error) { return "/run/sb", nil }
	r := NewRemote("box", "-oProxyCommand=evil", "")
	args := r.AttachCmd("42.my agent").Args[1:]
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "-t ") || strings.Contains(joined, "BatchMode") ||
		!strings.Contains(joined, "ControlPath=/run/sb/%C") ||
		!strings.HasSuffix(joined, "-- -oProxyCommand=evil screen -x '42.my agent'") {
		t.Errorf("attach args = %q", args)
	}
	args = r.sshArgs(false)
	if args[0] != "-T" || !strings.Contains(strings.Join(args, " "), "BatchMode=yes") || args[len(args)-2] != "--" {
		t.Errorf("request args = %q", args)
	}
}

func TestRemoteBacksOffAfterFailure(t *testing.T) {
	log := fakeSSH(t, "serve")
	r := NewRemote("box", "down", self(t))
	defer r.Close()
	_, _, err1 := r.List()
	_, _, err2 := r.List()
	if !errors.Is(err1, ErrOffline) || err2 != err1 {
		t.Errorf("errors = %v, %v", err1, err2)
	}
	calls, _ := os.ReadFile(log + ".ssh")
	if n := strings.Count(string(calls), "call"); n != 1 {
		t.Errorf("ssh started %d times, want 1", n)
	}
	r.Close()
	r.List()
	calls, _ = os.ReadFile(log + ".ssh")
	if n := strings.Count(string(calls), "call"); n != 2 {
		t.Errorf("after Close, ssh started %d times in total, want 2", n)
	}
}
