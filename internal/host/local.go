package host

import (
	"os"
	"os/exec"
	"strconv"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

// Local is this machine.
type Local struct {
	// Self is the screen session switchboard runs in ($STY), if any. Its screen is not read.
	Self string
}

// NewLocal returns this machine, with Self taken from $STY.
func NewLocal() Local { return Local{Self: os.Getenv("STY")} }

func (Local) Name() string { return "" }

func (l Local) List() ([]sessions.Info, error, error) {
	store, err := state.Open("")
	if err != nil {
		return nil, nil, err
	}
	return sessions.Collect(store, sessions.Options{Prune: true, Detect: true, Self: l.Self})
}

func (Local) Find(query string) (screen.Session, int, error) {
	s, err := screen.Find(query)
	if err != nil {
		return s, 0, err
	}
	if store, err := state.Open(""); err == nil {
		if rec, ok, _ := store.Get(s.ID); ok && rec.Window != "" {
			if w, err := strconv.Atoi(rec.Window); err == nil {
				return s, w, nil
			}
		}
	}
	return s, 0, nil
}

func (Local) Hardcopy(id string, window int, scrollback bool) ([]string, error) {
	if scrollback {
		return screen.Hardcopy(id, window)
	}
	return screen.Visible(id, window)
}

func (Local) Stuff(id string, window int, text string, enter bool) error {
	return screen.Stuff(id, window, text, enter)
}

func (Local) ReadDialog(id string, window int) (answer.Dialog, error) {
	return answer.Read(id, window)
}

func (Local) SendChoice(id string, window, n int, label, fingerprint string) error {
	return answer.Send(id, window, n, label, fingerprint)
}

func (Local) AttachCmd(id string) *exec.Cmd {
	args := screen.AttachArgs(id)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = screen.AttachEnv()
	return cmd
}

func (Local) Close() error { return nil }
