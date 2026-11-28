package main

import (
	"fmt"
	"runtime/debug"
)

// version, commit, date and builtBy are set for release builds with -ldflags "-X main.…" (see
// .goreleaser.yaml). For other builds, version stays "dev" until init fills it in from the build
// information that Go embeds in every binary.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "source"
)

func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		version, commit, date = devVersion(info.Main.Version, info.Settings)
	}
}

// devVersion describes a build that was not made by the release pipeline. Go records the module
// version, such as a pseudo-version for a branch, for go install and, since Go 1.24, for go build
// in a checkout. Without a module version, the commit is used.
func devVersion(module string, settings []debug.BuildSetting) (v, c, d string) {
	v, c, d = "dev", "none", "unknown"
	var modified bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			c = s.Value
		case "vcs.time":
			d = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	switch {
	case module != "" && module != "(devel)":
		v = module
	case c != "none":
		short := c
		if len(short) > 12 {
			short = short[:12]
		}
		v = "dev+" + short
		if modified {
			v += "-dirty"
		}
	}
	return v, c, d
}

// versionInfo is the text that --version prints after the program name.
func versionInfo() string {
	return fmt.Sprintf("%s (commit %s, built %s by %s)", version, commit, date, builtBy)
}
