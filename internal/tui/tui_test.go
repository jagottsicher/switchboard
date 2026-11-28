package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

func agent(id string, st state.Status, msg string) sessions.Info {
	return sessions.Info{
		Session: screen.Session{ID: id, State: screen.StateDetached},
		Agent:   &state.Record{Agent: true, Status: st, Message: msg, Window: "1"},
		Status:  st,
		Message: msg,
	}
}

func plain(id string) sessions.Info {
	return sessions.Info{Session: screen.Session{ID: id, State: screen.StateAttached}}
}

// fakes replaces all calls into screen and records sends.
type fakes struct {
	sent []string
}

func setup(t *testing.T) *fakes {
	t.Helper()
	f := &fakes{}
	oc, oh, os_, oa := collect, hardcopy, stuff, attachCmd
	t.Cleanup(func() { collect, hardcopy, stuff, attachCmd = oc, oh, os_, oa })
	collect = func(string) ([]sessions.Info, error, error) { return nil, nil, nil }
	hardcopy = func(hostName, id string, window int) ([]string, error) {
		return []string{"screen of " + id}, nil
	}
	stuff = func(hostName, id string, window int, text string, enter bool) error {
		f.sent = append(f.sent, id+":"+text)
		return nil
	}
	attachCmd = func(hostName, id string) *exec.Cmd { return exec.Command("true") }
	return f
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m, _ = update(t, m, key(string(r)))
	}
	return m
}

func loaded(t *testing.T, self string, infos ...sessions.Info) Model {
	t.Helper()
	m, _ := update(t, New(self), tea.WindowSizeMsg{Width: 100, Height: 20})
	m, _ = update(t, m, sessionsMsg{infos: infos})
	return m
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestSelectionFollowsSessionAcrossRefresh(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.a", state.StatusWorking, ""), agent("2.b", state.StatusIdle, ""))
	m, _ = update(t, m, key("down"))
	if m.selected != "2.b" {
		t.Fatalf("selected = %q", m.selected)
	}
	// 2.b now waits and moves to the top; the selection stays on it.
	m, _ = update(t, m, sessionsMsg{infos: []sessions.Info{
		agent("2.b", state.StatusWaiting, "Permission requested: Bash"), agent("1.a", state.StatusWorking, ""),
	}})
	if m.selected != "2.b" || m.cursor != 0 {
		t.Errorf("selected = %q, cursor = %d", m.selected, m.cursor)
	}
}

func TestStalePeekIsDropped(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""), agent("2.b", state.StatusIdle, ""))
	m, _ = update(t, m, peekStartMsg{seq: m.peekSeq})
	oldSeq := m.peekSeq
	m, _ = update(t, m, key("down"))
	// The answer for 1.a arrives after the selection moved to 2.b.
	m, _ = update(t, m, peekMsg{id: "1.a", seq: oldSeq, lines: []string{"old screen"}})
	if m.peekID != "" || m.peekLines != nil {
		t.Errorf("stale peek shown: %q %v", m.peekID, m.peekLines)
	}
	if strings.Contains(m.View(), "old screen") {
		t.Error("view shows the previous session's screen")
	}
	m, _ = update(t, m, peekMsg{id: "2.b", seq: m.peekSeq, lines: []string{"new screen"}})
	if !strings.Contains(m.View(), "new screen") {
		t.Error("view does not show the current peek")
	}
}

func TestPeekReadsTheAgentWindow(t *testing.T) {
	setup(t)
	var gotWindow = -1
	hardcopy = func(hostName, id string, window int) ([]string, error) { gotWindow = window; return nil, nil }
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""))
	cmd := m.startPeek()
	cmd()
	if gotWindow != 1 {
		t.Errorf("hardcopy window = %d, want 1", gotWindow)
	}
}

func TestFilterModeCapturesKeys(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.api", state.StatusIdle, ""), agent("2.web", state.StatusIdle, ""), plain("3.qa"))
	m, _ = update(t, m, key("/"))
	if m.mode != modeFilter {
		t.Fatal("not in filter mode")
	}
	// q and a are text here, not quit and the agents switch.
	m = typeText(t, m, "qa")
	if m.agentsOnly || m.mode != modeFilter {
		t.Fatalf("keys leaked into the list: agentsOnly=%v mode=%v", m.agentsOnly, m.mode)
	}
	if len(m.visible) != 1 || m.visible[0].ID != "3.qa" {
		t.Errorf("visible = %v", m.visible)
	}
	m, _ = update(t, m, key("enter"))
	if m.mode != modeList || m.filter != "qa" {
		t.Errorf("after enter: mode=%v filter=%q", m.mode, m.filter)
	}
	m, _ = update(t, m, key("esc"))
	if m.filter != "" || len(m.visible) != 3 {
		t.Errorf("esc did not clear the filter: %q %d", m.filter, len(m.visible))
	}
}

