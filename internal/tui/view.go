package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
	"github.com/jagottsicher/switchboard/internal/state"
)

var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleHeader   = lipgloss.NewStyle().Faint(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleSelected = lipgloss.NewStyle().Bold(true)
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleKey      = lipgloss.NewStyle().Bold(true)

	statusStyles = map[state.Status]lipgloss.Style{
		state.StatusWaiting: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208")),
		state.StatusWorking: lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		state.StatusIdle:    lipgloss.NewStyle().Foreground(lipgloss.Color("34")),
		state.StatusExited:  lipgloss.NewStyle().Faint(true),
		state.StatusUnknown: lipgloss.NewStyle().Faint(true),
	}
)

// Fixed widths of the list columns, without the two-space gaps.
const (
	colMarker = 2
	colStatus = 7
	colAge    = 4
	colScreen = 8
	gap       = 2
)

// layout returns the number of list rows and of lines for the view of the selected session.
// Title, column header, the title of the session view and the footer take one line each.
func (m Model) layout() (listRows, peekRows int) {
	avail := max(m.height-4, 2)
	n := max(len(m.visible), 1)
	limit := max(3, avail*2/5)
	listRows = min(n, limit, avail-1)
	return listRows, avail - listRows
}

func (m Model) listHeight() int {
	l, _ := m.layout()
	return l
}

// clampOffset scrolls the list so that the cursor is visible.
func (m *Model) clampOffset() {
	rows := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, min(m.offset, len(m.visible)-rows))
}

func (m Model) View() string {
	listRows, peekRows := m.layout()
	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteByte('\n')
	b.WriteString(m.listView(listRows))
	b.WriteString(m.peekView(peekRows))
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) titleLine() string {
	parts := []string{styleTitle.Render("switchboard")}
	waiting := 0
	for _, s := range m.all {
		if s.IsAgent() && s.Status == state.StatusWaiting {
			waiting++
		}
	}
	parts = append(parts, fmt.Sprintf("%d sessions", len(m.all)))
	if waiting > 0 {
		parts = append(parts, statusStyles[state.StatusWaiting].Render(fmt.Sprintf("%d waiting", waiting)))
	}
	if m.agentsOnly {
		parts = append(parts, "agents only")
	}
	if m.filter != "" && m.mode != modeFilter {
		parts = append(parts, "filter: "+m.filter)
	}
	return fit(strings.Join(parts, styleDim.Render(" · ")), m.width)
}

func (m Model) sessionWidth() int {
	w := len("SESSION")
	for _, s := range m.visible {
		w = max(w, ansi.StringWidth(m.sessionLabel(s)))
	}
	return min(w, 32)
}

func (m Model) sessionLabel(s sessions.Info) string {
	if m.self != "" && s.ID == m.self {
		return s.ID + " (here)"
	}
	return s.ID
}

