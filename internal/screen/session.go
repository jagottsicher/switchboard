package screen

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// newTimeout limits how long New waits for the new session to appear in `screen -ls`.
var newTimeout = 2 * time.Second

// AttachArgs returns the arguments for attaching to session id. It always uses `screen -x`
// (share), never `-D -r` (take the session away from other clients).
func AttachArgs(id string) []string {
	return []string{"screen", "-x", id}
}

// AttachEnv returns the environment for the screen process when attaching. STY is removed so
// that screen also starts from inside a screen session instead of talking to the outer one.
func AttachEnv() []string {
	env := os.Environ()
	out := env[:0:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "STY=") {
			out = append(out, e)
		}
	}
	return out
}

// AttachCommand builds the command for attaching, using the caller's terminal.
func AttachCommand(id string) *exec.Cmd {
	args := AttachArgs(id)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = AttachEnv()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

// InsideScreen reports whether the current process runs inside a screen session itself.
func InsideScreen() bool {
	return os.Getenv("STY") != ""
}

// ValidateName checks a name for a new session. A dot separates PID and name in screen IDs,
// digits only would be confused with a PID, and whitespace makes addressing awkward.
func ValidateName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("the session name must not be empty")
	case strings.ContainsAny(name, ". \t\n/"):
		return fmt.Errorf("the session name %q must not contain dots, whitespace or slashes", name)
	}
	if _, err := strconv.Atoi(name); err == nil {
		return fmt.Errorf("the session name %q must not consist of digits only", name)
	}
	return nil
}

// New starts command in a new, detached session name in directory dir and returns the new
// session. A name that is already in use is rejected.
func New(name, dir string, command []string) (Session, error) {
	if err := ValidateName(name); err != nil {
		return Session{}, err
	}
	if len(command) == 0 {
		return Session{}, fmt.Errorf("no command given")
	}
	before, err := List()
	if err != nil {
		return Session{}, err
	}
	known := map[string]bool{}
	for _, s := range before {
		if s.Name == name && s.State != StateDead {
			return Session{}, fmt.Errorf("a session named %q already exists (%s)", name, s.ID)
		}
		known[s.ID] = true
	}

	args := append([]string{"-dmS", name}, command...)
	if err := run(dir, args...); err != nil {
		return Session{}, err
	}

	deadline := time.Now().Add(newTimeout)
	for {
		sessions, err := List()
		if err == nil {
			for _, s := range sessions {
				if s.Name == name && !known[s.ID] {
					return s, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return Session{}, fmt.Errorf("session %q did not appear in screen -ls after starting "+
				"(did the command exit immediately?)", name)
		}
		sleep(50 * time.Millisecond)
	}
}
