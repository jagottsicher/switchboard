package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/state"
)

// The fixtures are hardcopies taken with screen 4.09 from Claude Code 2.1, cut to the bottom
// lines. They go through the same decoding as real hardcopies. The utf8-* files are the same
// screens as a screen version that writes correct UTF-8 would produce them.
func fixture(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return screen.ParseHardcopy(data)
}

func TestClaudeCodeFixtures(t *testing.T) {
	tests := []struct {
		name     string
		status   state.Status
		question string
		options  []string
	}{
		{"idle", state.StatusIdle, "", nil},
		{"idle-draft", state.StatusIdle, "", nil},
		{"working", state.StatusWorking, "", nil},
		{"working-tokens", state.StatusWorking, "", nil},
		{"permission", state.StatusWaiting, "Do you want to create sb-detect-probe.txt?",
			[]string{"Yes", "Yes, and switch to accept edits (auto-approve file edits and common file", "No"}},
		{"question", state.StatusWaiting, "Which do you prefer: tea or coffee?",
			[]string{"Tea", "Coffee", "Type something.", "Chat about this"}},
		{"trust", state.StatusWaiting, "", nil},
		{"denied", state.StatusIdle, "", nil},
		{"interrupted", state.StatusIdle, "", nil},
		{"question-declined", state.StatusIdle, "", nil},
		{"permission-narrow", state.StatusWaiting, "Do you want to create sb-detect-probe.txt?",
			[]string{"Yes", "Yes, and switch to accept edits", "No"}},
		{"permission-bash", state.StatusWaiting, "Do you want to proceed?",
			[]string{"Yes", "Yes, and always allow access to", "Yes, and switch to auto mode · auto mode handles these prompts for you", "No"}},
		{"menu-config", state.StatusUnknown, "", nil},
		{"menu-permissions", state.StatusUnknown, "", nil},
		{"utf8-idle", state.StatusIdle, "", nil},
		{"utf8-working", state.StatusWorking, "", nil},
		{"utf8-permission", state.StatusWaiting, "Do you want to create sb-detect-probe.txt?",
			[]string{"Yes", "Yes, and switch to accept edits (auto-approve file edits and common file", "No"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Detect(fixture(t, tt.name))
			if !ok {
				t.Fatal("not recognized")
			}
			if got.Status != tt.status || got.Question != tt.question || !reflect.DeepEqual(got.Options, tt.options) {
				t.Errorf("Detect() = %+v, want %s %q %q", got, tt.status, tt.question, tt.options)
			}
			if got.Agent != "claude-code" {
				t.Errorf("agent = %q", got.Agent)
			}
		})
	}
}

func TestOtherScreensAreNotRecognized(t *testing.T) {
	screens := map[string]string{
		"empty":       "",
		"shell":       "user@host:~/src$ ls\nfoo bar\nuser@host:~/src$ ",
		"fancy shell": "~/src on main\n❯ git status\nnothing to commit\n❯ ",
		"log line":    "12:00 press esc to interrupt the job\n12:01 done\n$ ",
		"menu":        "Pick one:\n1. a\n2. b\nEsc to cancel",
		"transcript":  "o Count from 1 to 3\n1\n2\n3\n$ ",
	}
	for name, text := range screens {
		if got, ok := Detect(strings.Split(text, "\n")); ok {
			t.Errorf("%s: recognized as %+v", name, got)
		}
	}
}

func TestDialogIgnoresListsAboveIt(t *testing.T) {
	lines := []string{
		"Plan:",
		"1. Write the parser",
		"2. Add tests",
		"Do you want to proceed?",
		"❯ 1. Yes",
		"  2. No",
		"Esc to cancel",
	}
	got, ok := Detect(lines)
	if !ok || got.Status != state.StatusWaiting || got.Question != "Do you want to proceed?" ||
		!reflect.DeepEqual(got.Options, []string{"Yes", "No"}) {
		t.Errorf("Detect() = %+v, %v", got, ok)
	}
}

// Agent output shown secondhand, by peek in a shell or in the preview of the overview, must not
// make that session an agent.
func TestSecondhandAgentScreensAreNotRecognized(t *testing.T) {
	for _, name := range []string{"idle", "working", "utf8-working", "permission"} {
		for follow, line := range map[string]string{
			"shell prompt":  "user@host:~$ ",
			"overview help": "enter attach  s send  / filter  a agents  r refresh  q quit  (back: Ctrl-a a d)",
			"overview msg":  "Sent to 123.agent.",
		} {
			lines := append(fixture(t, name), "", line)
			if got, ok := Detect(lines); ok {
				t.Errorf("%s followed by %s: recognized as %+v", name, follow, got)
			}
		}
	}
}

func TestDialogJoinsWrappedQuestions(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"wrapped", []string{" ────", " Do you want to", " create x.txt?", " ❯ 1. Yes", "   2. No"}, "Do you want to create x.txt?"},
		{"stops at a sentence", []string{" Done.", " Do you want to", " create x.txt?", " ❯ 1. Yes", "   2. No"}, "Do you want to create x.txt?"},
		{"stops at other indentation", []string{"  Bash command", " Do you want to proceed?", " ❯ 1. Yes", "   2. No"}, "Do you want to proceed?"},
		{"stops at control characters", []string{" \x10 Drink", " Tea or coffee?", " ❯ 1. Tea", "   2. Coffee"}, "Tea or coffee?"},
		{"at most three lines", []string{" a", " b", " c", " d", " e?", " ❯ 1. Yes", "   2. No"}, "b c d e?"},
		{"capitalized continuation is cut", []string{" Do you want to create", " Figurenbibel_v1.txt?", " ❯ 1. Yes", "   2. No"}, "Figurenbibel_v1.txt?"},
		{"new sentence", []string{" Bash command", " touch x.txt", " Do you want to proceed?", " ❯ 1. Yes", "   2. No"}, "Do you want to proceed?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := dialog(tt.lines); got != tt.want {
				t.Errorf("question = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPermissionWithUnreadableChoicesIsWaiting(t *testing.T) {
	lines := []string{
		" Do you want to",
		" create x.txt?",
		" ❯ 1. Yes",
		"   2. Yes, and",
		"      switch to",
		"      accept edits",
		"      (auto-approve",
		"      file edits)",
		"   3. No",
		"",
		" Esc to cancel · Tab to amend",
	}
	got, ok := Detect(lines)
	if !ok || got.Status != state.StatusWaiting {
		t.Errorf("Detect() = %+v, %v, want waiting", got, ok)
	}
}
