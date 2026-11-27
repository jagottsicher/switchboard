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
	"unicode"
	"unicode/utf8"

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
			question = joinWrapped(lines[:i], lines[i])
			break
		}
	}
	return question, options
}

// joinWrapped returns line together with the lines it continues. A narrow window wraps a long
// question, so its beginning ends up on the lines above. A line belongs to it if the text below
// it does not start with a capital letter, as a new sentence would, and if it has the same
// indentation and is plain text that neither ends a sentence nor is an item of a numbered list.
// At most three lines above are taken.
func joinWrapped(above []string, line string) string {
	text := strings.TrimSpace(line)
	indent := indentation(line)
	for i := len(above) - 1; i >= 0 && i >= len(above)-3; i-- {
		l := above[i]
		t := strings.TrimSpace(l)
		if t == "" || startsUpper(text) || indentation(l) != indent || !plainText(t) ||
			strings.ContainsAny(t[len(t)-1:], ".:?!") || optionLine.MatchString(l) {
			break
		}
		text = t + " " + text
	}
	return text
}

func startsUpper(t string) bool {
	r, _ := utf8.DecodeRuneInString(t)
	return unicode.IsUpper(r)
}

func indentation(l string) int {
	return len(l) - len(strings.TrimLeft(l, " "))
}

// plainText reports whether t is ordinary text: no control characters and not a rule drawn
// from one repeated character.
func plainText(t string) bool {
	runes := []rune(t)
	same := true
	for _, r := range runes {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			return false
		}
		if r != runes[0] {
			same = false
		}
	}
	return !same || len(runes) < 3
}
