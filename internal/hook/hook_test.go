package hook

import (
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/switchboard/internal/state"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func openStore(t *testing.T) *state.Store {
	t.Helper()
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestApplyStatusMapping(t *testing.T) {
	tests := []struct {
		name    string
		start   state.Status
		payload Payload
		want    state.Status
		message string
	}{
		{"session start", state.StatusUnknown, Payload{Event: "SessionStart"}, state.StatusIdle, ""},
		{"prompt", state.StatusIdle, Payload{Event: "UserPromptSubmit"}, state.StatusWorking, ""},
		{"pre tool", state.StatusIdle, Payload{Event: "PreToolUse", ToolName: "Bash"}, state.StatusWorking, ""},
		{"post tool ends waiting", state.StatusWaiting, Payload{Event: "PostToolUse"}, state.StatusWorking, ""},
		{"tool failure", state.StatusWorking, Payload{Event: "PostToolUseFailure"}, state.StatusWorking, ""},
		{"permission", state.StatusWorking, Payload{Event: "PermissionRequest", ToolName: "Bash"}, state.StatusWaiting, "Permission requested: Bash"},
		{"notification permission", state.StatusWorking, Payload{Event: "Notification", NotificationType: "permission_prompt", Message: "needs approval"}, state.StatusWaiting, "needs approval"},
		{"notification elicitation", state.StatusWorking, Payload{Event: "Notification", NotificationType: "elicitation_dialog", Message: "question"}, state.StatusWaiting, "question"},
		{"notification idle", state.StatusWorking, Payload{Event: "Notification", NotificationType: "idle_prompt", Message: "waiting for input"}, state.StatusIdle, "waiting for input"},
		{"notification other", state.StatusWorking, Payload{Event: "Notification", NotificationType: "auth_success", Message: "ok"}, state.StatusWorking, "ok"},
		{"stop", state.StatusWorking, Payload{Event: "Stop"}, state.StatusIdle, ""},
		{"stop failure", state.StatusWorking, Payload{Event: "StopFailure", Error: []byte(`"rate_limit"`), ErrorDetails: "429 Too Many Requests"}, state.StatusIdle, "Error: rate_limit 429 Too Many Requests"},
		{"session end", state.StatusIdle, Payload{Event: "SessionEnd", Reason: "prompt_input_exit"}, state.StatusExited, "Session ended: prompt_input_exit"},
		{"unknown event keeps status", state.StatusWorking, Payload{Event: "SubagentStop"}, state.StatusWorking, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := state.Record{Status: tt.start}
			Apply(&rec, tt.payload)
			if rec.Status != tt.want {
				t.Errorf("status = %s, want %s", rec.Status, tt.want)
			}
			if rec.Message != tt.message {
				t.Errorf("message = %q, want %q", rec.Message, tt.message)
			}
			if !rec.Agent || rec.Source != "hook" {
				t.Errorf("record not marked as agent from hook: %+v", rec)
			}
		})
	}
}

func TestLeavingWaitingClearsItsMessage(t *testing.T) {
	rec := state.Record{Status: state.StatusWaiting, Message: "Permission requested: Bash"}
	Apply(&rec, Payload{Event: "PostToolUse"})
	if rec.Status != state.StatusWorking || rec.Message != "" {
		t.Errorf("after PostToolUse: %+v", rec)
	}
	rec = state.Record{Status: state.StatusWorking, Message: "from notify"}
	Apply(&rec, Payload{Event: "PreToolUse"})
	if rec.Message != "from notify" {
		t.Errorf("message cleared without a waiting state: %+v", rec)
	}
}

func TestUserPromptClearsOldMessage(t *testing.T) {
	rec := state.Record{Status: state.StatusWaiting, Message: "Permission requested: Bash"}
	Apply(&rec, Payload{Event: "UserPromptSubmit"})
	if rec.Message != "" {
		t.Errorf("message = %q, want empty", rec.Message)
	}
}

func TestHandleStoresBySTYAndWindow(t *testing.T) {
	s := openStore(t)
	payload := `{"hook_event_name":"PermissionRequest","session_id":"abc","cwd":"/src/repo","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/x"}}`
	if err := Handle(s, strings.NewReader(payload), env(map[string]string{"STY": "42.agent", "WINDOW": "1"})); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.Get("42.agent")
	if err != nil || !ok {
		t.Fatalf("Get() = %v, %v", ok, err)
	}
	if rec.Status != state.StatusWaiting || rec.Window != "1" || rec.Cwd != "/src/repo" || rec.AgentSession != "abc" {
		t.Errorf("record = %+v", rec)
	}
}

func TestHandleOutsideScreenDoesNothing(t *testing.T) {
	s := openStore(t)
	if err := Handle(s, strings.NewReader(`{"hook_event_name":"Stop"}`), env(nil)); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.All(); len(all) != 0 {
		t.Errorf("records outside screen: %v", all)
	}
}

func TestHandleReadsLargeInputCompletely(t *testing.T) {
	s := openStore(t)
	big := `{"hook_event_name":"PostToolUse","tool_response":"` + strings.Repeat("x", 4<<20) + `"}`
	r := strings.NewReader(big)
	if err := Handle(s, r, env(map[string]string{"STY": "1.x"})); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 0 {
		t.Errorf("%d bytes left unread", r.Len())
	}
}

func TestHandleRejectsInvalidJSONButReadsAll(t *testing.T) {
	s := openStore(t)
	r := strings.NewReader("not json")
	if err := Handle(s, r, env(map[string]string{"STY": "1.x"})); err == nil {
		t.Error("invalid JSON reports no error")
	}
	if r.Len() != 0 {
		t.Error("input not read completely")
	}
}

func TestRunNeverPanicsOrFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	Run(strings.NewReader("garbage"), env(map[string]string{"STY": "1.x"}))
	Run(strings.NewReader(`{"hook_event_name":"Stop"}`), env(map[string]string{"STY": "1.x"}))
}

func TestNotify(t *testing.T) {
	s := openStore(t)
	rec, err := Notify(s, "7.job", "0", state.StatusIdle, "build finished")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != state.StatusIdle || rec.Message != "build finished" || !rec.Agent || rec.Source != "notify" {
		t.Errorf("Notify() = %+v", rec)
	}
	if time.Since(rec.MessageAt) > time.Minute {
		t.Errorf("MessageAt = %v", rec.MessageAt)
	}
	// Without status, the status stays.
	rec, _ = Notify(s, "7.job", "", "", "still idle")
	if rec.Status != state.StatusIdle || rec.Message != "still idle" {
		t.Errorf("Notify() without status = %+v", rec)
	}
}
