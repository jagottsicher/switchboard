package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hookEvent is an agent hook event that switchboard subscribes to.
type hookEvent struct {
	Name string
	// ToolMatcher marks events whose matcher selects tools; switchboard matches all of them.
	ToolMatcher bool
}

// Events lists the hook events switchboard installs, in the order they are written.
var Events = []hookEvent{
	{"SessionStart", false},
	{"UserPromptSubmit", false},
	{"PreToolUse", true},
	{"PostToolUse", true},
	{"PostToolUseFailure", true},
	{"PermissionRequest", true},
	{"Notification", false},
	{"Stop", false},
	{"StopFailure", false},
	{"SessionEnd", false},
}

// hookTimeout is the timeout in seconds for the hook command. The command finishes in
// milliseconds; the limit only matters if the disk hangs.
const hookTimeout = 10

// SettingsPath returns the path of the agent's user settings file:
// $CLAUDE_CONFIG_DIR/settings.json if that variable is set, otherwise ~/.claude/settings.json.
func SettingsPath() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// lookPath and executable are swappable for tests.
var (
	lookPath   = exec.LookPath
	executable = os.Executable
)

// Command returns the shell command that the hook settings should run: the absolute path of the
// switchboard binary, shell-quoted, followed by "hook". Paths of temporary builds (go run, test
// binaries) are rejected because they disappear.
func Command() (string, error) {
	path, err := lookPath("switchboard")
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		if path, err = executable(); err != nil {
			return "", err
		}
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	tmp := filepath.Clean(os.TempDir()) + string(filepath.Separator)
	if strings.Contains(path, "go-build") || strings.HasPrefix(path, tmp) {
		return "", fmt.Errorf("switchboard runs from a temporary build (%s); install it first, "+
			"for example with go install, or pass --command", path)
	}
	return shellQuote(path) + " hook", nil
}

// shellQuote quotes s for sh if it contains anything but safe characters.
func shellQuote(s string) string {
	safe := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// IsSwitchboardCommand reports whether a hook command runs `switchboard hook` or `swb hook`,
// with any path.
func IsSwitchboardCommand(cmd string) bool {
	rest, ok := strings.CutSuffix(strings.TrimSpace(cmd), " hook")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	rest = strings.Trim(rest, `'"`)
	base := filepath.Base(rest)
	return base == "switchboard" || base == "swb"
}

// StatusLineCommand returns the status line command for the switchboard of hookCommand, a command
// that ends in " hook". If then is not empty, the status line command runs it and passes its
// output through.
func StatusLineCommand(hookCommand, then string) string {
	c := strings.TrimSuffix(strings.TrimSpace(hookCommand), " hook") + " statusline"
	if then != "" {
		c += " --then " + shellQuote(then)
	}
	return c
}

// parseStatusLineCommand reports whether cmd is a status line command of switchboard, with any
// path, and returns the command it passes through.
func parseStatusLineCommand(cmd string) (then string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	i := strings.Index(cmd, " statusline")
	if i < 0 {
		return "", false
	}
	base := filepath.Base(strings.Trim(strings.TrimSpace(cmd[:i]), `'"`))
	if base != "switchboard" && base != "swb" {
		return "", false
	}
	rest := cmd[i+len(" statusline"):]
	if rest == "" {
		return "", true
	}
	arg, found := strings.CutPrefix(rest, " --then ")
	if !found {
		return "", false
	}
	then, ok = shellUnquote(strings.TrimSpace(arg))
	return then, ok
}

// shellUnquote reverses shellQuote.
func shellUnquote(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	if shellQuote(s) == s {
		return s, true
	}
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return "", false
	}
	out := strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	if shellQuote(out) != s {
		return "", false
	}
	return out, true
}

// hookEntry is one command hook in the settings.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// matcherGroup is one entry in the list of an event.
type matcherGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

func ourGroup(ev hookEvent, command string) matcherGroup {
	g := matcherGroup{Hooks: []hookEntry{{Type: "command", Command: command, Timeout: hookTimeout}}}
	if ev.ToolMatcher {
		g.Matcher = "*"
	}
	return g
}

