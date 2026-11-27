package sessions

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jagottsicher/switchboard/internal/detect"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/state"
)

func fakeScreens(t *testing.T, list []screen.Session, err error) {
	t.Helper()
	orig := listScreens
	t.Cleanup(func() { listScreens = orig })
	listScreens = func() ([]screen.Session, error) { return list, err }
}

func TestCollectMergesSortsAndPrunes(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	set := func(id string, st state.Status) {
		if _, err := store.Update(id, func(r *state.Record) { r.Agent, r.Status = true, st }); err != nil {
			t.Fatal(err)
		}
	}
	set("1.idle", state.StatusIdle)
	set("2.waiting", state.StatusWaiting)
	set("3.gone", state.StatusWorking)
	set("4.dead", state.StatusWorking)
	fakeScreens(t, []screen.Session{
		{ID: "1.idle"}, {ID: "2.waiting"}, {ID: "0.plain"}, {ID: "4.dead", State: screen.StateDead},
	}, nil)
	// Listing after the records were written, so the gone one is old enough to prune.
	origNow := now
	t.Cleanup(func() { now = origNow })
	now = func() time.Time { return time.Now().Add(time.Second) }

	infos, warn, err := Collect(store, Options{Prune: true})
	if err != nil || warn != nil {
		t.Fatal(err, warn)
	}
	var got []string
	for _, i := range infos {
		got = append(got, i.ID+"="+string(i.Status))
	}
	want := []string{"2.waiting=waiting", "1.idle=idle", "4.dead=exited", "0.plain="}
	if len(got) != len(want) {
		t.Fatalf("Collect() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Collect() = %v, want %v", got, want)
		}
	}
	if _, ok, _ := store.Get("3.gone"); ok {
		t.Error("record of the gone session was not pruned")
	}
}

