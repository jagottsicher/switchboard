//go:build integration

package answer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jagottsicher/switchboard/internal/screen"
)

// TestMain isolates the tests in their own socket directory, so they never touch the user's
// real sessions. The path stays short because Unix socket paths are limited to about 108 bytes.
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

// TestAnswerThroughScreen shows a permission dialog in a real session and records the input the
// session receives: only the digit of the choice may arrive, without Enter.
func TestAnswerThroughScreen(t *testing.T) {
	dialog, err := filepath.Abs("../detect/testdata/utf8-permission.txt")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	script := fmt.Sprintf("printf '\\033[H\\033[2J'; cat '%s'; stty raw -echo; exec cat > '%s'", dialog, out)
	s, err := screen.New("answer", dir, []string{"sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("screen", "-S", s.ID, "-X", "quit").Run() })

	var d Dialog
	for deadline := time.Now().Add(3 * time.Second); ; {
		if _, statErr := os.Stat(out); statErr == nil {
			if d, err = Read(s.ID, 0); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("dialog not readable: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if d.Question != "Do you want to create sb-detect-probe.txt?" {
		t.Errorf("question = %q", d.Question)
	}
	n, err := Match(d, "no")
	if err != nil || n != 3 {
		t.Fatalf("Match = %d, %v", n, err)
	}
	if err := Send(s.ID, 0, n, d.Options[n-1], d.Fingerprint); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "3" {
		t.Errorf("the session received %q, want only \"3\"", got)
	}
}