// Snippet returns the settings fragment with all switchboard hooks, for `hooks print`.
func Snippet(command string) ([]byte, error) {
	hooks := object{}
	for _, ev := range Events {
		raw, err := json.Marshal([]matcherGroup{ourGroup(ev, command)})
		if err != nil {
			return nil, err
		}
		hooks = hooks.set(ev.Name, raw)
	}
	hooksRaw, err := hooks.marshal()
	if err != nil {
		return nil, err
	}
	sl, err := json.Marshal(statusLine{Type: "command", Command: StatusLineCommand(command, "")})
	if err != nil {
		return nil, err
	}
	return indent(object{{Key: "hooks", Value: hooksRaw}, {Key: "statusLine", Value: sl}})
}

// statusLine is the status line setting as switchboard writes it.
type statusLine struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// mergeStatusLine sets the status line command of switchboard in root, or removes it if remove is
// set. A status line command of the user is kept: switchboard's command runs it and passes its
// output through, and removing switchboard's command puts it back. Other status line settings,
// such as padding, stay as they are.
func mergeStatusLine(root object, hookCommand string, remove bool) (object, error) {
	sl := object{}
	then, ours := "", false
	if raw, ok := root.get("statusLine"); ok {
		var err error
		if sl, err = parseObject(raw); err != nil {
			return nil, fmt.Errorf(`"statusLine" is not a JSON object: %w`, err)
		}
		var cmd string
		if c, ok := sl.get("command"); ok {
			if err := json.Unmarshal(c, &cmd); err != nil {
				return nil, fmt.Errorf("statusLine.command: %w", err)
			}
		}
		if then, ours = parseStatusLineCommand(cmd); !ours {
			then = cmd
		}
	}
	str := func(v string) json.RawMessage {
		raw, _ := json.Marshal(v)
		return raw
	}
	switch {
	case remove && !ours:
		return root, nil
	case remove && then == "":
		return root.del("statusLine"), nil
	case remove:
		sl = sl.set("command", str(then))
	default:
		sl = sl.set("type", str("command")).set("command", str(StatusLineCommand(hookCommand, then)))
	}
	raw, err := sl.marshal()
	if err != nil {
		return nil, err
	}
	return root.set("statusLine", raw), nil
}

// Merge returns settings with all switchboard hooks removed and, unless remove is set, added again
// with command. All other content keeps its order and values. changed reports whether the result
// differs from the input in substance.
func Merge(settings []byte, command string, remove bool) (out []byte, changed bool, err error) {
	root := object{}
	if len(bytes.TrimSpace(settings)) > 0 {
		if root, err = parseObject(settings); err != nil {
			return nil, false, fmt.Errorf("settings file is not a JSON object: %w", err)
		}
	}
	hooks := object{}
	if raw, ok := root.get("hooks"); ok {
		if hooks, err = parseObject(raw); err != nil {
			return nil, false, fmt.Errorf(`"hooks" is not a JSON object: %w`, err)
		}
	}

	// Remove existing switchboard entries from every event.
	var cleaned object
	for _, kv := range hooks {
		raw, err := withoutSwitchboard(kv.Value)
		if err != nil {
			return nil, false, fmt.Errorf("hooks.%s: %w", kv.Key, err)
		}
		if raw != nil {
			cleaned = append(cleaned, member{Key: kv.Key, Value: raw})
		}
	}
	hooks = cleaned

	if !remove {
		for _, ev := range Events {
			var groups []json.RawMessage
			if raw, ok := hooks.get(ev.Name); ok {
				if err := json.Unmarshal(raw, &groups); err != nil {
					return nil, false, fmt.Errorf("hooks.%s: %w", ev.Name, err)
				}
			}
			g, err := json.Marshal(ourGroup(ev, command))
			if err != nil {
				return nil, false, err
			}
			groups = append(groups, g)
			raw, err := json.Marshal(groups)
			if err != nil {
				return nil, false, err
			}
			hooks = hooks.set(ev.Name, raw)
		}
	}

	if root, err = mergeStatusLine(root, command, remove); err != nil {
		return nil, false, err
	}
	if len(hooks) == 0 {
		root = root.del("hooks")
	} else {
		raw, err := hooks.marshal()
		if err != nil {
			return nil, false, err
		}
		root = root.set("hooks", raw)
	}
	out, err = indent(root)
	if err != nil {
		return nil, false, err
	}
	return out, !sameJSON(settings, out), nil
}

