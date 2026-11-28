// Package tui is the interactive overview of switchboard: a list of all screen sessions with
// the status of their agents, a live view of the selected session, sending text, answering
// dialogs, and attaching with screen -x.
package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/config"
	"github.com/jagottsicher/switchboard/internal/host"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

// registry holds the hosts of the overview by name; "" is this machine.
var registry = map[string]host.Host{"": host.NewLocal()}

func lookup(name string) (host.Host, error) {
	if h, ok := registry[name]; ok {
		return h, nil
	}
	return nil, fmt.Errorf("unknown host %q", name)
}

// Calls into the hosts. They are package variables so that tests can replace them; none of them
// runs inside Update.
var (
	collect = func(hostName string) ([]sessions.Info, error, error) {
		h, err := lookup(hostName)
		if err != nil {
			return nil, nil, err
		}
		return h.List()
	}
	hardcopy = func(hostName, id string, window int) ([]string, error) {
		h, err := lookup(hostName)
		if err != nil {
			return nil, err
		}
		return h.Hardcopy(id, window, true)
	}
	stuff = func(hostName, id string, window int, text string, enter bool) error {
		h, err := lookup(hostName)
		if err != nil {
			return err
		}
		return h.Stuff(id, window, text, enter)
	}
	readDialog = func(hostName, id string, window int) (answer.Dialog, error) {
		h, err := lookup(hostName)
		if err != nil {
			return answer.Dialog{}, err
		}
		return h.ReadDialog(id, window)
	}
	sendChoice = func(hostName, id string, window, n int, label, fingerprint string) error {
		h, err := lookup(hostName)
		if err != nil {
			return err
		}
		return h.SendChoice(id, window, n, label, fingerprint)
	}
	attachCmd = func(hostName, id string) *exec.Cmd {
		h, err := lookup(hostName)
		if err != nil {
			return exec.Command("sh", "-c", "exit 1")
		}
		return h.AttachCmd(id)
	}
)

const (
	// refreshInterval is how often the list and the view of the selected session are updated.
	refreshInterval = 2 * time.Second
	// peekDelay is the pause after moving the selection before the new session is read, so
	// that scrolling through the list does not start a hardcopy for every row passed.
	peekDelay = 150 * time.Millisecond
)

// now is the clock; swappable for tests.
var now = time.Now

type mode int

const (
	modeList mode = iota
	modeFilter
	modeSend
	modeAnswer
)

type (
	tickMsg     struct{}
	sessionsMsg struct {
		host      string
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
	dialogMsg struct {
		id  string
		n   int
		d   answer.Dialog
		err error
	}
	answeredMsg struct {
		id    string
		n     int
		label string
		err   error
	}
)

// Model is the Bubble Tea model of the overview.
type Model struct {
	all        []sessions.Info // as collected, sorted
	visible    []sessions.Info // after the agents-only switch and the filter
	selected   string          // key (HOST:ID) of the selected session
	cursor     int
	offset     int // first visible row of the list
	agentsOnly bool
	filter     string

	mode       mode
	input      textinput.Model
	sendTarget sessions.Info

	// Answering a dialog: the choice the user pressed, and the dialog as read from the screen
	// right after the key press. answerReady is false while that read is still running.
	answerTarget sessions.Info
	answerN      int
	answerDialog answer.Dialog
	answerReady  bool

	peekID    string // key of the session the view shows
	peekLines []string
	peekErr   error
	peekSeq   int // number of the latest peek request; older answers are dropped
	peekBusy  int // peek requests still running
	// peekScroll is how many lines the view of the session is scrolled up. While it is above 0,
	// the view keeps the screen it shows instead of following the session live.
	peekScroll int

	hosts      []string                   // names of the hosts; "" is this machine
	byHost     map[string][]sessions.Info // sessions as collected per host
	hostErr    map[string]error           // hosts that could not be listed
	refreshing map[string]bool            // hosts whose list is being read
	loaded     bool                       // this machine has been listed once
	listErr    error                      // listing this machine failed
	status     string
	statusErr  bool

	self          string // $STY of the screen session the TUI itself runs in
	width, height int
}

// New returns the initial model for this machine and the given other hosts. self is the ID of
// the screen session switchboard runs in, or empty.
func New(self string, hosts ...string) Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0
	return Model{
		self: self, input: in, width: 80, height: 24,
		hosts:      append([]string{""}, hosts...),
		byHost:     map[string][]sessions.Info{},
		hostErr:    map[string]error{},
		refreshing: map[string]bool{},
	}
}

