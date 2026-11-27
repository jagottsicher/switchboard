package detect

import (
	"strings"

	"github.com/jagottsicher/switchboard/internal/state"
)

// Claude Code shows an input line that starts with "❯" and a no-break space, with a footer
// below it. While it works, the footer says "esc to interrupt". A dialog (permission, question,
// trust) replaces input line and footer and ends with a line that offers "Esc to cancel".
//
// A hardcopy of screen 4.09 turns "❯" into "o", so the input line starts with "o" and a no-break
// space. Lines of the transcript start with "❯" or "o" and a normal space, and shell prompts
// use normal spaces as well, so the no-break space is what identifies the input line.
//
// The agent's screen must also end with one of its own lines: the input line, an indented
// footer line, or the line offering "Esc to cancel". Agent output shown secondhand, by peek in
// a shell or in the preview of the overview, is followed by a prompt or a help line at the left
// edge and is not taken for an agent.

const claudeCodeName = "claude-code"

func isInputLine(l string) bool {
	l = strings.TrimLeft(l, " ")
	return strings.HasPrefix(l, "o\u00a0") || strings.HasPrefix(l, "❯\u00a0")
}

func claudeCode(lines []string) (Result, bool) {
	tail := bottom(lines, 20)
	if len(tail) == 0 {
		return Result{}, false
	}
	footer := tail[max(len(tail)-6, 0):]
	input := false
	for _, l := range footer {
		if isInputLine(l) {
			input = true
		}
	}
	last := tail[len(tail)-1]
	if !isInputLine(last) && !strings.HasPrefix(last, " ") && !strings.Contains(last, "Esc to cancel") {
		return Result{}, false
	}

	if input && strings.Contains(strings.Join(footer, "\n"), "esc to interrupt") {
		return Result{Agent: claudeCodeName, Status: state.StatusWorking}, true
	}
	if strings.Contains(last, "Esc to cancel") {
		question, options := dialog(tail[:len(tail)-1])
		if (question != "" && len(options) >= 2) ||
			strings.Contains(last, "Enter to confirm") || strings.Contains(last, "Enter to select") {
			return Result{Agent: claudeCodeName, Status: state.StatusWaiting, Question: question, Options: options}, true
		}
	}
	if input {
		return Result{Agent: claudeCodeName, Status: state.StatusIdle}, true
	}
	return Result{}, false
}