// withoutSwitchboard removes switchboard commands from the list of one event. It returns nil if
// the list is empty afterwards. Groups that held only switchboard commands are dropped; other
// groups and entries stay untouched.
func withoutSwitchboard(raw json.RawMessage) (json.RawMessage, error) {
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	var kept []json.RawMessage
	for _, g := range groups {
		obj, err := parseObject(g)
		if err != nil {
			kept = append(kept, g)
			continue
		}
		hooksRaw, ok := obj.get("hooks")
		if !ok {
			kept = append(kept, g)
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(hooksRaw, &entries); err != nil {
			kept = append(kept, g)
			continue
		}
		var keptEntries []json.RawMessage
		for _, e := range entries {
			var h struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(e, &h) == nil && IsSwitchboardCommand(h.Command) {
				continue
			}
			keptEntries = append(keptEntries, e)
		}
		if len(keptEntries) == len(entries) {
			kept = append(kept, g)
			continue
		}
		if len(keptEntries) == 0 {
			continue
		}
		newHooks, err := json.Marshal(keptEntries)
		if err != nil {
			return nil, err
		}
		g, err = obj.set("hooks", newHooks).marshal()
		if err != nil {
			return nil, err
		}
		kept = append(kept, g)
	}
	if len(kept) == 0 {
		return nil, nil
	}
	return json.Marshal(kept)
}

// sameJSON reports whether a and b hold the same JSON value; an empty input counts as {}.
func sameJSON(a, b []byte) bool {
	norm := func(d []byte) (any, error) {
		if len(bytes.TrimSpace(d)) == 0 {
			d = []byte("{}")
		}
		var v any
		err := json.Unmarshal(d, &v)
		return v, err
	}
	va, errA := norm(a)
	vb, errB := norm(b)
	if errA != nil || errB != nil {
		return false
	}
	ja, _ := json.Marshal(va)
	jb, _ := json.Marshal(vb)
	return bytes.Equal(ja, jb)
}

// member is one key-value pair of a JSON object.
type member struct {
	Key   string
	Value json.RawMessage
}

// object is a JSON object that keeps the order of its members.
type object []member

func parseObject(data []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("expected an object")
	}
	var obj object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("expected an object key")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		obj = append(obj, member{Key: key, Value: val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("unexpected data after the object")
	}
	return obj, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, m := range o {
		if m.Key == key {
			return m.Value, true
		}
	}
	return nil, false
}

func (o object) set(key string, val json.RawMessage) object {
	for i, m := range o {
		if m.Key == key {
			out := append(object(nil), o...)
			out[i].Value = val
			return out
		}
	}
	return append(append(object(nil), o...), member{Key: key, Value: val})
}

func (o object) del(key string) object {
	var out object
	for _, m := range o {
		if m.Key != key {
			out = append(out, m)
		}
	}
	return out
}

func (o object) marshal() (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(m.Key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.Value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func indent(o object) ([]byte, error) {
	raw, err := o.marshal()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// WriteSettings writes data to the settings file at path. A symlinked file is written through to
// its target, the file mode is kept, and an existing file is first copied to backup (if backup
// is not empty). The write is atomic.
func WriteSettings(path string, data []byte, backup string) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
		if backup != "" {
			old, err := os.ReadFile(target)
			if err != nil {
				return err
			}
			if err := os.WriteFile(backup, old, mode); err != nil {
				return fmt.Errorf("writing backup: %w", err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
