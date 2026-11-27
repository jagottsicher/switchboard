// Package state stores what switchboard knows about agents in screen sessions: their status, the
// last message and where they run. Each screen session has one JSON file that hooks and the
// notify command update and that list and the TUI read. There is no daemon; files are the only
// shared state.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Status is the status of an agent.
type Status string

// Possible agent statuses.
const (
	StatusUnknown Status = "unknown"
	StatusWorking Status = "working"
	StatusWaiting Status = "waiting" // needs input or a permission decision
	StatusIdle    Status = "idle"    // finished its turn
	StatusExited  Status = "exited"
)

// ParseStatus checks s against the known statuses.
func ParseStatus(s string) (Status, error) {
	switch st := Status(s); st {
	case StatusUnknown, StatusWorking, StatusWaiting, StatusIdle, StatusExited:
		return st, nil
	}
	return "", fmt.Errorf("unknown status %q (valid: working, waiting, idle, exited, unknown)", s)
}

// Record is the stored state of one screen session.
type Record struct {
	// Session is the screen session ID "<pid>.<name>", as in $STY.
	Session string `json:"session"`
	// Window is the screen window the agent runs in, as in $WINDOW.
	Window string `json:"window,omitempty"`
	// Agent marks sessions that run an agent: started via `switchboard new` or reported by a hook.
	Agent bool `json:"agent"`
	// Source names what last reported the status, e.g. "hook" or "notify".
	Source string `json:"source,omitempty"`
	Status Status `json:"status"`
	// Event is the name of the last hook event.
	Event string `json:"event,omitempty"`
	// Message is the last message from the agent, with its time.
	Message   string    `json:"message,omitempty"`
	MessageAt time.Time `json:"message_at,omitzero"`
	// AgentSession is the agent's own session ID from the hook payload.
	AgentSession string    `json:"agent_session,omitempty"`
	Cwd          string    `json:"cwd,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Now returns the current time. Swappable for tests.
var Now = time.Now

// Store is the directory with all records.
type Store struct {
	dir string
}

// DefaultDir returns $XDG_STATE_HOME/switchboard, or ~/.local/state/switchboard.
func DefaultDir() (string, error) {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "switchboard"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "switchboard"), nil
}

// Open opens the store in dir and creates the directories with mode 0700. An empty dir means
// DefaultDir.
func Open(dir string) (*Store, error) {
	if dir == "" {
		var err error
		if dir, err = DefaultDir(); err != nil {
			return nil, err
		}
	}
	for _, d := range []string{dir, filepath.Join(dir, "sessions"), filepath.Join(dir, "events")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir}, nil
}

// Dir returns the store directory.
func (s *Store) Dir() string { return s.dir }

// fileName turns a session ID into a safe file name. Session names may contain spaces.
func fileName(session string) string {
	return url.PathEscape(session)
}

func (s *Store) recordPath(session string) string {
	return filepath.Join(s.dir, "sessions", fileName(session)+".json")
}

func (s *Store) lockPath(session string) string {
	return filepath.Join(s.dir, "sessions", fileName(session)+".lock")
}

func (s *Store) eventsPath(session string) string {
	return filepath.Join(s.dir, "events", fileName(session)+".jsonl")
}

// Get returns the record of session. ok is false if there is none.
func (s *Store) Get(session string) (rec Record, ok bool, err error) {
	data, err := os.ReadFile(s.recordPath(session))
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, false, fmt.Errorf("%s: %w", s.recordPath(session), err)
	}
	return rec, true, nil
}

// Update reads the record of session, applies fn and writes it back atomically. A file lock
// serializes concurrent updates, for example from hooks that fire in quick succession.
func (s *Store) Update(session string, fn func(*Record)) (Record, error) {
	lock, err := os.OpenFile(s.lockPath(session), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Record{}, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Record{}, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	rec, ok, err := s.Get(session)
	if err != nil {
		// A damaged file must not block updates forever; start over.
		ok = false
	}
	if !ok {
		rec = Record{Session: session, Status: StatusUnknown}
	}
	fn(&rec)
	rec.Session = session
	rec.UpdatedAt = Now()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return Record{}, err
	}
	return rec, writeAtomic(s.recordPath(session), append(data, '\n'))
}

// writeAtomic writes data to a temporary file in the same directory and renames it.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// maxEventsSize is the size from which the event log of a session is rotated.
const maxEventsSize = 1 << 20

// AppendEvent appends one JSON line to the event log of session. The log is kept for debugging;
// when it grows beyond maxEventsSize, it is renamed to *.jsonl.1 and started anew.
func (s *Store) AppendEvent(session string, event any) error {
	path := s.eventsPath(session)
	if info, err := os.Stat(path); err == nil && info.Size() > maxEventsSize {
		os.Rename(path, path+".1")
	}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// All returns all records, keyed by session ID. Unreadable files are skipped.
func (s *Store) All() (map[string]Record, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "sessions"))
	if err != nil {
		return nil, err
	}
	out := map[string]Record{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		session, err := url.PathUnescape(name)
		if err != nil {
			continue
		}
		rec, found, err := s.Get(session)
		if err != nil || !found {
			continue
		}
		out[session] = rec
	}
	return out, nil
}

// Prune removes records, locks and event logs of sessions that are not in alive. Call it only
// with the result of a successful `screen -ls`. Records updated at or after before are kept: a
// session started after that listing may already have reported. A zero before prunes all
// records of sessions that are not alive.
func (s *Store) Prune(alive map[string]bool, before time.Time) error {
	recs, err := s.All()
	if err != nil {
		return err
	}
	var errs []error
	for session, rec := range recs {
		if alive[session] || (!before.IsZero() && !rec.UpdatedAt.Before(before)) {
			continue
		}
		for _, p := range []string{s.recordPath(session), s.lockPath(session), s.eventsPath(session), s.eventsPath(session) + ".1"} {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// LogError appends a line to hook.log in the store directory. Hooks must never write to stdout,
// so this is where their problems go.
func LogError(dir string, msg string) {
	if dir == "" {
		var err error
		if dir, err = DefaultDir(); err != nil {
			return
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "hook.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}
