package state

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedNow(t *testing.T, ts time.Time) {
	t.Helper()
	orig := Now
	t.Cleanup(func() { Now = orig })
	Now = func() time.Time { return ts }
}

func TestUpdateCreatesAndReadsBack(t *testing.T) {
	ts := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	fixedNow(t, ts)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("1.x"); ok || err != nil {
		t.Fatalf("Get on an empty store = %v, %v", ok, err)
	}
	rec, err := s.Update("12.name with space", func(r *Record) {
		r.Status = StatusWorking
		r.Agent = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Session != "12.name with space" || !rec.UpdatedAt.Equal(ts) {
		t.Errorf("Update() = %+v", rec)
	}
	got, ok, err := s.Get("12.name with space")
	if err != nil || !ok || got.Status != StatusWorking || !got.Agent {
		t.Errorf("Get() = %+v, %v, %v", got, ok, err)
	}
	all, err := s.All()
	if err != nil || len(all) != 1 {
		t.Errorf("All() = %v, %v", all, err)
	}
}

func TestStoreDirectoriesArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "st")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Join(dir, "sessions"), filepath.Join(dir, "events")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s has mode %v, want 0700", d, info.Mode().Perm())
		}
	}
}

func TestConcurrentUpdatesDoNotLoseWrites(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Update("1.x", func(r *Record) { r.Message += "x" })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rec, _, _ := s.Get("1.x")
	if len(rec.Message) != n {
		t.Errorf("message has %d characters, want %d (lost updates)", len(rec.Message), n)
	}
}

func TestDamagedRecordIsReplaced(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.recordPath("1.x"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("1.x", func(r *Record) { r.Status = StatusIdle }); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.Get("1.x")
	if err != nil || !ok || rec.Status != StatusIdle {
		t.Errorf("Get() = %+v, %v, %v", rec, ok, err)
	}
}

func TestPruneRemovesOnlyGoneSessions(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1.alive", "2.gone"} {
		if _, err := s.Update(id, func(*Record) {}); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendEvent(id, map[string]string{"event": "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(map[string]bool{"1.alive": true}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.All()
	if _, ok := all["1.alive"]; !ok || len(all) != 1 {
		t.Errorf("after Prune: %v", all)
	}
	if _, err := os.Stat(s.eventsPath("2.gone")); !os.IsNotExist(err) {
		t.Errorf("event log of the gone session still exists: %v", err)
	}
}

func TestAppendEventRotates(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", maxEventsSize+1)
	if err := os.WriteFile(s.eventsPath("1.x"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent("1.x", map[string]string{"event": "y"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.eventsPath("1.x") + ".1"); err != nil {
		t.Errorf("no rotated log: %v", err)
	}
	data, _ := os.ReadFile(s.eventsPath("1.x"))
	if string(data) != "{\"event\":\"y\"}\n" {
		t.Errorf("new log = %q", data)
	}
}

func TestParseStatus(t *testing.T) {
	if st, err := ParseStatus("waiting"); err != nil || st != StatusWaiting {
		t.Errorf("ParseStatus(waiting) = %v, %v", st, err)
	}
	if _, err := ParseStatus("busy"); err == nil {
		t.Error("ParseStatus(busy) should fail")
	}
}
