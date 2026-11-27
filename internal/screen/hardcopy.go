package screen

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Timing for hardcopy. `screen -X` returns before the screen server has written the file, so the
// file is polled. Variables so that tests can change them.
var (
	hardcopyTimeout = 2 * time.Second
	hardcopyPoll    = 10 * time.Millisecond
)

// Hardcopy reads the contents of window window of session id, including scrollback. Trailing
// whitespace on each line and trailing empty lines are removed.
func Hardcopy(id string, window int) ([]string, error) {
	dir, err := privateTempDir()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	// A fresh file per call, so that a .screenrc with "hardcopy_append on" does not interfere.
	file := filepath.Join(dir, "hardcopy")
	if err := run("", "-S", id, "-p", strconv.Itoa(window), "-X", "hardcopy", "-h", file); err != nil {
		return nil, err
	}
	data, err := waitForFile(file)
	if err != nil {
		return nil, fmt.Errorf("no output from session %s, window %d (does the window exist?): %w",
			id, window, err)
	}
	return trimLines(decodeHardcopy(data)), nil
}

// decodeHardcopy turns the contents of a hardcopy file into text.
//
// screen 4.09 writes only the low byte of each cell's character, even in UTF-8 mode: "ü"
// (U+00FC) becomes 0xfc, "─" (U+2500) becomes 0x00, and the second cell of a wide character
// becomes 0xff. Valid UTF-8 (such as plain ASCII, or a screen version that encodes correctly)
// is kept as is. Anything else is read as Latin-1: umlauts come out right, control bytes become
// spaces and the 0xff filler cells are dropped. Characters outside Latin-1 cannot be recovered.
func decodeHardcopy(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		switch {
		case c == 0xff:
			// Filler cell behind a wide character.
		case c == '\n' || c == '\t':
			b.WriteByte(c)
		case c < 0x20 || (c >= 0x7f && c < 0xa0):
			b.WriteByte(' ')
		default:
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

// privateTempDir creates a directory with mode 0700, preferably under $XDG_RUNTIME_DIR. That
// variable is often unset in ssh logins; the system temp directory is the fallback.
func privateTempDir() (string, error) {
	base := os.TempDir()
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		candidate := filepath.Join(rt, "switchboard")
		if err := os.MkdirAll(candidate, 0o700); err == nil {
			base = candidate
		}
	}
	// os.MkdirTemp creates the directory with mode 0700.
	return os.MkdirTemp(base, "hardcopy-")
}

// waitForFile waits until the file exists and its size is unchanged between two polls.
func waitForFile(path string) ([]byte, error) {
	deadline := time.Now().Add(hardcopyTimeout)
	lastSize := int64(-1)
	for {
		if info, err := os.Stat(path); err == nil {
			if info.Size() == lastSize {
				return os.ReadFile(path)
			}
			lastSize = info.Size()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no file after %s", hardcopyTimeout)
		}
		time.Sleep(hardcopyPoll)
	}
}

// trimLines splits text into lines and removes trailing whitespace and trailing empty lines.
func trimLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Tail returns the last n lines, or all lines if n <= 0.
func Tail(lines []string, n int) []string {
	if n <= 0 || n >= len(lines) {
		return lines
	}
	return lines[len(lines)-n:]
}
