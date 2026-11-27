package main

import (
	"bytes"
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
