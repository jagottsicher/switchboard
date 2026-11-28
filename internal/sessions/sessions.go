// Package sessions combines the screen sessions with what switchboard knows about their agents.
// The list command and the TUI both use it.
package sessions

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jagottsicher/switchboard/internal/detect"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/state"
)

// Info is a screen session together with what switchboard knows about its agent: the stored
// record from hooks and notify, and what the screen shows. Status and Message are the result of
// both.
type Info struct {
	screen.Session
	// Host is the configured name of the machine the session runs on, empty for this machine.
	Host     string         `json:"host,omitempty"`
	Agent    *state.Record  `json:"agent,omitempty"`
	Detected *detect.Result `json:"detected,omitempty"`
	Status   state.Status   `json:"status,omitempty"`
	Message  string         `json:"message,omitempty"`
}

// IsAgent reports whether the session is known to run an agent.
func (i Info) IsAgent() bool {
	return (i.Agent != nil && i.Agent.Agent) || i.Detected != nil
}

// Window returns the screen window the agent reported, or 0.
func (i Info) Window() int {
	if i.Agent != nil {
		if w, err := strconv.Atoi(i.Agent.Window); err == nil {
			return w
		}
	}
	return 0
}

// Swappable for tests.
var (
	listScreens = screen.List
	readScreen  = screen.Visible
	now         = time.Now
)

const (
	// hookGrace is how long a hook event takes precedence over the screen. The screen lags
	// behind: right after a prompt was submitted, it may still show the idle input line.
	hookGrace = 5 * time.Second
	// parallelReads limits how many screens are read at the same time.
	parallelReads = 4
)

// Options control Collect.
type Options struct {
	// Prune removes the records of sessions that no longer exist.
	Prune bool
	// Detect reads the visible screen of each session and recognizes agents on it.
	Detect bool
	// Self is the session switchboard runs in ($STY), if any. Its screen is not read: it may
	// show another agent's screen.
	Self string
}

// Collect lists the screen sessions together with their stored state, sorted so that sessions
// needing attention come first. A problem with pruning is returned as warn while the list
// itself is still valid.
func Collect(store *state.Store, opts Options) (infos []Info, warn error, err error) {
	listed := now()
	list, err := listScreens()
	if err != nil {
		return nil, nil, err
	}
	records, err := store.All()
	if err != nil {
		return nil, nil, err
	}
	alive := map[string]bool{}
	infos = make([]Info, 0, len(list))
	for _, s := range list {
		alive[s.ID] = true
		info := Info{Session: s}
		if rec, ok := records[s.ID]; ok {
			info.Agent = &rec
		}
		infos = append(infos, info)
	}
	var detected []*detect.Result
	if opts.Detect {
		detected = detectAll(infos, opts.Self)
	}
	for i := range infos {
		var d *detect.Result
		if detected != nil {
			d = detected[i]
		}
		resolve(&infos[i], d)
	}
	// Pruning only follows a successful screen -ls, so a broken screen setup never wipes state.
	// Records written after the listing started belong to sessions that may be too new for it.
	if opts.Prune {
		if err := store.Prune(alive, listed); err != nil {
			warn = fmt.Errorf("cleaning up old state: %w", err)
		}
	}
	Sort(infos)
	return infos, warn, nil
}

// detectAll reads the visible screen of every live session and runs the detector on it. Read
// errors only mean that nothing is detected for that session.
func detectAll(infos []Info, self string) []*detect.Result {
	out := make([]*detect.Result, len(infos))
	sem := make(chan struct{}, parallelReads)
	var wg sync.WaitGroup
	for i, info := range infos {
		if info.State == screen.StateDead || (self != "" && info.ID == self) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			lines, err := readScreen(info.ID, info.Window())
			if err != nil {
				return
			}
			if r, ok := detect.Detect(lines); ok {
				out[i] = &r
			}
		}()
	}
	wg.Wait()
	return out
}

// resolve sets Status, Message and Detected of info from the stored record and what the screen
// shows. A dead session is exited, and so is an agent that reported its end. A recent hook
// event wins, because the screen lags behind it. Otherwise a recognized screen wins, which
// covers changes that send no hook event, such as an interrupt with Esc or a denied permission.
// Without either, the record decides.
//
// An open menu of the agent is detected as unknown. It only replaces a stale record, so that a
// status left behind by an interrupt does not stay on screen; on its own, it does not mark a
// session as an agent.
//
// While the agent waits, the question of its dialog is the message, because it says more than
// the hook messages and stays the same while hook events arrive.
func resolve(info *Info, d *detect.Result) {
	rec := info.Agent
	if d != nil && d.Status == state.StatusUnknown && (rec == nil || !rec.Agent) {
		d = nil
	}
	info.Detected = d
	switch {
	case info.State == screen.StateDead:
		info.Status = state.StatusExited
		if rec != nil {
			info.Message = rec.Message
		}
	case rec != nil && rec.Status == state.StatusExited:
		info.Status = state.StatusExited
		info.Message = rec.Message
	case d != nil && (rec == nil || rec.Status == "" || now().Sub(rec.UpdatedAt) > hookGrace):
		info.Status = d.Status
		info.Message = ""
		switch {
		case d.Status == state.StatusWaiting && d.Question != "":
			info.Message = d.Question
		case rec != nil && rec.Status == d.Status:
			info.Message = rec.Message
		}
	case rec != nil:
		info.Status = rec.Status
		info.Message = rec.Message
		if rec.Status == state.StatusWaiting && d != nil && d.Status == state.StatusWaiting && d.Question != "" {
			info.Message = d.Question
		}
	}
}

// Sort orders sessions by Rank, then by host (this machine first) and ID.
func Sort(infos []Info) {
	sort.SliceStable(infos, func(i, j int) bool {
		ri, rj := Rank(infos[i]), Rank(infos[j])
		if ri != rj {
			return ri < rj
		}
		if infos[i].Host != infos[j].Host {
			return infos[i].Host < infos[j].Host
		}
		return infos[i].ID < infos[j].ID
	})
}

// Key identifies a session across hosts: "host:ID", or the ID alone on this machine.
func (i Info) Key() string {
	if i.Host == "" {
		return i.ID
	}
	return i.Host + ":" + i.ID
}

// Rank orders sessions: waiting agents first, then working, idle, other agents, and plain
// screen sessions last.
func Rank(s Info) int {
	if !s.IsAgent() {
		return 9
	}
	switch s.Status {
	case state.StatusWaiting:
		return 0
	case state.StatusWorking:
		return 1
	case state.StatusIdle:
		return 2
	case state.StatusUnknown:
		return 3
	}
	return 4
}

// Age formats the time since t compactly, e.g. "45s", "12m", "3h", "2d".
func Age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now().Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Shorten cuts s to at most n characters and keeps it on one line.
func Shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
