package screen

import (
	"reflect"
	"strings"
	"testing"
)

func TestTrimLinesAndTail(t *testing.T) {
	got := trimLines("one  \ntwo\t\n\nthree \r\n   \n\n")
	want := []string{"one", "two", "", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trimLines() = %q, want %q", got, want)
	}
	if got := Tail(want, 2); !reflect.DeepEqual(got, []string{"", "three"}) {
		t.Errorf("Tail(2) = %q", got)
	}
	if got := Tail(want, 0); len(got) != 4 {
		t.Errorf("Tail(0) = %q, want all lines", got)
	}
	if got := Tail(want, 99); len(got) != 4 {
		t.Errorf("Tail(99) = %q, want all lines", got)
	}
}

func TestDecodeHardcopy(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"ascii", []byte("plain $ ^ \\ text\n"), "plain $ ^ \\ text\n"},
		{"utf8", []byte("schön ─ 日本\n"), "schön ─ 日本\n"},
		// Real capture from screen 4.09 for "Xü日本X": low bytes plus 0xff filler cells.
		{"screen-4.09", []byte{'X', 0xfc, 0xe5, 0xff, 0x2c, 0xff, 'X', '\n'}, "Xüå,X\n"},
		{"control-bytes", []byte{'a', 0x00, 0x02, 'b', 0xfc, 0x85, '\t', 'c'}, "a  bü \tc"},
	}
	for _, tt := range tests {
		if got := decodeHardcopy(tt.in); got != tt.want {
			t.Errorf("%s: decodeHardcopy() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestAttachUsesSharedMode(t *testing.T) {
	if got := AttachArgs("1.x"); !reflect.DeepEqual(got, []string{"screen", "-x", "1.x"}) {
		t.Errorf("AttachArgs() = %q", got)
	}
}

func TestAttachEnvDropsSTY(t *testing.T) {
	t.Setenv("STY", "999.outer")
	for _, e := range AttachEnv() {
		if strings.HasPrefix(e, "STY=") {
			t.Errorf("AttachEnv() contains %q", e)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"agent", "repo-1", "a_b"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a.b", "a b", "123", "a/b"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) should fail", bad)
		}
	}
}

func TestNewRejectsDuplicateName(t *testing.T) {
	stubScreen(t, func(_ string, args ...string) ([]byte, []byte, error) {
		if args[0] == "-ls" {
			return []byte("There is a screen on:\r\n\t5.agent\t(Detached)\n1 Socket in /x.\r\n"), nil, nil
		}
		t.Errorf("unexpected call %q", args)
		return nil, nil, nil
	})
	if _, err := New("agent", "", []string{"sh"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("New() error = %v, want a note about the existing session", err)
	}
}

func TestNewStartsDetachedInDir(t *testing.T) {
	started := false
	var gotDir string
	var gotArgs []string
	stubScreen(t, func(dir string, args ...string) ([]byte, []byte, error) {
		if args[0] == "-ls" {
			if started {
				return []byte("There is a screen on:\r\n\t77.agent\t(Detached)\n1 Socket in /x.\r\n"), nil, nil
			}
			return []byte("No Sockets found in /x.\r\n"), nil, nil
		}
		started, gotDir, gotArgs = true, dir, args
		return nil, nil, nil
	})
	s, err := New("agent", "/repo", []string{"agent-cli", "--resume"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "77.agent" || gotDir != "/repo" {
		t.Errorf("New() = %+v, dir %q", s, gotDir)
	}
	if want := []string{"-dmS", "agent", "agent-cli", "--resume"}; !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %q, want %q", gotArgs, want)
	}
}
