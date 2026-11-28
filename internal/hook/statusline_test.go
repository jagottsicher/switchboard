package hook

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/switchboard/internal/state"
)

// statusInputJSON has the shape of the agent's status line input, cut to a few fields.
const statusInputJSON = `{
  "hook_event_name": "Status",
  "session_id": "abc",
  "model": {"id": "m", "display_name": "M"},
  "rate_limits": {
    "five_hour": {"used_percentage": 42.7, "resets_at": 1899000000},
    "seven_day": {"used_percentage": 83, "resets_at": 1899500000}
  }
}`

func TestHandleStatusLineStoresLimits(t *testing.T) {
	store := openStore(t)
	limits, err := HandleStatusLine(store, []byte(statusInputJSON), env(map[string]string{"STY": "1.a", "WINDOW": "2"}))
	if err != nil {
		t.Fatal(err)
	}
	want := []state.Limit{
		{Window: "5h", UsedPercent: 42.7, ResetsAt: time.Unix(1899000000, 0)},
		{Window: "7d", UsedPercent: 83, ResetsAt: time.Unix(1899500000, 0)},
	}
	if !state.SameLimits(limits, want) {
		t.Fatalf("limits = %+v", limits)
	}
	rec, ok, _ := store.Get("1.a")
	if !ok || !rec.Agent || rec.Window != "2" || !state.SameLimits(rec.Limits, want) || !rec.UpdatedAt.IsZero() {
		t.Errorf("record = %+v", rec)
	}

	// Input without limits, as before the agent's first response, keeps the stored ones.
	if _, err := HandleStatusLine(store, []byte(`{"session_id": "new"}`), env(map[string]string{"STY": "1.a"})); err != nil {
		t.Fatal(err)
	}
	if rec, _, _ := store.Get("1.a"); !state.SameLimits(rec.Limits, want) {
		t.Errorf("limits were dropped: %+v", rec.Limits)
	}
}

func TestHandleStatusLineOutsideScreen(t *testing.T) {
	store := openStore(t)
	limits, err := HandleStatusLine(store, []byte(statusInputJSON), env(nil))
	if err != nil || len(limits) != 2 {
		t.Fatalf("limits = %+v, %v", limits, err)
	}
	if recs, _ := store.All(); len(recs) != 0 {
		t.Errorf("records written outside screen: %+v", recs)
	}
}

func TestStatusLineWindowsAndResetTimes(t *testing.T) {
	in := `{"rate_limits": {"five_hour": {"used_percentage": 5, "resets_at": "2026-11-27T15:00:00Z"}, "seven_day": {"resets_at": 1}}}`
	limits, err := HandleStatusLine(openStore(t), []byte(in), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 1 || !limits[0].ResetsAt.Equal(time.Date(2026, 11, 27, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("limits = %+v", limits)
	}
	if _, err := HandleStatusLine(openStore(t), []byte("not json"), env(nil)); err == nil {
		t.Error("invalid input accepted")
	}
}

func TestFormatLimits(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	limits := []state.Limit{
		{Window: "5h", UsedPercent: 79.9, ResetsAt: now.Add(time.Hour)},
		{Window: "7d", UsedPercent: 80},
		{Window: "x", UsedPercent: 95, ResetsAt: now.Add(-time.Minute)},
	}
	got := FormatLimits(limits, now)
	want := "5h 79% · " + ansiWarning + "7d 80%" + ansiReset
	if got != want {
		t.Errorf("FormatLimits() = %q, want %q", got, want)
	}
	if got := FormatLimits([]state.Limit{{Window: "5h", UsedPercent: 90}}, now); got != ansiCritical+"5h 90%"+ansiReset {
		t.Errorf("critical = %q", got)
	}
}

func TestRunStatusLine(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	vars := env(map[string]string{"STY": "1.a"})

	var out, errOut bytes.Buffer
	if code := RunStatusLine(strings.NewReader(statusInputJSON), &out, &errOut, vars, ""); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "5h 42%") || !strings.Contains(out.String(), "7d") {
		t.Errorf("own status line = %q", out.String())
	}

	// With a previous status line command, its output is passed through and it gets the input.
	out.Reset()
	code := RunStatusLine(strings.NewReader(statusInputJSON), &out, &errOut, vars, `grep -o '"session_id": "[a-z]*"'; exit 3`)
	if code != 3 || out.String() != "\"session_id\": \"abc\"\n" {
		t.Errorf("passed through: exit %d, output %q", code, out.String())
	}
	store, _ := state.Open("")
	if rec, ok, _ := store.Get("1.a"); !ok || len(rec.Limits) != 2 {
		t.Errorf("limits not stored: %+v", rec)
	}
}

func TestStatusLineCommandRoundTrip(t *testing.T) {
	for _, then := range []string{"", "my-status", `bash -c 'echo "it'\''s $PWD"'`, "~/bin/line.sh --short"} {
		cmd := StatusLineCommand("'/my dir/switchboard' hook", then)
		got, ok := parseStatusLineCommand(cmd)
		if !ok || got != then {
			t.Errorf("parse(%q) = %q, %v; want %q", cmd, got, ok, then)
		}
	}
	for _, cmd := range []string{"my-status", "/usr/bin/foo statusline", "switchboard statusline --other", "switchboard statusline --then 'broken"} {
		if _, ok := parseStatusLineCommand(cmd); ok {
			t.Errorf("parse(%q) accepted", cmd)
		}
	}
	if got, ok := parseStatusLineCommand("swb statusline"); !ok || got != "" {
		t.Errorf("swb statusline = %q, %v", got, ok)
	}
}

func TestMergeStatusLine(t *testing.T) {
	const cmd = "/bin/switchboard hook"
	// No status line yet: switchboard's own is added.
	out, _, err := Merge([]byte(`{}`), cmd, false)
	if err != nil || !strings.Contains(string(out), `"command": "/bin/switchboard statusline"`) {
		t.Fatalf("Merge() = %s, %v", out, err)
	}

	// An existing status line command is wrapped, other settings stay, and installing again
	// does not wrap it twice.
	in := `{"statusLine": {"type": "command", "command": "~/bin/line.sh", "padding": 2}}`
	out, _, err = Merge([]byte(in), cmd, false)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := `"command": "/bin/switchboard statusline --then '~/bin/line.sh'"`
	if s := string(out); !strings.Contains(s, wrapped) || !strings.Contains(s, `"padding": 2`) {
		t.Fatalf("wrapped =\n%s", s)
	}
	again, changed, _ := Merge(out, cmd, false)
	if changed || string(again) != string(out) {
		t.Errorf("second install changed the settings:\n%s", again)
	}

	// Uninstalling puts the user's command back, or removes switchboard's own.
	restored, _, _ := Merge(out, "", true)
	if s := string(restored); !strings.Contains(s, `"command": "~/bin/line.sh"`) || strings.Contains(s, "switchboard") || !strings.Contains(s, `"padding": 2`) {
		t.Errorf("restored =\n%s", s)
	}
	own, _, _ := Merge([]byte(`{"statusLine": {"type": "command", "command": "switchboard statusline"}, "x": 1}`), "", true)
	if strings.Contains(string(own), "statusLine") {
		t.Errorf("own status line not removed:\n%s", own)
	}

	// Uninstalling leaves a foreign status line alone.
	foreign := `{"statusLine": {"type": "command", "command": "my-line"}}`
	if out, changed, _ := Merge([]byte(foreign), "", true); changed {
		t.Errorf("foreign status line changed:\n%s", out)
	}
}
