package main

import (
	"runtime/debug"
	"testing"
)

func TestDevVersion(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "babe03bf951c1cf18b4e1fd5153688c3f9c15397"},
		{Key: "vcs.time", Value: "2026-11-28T09:08:28Z"},
		{Key: "vcs.modified", Value: "false"},
	}
	dirty := append(vcs[:2:2], debug.BuildSetting{Key: "vcs.modified", Value: "true"})
	tests := []struct {
		name     string
		module   string
		settings []debug.BuildSetting
		want     [3]string
	}{
		{"go install of a branch", "v0.0.0-20261128090828-babe03bf951c", nil,
			[3]string{"v0.0.0-20261128090828-babe03bf951c", "none", "unknown"}},
		{"go build in a checkout", "(devel)", vcs,
			[3]string{"dev+babe03bf951c", "babe03bf951c1cf18b4e1fd5153688c3f9c15397", "2026-11-28T09:08:28Z"}},
		{"uncommitted changes", "(devel)", dirty,
			[3]string{"dev+babe03bf951c-dirty", "babe03bf951c1cf18b4e1fd5153688c3f9c15397", "2026-11-28T09:08:28Z"}},
		{"nothing known", "(devel)", nil, [3]string{"dev", "none", "unknown"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, c, d := devVersion(tt.module, tt.settings)
			if got := [3]string{v, c, d}; got != tt.want {
				t.Errorf("devVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
