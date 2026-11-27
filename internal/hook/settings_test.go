package hook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cmd = "/home/u/go/bin/switchboard hook"

func TestSnippetIsValidAndComplete(t *testing.T) {
	out, err := Snippet(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Hooks map[string][]matcherGroup `json:"hooks"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	for _, ev := range Events {
		groups := v.Hooks[ev.Name]
		if len(groups) != 1 || groups[0].Hooks[0].Command != cmd {
			t.Errorf("%s: %+v", ev.Name, groups)
		}
		if ev.ToolMatcher != (groups[0].Matcher == "*") {
			t.Errorf("%s: matcher %q", ev.Name, groups[0].Matcher)
		}
	}
}

func TestMergeKeepsOtherContentAndOrder(t *testing.T) {
	in := `{
  "model": "x",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/opt/guard.py"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]}
    ]
  },
  "permissions": {"allow": ["Bash(ls)"]}
}`
	out, changed, err := Merge([]byte(in), cmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("changed = false")
	}
	s := string(out)
	if !(strings.Index(s, `"model"`) < strings.Index(s, `"hooks"`) && strings.Index(s, `"hooks"`) < strings.Index(s, `"permissions"`)) {
		t.Errorf("top-level order changed:\n%s", s)
	}
	for _, keep := range []string{"/opt/guard.py", "notify-send done", "Bash(ls)"} {
		if !strings.Contains(s, keep) {
			t.Errorf("lost %q:\n%s", keep, s)
		}
	}
	if strings.Index(s, "/opt/guard.py") > strings.Index(s, cmd) {
		t.Errorf("own PreToolUse entry should come after the existing one:\n%s", s)
	}
	if got := strings.Count(s, cmd); got != len(Events) {
		t.Errorf("%d switchboard entries, want %d", got, len(Events))
	}
}

func TestMergeIsIdempotentAndReplacesOldPath(t *testing.T) {
	first, _, err := Merge(nil, "/old/path/switchboard hook", false)
	if err != nil {
		t.Fatal(err)
	}
	second, changed, err := Merge(first, cmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || strings.Contains(string(second), "/old/path") || strings.Count(string(second), cmd) != len(Events) {
		t.Errorf("old path not replaced:\n%s", second)
	}
	third, changed, err := Merge(second, cmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(third) != string(second) {
		t.Errorf("second install changed the file:\n%s", third)
	}
}

func TestMergeRemove(t *testing.T) {
	in := `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}, {"type": "command", "command": "'/my dir/switchboard' hook"}]}], "PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "switchboard hook"}]}]}}`
	out, changed, err := Merge([]byte(in), "", true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !changed || strings.Contains(s, "switchboard") || strings.Contains(s, "PreToolUse") || !strings.Contains(s, "notify-send done") {
		t.Errorf("Merge(remove) =\n%s", s)
	}
	out, _, _ = Merge([]byte(`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "switchboard hook"}]}]}, "x": 1}`), "", true)
	if strings.Contains(string(out), "hooks") {
		t.Errorf("empty hooks object should be removed:\n%s", out)
	}
}

func TestMergeRejectsInvalidSettings(t *testing.T) {
	for _, in := range []string{"[1,2]", "{broken", `{"hooks": []}`, `{"a":1} {"b":2}`} {
		if _, _, err := Merge([]byte(in), cmd, false); err == nil {
			t.Errorf("Merge(%q) should fail", in)
		}
	}
}

func TestIsSwitchboardCommand(t *testing.T) {
	yes := []string{"switchboard hook", "/home/u/go/bin/switchboard hook", "'/my dir/switchboard' hook", "  switchboard hook  "}
	no := []string{"switchboard notify", "/opt/other hook", "switchboard-hook", "echo switchboard hook x"}
	for _, c := range yes {
		if !IsSwitchboardCommand(c) {
			t.Errorf("IsSwitchboardCommand(%q) = false", c)
		}
	}
	for _, c := range no {
		if IsSwitchboardCommand(c) {
			t.Errorf("IsSwitchboardCommand(%q) = true", c)
		}
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"/home/u/go/bin/switchboard": "/home/u/go/bin/switchboard",
		"/my dir/switchboard":        "'/my dir/switchboard'",
		"/it's/switchboard":          `'/it'\''s/switchboard'`,
	}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandRejectsTemporaryBuilds(t *testing.T) {
	origLook, origExe := lookPath, executable
	t.Cleanup(func() { lookPath, executable = origLook, origExe })
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	executable = func() (string, error) { return "/tmp/go-build123/b001/exe/switchboard", nil }
	if _, err := Command(); err == nil {
		t.Error("temporary build path was accepted")
	}

	lookPath = func(string) (string, error) { return "/usr/local/bin/switchboard", nil }
	got, err := Command()
	if err != nil || got != "/usr/local/bin/switchboard hook" {
		t.Errorf("Command() = %q, %v", got, err)
	}
}

func TestWriteSettingsFollowsSymlinkKeepsModeAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"old":true}`), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "settings.json.bak")
	if err := WriteSettings(link, []byte(`{"new":true}`), backup); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink was replaced: %v", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != `{"new":true}` {
		t.Errorf("target = %q", data)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(backup); string(b) != `{"old":true}` {
		t.Errorf("backup = %q", b)
	}
}
