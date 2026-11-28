// Package config reads the configuration file of switchboard,
// $XDG_CONFIG_HOME/switchboard/config.toml (by default ~/.config/switchboard/config.toml).
//
// The file is optional. It lists the other machines whose sessions switchboard shows:
//
//	[[host]]
//	name = "agents"                     # name used in switchboard, as in agents:SESSION
//	ssh = "agents.example.org"          # host alias for ssh; defaults to name
//	binary = "~/.local/bin/switchboard" # switchboard on that machine; defaults to switchboard
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Host is another machine with screen sessions.
type Host struct {
	Name   string `toml:"name"`
	SSH    string `toml:"ssh"`
	Binary string `toml:"binary"`
}

// Config is the content of the configuration file.
type Config struct {
	Hosts []Host `toml:"host"`
}

// Path returns the path of the configuration file.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "switchboard", "config.toml"), nil
}

// Load reads the configuration file. A missing file is an empty configuration.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return load(path)
}

func load(path string) (Config, error) {
	var c Config
	meta, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("%s: unknown setting %s", path, undecoded[0])
	}
	seen := map[string]bool{}
	for _, h := range c.Hosts {
		switch {
		case h.Name == "":
			return Config{}, fmt.Errorf("%s: a host has no name", path)
		case strings.ContainsAny(h.Name, ": \t"):
			return Config{}, fmt.Errorf("%s: host name %q must not contain colons or spaces", path, h.Name)
		case strings.HasPrefix(h.Name, "-") || strings.HasPrefix(h.SSH, "-"):
			return Config{}, fmt.Errorf("%s: host %q must not start with a dash", path, h.Name)
		case seen[h.Name]:
			return Config{}, fmt.Errorf("%s: host %q is listed twice", path, h.Name)
		}
		seen[h.Name] = true
	}
	return c, nil
}

// Host returns the host with the given name.
func (c Config) Host(name string) (Host, bool) {
	for _, h := range c.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return Host{}, false
}
