package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

func on(hostName string, s sessions.Info) sessions.Info {
	s.Host = hostName
	return s
}

func twoHosts(t *testing.T, self string) Model {
	t.Helper()
	m, _ := update(t, New(self, "box"), tea.WindowSizeMsg{Width: 100, Height: 20})
	m, _ = update(t, m, sessionsMsg{infos: []sessions.Info{agent("1.here", state.StatusIdle, "")}})
	m, _ = update(t, m, sessionsMsg{host: "box", infos: []sessions.Info{
		on("box", waiting("1.here", bashOptions...)),
	}})
	return m
}

func TestSessionsOfAllHostsAreMerged(t *testing.T) {
	setup(t)
	m := twoHosts(t, "1.here")
	if len(m.visible) != 2 || m.visible[0].Key() != "box:1.here" || m.visible[1].Key() != "1.here" {
		t.Fatalf("visible = %v, %v", m.visible[0].Key(), m.visible[1].Key())
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "box:1.here") || !strings.Contains(view, "1.here (here)") {
		t.Errorf("view:\n%s", view)
	}
	// The remote session has the same ID as the overview's own session, but it is not it.
	if m.isSelf(m.visible[0]) || !m.isSelf(m.visible[1]) {
		t.Error("self is not limited to this machine")
	}
}

func TestCallsGoToTheSessionsHost(t *testing.T) {
	setup(t)
	var calls []string
	hardcopy = func(hostName, id string, window int) ([]string, error) {
		calls = append(calls, "peek "+hostName+":"+id)
		return nil, nil
	}
	stuff = func(hostName, id string, window int, text string, enter bool) error {
		calls = append(calls, "send "+hostName+":"+id+" "+text)
		return nil
	}
	or, os_ := readDialog, sendChoice
	t.Cleanup(func() { readDialog, sendChoice = or, os_ })
	readDialog = func(hostName, id string, window int) (answer.Dialog, error) {
		calls = append(calls, "read "+hostName+":"+id)
		return answer.Dialog{Options: bashOptions, Fingerprint: "fp"}, nil
	}
	sendChoice = func(hostName, id string, window, n int, label, fp string) error {
		calls = append(calls, "choose "+hostName+":"+id+" "+label)
		return nil
	}
	m := twoHosts(t, "")
	m, _ = update(t, m, key("g"))
	if m.selected != "box:1.here" {
		t.Fatalf("selected = %q", m.selected)
	}
	m, cmd := update(t, m, peekStartMsg{seq: m.peekSeq})
	m, _ = update(t, m, cmd())
	m, _ = update(t, m, key("s"))
	m = typeText(t, m, "hi")
	m, cmd = update(t, m, key("enter"))
	cmd()
	m, cmd = update(t, m, key("4"))
	m, _ = update(t, m, cmd())
	_, cmd = update(t, m, key("y"))
	cmd()
	want := []string{"peek box:1.here", "send box:1.here hi", "read box:1.here", "choose box:1.here No"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %q", calls)
	}
}

func TestOfflineHost(t *testing.T) {
	setup(t)
	m := twoHosts(t, "")
	offline := errors.New("box: host is offline: connection refused")
	m, _ = update(t, m, sessionsMsg{host: "box", err: offline})
	if len(m.visible) != 1 || m.visible[0].Key() != "1.here" {
		t.Errorf("sessions of the offline host are still listed")
	}
	if !strings.Contains(ansi.Strip(m.titleLine()), "box offline") || !strings.Contains(m.status, "connection refused") {
		t.Errorf("title %q, status %q", ansi.Strip(m.titleLine()), m.status)
	}
	if m.listErr != nil {
		t.Error("an offline host counts as a local error")
	}
	m, _ = update(t, m, sessionsMsg{host: "box", infos: []sessions.Info{on("box", agent("2.x", state.StatusIdle, ""))}})
	if strings.Contains(ansi.Strip(m.titleLine()), "offline") || len(m.visible) != 2 {
		t.Error("host did not come back")
	}
}

func TestRefreshSkipsHostsStillReading(t *testing.T) {
	setup(t)
	var asked []string
	collect = func(hostName string) ([]sessions.Info, error, error) {
		asked = append(asked, hostName)
		return nil, nil, nil
	}
	m := New("", "box", "lab")
	m.refreshing["box"] = true
	collectMsgs(m.refreshAll())
	if strings.Join(asked, ",") != ",lab" && strings.Join(asked, ",") != "lab," {
		t.Errorf("asked %q, want this machine and lab", asked)
	}
}