// loadConfig is swappable for tests.
var loadConfig = config.Load

// Run starts the overview on the terminal with this machine and the configured hosts, and
// returns when the user quits.
func Run() error {
	m, hosts := newFromConfig(os.Getenv("STY"))
	for _, h := range hosts {
		defer h.Close()
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// newFromConfig returns the model for this machine and the configured hosts and registers the
// hosts. A configuration that cannot be read leaves only this machine, with the error in the
// status line, so a typo in the file never keeps the overview from starting.
func newFromConfig(self string) (Model, []host.Host) {
	cfg, err := loadConfig()
	if err != nil {
		m := New(self)
		m.setStatus(err.Error(), true)
		return m, nil
	}
	hosts := host.FromConfig(cfg)
	var names []string
	for _, h := range hosts {
		registry[h.Name()] = h
		names = append(names, h.Name())
	}
	return New(self, names...), hosts
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	for _, h := range m.hosts {
		m.refreshing[h] = true
		cmds = append(cmds, refreshCmd(h))
	}
	return tea.Batch(cmds...)
}

// refreshAll starts reading the hosts whose list is not being read already.
func (m *Model) refreshAll() tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.hosts {
		if !m.refreshing[h] {
			m.refreshing[h] = true
			cmds = append(cmds, refreshCmd(h))
		}
	}
	return tea.Batch(cmds...)
}

// merge rebuilds the list of all sessions from the lists of the hosts.
func (m *Model) merge() {
	m.all = m.all[:0:0]
	for _, h := range m.hosts {
		m.all = append(m.all, m.byHost[h]...)
	}
	sessions.Sort(m.all)
}

// isSelf reports whether s is the session the overview runs in.
func (m Model) isSelf(s sessions.Info) bool {
	return m.self != "" && s.Host == "" && s.ID == m.self
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func refreshCmd(hostName string) tea.Cmd {
	return func() tea.Msg {
		infos, warn, err := collect(hostName)
		return sessionsMsg{host: hostName, infos: infos, warn: warn, err: err}
	}
}

func peekCmd(s sessions.Info, seq int) tea.Cmd {
	hostName, id, key, window := s.Host, s.ID, s.Key(), s.Window()
	return func() tea.Msg {
		lines, err := hardcopy(hostName, id, window)
		return peekMsg{id: key, seq: seq, lines: lines, err: err}
	}
}

func sendCmd(s sessions.Info, text string) tea.Cmd {
	hostName, id, key, window := s.Host, s.ID, s.Key(), s.Window()
	return func() tea.Msg {
		return sentMsg{id: key, err: stuff(hostName, id, window, text, true)}
	}
}

func readDialogCmd(s sessions.Info, n int) tea.Cmd {
	hostName, id, key, window := s.Host, s.ID, s.Key(), s.Window()
	return func() tea.Msg {
		d, err := readDialog(hostName, id, window)
		return dialogMsg{id: key, n: n, d: d, err: err}
	}
}

func answerCmd(s sessions.Info, n int, label, fingerprint string) tea.Cmd {
	hostName, id, key, window := s.Host, s.ID, s.Key(), s.Window()
	return func() tea.Msg {
		return answeredMsg{id: key, n: n, label: label, err: sendChoice(hostName, id, window, n, label, fingerprint)}
	}
}

// choices returns the choices of the dialog that s shows, if they could be read.
func choices(s sessions.Info) []string {
	if s.Status != state.StatusWaiting || s.Detected == nil {
		return nil
	}
	return s.Detected.Options
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
		if s.Key() == m.selected {
			m.cursor = i
			break
		}
	}
	m.cursor = max(m.cursor, 0)
	if s, ok := m.current(); ok {
		m.selected = s.Key()
	} else {
		m.selected = ""
	}
}

func searchText(s sessions.Info) string {
	t := s.Key() + " " + string(s.Status) + " " + s.Message
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
	m.peekScroll = 0
	if m.selected == "" {
		return nil
	}
	seq := m.peekSeq
	return tea.Tick(peekDelay, func(time.Time) tea.Msg { return peekStartMsg{seq: seq} })
}

// startPeek reads the selected session now.
func (m *Model) startPeek() tea.Cmd {
	s, ok := m.current()
	if !ok || m.isSelf(s) {
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
		cmd := tea.Batch(tickCmd(), m.refreshAll())
		return m, cmd

	case sessionsMsg:
		m.refreshing[msg.host] = false
		if msg.host == "" {
			m.loaded = true
			m.listErr = msg.err
		}
		if msg.err != nil {
			if msg.host == "" {
				m.hostErr[msg.host] = msg.err
				return m, nil
			}
			if m.hostErr[msg.host] == nil {
				m.setStatus(msg.err.Error(), true)
			}
			m.hostErr[msg.host] = msg.err
			m.byHost[msg.host] = nil
		} else {
			delete(m.hostErr, msg.host)
			m.byHost[msg.host] = msg.infos
		}
		if msg.warn != nil {
			prefix := "Warning: "
			if msg.host != "" {
				prefix += msg.host + ": "
			}
			m.setStatus(prefix+msg.warn.Error(), true)
		}
		before := m.selected
		m.merge()
		m.applyView()
		m.clampOffset()
		if cmd := m.selectionChanged(before); cmd != nil {
			return m, cmd
		}
		if m.peekBusy == 0 && m.peekScroll == 0 {
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
		if msg.seq != m.peekSeq || msg.id != m.selected || m.peekScroll > 0 {
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
		cmd := tea.Batch(m.refreshAll(), m.startPeek())
		return m, cmd

	case dialogMsg:
		if m.mode != modeAnswer || m.answerReady || msg.id != m.answerTarget.Key() || msg.n != m.answerN {
			return m, nil
		}
		switch {
		case msg.err != nil:
			m.mode = modeList
			m.setStatus(fmt.Sprintf("%s: %v", msg.id, msg.err), true)
		case msg.n > len(msg.d.Options):
			m.mode = modeList
			m.setStatus(fmt.Sprintf("%s: the dialog has only %d choices.", msg.id, len(msg.d.Options)), true)
		default:
			m.answerDialog, m.answerReady = msg.d, true
		}
		return m, nil

	case answeredMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Answering %s failed: %v", msg.id, msg.err), true)
		} else {
			text := fmt.Sprintf("Answered %s: %d. %s", msg.id, msg.n, msg.label)
			if strings.EqualFold(msg.label, "Type something.") {
				text += " Type the answer with s."
			}
			m.setStatus(text, false)
		}
		cmd := tea.Batch(m.refreshAll(), m.startPeek())
		return m, cmd

	case attachedMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Attaching to %s failed: %v", msg.id, msg.err), true)
		} else {
			m.setStatus("Back from "+msg.id+".", false)
		}
		cmd := tea.Batch(m.refreshAll(), m.startPeek())
		return m, cmd

	case tea.KeyMsg:
		// Over ssh, several keys typed quickly can arrive as one message. In the list, each
		// of them is a command of its own; once one opens an input, the rest is typed there.
		if m.mode == modeList && msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			// A choice must be confirmed with a key of its own, so the rest of the burst after a
			// digit is dropped.
			var model tea.Model = m
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				cmds = append(cmds, cmd)
				if model.(Model).mode == modeAnswer {
					break
				}
			}
			return model, tea.Batch(cmds...)
		}
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeSend:
			return m.updateSend(msg)
		case modeAnswer:
			return m.updateAnswer(msg)
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
		m.selected = m.visible[m.cursor].Key()
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
		return m.scrollPeek(m.peekPage())
	case "pgdown":
		return m.scrollPeek(-m.peekPage())
	case "home", "g":
		return move(0)
	case "end", "G":
		return move(len(m.visible) - 1)
	case "r":
		m.setStatus("", false)
		return m, m.refreshAll()
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
		if m.peekScroll > 0 {
			return m.scrollPeek(-m.peekScroll)
		}
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
			m.setStatus(s.Key()+" is dead; screen -wipe removes it.", true)
			return m, nil
		}
		m.mode = modeSend
		m.sendTarget = s
		m.input.SetValue("")
		cmd := m.input.Focus()
		return m, cmd
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		n, _ := strconv.Atoi(msg.String())
		return m.startAnswer(n)
	case "enter":
		s, ok := m.current()
		if !ok {
			return m, nil
		}
		if err := m.canAttach(s); err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		key := s.Key()
		return m, tea.ExecProcess(attachCmd(s.Host, s.ID), func(err error) tea.Msg {
			return attachedMsg{id: key, err: err}
		})
	}
	return m, nil
}

