package screen

import (
	"strconv"
	"strings"
	"time"
)

// maxChunk is the maximum length of an escaped stuff argument in bytes. Measured with screen 4.09:
// 700 bytes arrive, from about 760 bytes on screen silently drops the argument.
const maxChunk = 512

// enterDelay is the pause before the final Enter. Terminal UIs that treat fast input as a paste
// would otherwise take an immediately following Enter as a line break in the text instead of a
// submit. Variable so that tests can change it.
var enterDelay = 200 * time.Millisecond

// sleep is swappable so that unit tests do not wait.
var sleep = time.Sleep

// Stuff types text into window window of session id. With enter, a carriage return (\r) follows
// after a short pause. Long text is sent in several parts.
func Stuff(id string, window int, text string, enter bool) error {
	win := strconv.Itoa(window)
	for _, chunk := range chunks(text, maxChunk) {
		if err := run("", "-S", id, "-p", win, "-X", "stuff", chunk); err != nil {
			return err
		}
	}
	if enter {
		if text != "" {
			sleep(enterDelay)
		}
		return run("", "-S", id, "-p", win, "-X", "stuff", "\r")
	}
	return nil
}

// escapeRune escapes the characters that screen interprets in stuff arguments: backslash
// (escape sequences), caret (control characters such as ^M) and dollar (variable expansion).
// All other characters, including control characters and newlines, arrive unchanged.
func escapeRune(r rune) string {
	switch r {
	case '\\', '^', '$':
		return `\` + string(r)
	}
	return string(r)
}

// escape escapes a whole text for stuff.
func escape(text string) string {
	var b strings.Builder
	for _, r := range text {
		b.WriteString(escapeRune(r))
	}
	return b.String()
}

// chunks escapes text and splits it into parts of at most limit bytes. It splits only between
// characters, never inside a UTF-8 sequence or an escape sequence.
func chunks(text string, limit int) []string {
	var out []string
	var b strings.Builder
	for _, r := range text {
		e := escapeRune(r)
		if b.Len()+len(e) > limit && b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
		b.WriteString(e)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}
