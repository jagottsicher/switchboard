package sessions

import (
	"errors"
	"testing"
	"time"

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

	infos, warn, err := Collect(store, true)
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
	if _, _, err := Collect(store, false); err != nil {
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
	if _, _, err := Collect(store, true); err == nil {
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
