// Package answer picks a choice in the dialog an agent shows, such as a permission prompt or a
// question with numbered choices.
//
// The agent acts on the digit of a choice at once, without confirmation, and the numbers differ
// between dialogs: "No" is choice 3 when creating a file but choice 4 for a shell command, where
// 3 switches the agent to auto mode. So choices are always named by their label, and a choice is
// only sent if the screen still shows the same dialog that the user looked at.
package answer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jagottsicher/switchboard/internal/detect"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/state"
)

// Swappable for tests.
var (
	readScreen = screen.Visible
	stuff      = screen.Stuff
)

// maxChoice is the highest choice that can be picked with a single digit.
const maxChoice = 9

var (
	// ErrNoDialog means that the screen shows no agent waiting in a dialog.
	ErrNoDialog = errors.New("no dialog is open")
	// ErrNoChoices means that a dialog is open but its choices could not be read, as with the
	// trust dialog, which has no numbers.
	ErrNoChoices = errors.New("the choices of the dialog cannot be read; attach to answer it")
	// ErrChanged means that the screen no longer shows the dialog the choice was made for.
	ErrChanged = errors.New("the dialog has changed; nothing was sent")
)

// Dialog is a dialog as the screen shows it.
type Dialog struct {
	Question string
	Options  []string
	// Fingerprint identifies the screen the dialog was read from. A waiting dialog does not
	// change, so the same fingerprint means the same dialog, even if two dialogs ask the same
	// question with the same choices.
	Fingerprint string
}

// Choices lists the choices with their numbers, for messages.
func (d Dialog) Choices() string {
	parts := make([]string, len(d.Options))
	for i, o := range d.Options {
		parts[i] = fmt.Sprintf("%d %s", i+1, o)
	}
	return strings.Join(parts, ", ")
}

// Read reads the dialog that the session shows in window.
func Read(id string, window int) (Dialog, error) {
	lines, err := readScreen(id, window)
	if err != nil {
		return Dialog{}, err
	}
	r, ok := detect.Detect(lines)
	if !ok || r.Status != state.StatusWaiting {
		return Dialog{}, ErrNoDialog
	}
	if len(r.Options) == 0 {
		return Dialog{}, ErrNoChoices
	}
	return Dialog{Question: r.Question, Options: r.Options, Fingerprint: fingerprint(lines)}, nil
}

func fingerprint(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// Match returns the number of the choice with the given label. Labels are compared without
// regard to case, and only whole labels count: "Yes" never picks "Yes, and switch to auto
// mode".
func Match(d Dialog, label string) (int, error) {
	label = strings.TrimSpace(label)
	n := 0
	for i, o := range d.Options {
		if strings.EqualFold(o, label) {
			if n != 0 {
				return 0, fmt.Errorf("%q names more than one choice: %s", label, d.Choices())
			}
			n = i + 1
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("the dialog has no choice %q; it offers: %s", label, d.Choices())
	}
	if n > maxChoice {
		return 0, fmt.Errorf("choice %d (%s) cannot be picked with one key; attach to answer", n, label)
	}
	return n, nil
}

// Send picks choice n, labeled label, in the dialog that was read with the given fingerprint.
// It reads the screen again first and sends nothing unless it still shows that dialog with
// that label at number n. Only the digit is sent, without Enter: the agent acts on it at once.
func Send(id string, window int, n int, label, fingerprint string) error {
	d, err := Read(id, window)
	if errors.Is(err, ErrNoDialog) || errors.Is(err, ErrNoChoices) {
		return ErrChanged
	}
	if err != nil {
		return err
	}
	if d.Fingerprint != fingerprint || n < 1 || n > len(d.Options) || n > maxChoice || d.Options[n-1] != label {
		return ErrChanged
	}
	return stuff(id, window, strconv.Itoa(n), false)
}