func TestCollectWithoutPruneKeepsRecords(t *testing.T) {
	store, _ := state.Open(t.TempDir())
	if _, err := store.Update("3.gone", func(*state.Record) {}); err != nil {
		t.Fatal(err)
	}
	fakeScreens(t, nil, nil)
	if _, _, err := Collect(store, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get("3.gone"); !ok {
		t.Error("record removed without prune")
	}
}

func TestCollectKeepsStateWhenScreenFails(t *testing.T) {
	store, _ := state.Open(t.TempDir())
	if _, err := store.Update("1.x", func(*state.Record) {}); err != nil {
		t.Fatal(err)
	}
	fakeScreens(t, nil, errors.New("boom"))
	if _, _, err := Collect(store, Options{Prune: true}); err == nil {
		t.Fatal("no error")
	}
	if _, ok, _ := store.Get("1.x"); !ok {
		t.Error("record removed although screen -ls failed")
	}
}

func TestWindow(t *testing.T) {
	if w := (Info{}).Window(); w != 0 {
		t.Errorf("Window() without agent = %d", w)
	}
	if w := (Info{Agent: &state.Record{Window: "3"}}).Window(); w != 3 {
		t.Errorf("Window() = %d, want 3", w)
	}
}

func TestShortenAndAge(t *testing.T) {
	if got := Shorten("a  b\nc", 10); got != "a b c" {
		t.Errorf("Shorten() = %q", got)
	}
	if got := Shorten("äöüäöü", 4); got != "äöü…" {
		t.Errorf("Shorten() = %q", got)
	}
	origNow := now
	t.Cleanup(func() { now = origNow })
	base := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	for d, want := range map[time.Duration]string{
		5 * time.Second: "5s", 3 * time.Minute: "3m", 5 * time.Hour: "5h", 72 * time.Hour: "3d",
	} {
		if got := Age(base.Add(-d)); got != want {
			t.Errorf("Age(-%v) = %q, want %q", d, got, want)
		}
	}
	if Age(time.Time{}) != "-" {
		t.Error("Age(zero) should be -")
	}
}

func TestResolve(t *testing.T) {
	base := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	origNow := now
	t.Cleanup(func() { now = origNow })
	now = func() time.Time { return base }
	rec := func(st state.Status, age time.Duration, msg string) *state.Record {
		return &state.Record{Agent: true, Status: st, UpdatedAt: base.Add(-age), Message: msg}
	}
	det := func(st state.Status, q string) *detect.Result {
		return &detect.Result{Agent: "claude-code", Status: st, Question: q}
	}
	tests := []struct {
		name    string
		dead    bool
		rec     *state.Record
		det     *detect.Result
		status  state.Status
		message string
	}{
		{"esc after working", false, rec(state.StatusWorking, 30*time.Second, ""), det(state.StatusIdle, ""), state.StatusIdle, ""},
		{"denied permission", false, rec(state.StatusWaiting, 30*time.Second, "Permission requested: Bash"), det(state.StatusIdle, ""), state.StatusIdle, ""},
		{"fresh prompt, screen lags", false, rec(state.StatusWorking, time.Second, ""), det(state.StatusIdle, ""), state.StatusWorking, ""},
		{"no hooks, waiting", false, nil, det(state.StatusWaiting, "Do you want to proceed?"), state.StatusWaiting, "Do you want to proceed?"},
		{"waiting shows the question", false, rec(state.StatusWaiting, 10*time.Second, "Permission requested: Bash"), det(state.StatusWaiting, "Do you want to create x?"), state.StatusWaiting, "Do you want to create x?"},
		{"same status keeps the hook message", false, rec(state.StatusIdle, time.Minute, "waiting for input"), det(state.StatusIdle, ""), state.StatusIdle, "waiting for input"},
		{"long thinking stays working", false, rec(state.StatusWorking, time.Minute, ""), det(state.StatusWorking, ""), state.StatusWorking, ""},
		{"nothing detected keeps the record", false, rec(state.StatusWaiting, time.Hour, "Need a decision"), nil, state.StatusWaiting, "Need a decision"},
		{"marked agent without hook", false, &state.Record{Agent: true}, det(state.StatusIdle, ""), state.StatusIdle, ""},
		{"reported end wins", false, rec(state.StatusExited, time.Minute, "Session ended: prompt_input_exit"), det(state.StatusIdle, ""), state.StatusExited, "Session ended: prompt_input_exit"},
		{"dead wins", true, rec(state.StatusWorking, time.Hour, "x"), det(state.StatusIdle, ""), state.StatusExited, "x"},
		{"plain session", false, nil, nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := Info{Session: screen.Session{ID: "1.x"}, Agent: tt.rec}
			if tt.dead {
				info.State = screen.StateDead
			}
			resolve(&info, tt.det)
			if info.Status != tt.status || info.Message != tt.message {
				t.Errorf("resolve() = %s %q, want %s %q", info.Status, info.Message, tt.status, tt.message)
			}
		})
	}
}

func TestCollectDetectsAgentsOnScreen(t *testing.T) {
	store, _ := state.Open(t.TempDir())
	fakeScreens(t, []screen.Session{{ID: "1.agent"}, {ID: "2.shell"}, {ID: "3.dead", State: screen.StateDead}, {ID: "4.self"}}, nil)
	orig := readScreen
	t.Cleanup(func() { readScreen = orig })
	var read []string
	var mu sync.Mutex
	readScreen = func(id string, window int) ([]string, error) {
		mu.Lock()
		read = append(read, id)
		mu.Unlock()
		if id == "1.agent" {
			return []string{"Do you want to proceed?", "❯ 1. Yes", "  2. No", "Esc to cancel"}, nil
		}
		return []string{"$ "}, nil
	}
	infos, _, err := Collect(store, Options{Detect: true, Self: "4.self"})
	if err != nil {
		t.Fatal(err)
	}
	if infos[0].ID != "1.agent" || infos[0].Status != state.StatusWaiting || !infos[0].IsAgent() ||
		infos[0].Message != "Do you want to proceed?" {
		t.Errorf("first = %+v", infos[0])
	}
	for _, i := range infos[1:] {
		if i.IsAgent() {
			t.Errorf("%s taken for an agent", i.ID)
		}
	}
	for _, id := range read {
		if id == "3.dead" || id == "4.self" {
			t.Errorf("the screen of %s was read", id)
		}
	}
}
