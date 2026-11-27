// Package detect recognizes the state of an agent from what its screen shows. It complements
// the hooks: some changes, such as an interrupt with Esc or a denied permission, send no hook
// event, and agents without hooks report nothing at all.
//
// Detection only uses plain ASCII phrases and the layout of the bottom lines, because screen
// 4.09 keeps only the low byte of each character in a hardcopy (see screen.ParseHardcopy).
package detect

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jagottsicher/switchboard/internal/state"
)

// Result is what the screen of a session shows.
type Result struct {
	Agent  string       `json:"agent"`
	Status state.Status `json:"status"`
	// Question and Options describe the dialog of a waiting agent, if it could be read.
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// Detect looks at the visible lines of a screen and reports the state of a known agent. It
// returns false if the screen does not clearly belong to an agent it knows.
func Detect(lines []string) (Result, bool) {
	return claudeCode(lines)
}

// bottom returns the last n non-empty lines, oldest first.
func bottom(lines []string, n int) []string {
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// optionLine matches a numbered choice such as "❯ 1. Yes" or "  2. No". The marker of the
// selected choice is "❯", which a hardcopy of screen 4.09 turns into "o".
var optionLine = regexp.MustCompile(`^\s*(?:[o❯>]\s+)?(\d+)\.\s+(\S.*)$`)

// dialog reads the question and the choices of a dialog at the end of lines. It walks up from
// the bottom, collecting choices from the highest number down to 1, so that a numbered list
// further up in the transcript is not taken for a choice.
func dialog(lines []string) (question string, options []string) {
	first, want, gap := -1, 0, 0
	for i := len(lines) - 1; i >= 0 && gap <= 3; i-- {
		m := optionLine.FindStringSubmatch(lines[i])
		if m == nil {
			gap++
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if want != 0 && n != want {
			break
		}
		options = append([]string{strings.TrimSpace(m[2])}, options...)
		gap, want = 0, n-1
		if n == 1 {
			first = i
			break
		}
	}
	if first < 0 {
		options = nil
		first = len(lines)
	}
	for i := first - 1; i >= 0 && i >= first-4; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasSuffix(l, "?") {
			question = l
			break
		}
	}
	return question, options
}
