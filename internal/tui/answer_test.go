package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/detect"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

var bashOptions = []string{"Yes", "Yes, and always allow access to", "Yes, and switch to auto mode", "No"}

func waiting(id string, options ...string) sessions.Info {
	s := agent(id, state.StatusWaiting, "Do you want to proceed?")
	s.Detected = &detect.Result{Agent: "claude-code", Status: state.StatusWaiting, Question: "Do you want to proceed?", Options: options}
	return s
}

type answerFakes struct {
	reads int
	sent  []string
	err   error
}

func setupAnswer(t *testing.T) *answerFakes {
	t.Helper()
	setup(t)
	f := &answerFakes{}
	or, os_ := readDialog, sendChoice
	t.Cleanup(func() { readDialog, sendChoice = or, os_ })
	readDialog = func(hostName, id string, window int) (answer.Dialog, error) {
		f.reads++
		if f.err != nil {
			return answer.Dialog{}, f.err
		}
		return answer.Dialog{Question: "Do you want to proceed?", Options: bashOptions, Fingerprint: "fp"}, nil
	}
	sendChoice = func(hostName, id string, window int, n int, label, fingerprint string) error {
		f.sent = append(f.sent, strings.Join([]string{id, string(rune('0' + n)), label, fingerprint}, "|"))
		return nil
	}
	return f
}

func footer(m Model) string { return ansi.Strip(m.footer()) }

func TestAnswerAfterConfirmation(t *testing.T) {
	f := setupAnswer(t)
	m := loaded(t, "", waiting("1.a", bashOptions...))
	if !strings.Contains(footer(m), "1-4 answer") {
		t.Errorf("help line without answer key: %q", footer(m))
	}
	m, cmd := update(t, m, key("4"))
	if m.mode != modeAnswer || cmd == nil || !strings.Contains(footer(m), "Reading") {
		t.Fatalf("mode %v, footer %q", m.mode, footer(m))
	}
	m, _ = update(t, m, key("y")) // before the dialog was read: ignored
	if m.mode != modeAnswer || len(f.sent) != 0 {
		t.Fatal("confirmed before the dialog was read")
	}
	m, _ = update(t, m, cmd())
	if want := `Answer 1.a with y (other key cancels): 4 "No"`; !strings.Contains(footer(m), want) {
		t.Fatalf("footer %q, want %q", footer(m), want)
	}
	m, cmd = update(t, m, key("y"))
	if m.mode != modeList || cmd == nil {
		t.Fatal("y did not send")
	}
	m, _ = update(t, m, cmd())
	if len(f.sent) != 1 || f.sent[0] != "1.a|4|No|fp" {
		t.Errorf("sent %q", f.sent)
	}
	if !strings.Contains(m.status, "Answered 1.a: 4. No") {
		t.Errorf("status %q", m.status)
	}
}

func TestAnswerBurstDoesNotConfirm(t *testing.T) {
	f := setupAnswer(t)
	m := loaded(t, "", waiting("1.a", bashOptions...))
	m, cmd := update(t, m, key("4y"))
	if m.mode != modeAnswer || m.answerReady {
		t.Fatalf("mode %v ready %v", m.mode, m.answerReady)
	}
	var read Model = m
	for _, msg := range collectMsgs(cmd) {
		read, _ = update(t, read, msg)
	}
	if !read.answerReady || read.mode != modeAnswer || len(f.sent) != 0 {
		t.Errorf("burst confirmed: mode %v ready %v sent %q", read.mode, read.answerReady, f.sent)
	}
}

// collectMsgs runs cmd and the commands of a batch and returns their messages.
func collectMsgs(cmd tea.Cmd) []any {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []any
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []any{msg}
}

func TestAnswerOtherKeyCancels(t *testing.T) {
	f := setupAnswer(t)
	m := loaded(t, "", waiting("1.a", bashOptions...))
	m, cmd := update(t, m, key("4"))
	m, _ = update(t, m, cmd())
	m, cmd = update(t, m, key("n"))
	if m.mode != modeList || cmd != nil || len(f.sent) != 0 || m.status != "Nothing sent." {
		t.Errorf("mode %v status %q sent %q", m.mode, m.status, f.sent)
	}
	m, _ = update(t, m, dialogMsg{id: "1.a", n: 4}) // late answer of an old read
	if m.mode != modeList {
		t.Error("late dialog reopened the confirmation")
	}
}

func TestAnswerRefusals(t *testing.T) {
	dead := waiting("1.dead", bashOptions...)
	dead.State = screen.StateDead
	tests := []struct {
		name string
		s    sessions.Info
		self string
		key  string
		want string
	}{
		{"idle", agent("1.a", state.StatusIdle, ""), "", "1", "shows no dialog"},
		{"no choices", waiting("1.a"), "", "1", "cannot be read"},
		{"beyond the choices", waiting("1.a", "Yes", "No"), "", "3", "only 2 choices"},
		{"dead", dead, "", "1", "dead"},
		{"self", waiting("1.self", bashOptions...), "1.self", "1", "runs in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setupAnswer(t)
			m := loaded(t, tt.self, tt.s)
			m, cmd := update(t, m, key(tt.key))
			if m.mode != modeList || cmd != nil || f.reads != 0 || !strings.Contains(m.status, tt.want) {
				t.Errorf("mode %v status %q reads %d", m.mode, m.status, f.reads)
			}
		})
	}
}

func TestAnswerReadErrorAndChangedDialog(t *testing.T) {
	f := setupAnswer(t)
	f.err = answer.ErrNoDialog
	m := loaded(t, "", waiting("1.a", bashOptions...))
	m, cmd := update(t, m, key("4"))
	m, _ = update(t, m, cmd())
	if m.mode != modeList || !strings.Contains(m.status, "no dialog") {
		t.Errorf("mode %v status %q", m.mode, m.status)
	}
	m, _ = update(t, m, answeredMsg{id: "1.a", n: 4, label: "No", err: answer.ErrChanged})
	if !m.statusErr || !strings.Contains(m.status, "changed") {
		t.Errorf("status %q", m.status)
	}
}

func TestAnswerHelpOnlyWithChoices(t *testing.T) {
	setupAnswer(t)
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""))
	if strings.Contains(footer(m), "answer") {
		t.Errorf("help line offers answering: %q", footer(m))
	}
}

func TestTypeSomethingPointsToSend(t *testing.T) {
	setupAnswer(t)
	m := loaded(t, "", waiting("1.a", "Tea", "Coffee", "Type something."))
	m, _ = update(t, m, answeredMsg{id: "1.a", n: 3, label: "Type something."})
	if !strings.Contains(m.status, "with s") {
		t.Errorf("status %q", m.status)
	}
}