func (m Model) listView(rows int) string {
	var b strings.Builder
	sw := m.sessionWidth()
	infoW := max(m.width-colMarker-sw-colStatus-colAge-colScreen-4*gap, 0)
	header := strings.Repeat(" ", colMarker) + pad("SESSION", sw) + spaces(gap) + pad("STATUS", colStatus) +
		spaces(gap) + pad("AGE", colAge) + spaces(gap) + pad("SCREEN", colScreen) + spaces(gap) + "MESSAGE / DIRECTORY"
	b.WriteString(styleHeader.Render(fit(header, m.width)))
	b.WriteByte('\n')

	lines := 0
	switch {
	case !m.loaded:
		b.WriteString(styleDim.Render("  Reading screen sessions …") + "\n")
		lines++
	case m.listErr != nil:
		b.WriteString(styleError.Render(fit("  "+m.listErr.Error(), m.width)) + "\n")
		lines++
	case len(m.visible) == 0:
		msg := "  No screen sessions found."
		if len(m.all) > 0 {
			msg = "  No session matches."
		}
		b.WriteString(styleDim.Render(msg) + "\n")
		lines++
	}
	for i := m.offset; i < len(m.visible) && lines < rows; i++ {
		b.WriteString(m.row(m.visible[i], i == m.cursor, sw, infoW))
		b.WriteByte('\n')
		lines++
	}
	for ; lines < rows; lines++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func (m Model) row(s sessions.Info, selected bool, sw, infoW int) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	status, since, info := "-", "-", ""
	statusStyle := styleDim
	if s.Status != "" {
		status = string(s.Status)
		if st, ok := statusStyles[s.Status]; ok && (s.IsAgent() || s.Status == state.StatusExited) {
			statusStyle = st
		}
	}
	infoStyle := lipgloss.NewStyle()
	info = s.Message
	if s.Agent != nil {
		since = sessions.Age(s.Agent.UpdatedAt)
		if info == "" && s.Agent.Cwd != "" {
			info, infoStyle = homeShort(s.Agent.Cwd), styleDim
		}
	}
	scr := string(s.State)
	if s.Multi {
		scr = "multi"
	}
	name := pad(m.sessionLabel(s), sw)
	if selected {
		name = styleSelected.Render(name)
	}
	return fit(marker+name+spaces(gap)+statusStyle.Render(pad(status, colStatus))+spaces(gap)+
		pad(since, colAge)+spaces(gap)+styleDim.Render(pad(scr, colScreen))+spaces(gap)+
		infoStyle.Render(fit(sessions.Shorten(info, max(infoW, 1)), infoW)), m.width)
}

func (m Model) peekView(rows int) string {
	if rows <= 0 {
		return ""
	}
	var b strings.Builder
	s, ok := m.current()
	title := "── "
	if ok {
		title += fmt.Sprintf("%s · window %d ", s.ID, s.Window())
	}
	b.WriteString(styleDim.Render(ansi.Truncate(title+strings.Repeat("─", max(m.width, 0)), max(m.width, 0), "")))
	b.WriteByte('\n')
	body := rows - 1
	var lines []string
	switch {
	case !ok:
	case m.self != "" && s.ID == m.self:
		lines = []string{styleDim.Render("This is the session the overview runs in.")}
	case m.peekID != s.ID:
		lines = []string{styleDim.Render("Reading the screen …")}
	case m.peekErr != nil:
		lines = []string{styleError.Render(fit(m.peekErr.Error(), m.width))}
	default:
		for _, l := range screen.Tail(m.peekLines, body) {
			lines = append(lines, fit(clean(l), m.width))
		}
	}
	for i := 0; i < body; i++ {
		if i < len(lines) {
			b.WriteString(lines[i])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (m Model) footer() string {
	switch m.mode {
	case modeFilter:
		m.input.Width = max(m.width-12, 1)
		return "Filter: " + m.input.View()
	case modeSend:
		prefix := "Send to " + m.sendTarget.ID + ": "
		hint := styleDim.Render("  enter send · esc cancel")
		m.input.Width = max(m.width-ansi.StringWidth(prefix)-ansi.StringWidth(hint)-1, 1)
		return fit(prefix+m.input.View()+hint, m.width)
	}
	if m.status != "" {
		if m.statusErr {
			return styleError.Render(fit(m.status, m.width))
		}
		return fit(m.status, m.width)
	}
	keys := [][2]string{
		{"enter", "attach"}, {"s", "send"}, {"/", "filter"}, {"a", "agents"}, {"r", "refresh"}, {"q", "quit"},
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, styleKey.Render(k[0])+" "+styleDim.Render(k[1]))
	}
	help := strings.Join(parts, "  ")
	if m.self != "" {
		help += styleDim.Render("  (back: Ctrl-a a d)")
	} else {
		help += styleDim.Render("  (back: Ctrl-a d)")
	}
	return fit(help, m.width)
}

// fit cuts s, which may contain styles, to the given display width.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// pad cuts or fills s to exactly width display columns.
func pad(s string, width int) string {
	s = fit(s, width)
	return s + spaces(width-ansi.StringWidth(s))
}

func spaces(n int) string {
	return strings.Repeat(" ", max(n, 0))
}

// clean replaces control characters, which would disturb the terminal, with spaces.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}

func homeShort(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if dir == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(dir, home+"/"); ok {
			return "~/" + rest
		}
	}
	return dir
}