func TestAgentsOnly(t *testing.T) {
	setup(t)
	m := loaded(t, "", plain("1.shell"), agent("2.a", state.StatusIdle, ""))
	m, _ = update(t, m, key("a"))
	if len(m.visible) != 1 || m.selected != "2.a" {
		t.Errorf("visible = %v, selected = %q", m.visible, m.selected)
	}
}

func TestSendKeepsTargetAndCapturesKeys(t *testing.T) {
	f := setup(t)
	m := loaded(t, "", agent("1.a", state.StatusWaiting, ""), agent("2.b", state.StatusIdle, ""))
	m, _ = update(t, m, key("s"))
	if m.mode != modeSend {
		t.Fatal("not in send mode")
	}
	m = typeText(t, m, "yes q")
	// A refresh re-sorts the list while typing; the text still goes to 1.a.
	m, _ = update(t, m, sessionsMsg{infos: []sessions.Info{
		agent("2.b", state.StatusWaiting, ""), agent("1.a", state.StatusWorking, ""),
	}})
	m, cmd := update(t, m, key("enter"))
	if m.mode != modeList || cmd == nil {
		t.Fatal("enter did not send")
	}
	msg := cmd()
	if len(f.sent) != 1 || f.sent[0] != "1.a:yes q" {
		t.Errorf("sent = %v", f.sent)
	}
	m, _ = update(t, m, msg)
	if !strings.Contains(m.status, "Sent to 1.a") {
		t.Errorf("status = %q", m.status)
	}
}

func TestSendEscSendsNothing(t *testing.T) {
	f := setup(t)
	m := loaded(t, "", agent("1.a", state.StatusWaiting, ""))
	m, _ = update(t, m, key("s"))
	m = typeText(t, m, "no")
	m, cmd := update(t, m, key("esc"))
	if cmd != nil || m.mode != modeList || len(f.sent) != 0 {
		t.Errorf("esc: cmd=%v mode=%v sent=%v", cmd, m.mode, f.sent)
	}
}

func TestSendError(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""))
	m, _ = update(t, m, sentMsg{id: "1.a", err: errors.New("boom")})
	if !m.statusErr || !strings.Contains(m.status, "boom") {
		t.Errorf("status = %q", m.status)
	}
}

func TestAttachRefusesOwnAndDeadSessions(t *testing.T) {
	setup(t)
	dead := plain("2.dead")
	dead.State = screen.StateDead
	m := loaded(t, "1.self", plain("1.self"), dead, plain("3.ok"))
	for _, id := range []string{"1.self", "2.dead"} {
		for m.selected != id {
			m, _ = update(t, m, key("down"))
		}
		var cmd tea.Cmd
		m, cmd = update(t, m, key("enter"))
		if cmd != nil || !m.statusErr {
			t.Errorf("attach to %s was not refused: status %q", id, m.status)
		}
	}
	m, _ = update(t, m, key("down"))
	if _, cmd := update(t, m, key("enter")); cmd == nil {
		t.Error("attach to 3.ok was refused")
	}
	m, _ = update(t, m, key("g"))
	if cmd := m.startPeek(); cmd != nil {
		t.Error("the overview reads its own screen")
	}
	if !strings.Contains(m.View(), "1.self (here)") {
		t.Error("own session is not marked")
	}
}

func TestQuitOnlyInListMode(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""))
	if _, cmd := update(t, m, key("q")); !isQuit(cmd) {
		t.Error("q does not quit in the list")
	}
	m, _ = update(t, m, key("s"))
	if _, cmd := update(t, m, key("q")); isQuit(cmd) {
		t.Error("q quits while typing")
	}
}

