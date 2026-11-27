// Package hook turns agent hook events and notify calls into stored session state.
//
// The hook command runs on every tool call of the agent, so it must be fast and must never get
// in the way: it never writes to stdout (for some events stdout ends up in the agent's context),
// it always exits 0 (exit 2 would block a tool call), it reads its whole input, and it does not
// call screen.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jagottsicher/switchboard/internal/state"
)

// Payload holds the fields of a hook event that switchboard uses. Everything else in the JSON
// input is ignored.
type Payload struct {
	Event            string          `json:"hook_event_name"`
	SessionID        string          `json:"session_id"`
	Cwd              string          `json:"cwd"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	ToolName         string          `json:"tool_name"`
	Error            json.RawMessage `json:"error"`
	ErrorDetails     string          `json:"error_details"`
	Reason           string          `json:"reason"`
	AgentID          string          `json:"agent_id"`
}

// errorText returns the error field as text, whatever its JSON type is.
func (p Payload) errorText() string {
	if len(p.Error) == 0 || string(p.Error) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(p.Error, &s) == nil {
		return s
	}
	return string(p.Error)
}

// Apply updates rec according to the hook event in p. Events that switchboard does not know
// leave the status unchanged, so that for example a finishing subagent does not mark the main
// agent as idle in the middle of its turn.
func Apply(rec *state.Record, p Payload) {
	now := state.Now()
	rec.Agent = true
	rec.Source = "hook"
	if p.Event != "" {
		rec.Event = p.Event
	}
	if p.SessionID != "" {
		rec.AgentSession = p.SessionID
	}
	if p.Cwd != "" {
		rec.Cwd = p.Cwd
	}
	setMessage := func(msg string) {
		rec.Message = msg
		rec.MessageAt = now
	}

	switch p.Event {
	case "SessionStart":
		rec.Status = state.StatusIdle
	case "UserPromptSubmit":
		rec.Status = state.StatusWorking
		rec.Message, rec.MessageAt = "", now
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		// PostToolUse also ends a waiting state after a permission was granted. The message
		// that belonged to the waiting state no longer applies then.
		if rec.Status == state.StatusWaiting {
			rec.Message, rec.MessageAt = "", now
		}
		rec.Status = state.StatusWorking
	case "PermissionRequest":
		rec.Status = state.StatusWaiting
		if p.ToolName != "" {
			setMessage("Permission requested: " + p.ToolName)
		} else {
			setMessage("Permission requested")
		}
	case "Notification":
		if p.Message != "" {
			setMessage(p.Message)
		}
		switch p.NotificationType {
		case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog":
			rec.Status = state.StatusWaiting
		case "idle_prompt":
			rec.Status = state.StatusIdle
		}
	case "Stop":
		rec.Status = state.StatusIdle
	case "StopFailure":
		rec.Status = state.StatusIdle
		msg := strings.TrimSpace("Error: " + p.errorText() + " " + p.ErrorDetails)
		setMessage(msg)
	case "SessionEnd":
		rec.Status = state.StatusExited
		if p.Reason != "" {
			setMessage("Session ended: " + p.Reason)
		}
	}
}

// event is the compact form of a hook event in the event log. It deliberately leaves out tool
// input and output, which may contain secrets.
type event struct {
	Time             string `json:"time"`
	Event            string `json:"event"`
	Window           string `json:"window,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	NotificationType string `json:"notification_type,omitempty"`
	Message          string `json:"message,omitempty"`
	Status           string `json:"status"`
}

// Handle reads one hook event from r and stores it for the screen session in $STY. Outside screen
// it does nothing. getenv is os.Getenv in production.
func Handle(store *state.Store, r io.Reader, getenv func(string) string) error {
	// Read the whole input in any case; a large payload that is not read can stall the agent.
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	sty := getenv("STY")
	if sty == "" {
		return nil
	}
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("invalid hook payload: %w", err)
	}
	window := getenv("WINDOW")
	rec, err := store.Update(sty, func(rec *state.Record) {
		if window != "" {
			rec.Window = window
		}
		Apply(rec, p)
	})
	if err != nil {
		return err
	}
	return store.AppendEvent(sty, event{
		Time:             rec.UpdatedAt.Format("2006-01-02T15:04:05.000Z07:00"),
		Event:            p.Event,
		Window:           window,
		ToolName:         p.ToolName,
		NotificationType: p.NotificationType,
		Message:          p.Message,
		Status:           string(rec.Status),
	})
}

// Run is the entry point of `switchboard hook`. It never fails: errors and panics are written to
// the log in the state directory, and the caller always exits 0.
func Run(r io.Reader, getenv func(string) string) {
	defer func() {
		if v := recover(); v != nil {
			state.LogError("", fmt.Sprintf("hook panic: %v", v))
		}
	}()
	store, err := state.Open("")
	if err != nil {
		io.Copy(io.Discard, r)
		state.LogError("", "hook: "+err.Error())
		return
	}
	if err := Handle(store, r, getenv); err != nil {
		state.LogError(store.Dir(), "hook: "+err.Error())
	}
}

// Notify stores a message, and optionally a status, for session from any agent or script.
func Notify(store *state.Store, session, window string, status state.Status, text string) (state.Record, error) {
	rec, err := store.Update(session, func(rec *state.Record) {
		rec.Agent = true
		rec.Source = "notify"
		rec.Event = "notify"
		if window != "" {
			rec.Window = window
		}
		if status != "" {
			rec.Status = status
		}
		if text != "" {
			rec.Message = text
			rec.MessageAt = state.Now()
		}
	})
	if err != nil {
		return rec, err
	}
	return rec, store.AppendEvent(session, event{
		Time:    rec.UpdatedAt.Format("2006-01-02T15:04:05.000Z07:00"),
		Event:   "notify",
		Window:  window,
		Message: text,
		Status:  string(rec.Status),
	})
}
