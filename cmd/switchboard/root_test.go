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
		t.Errorf("Ausgabe %q enthält die Version %q nicht", out.String(), version)
	}
}
