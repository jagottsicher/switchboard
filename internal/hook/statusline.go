package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jagottsicher/switchboard/internal/state"
)

// The agent runs its status line command whenever its status line needs to be redrawn and passes
// the state of the session as JSON on stdin. Among it are the usage limits of the account, which
// switchboard stores for the screen session. Whatever the command prints becomes the status line,
// so switchboard either runs the status line command the user had before and passes its output
// through, or prints the usage limits itself.

// statusInput holds the fields of the status line input that switchboard uses.
type statusInput struct {
	RateLimits *struct {
		FiveHour *limitWindow `json:"five_hour"`
		SevenDay *limitWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type limitWindow struct {
	UsedPercentage *float64        `json:"used_percentage"`
	ResetsAt       json.RawMessage `json:"resets_at"`
}

// resetTime reads resets_at, which the agent documents as Unix time in seconds. An ISO 8601 text
// is accepted as well.
func (w limitWindow) resetTime() time.Time {
	raw := bytes.TrimSpace(w.ResetsAt)
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		t, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return time.Time{}
		}
		return t
	}
	secs, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(secs), 0)
}

// limits returns the usage limits in the input, or nil if it has none. The agent only reports
// them for subscription accounts, and only after its first response from the API.
func (in statusInput) limits() []state.Limit {
	if in.RateLimits == nil {
		return nil
	}
	var out []state.Limit
	for _, w := range []struct {
		name string
		win  *limitWindow
	}{{"5h", in.RateLimits.FiveHour}, {"7d", in.RateLimits.SevenDay}} {
		if w.win == nil || w.win.UsedPercentage == nil {
			continue
		}
		out = append(out, state.Limit{Window: w.name, UsedPercent: *w.win.UsedPercentage, ResetsAt: w.win.resetTime()})
	}
	return out
}

// HandleStatusLine stores the usage limits from one status line input for the screen session in
// $STY. It returns the limits it found. Inputs without limits leave the stored ones as they are,
// because the limits belong to the account and stay valid while a new agent session starts.
func HandleStatusLine(store *state.Store, data []byte, getenv func(string) string) ([]state.Limit, error) {
	var in statusInput
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("invalid status line input: %w", err)
	}
	limits := in.limits()
	sty := getenv("STY")
	if sty == "" || limits == nil {
		return limits, nil
	}
	window := getenv("WINDOW")
	err := store.Annotate(sty, func(rec *state.Record) bool {
		changed := !rec.Agent || !state.SameLimits(rec.Limits, limits) || (window != "" && rec.Window != window)
		rec.Agent = true
		rec.Limits = limits
		if window != "" {
			rec.Window = window
		}
		return changed
	})
	return limits, err
}

// ANSI colors for the status line that switchboard prints itself.
const (
	ansiWarning  = "\x1b[38;5;208m"
	ansiCritical = "\x1b[31m"
	ansiReset    = "\x1b[0m"
)

// FormatLimits returns the limits as a status line, such as "5h 42% · 7d 83%", with warnings in
// orange and critical values in red.
func FormatLimits(limits []state.Limit, now time.Time) string {
	var parts []string
	for _, l := range state.ActiveLimits(limits, now) {
		text := fmt.Sprintf("%s %d%%", l.Window, l.Percent())
		switch p := l.Percent(); {
		case p >= state.LimitCritical:
			text = ansiCritical + text + ansiReset
		case p >= state.LimitWarning:
			text = ansiWarning + text + ansiReset
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

// RunStatusLine is the entry point of `switchboard statusline`. It stores the usage limits and
// then runs then, the user's previous status line command, with the same input, or prints the
// limits if there is none. Storing never makes it fail: errors go to the log in the state
// directory. The result is the exit status for the agent.
func RunStatusLine(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, then string) int {
	data, err := io.ReadAll(stdin)
	if err != nil {
		state.LogError("", "statusline: "+err.Error())
	}
	limits := storeLimits(data, getenv)
	if then == "" {
		if line := FormatLimits(limits, state.Now()); line != "" {
			fmt.Fprintln(stdout, line)
		}
		return 0
	}
	cmd := exec.Command("sh", "-c", then)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(stderr, "switchboard statusline:", err)
		return 1
	}
	return 0
}

// storeLimits stores the limits in data and returns them. It never fails.
func storeLimits(data []byte, getenv func(string) string) (limits []state.Limit) {
	defer func() {
		if v := recover(); v != nil {
			state.LogError("", fmt.Sprintf("statusline panic: %v", v))
		}
	}()
	store, err := state.Open("")
	if err != nil {
		state.LogError("", "statusline: "+err.Error())
		return nil
	}
	limits, err = HandleStatusLine(store, data, getenv)
	if err != nil {
		state.LogError(store.Dir(), "statusline: "+err.Error())
	}
	return limits
}
