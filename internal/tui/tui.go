// Package tui is the interactive overview of switchboard: a list of all screen sessions with
// the status of their agents, a live view of the selected session, sending text, and attaching
// with screen -x.
package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

// Calls into screen and the state store. They are package variables so that tests can replace
// them; none of them runs inside Update.
var (
	collect = func() ([]sessions.Info, error, error) {
		store, err := state.Open("")
		if err != nil {
			return nil, nil, err
		}
		infos, warn, err := sessions.Collect(store, sessions.Options{Prune: true, Detect: true, Self: os.Getenv("STY")})
		return infos, warn, err
	}
	hardcopy  = screen.Hardcopy
	stuff     = screen.Stuff
	attachCmd = func(id string) *exec.Cmd {
		args := screen.AttachArgs(id)
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = screen.AttachEnv()
		return cmd
	}
)

const (
	// refreshInterval is how often the list and the view of the selected session are updated.
	refreshInterval = 2 * time.Second
	// peekDelay is the pause after moving the selection before the new session is read, so
	// that scrolling through the list does not start a hardcopy for every row passed.
	peekDelay = 150 * time.Millisecond
)

type mode int

const (
	modeList mode = iota
	modeFilter
	modeSend
)

type (
	tickMsg     struct{}
	sessionsMsg struct {
		infos     []sessions.Info
		warn, err error
	}
	peekStartMsg struct{ seq int }
	peekMsg      struct {
		id    string
		seq   int
		lines []string
		err   error
	}
	sentMsg struct {
		id  string
		err error
	}
	attachedMsg struct {
		id  string
		err error
	}
)

// Model is the Bubble Tea model of the overview.
type Model struct {
	all        []sessions.Info // as collected, sorted
	visible    []sessions.Info // after the agents-only switch and the filter
	selected   string          // ID of the selected session
	cursor     int
	offset     int // first visible row of the list
	agentsOnly bool
	filter     string

	mode       mode
	input      textinput.Model
	sendTarget sessions.Info

	peekID    string
	peekLines []string
	peekErr   error
	peekSeq   int // number of the latest peek request; older answers are dropped
	peekBusy  int // peek requests still running

	loaded     bool
	refreshing bool
	listErr    error
	status     string
	statusErr  bool

	self          string // $STY of the screen session the TUI itself runs in
	width, height int
}

// New returns the initial model. self is the ID of the screen session switchboard runs in, or
// empty.
func New(self string) Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0
	return Model{self: self, input: in, width: 80, height: 24}
}

