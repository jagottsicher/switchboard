// Package host gives the overview and the commands one interface to the screen sessions of this
// machine and of other machines, which are reached over ssh.
//
// A remote machine runs the same binary as `switchboard serve --stdio`. The client starts it
// once through the system ssh and keeps the connection open: it writes one JSON request per line
// and reads one JSON response per line. Nothing but the protocol goes over standard output.
package host

import (
	"os/exec"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
)

// Host is a machine with screen sessions.
type Host interface {
	// Name is the configured name of the host, empty for this machine.
	Name() string
	// List returns the sessions with the status of their agents. A problem that leaves the
	// list valid is returned as warn.
	List() (infos []sessions.Info, warn error, err error)
	// Find resolves a session ID, PID or unique name. window is the window an agent reported,
	// or 0.
	Find(query string) (s screen.Session, window int, err error)
	// Hardcopy reads the screen of a window, with the scrollback if scrollback is set.
	Hardcopy(id string, window int, scrollback bool) ([]string, error)
	// Stuff types text into a window and presses Enter if enter is set.
	Stuff(id string, window int, text string, enter bool) error
	// ReadDialog and SendChoice answer a dialog, see package answer.
	ReadDialog(id string, window int) (answer.Dialog, error)
	SendChoice(id string, window, n int, label, fingerprint string) error
	// AttachCmd returns the command that attaches the caller's terminal to a session.
	AttachCmd(id string) *exec.Cmd
	// Close ends the connection to the host, if there is one.
	Close() error
}