// peekBody returns the number of screen lines the view of the selected session shows.
func (m Model) peekBody() int {
	_, rows := m.layout()
	return max(rows-1, 1)
}

// peekPage is how far one key scrolls the view of the session: a page, minus one line that stays
// visible for orientation.
func (m Model) peekPage() int {
	return max(m.peekBody()-1, 1)
}

// scrollPeek scrolls the view of the selected session by n lines, up for n > 0. Scrolled back to
// the bottom, the view follows the session live again.
func (m Model) scrollPeek(n int) (tea.Model, tea.Cmd) {
	if m.peekID == "" || m.peekID != m.selected {
		return m, nil
	}
	before := m.peekScroll
	m.peekScroll = min(max(m.peekScroll+n, 0), max(len(m.peekLines)-m.peekBody(), 0))
	if before > 0 && m.peekScroll == 0 {
		cmd := m.startPeek()
		return m, cmd
	}
	return m, nil
}

// startAnswer begins answering the dialog of the selected session with choice n. The dialog is
// read again from the screen, and the choice is only sent after the user confirmed its label.
func (m Model) startAnswer(n int) (tea.Model, tea.Cmd) {
	s, ok := m.current()
	if !ok {
		return m, nil
	}
	opts := choices(s)
	switch {
	case s.State == screen.StateDead:
		m.setStatus(s.Key()+" is dead; screen -wipe removes it.", true)
		return m, nil
	case m.isSelf(s):
		m.setStatus("This is the session switchboard runs in.", true)
		return m, nil
	case s.Status != state.StatusWaiting:
		m.setStatus(s.Key()+" shows no dialog.", true)
		return m, nil
	case len(opts) == 0:
		m.setStatus(s.Key()+": the choices of the dialog cannot be read; attach to answer it.", true)
		return m, nil
	case n > len(opts):
		m.setStatus(fmt.Sprintf("%s: the dialog has only %d choices.", s.Key(), len(opts)), true)
		return m, nil
	}
	m.mode = modeAnswer
	m.answerTarget, m.answerN = s, n
	m.answerDialog, m.answerReady = answer.Dialog{}, false
	m.setStatus("", false)
	return m, readDialogCmd(s, n)
}

func (m Model) updateAnswer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if !m.answerReady && key != "esc" {
		return m, nil
	}
	m.mode = modeList
	if key != "y" {
		m.setStatus("Nothing sent.", false)
		return m, nil
	}
	label := m.answerDialog.Options[m.answerN-1]
	m.setStatus(fmt.Sprintf("Answering %s: %d. %s …", m.answerTarget.Key(), m.answerN, label), false)
	return m, answerCmd(m.answerTarget, m.answerN, label, m.answerDialog.Fingerprint)
}

// canAttach refuses sessions that cannot or must not be attached from here.
func (m Model) canAttach(s sessions.Info) error {
	switch {
	case s.State == screen.StateDead:
		return fmt.Errorf("%s is dead; screen -wipe removes it", s.Key())
	case m.isSelf(s):
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
		m.setStatus("Sending to "+m.sendTarget.Key()+" …", false)
		return m, sendCmd(m.sendTarget, text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