// Run starts the overview on the terminal and returns when the user quits.
func Run() error {
	p := tea.NewProgram(New(os.Getenv("STY")), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func refreshCmd() tea.Cmd {
	return func() tea.Msg {
		infos, warn, err := collect()
		return sessionsMsg{infos: infos, warn: warn, err: err}
	}
}

func peekCmd(s sessions.Info, seq int) tea.Cmd {
	id, window := s.ID, s.Window()
	return func() tea.Msg {
		lines, err := hardcopy(id, window)
		return peekMsg{id: id, seq: seq, lines: lines, err: err}
	}
}

func sendCmd(s sessions.Info, text string) tea.Cmd {
	id, window := s.ID, s.Window()
	return func() tea.Msg {
		return sentMsg{id: id, err: stuff(id, window, text, true)}
	}
}

// current returns the selected session.
func (m Model) current() (sessions.Info, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return sessions.Info{}, false
	}
	return m.visible[m.cursor], true
}

func (m *Model) setStatus(msg string, isErr bool) {
	m.status, m.statusErr = msg, isErr
}

// applyView recomputes the visible rows and keeps the selection on the same session if it is
// still there.
func (m *Model) applyView() {
	m.visible = m.visible[:0:0]
	needle := strings.ToLower(m.filter)
	for _, s := range m.all {
		if m.agentsOnly && !s.IsAgent() {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(searchText(s)), needle) {
			continue
		}
		m.visible = append(m.visible, s)
	}
	m.cursor = min(m.cursor, len(m.visible)-1)
	for i, s := range m.visible {
		if s.ID == m.selected {
			m.cursor = i
			break
		}
	}
	m.cursor = max(m.cursor, 0)
	if s, ok := m.current(); ok {
		m.selected = s.ID
	} else {
		m.selected = ""
	}
}

func searchText(s sessions.Info) string {
	t := s.ID + " " + string(s.Status) + " " + s.Message
	if s.Agent != nil {
		t += " " + s.Agent.Cwd
	}
	return t
}

// selectionChanged starts reading the newly selected session after a short delay.
func (m *Model) selectionChanged(before string) tea.Cmd {
	if m.selected == before {
		return nil
	}
	m.peekSeq++
	m.peekID, m.peekLines, m.peekErr = "", nil, nil
	if m.selected == "" {
		return nil
	}
	seq := m.peekSeq
	return tea.Tick(peekDelay, func(time.Time) tea.Msg { return peekStartMsg{seq: seq} })
}

// startPeek reads the selected session now.
func (m *Model) startPeek() tea.Cmd {
	s, ok := m.current()
	if !ok || (m.self != "" && s.ID == m.self) {
		return nil
	}
	m.peekSeq++
	m.peekBusy++
	return peekCmd(s, m.peekSeq)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd()}
		if !m.refreshing {
			m.refreshing = true
			cmds = append(cmds, refreshCmd())
		}
		return m, tea.Batch(cmds...)

	case sessionsMsg:
		m.refreshing = false
		m.loaded = true
		m.listErr = msg.err
		if msg.err != nil {
			return m, nil
		}
		if msg.warn != nil {
			m.setStatus("Warning: "+msg.warn.Error(), true)
		}
		before := m.selected
		m.all = msg.infos
		m.applyView()
		m.clampOffset()
		if cmd := m.selectionChanged(before); cmd != nil {
			return m, cmd
		}
		if m.peekBusy == 0 {
			cmd := m.startPeek()
			return m, cmd
		}
		return m, nil

	case peekStartMsg:
		if msg.seq != m.peekSeq {
			return m, nil
		}
		cmd := m.startPeek()
		return m, cmd

	case peekMsg:
		m.peekBusy = max(m.peekBusy-1, 0)
		if msg.seq != m.peekSeq || msg.id != m.selected {
			return m, nil
		}
		m.peekID, m.peekLines, m.peekErr = msg.id, msg.lines, msg.err
		return m, nil

	case sentMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Sending to %s failed: %v", msg.id, msg.err), true)
			return m, nil
		}
		m.setStatus("Sent to "+msg.id+".", false)
		cmd := tea.Batch(refreshCmd(), m.startPeek())
		return m, cmd

	case attachedMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Attaching to %s failed: %v", msg.id, msg.err), true)
		} else {
			m.setStatus("Back from "+msg.id+".", false)
		}
		m.refreshing = true
		cmd := tea.Batch(refreshCmd(), m.startPeek())
		return m, cmd

	case tea.KeyMsg:
		// Over ssh, several keys typed quickly can arrive as one message. In the list, each
		// of them is a command of its own; once one opens an input, the rest is typed there.
		if m.mode == modeList && msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var model tea.Model = m
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				cmds = append(cmds, cmd)
			}
			return model, tea.Batch(cmds...)
		}
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeSend:
			return m.updateSend(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.selected
	move := func(to int) (tea.Model, tea.Cmd) {
		if len(m.visible) == 0 {
			return m, nil
		}
		m.cursor = min(max(to, 0), len(m.visible)-1)
		m.selected = m.visible[m.cursor].ID
		m.clampOffset()
		cmd := m.selectionChanged(before)
		return m, cmd
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		return move(m.cursor - 1)
	case "down", "j":
		return move(m.cursor + 1)
	case "pgup":
		return move(m.cursor - m.listHeight())
	case "pgdown":
		return move(m.cursor + m.listHeight())
	case "home", "g":
		return move(0)
	case "end", "G":
		return move(len(m.visible) - 1)
	case "r":
		m.setStatus("", false)
		if !m.refreshing {
			m.refreshing = true
			return m, refreshCmd()
		}
		return m, nil
	case "a":
		m.agentsOnly = !m.agentsOnly
		m.applyView()
		m.clampOffset()
		cmd := m.selectionChanged(before)
		return m, cmd
	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.CursorEnd()
		cmd := m.input.Focus()
		return m, cmd
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyView()
			m.clampOffset()
			cmd := m.selectionChanged(before)
			return m, cmd
		}
		m.setStatus("", false)
		return m, nil
	case "s":
		s, ok := m.current()
		if !ok {
			return m, nil
		}
		if s.State == screen.StateDead {
			m.setStatus(s.ID+" is dead; screen -wipe removes it.", true)
			return m, nil
		}
		m.mode = modeSend
		m.sendTarget = s
		m.input.SetValue("")
		cmd := m.input.Focus()
		return m, cmd
	case "enter":
		s, ok := m.current()
		if !ok {
			return m, nil
		}
		if err := m.canAttach(s); err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		id := s.ID
		return m, tea.ExecProcess(attachCmd(id), func(err error) tea.Msg {
			return attachedMsg{id: id, err: err}
		})
	}
	return m, nil
}

// canAttach refuses sessions that cannot or must not be attached from here.
func (m Model) canAttach(s sessions.Info) error {
	switch {
	case s.State == screen.StateDead:
		return fmt.Errorf("%s is dead; screen -wipe removes it", s.ID)
	case m.self != "" && s.ID == m.self:
		return errors.New("this is the session switchboard runs in; attaching to it would loop")
	}
	return nil
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.selected
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		m.mode = modeList
		m.input.Blur()
		return m, nil
	case "esc":
		m.mode = modeList
		m.input.Blur()
		m.filter = ""
		m.applyView()
		m.clampOffset()
		cmd := m.selectionChanged(before)
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filter = m.input.Value()
	m.applyView()
	m.clampOffset()
	cmd = tea.Batch(cmd, m.selectionChanged(before))
	return m, cmd
}

func (m Model) updateSend(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		m.input.Blur()
		m.setStatus("Nothing sent.", false)
		return m, nil
	case "enter":
		m.mode = modeList
		m.input.Blur()
		text := m.input.Value()
		m.setStatus("Sending to "+m.sendTarget.ID+" …", false)
		return m, sendCmd(m.sendTarget, text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
