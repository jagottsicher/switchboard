package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	c, err := load(write(t, `
[[host]]
name = "agents"
ssh = "agents.example.org"
binary = "~/.local/bin/switchboard"

[[host]]
name = "lab"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Hosts) != 2 || c.Hosts[0] != (Host{"agents", "agents.example.org", "~/.local/bin/switchboard"}) {
		t.Errorf("hosts = %+v", c.Hosts)
	}
	if h, ok := c.Host("lab"); !ok || h.SSH != "" {
		t.Errorf("Host(lab) = %+v, %v", h, ok)
	}
	if _, ok := c.Host("nope"); ok {
		t.Error("unknown host found")
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	c, err := load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || len(c.Hosts) != 0 {
		t.Errorf("load() = %+v, %v", c, err)
	}
}

func TestLoadRejectsBadHosts(t *testing.T) {
	tests := map[string]string{
		"no name":  "[[host]]\nssh = \"x\"\n",
		"colon":    "[[host]]\nname = \"a:b\"\n",
		"dash":     "[[host]]\nname = \"a\"\nssh = \"-oProxyCommand=x\"\n",
		"twice":    "[[host]]\nname = \"a\"\n[[host]]\nname = \"a\"\n",
		"typo":     "[[host]]\nname = \"a\"\nbinery = \"x\"\n",
		"bad toml": "[[host]\n",
	}
	for name, text := range tests {
		if _, err := load(write(t, text)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), "config.toml") {
			t.Errorf("%s: error without path: %v", name, err)
		}
	}
}

func TestPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if p, _ := Path(); p != "/cfg/switchboard/config.toml" {
		t.Errorf("Path() = %s", p)
	}
}
