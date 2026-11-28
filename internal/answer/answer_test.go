package answer

import (
	"errors"
	"strings"
	"testing"
)

var bashDialog = []string{
	" Bash command",
	" touch x.txt",
	" Do you want to proceed?",
	" ❯ 1. Yes",
	"   2. Yes, and always allow access to",
	"      /home/user/project from this project",
	"   3. Yes, and switch to auto mode · auto mode handles these prompts for you",
	"   4. No",
	"",
	" Esc to cancel · Tab to amend",
}

type sent struct {
	id, text string
	window   int
	enter    bool
}

// fake serves screens in turn (the last one repeats) and records what is sent.
func fake(t *testing.T, screens ...[]string) *[]sent {
	t.Helper()
	origRead, origStuff := readScreen, stuff
	t.Cleanup(func() { readScreen, stuff = origRead, origStuff })
	i := 0
	readScreen = func(id string, window int) ([]string, error) {
		s := screens[min(i, len(screens)-1)]
		i++
		return s, nil
	}
	var out []sent
	stuff = func(id string, window int, text string, enter bool) error {
		out = append(out, sent{id, text, window, enter})
		return nil
	}
	return &out
}

func TestReadAndMatch(t *testing.T) {
	fake(t, bashDialog)
	d, err := Read("1.a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Question != "Do you want to proceed?" || len(d.Options) != 4 || d.Fingerprint == "" {
		t.Fatalf("Read() = %+v", d)
	}
	tests := []struct {
		label string
		want  int
		err   string
	}{
		{"No", 4, ""},
		{"  no ", 4, ""},
		{"YES", 1, ""},
		{"Yes, and switch", 0, "no choice"},
		{"3", 0, "no choice"},
		{"", 0, "no choice"},
	}
	for _, tt := range tests {
		n, err := Match(d, tt.label)
		if n != tt.want || (err == nil) != (tt.err == "") || (err != nil && !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("Match(%q) = %d, %v; want %d, %q", tt.label, n, err, tt.want, tt.err)
		}
	}
	if _, err := Match(d, "Maybe"); err == nil || !strings.Contains(err.Error(), "4 No") {
		t.Errorf("a miss should list the choices, got %v", err)
	}
}

func TestMatchRefusesAmbiguousAndHighChoices(t *testing.T) {
	d := Dialog{Options: []string{"A", "a"}}
	if _, err := Match(d, "a"); err == nil {
		t.Error("ambiguous label accepted")
	}
	d = Dialog{Options: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "Ten"}}
	if _, err := Match(d, "Ten"); err == nil {
		t.Error("choice 10 accepted")
	}
}

func TestReadRefusesScreensWithoutChoices(t *testing.T) {
	screens := map[string]struct {
		lines []string
		err   error
	}{
		"idle":  {[]string{"o ", "  ? for shortcuts"}, ErrNoDialog},
		"menu":  {[]string{"  ❯ 1. Opus", "    2. Haiku", "", "  Enter to save · Esc to cancel"}, ErrNoDialog},
		"shell": {[]string{"$ ls", "1. a", "2. b", "$ "}, ErrNoDialog},
		"trust": {[]string{" Is this a project you trust?", " ❯ No, exit", "   Yes, I trust this folder", "", " Enter to confirm · Esc to cancel"}, ErrNoChoices},
	}
	for name, s := range screens {
		fake(t, s.lines)
		if _, err := Read("1.a", 0); !errors.Is(err, s.err) {
			t.Errorf("%s: err = %v, want %v", name, err, s.err)
		}
	}
}

func TestSendSendsTheDigitAlone(t *testing.T) {
	out := fake(t, bashDialog)
	d, _ := Read("1.a", 2)
	if err := Send("1.a", 2, 4, "No", d.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if len(*out) != 1 || (*out)[0] != (sent{"1.a", "4", 2, false}) {
		t.Errorf("sent %+v, want only the digit 4 without Enter", *out)
	}
}

func TestSendRefusesAChangedScreen(t *testing.T) {
	other := append([]string{}, bashDialog...)
	other[1] = " rm -rf build"
	idle := []string{"o ", "  ? for shortcuts"}
	tests := map[string]struct {
		later []string
		n     int
		label string
	}{
		"same question, other command": {other, 4, "No"},
		"dialog closed":                {idle, 4, "No"},
		"label not at that number":     {bashDialog, 3, "No"},
	}
	for name, tt := range tests {
		out := fake(t, bashDialog, tt.later)
		d, _ := Read("1.a", 0)
		if err := Send("1.a", 0, tt.n, tt.label, d.Fingerprint); !errors.Is(err, ErrChanged) {
			t.Errorf("%s: err = %v, want ErrChanged", name, err)
		}
		if len(*out) != 0 {
			t.Errorf("%s: sent %+v", name, *out)
		}
	}
}
