package screen

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseList(t *testing.T) {
	tests := []struct {
		file string
		want []Session
	}{
		{"ls-4.09-detached.txt", []Session{
			{ID: "36545.sb-fix-dead", PID: 36545, Name: "sb-fix-dead", State: StateDetached},
			{ID: "36542.sb-fix two", PID: 36542, Name: "sb-fix two", State: StateDetached},
			{ID: "36539.sb-fix-one", PID: 36539, Name: "sb-fix-one", State: StateDetached},
		}},
		{"ls-4.09-dead.txt", []Session{
			{ID: "36542.sb-fix two", PID: 36542, Name: "sb-fix two", State: StateDetached},
			{ID: "36539.sb-fix-one", PID: 36539, Name: "sb-fix-one", State: StateDetached},
			{ID: "36545.sb-fix-dead", PID: 36545, Name: "sb-fix-dead", State: StateDead},
		}},
		{"ls-4.09-attached.txt", []Session{
			{ID: "36593.sb-fix-att", PID: 36593, Name: "sb-fix-att", State: StateAttached},
		}},
		{"ls-4.09-none.txt", nil},
		{"ls-5.0.2-de.txt", []Session{
			{ID: "3343442.bt-test-2", PID: 3343442, Name: "bt-test-2", State: StateDetached},
			{ID: "3343440.bt-test-1", PID: 3343440, Name: "bt-test-1", State: StateDetached},
			{ID: "10065.breakthrough", PID: 10065, Name: "breakthrough", State: StateAttached},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, ok := parseList(fixture(t, tt.file))
			if !ok {
				t.Fatal("parseList reports unrecognized output")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseList() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestParseLineVariants(t *testing.T) {
	tests := []struct {
		line string
		want Session
	}{
		{"\t12345.mysession\t(Detached)", Session{ID: "12345.mysession", PID: 12345, Name: "mysession", State: StateDetached}},
		{"\t12.a.b\t(Multi, attached)", Session{ID: "12.a.b", PID: 12, Name: "a.b", State: StateAttached, Multi: true}},
		{"\t12.x\t(Multi, detached)", Session{ID: "12.x", PID: 12, Name: "x", State: StateDetached, Multi: true}},
		{"\t12.x\t(Remote or dead)", Session{ID: "12.x", PID: 12, Name: "x", State: StateDead}},
		{"\t12.x\t(Something new)", Session{ID: "12.x", PID: 12, Name: "x", State: StateUnknown}},
	}
	for _, tt := range tests {
		got, ok := parseLine(tt.line)
		if !ok || got != tt.want {
			t.Errorf("parseLine(%q) = %+v, %v; want %+v", tt.line, got, ok, tt.want)
		}
	}
	for _, line := range []string{"", "There are screens on:", "\tnopid\t(Detached)", "\t12.x", "\tabc.x\t(Detached)"} {
		if _, ok := parseLine(line); ok {
			t.Errorf("parseLine(%q) wrongly recognizes a session", line)
		}
	}
}

func TestParseListRejectsUnknownOutput(t *testing.T) {
	for _, text := range []string{"", "Directory '/run/screen' must have mode 777.\n", "There are screens on:\r\n1 Socket in /x.\r\n"} {
		if _, ok := parseList(text); ok {
			t.Errorf("parseList(%q) should be unrecognized", text)
		}
	}
}

func stubScreen(t *testing.T, fn func(dir string, args ...string) ([]byte, []byte, error)) {
	t.Helper()
	orig := execScreen
	t.Cleanup(func() { execScreen = orig })
	execScreen = fn
}

func TestListIgnoresExitCode(t *testing.T) {
	text := fixture(t, "ls-4.09-none.txt")
	stubScreen(t, func(string, ...string) ([]byte, []byte, error) {
		return []byte(text), nil, &exec.ExitError{}
	})
	got, err := List()
	if err != nil || got != nil {
		t.Errorf("List() = %v, %v; want nil, nil", got, err)
	}
}

func TestListReportsBrokenSocketDir(t *testing.T) {
	stubScreen(t, func(string, ...string) ([]byte, []byte, error) {
		return []byte("Directory '/run/screen' must have mode 777.\n"), nil, &exec.ExitError{}
	})
	_, err := List()
	if err == nil || !strings.Contains(err.Error(), "mode 777") {
		t.Errorf("List() error = %v, want screen's message", err)
	}
}

func TestListNotInstalled(t *testing.T) {
	stubScreen(t, func(string, ...string) ([]byte, []byte, error) { return nil, nil, ErrNotInstalled })
	if _, err := List(); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("List() error = %v, want ErrNotInstalled", err)
	}
}

func TestResolve(t *testing.T) {
	sessions := []Session{
		{ID: "100.alpha", PID: 100, Name: "alpha"},
		{ID: "200.beta", PID: 200, Name: "beta"},
		{ID: "300.beta", PID: 300, Name: "beta"},
		{ID: "400.200", PID: 400, Name: "200"},
	}
	tests := []struct {
		query, wantID, wantErr string
	}{
		{"100.alpha", "100.alpha", ""},
		{"alpha", "100.alpha", ""},
		{"100", "100.alpha", ""},
		{"300.beta", "300.beta", ""},
		{"200", "200.beta", ""}, // The PID takes precedence over a name made of digits.
		{"beta", "", "ambiguous"},
		{"gamma", "", "no screen session"},
	}
	for _, tt := range tests {
		got, err := Resolve(sessions, tt.query)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Resolve(%q) error = %v, want %q", tt.query, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got.ID != tt.wantID {
			t.Errorf("Resolve(%q) = %v, %v; want %s", tt.query, got.ID, err, tt.wantID)
		}
	}
}
