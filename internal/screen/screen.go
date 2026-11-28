// Package screen wraps all calls to GNU screen: listing sessions, reading screen contents
// (hardcopy), typing text (stuff), attaching (screen -x) and starting new sessions.
//
// The parser for `screen -ls` is derived from internal/multiplex/screen.go in the project
// breakthrough (Apache-2.0, same author). Unlike the original, lines are split at tabs because
// screen session names may contain spaces.
package screen

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// State is the state of a session as reported by `screen -ls`.
type State string

// Possible session states.
const (
	StateAttached State = "attached"
	StateDetached State = "detached"
	StateDead     State = "dead"
	StateUnknown  State = "unknown"
)

// Session describes a screen session.
type Session struct {
	// ID is the full identifier "<pid>.<name>" as printed by screen. Every call to screen uses
	// this ID, because screen resolves bare names loosely.
	ID    string `json:"id"`
	PID   int    `json:"pid"`
	Name  string `json:"name"`
	State State  `json:"state"`
	// Multi is set when the session runs in multiuser mode.
	Multi bool `json:"multi,omitempty"`
}

// ErrNotInstalled reports that the screen binary was not found.
var ErrNotInstalled = errors.New("screen is not installed or not in PATH")

// execScreen runs screen with args in directory dir. It is a package variable so that unit tests
// can run without screen.
var execScreen = func(dir string, args ...string) (stdout, stderr []byte, err error) {
	path, err := exec.LookPath("screen")
	if err != nil {
		return nil, nil, ErrNotInstalled
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// run executes screen and turns a failure into an error that includes screen's output.
func run(dir string, args ...string) error {
	stdout, stderr, err := execScreen(dir, args...)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotInstalled) {
		return err
	}
	msg := strings.TrimSpace(string(stderr) + " " + string(stdout))
	if msg == "" {
		return fmt.Errorf("screen %s: %w", strings.Join(args, " "), err)
	}
	return fmt.Errorf("screen %s: %s", strings.Join(args, " "), msg)
}

// List returns all screen sessions of the current user.
//
// `screen -ls` often exits with a non-zero code even on success, so only its output counts. If the
// output contains neither a session nor "No Sockets found", something went wrong (for example a
// missing socket directory or one with the wrong mode), and the message is returned as an error
// instead of pretending there are no sessions.
func List() ([]Session, error) {
	stdout, stderr, err := execScreen("", "-ls")
	if errors.Is(err, ErrNotInstalled) {
		return nil, err
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("screen -ls: %w", err)
	}
	sessions, ok := parseList(string(stdout))
	if !ok {
		msg := strings.TrimSpace(string(stdout) + "\n" + string(stderr))
		return nil, fmt.Errorf("unexpected output from screen -ls: %s", msg)
	}
	return sessions, nil
}

// parseList parses the output of `screen -ls`. ok is false if the output contains neither
// sessions nor the message "No Sockets found".
func parseList(text string) (sessions []Session, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "No Sockets found") {
			ok = true
			continue
		}
		if s, found := parseLine(line); found {
			sessions = append(sessions, s)
			ok = true
		}
	}
	return sessions, ok
}

// parseLine recognizes a session line such as "\t12345.name\t(11/27/26 10:00:00)\t(Detached)".
// The timestamp column is optional and ignored; the last parenthesized field holds the state.
func parseLine(line string) (Session, bool) {
	if !strings.HasPrefix(line, "\t") {
		return Session{}, false
	}
	fields := strings.Split(strings.TrimPrefix(line, "\t"), "\t")
	if len(fields) < 2 {
		return Session{}, false
	}
	id := fields[0]
	pidStr, name, found := strings.Cut(id, ".")
	if !found || name == "" {
		return Session{}, false
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return Session{}, false
	}
	last := strings.TrimSpace(fields[len(fields)-1])
	if !strings.HasPrefix(last, "(") || !strings.HasSuffix(last, ")") {
		return Session{}, false
	}
	status := strings.ToLower(strings.Trim(last, "()"))
	s := Session{ID: id, PID: pid, Name: name, State: StateUnknown}
	switch {
	case strings.Contains(status, "dead"):
		s.State = StateDead
	case strings.Contains(status, "detached"):
		s.State = StateDetached
	case strings.Contains(status, "attached"):
		s.State = StateAttached
	}
	s.Multi = strings.Contains(status, "multi")
	return s, true
}

// Resolve finds the session for query: first by full ID, then by PID, then by unique name.
func Resolve(sessions []Session, query string) (Session, error) {
	for _, s := range sessions {
		if s.ID == query {
			return s, nil
		}
	}
	if pid, err := strconv.Atoi(query); err == nil {
		for _, s := range sessions {
			if s.PID == pid {
				return s, nil
			}
		}
	}
	var matches []Session
	for _, s := range sessions {
		if s.Name == query {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Session{}, fmt.Errorf("no screen session %q found", query)
	default:
		ids := make([]string, len(matches))
		for i, s := range matches {
			ids[i] = s.ID
		}
		return Session{}, fmt.Errorf("name %q is ambiguous, please use the full ID: %s",
			query, strings.Join(ids, ", "))
	}
}

// Find lists the sessions and resolves query.
func Find(query string) (Session, error) {
	sessions, err := List()
	if err != nil {
		return Session{}, err
	}
	return Resolve(sessions, query)
}
