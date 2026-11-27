//go:build integration

package screen

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain isolates all integration tests in their own socket directory, so they never touch the
// user's real sessions, not even on a machine with running agents. The path stays short because
// Unix socket paths are limited to about 108 bytes.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("screen"); err != nil {
		fmt.Println("screen not installed, skipping integration tests")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		panic(err)
	}
	os.Setenv("SCREENDIR", dir)
	code := m.Run()
	exec.Command("screen", "-wipe").Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

var sessionCounter int

// startSession starts command in a new session and quits it when the test ends.
func startSession(t *testing.T, command ...string) Session {
	t.Helper()
	sessionCounter++
	s, err := New(fmt.Sprintf("it%d", sessionCounter), t.TempDir(), command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("screen", "-S", s.ID, "-X", "quit").Run() })
	return s
}

// startRecorder starts a session that writes all input unchanged to a file. The terminal runs in
// raw mode so that \r is not translated and lines are not truncated.
func startRecorder(t *testing.T) (Session, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	s := startSession(t, "sh", "-c", "stty raw -echo; exec cat > "+out)
	waitFor(t, func() bool { _, err := os.Stat(out); return err == nil })
	return s, out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIntegrationListFindsNewSession(t *testing.T) {
	s := startSession(t, "sleep", "60")
	found, err := Find(s.Name)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != s.ID || found.State != StateDetached {
		t.Errorf("Find() = %+v, want %+v", found, s)
	}
}

func TestIntegrationNewRejectsDuplicate(t *testing.T) {
	s := startSession(t, "sleep", "60")
	if _, err := New(s.Name, "", []string{"sleep", "60"}); err == nil {
		t.Error("duplicate name was accepted")
	}
}

func TestIntegrationNewUsesDirectory(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwd")
	sessionCounter++
	s, err := New(fmt.Sprintf("it%d", sessionCounter), dir, []string{"sh", "-c", "pwd > pwd; sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("screen", "-S", s.ID, "-X", "quit").Run() })
	waitFor(t, func() bool { b, _ := os.ReadFile(marker); return len(b) > 0 })
	got, _ := os.ReadFile(marker)
	if strings.TrimSpace(string(got)) != dir {
		t.Errorf("working directory = %q, want %q", got, dir)
	}
}

func TestIntegrationStuffDeliversExactBytes(t *testing.T) {
	long := strings.Repeat("0123456789abcdef\\^$ü", 300) // far above maxChunk, with escapes
	tests := []struct {
		name, text string
		enter      bool
		want       string
	}{
		{"special-chars", `a\b ^c $HOME ${X} $(id) "q" 'q' ; # % ~`, false, `a\b ^c $HOME ${X} $(id) "q" 'q' ; # % ~`},
		{"non-ascii", "ümläut ß 日本語 🙂", false, "ümläut ß 日本語 🙂"},
		{"multiline", "line one\nline two", false, "line one\nline two"},
		{"control-chars", "\x1b[A\t\x01", false, "\x1b[A\t\x01"},
		{"enter", "go", true, "go\r"},
		{"enter-only", "", true, "\r"},
		{"long", long, false, long},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, out := startRecorder(t)
			if err := Stuff(s.ID, 0, tt.text, tt.enter); err != nil {
				t.Fatal(err)
			}
			var got []byte
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				got, _ = os.ReadFile(out)
				if len(got) >= len(tt.want) {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond) // Catch late extra bytes.
			got, _ = os.ReadFile(out)
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Errorf("received %d bytes %q\nwant %d bytes %q", len(got), abbreviate(got), len(tt.want), abbreviate([]byte(tt.want)))
			}
		})
	}
}

func abbreviate(b []byte) string {
	if len(b) > 120 {
		return string(b[:60]) + "…" + string(b[len(b)-60:])
	}
	return string(b)
}

func TestIntegrationHardcopyReadsScreen(t *testing.T) {
	s := startSession(t, "sh", "-c", "for i in 1 2 3; do echo line-$i; done; echo 'with $ and ^ and ü'; exec sleep 60")
	var lines []string
	waitFor(t, func() bool {
		var err error
		lines, err = Hardcopy(s.ID, 0)
		return err == nil && len(lines) >= 4
	})
	want := []string{"line-1", "line-2", "line-3", "with $ and ^ and ü"}
	got := Tail(lines, 4)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Hardcopy() ends with %q, want %q", got, want)
	}
}

func TestIntegrationHardcopyMissingWindow(t *testing.T) {
	orig := hardcopyTimeout
	t.Cleanup(func() { hardcopyTimeout = orig })
	hardcopyTimeout = 500 * time.Millisecond
	s := startSession(t, "sleep", "60")
	if _, err := Hardcopy(s.ID, 7); err == nil {
		t.Error("Hardcopy of a missing window reports no error")
	}
}

func TestIntegrationEndedSessionDisappears(t *testing.T) {
	s := startSession(t, "sleep", "60")
	if err := exec.Command("screen", "-S", s.ID, "-X", "quit").Run(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, err := Find(s.ID); return err != nil })
}
