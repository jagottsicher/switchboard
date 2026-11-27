package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootVersion(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), version) {
		t.Errorf("output %q does not contain version %q", out.String(), version)
	}
}

func TestProgName(t *testing.T) {
	tests := map[string]string{
		"switchboard":                  "switchboard",
		"/home/u/.local/bin/swb":       "swb",
		"swb":                          "swb",
		"/tmp/go-build1/b001/exe/main": "switchboard",
		"/usr/bin/swbx":                "switchboard",
	}
	for arg0, want := range tests {
		if got := progName(arg0); got != want {
			t.Errorf("progName(%q) = %q, want %q", arg0, got, want)
		}
	}
}

func TestRootWithoutTerminalDoesNotStartTUI(t *testing.T) {
	origTUI, origTerm := runTUI, isTerminal
	t.Cleanup(func() { runTUI, isTerminal = origTUI, origTerm })
	started := false
	runTUI = func() error { started = true; return nil }
	isTerminal = func(*os.File) bool { return false }
	cmd := newRootCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("Execute() = %v", err)
	}
	if started {
		t.Error("TUI started without a terminal")
	}
	isTerminal = func(*os.File) bool { return true }
	if err := cmd.Execute(); err != nil || !started {
		t.Errorf("Execute() on a terminal = %v, started = %v", err, started)
	}
}