func TestViewFitsTheTerminal(t *testing.T) {
	setup(t)
	infos := []sessions.Info{agent("1.waiting-agent", state.StatusWaiting, strings.Repeat("long message ", 20))}
	for i := 0; i < 30; i++ {
		infos = append(infos, plain("1000"+string(rune('a'+i%26))+".shell"))
	}
	m := loaded(t, "", infos...)
	m, _ = update(t, m, peekMsg{id: m.selected, seq: m.peekSeq, lines: []string{strings.Repeat("界", 80), "\x1b[31mred"}})
	for _, size := range [][2]int{{100, 20}, {40, 10}, {20, 5}} {
		m, _ = update(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(strings.TrimSuffix(m.View(), "\n"), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line is %d wide: %q", size[0], size[1], w, l)
			}
		}
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	v := m.View()
	if !strings.Contains(v, "1 waiting") || strings.Contains(v, "\x1b[31mred") {
		t.Errorf("view:\n%s", v)
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	setup(t)
	var infos []sessions.Info
	for i := 0; i < 40; i++ {
		infos = append(infos, plain(string(rune('A'+i))+".s"))
	}
	m := loaded(t, "", infos...)
	for i := 0; i < 30; i++ {
		m, _ = update(t, m, key("down"))
	}
	if !strings.Contains(m.View(), "› "+m.selected) {
		t.Errorf("selected row %s is not visible", m.selected)
	}
}

func TestListError(t *testing.T) {
	setup(t)
	m, _ := update(t, New(""), sessionsMsg{err: screen.ErrNotInstalled})
	if !strings.Contains(m.View(), screen.ErrNotInstalled.Error()) {
		t.Error("error not shown")
	}
}

func TestKeysArrivingTogetherAreHandledOneByOne(t *testing.T) {
	setup(t)
	m := loaded(t, "", plain("1.a"), plain("2.b"), plain("3.c"), plain("4.dab"))
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jj")})
	if m.selected != "3.c" {
		t.Errorf("after jj: selected = %q, want 3.c", m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/ab")})
	if m.mode != modeFilter || m.filter != "ab" {
		t.Errorf("after /ab: mode = %v, filter = %q", m.mode, m.filter)
	}
	// A paste in the list is not a series of commands.
	m, _ = update(t, m, key("esc"))
	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qq"), Paste: true})
	if isQuit(cmd) || m.mode != modeList {
		t.Error("a paste was taken as commands")
	}
}

func TestSessionViewScrolls(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""), agent("2.b", state.StatusIdle, ""))
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %03d", i))
	}
	m, _ = update(t, m, peekMsg{id: "1.a", seq: m.peekSeq, lines: lines})
	if v := m.View(); !strings.Contains(v, "line 100") || strings.Contains(v, "line 050") {
		t.Fatalf("view does not end at the bottom:\n%s", v)
	}

	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	page := m.peekPage()
	if m.peekScroll != page {
		t.Fatalf("peekScroll = %d, want %d", m.peekScroll, page)
	}
	last := fmt.Sprintf("line %03d", 100-page)
	if v := m.View(); !strings.Contains(v, last) || strings.Contains(v, "line 100") ||
		!strings.Contains(v, "lines up, paused") {
		t.Errorf("scrolled view should end at %s:\n%s", last, v)
	}

	// While scrolled, new screens do not move the view.
	m, _ = update(t, m, peekMsg{id: "1.a", seq: m.peekSeq, lines: []string{"new"}})
	if strings.Contains(m.View(), "new") {
		t.Error("a new screen replaced the scrolled view")
	}

	// Scrolling stops at the top.
	for i := 0; i < 20; i++ {
		m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if v := m.View(); !strings.Contains(v, "line 001") || m.peekScroll != 100-m.peekBody() {
		t.Errorf("peekScroll = %d at the top:\n%s", m.peekScroll, v)
	}

	// esc returns to the live view and reads the screen again.
	m, cmd := update(t, m, key("esc"))
	if m.peekScroll != 0 || cmd == nil {
		t.Errorf("esc: peekScroll = %d, cmd = %v", m.peekScroll, cmd)
	}

	// Another session starts at the bottom.
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	m, _ = update(t, m, key("down"))
	if m.peekScroll != 0 {
		t.Errorf("peekScroll = %d after changing the session", m.peekScroll)
	}
}

func TestShortScreenDoesNotScroll(t *testing.T) {
	setup(t)
	m := loaded(t, "", agent("1.a", state.StatusIdle, ""))
	m, _ = update(t, m, peekMsg{id: "1.a", seq: m.peekSeq, lines: []string{"only line"}})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.peekScroll != 0 {
		t.Errorf("peekScroll = %d for a screen that fits", m.peekScroll)
	}
}

func TestSessionViewShowsUsageLimits(t *testing.T) {
	setup(t)
	ts := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	orig := now
	t.Cleanup(func() { now = orig })
	now = func() time.Time { return ts }

	a := agent("1.a", state.StatusIdle, "")
	a.Agent.Limits = []state.Limit{
		{Window: "5h", UsedPercent: 42.7, ResetsAt: ts.Add(time.Hour)},
		{Window: "7d", UsedPercent: 91},
		{Window: "1d", UsedPercent: 99, ResetsAt: ts.Add(-time.Minute)},
	}
	m := loaded(t, "", a)
	title := ""
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(l), "window 1") {
			title = l
		}
	}
	plainTitle := ansi.Strip(title)
	if !strings.Contains(plainTitle, "· 5h 42% · 7d 91% ") || strings.Contains(plainTitle, "1d") {
		t.Errorf("title = %q", plainTitle)
	}
	if got := limitsText(a.Agent.Limits[1:2]); got != styleDim.Render("· ")+styleLimitCritical.Render("7d 91%")+styleDim.Render(" ") {
		t.Errorf("critical limit = %q", got)
	}
	if got := limitsText([]state.Limit{{Window: "5h", UsedPercent: 85}}); !strings.Contains(got, styleLimitWarning.Render("5h 85%")) {
		t.Errorf("warning limit = %q", got)
	}
}
