package screen

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestEscape(t *testing.T) {
	tests := map[string]string{
		`plain text`:  `plain text`,
		`a\b`:         `a\\b`,
		`x^y`:         `x\^y`,
		`$HOME`:       `\$HOME`,
		"line\ntwo\r": "line\ntwo\r",
		"ümläut 🙂":    "ümläut 🙂",
		`"'; #{}%`:    `"'; #{}%`,
	}
	for in, want := range tests {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChunksRespectLimitAndBoundaries(t *testing.T) {
	text := strings.Repeat(`ab\c^ü🙂$`, 200)
	parts := chunks(text, 16)
	if got := strings.Join(parts, ""); got != escape(text) {
		t.Fatal("the parts do not add up to the escaped text")
	}
	for _, p := range parts {
		if len(p) > 16 {
			t.Errorf("part %q is longer than 16 bytes", p)
		}
		if !utf8.ValidString(p) {
			t.Errorf("part %q splits a UTF-8 sequence", p)
		}
		if strings.HasSuffix(p, `\`) && !strings.HasSuffix(p, `\\`) {
			t.Errorf("part %q ends inside an escape sequence", p)
		}
	}
}

func TestChunksEmpty(t *testing.T) {
	if got := chunks("", maxChunk); got != nil {
		t.Errorf("chunks(\"\") = %v, want nil", got)
	}
}

func TestStuffSendsChunksThenEnter(t *testing.T) {
	var calls [][]string
	stubScreen(t, func(_ string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, args)
		return nil, nil, nil
	})
	var slept time.Duration
	origSleep := sleep
	t.Cleanup(func() { sleep = origSleep })
	sleep = func(d time.Duration) { slept += d }

	if err := Stuff("1.x", 2, "hi$", true); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-S", "1.x", "-p", "2", "-X", "stuff", `hi\$`},
		{"-S", "1.x", "-p", "2", "-X", "stuff", "\r"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
	if slept != enterDelay {
		t.Errorf("pause before Enter = %v, want %v", slept, enterDelay)
	}
}

func TestStuffEnterOnlyDoesNotWait(t *testing.T) {
	var calls int
	stubScreen(t, func(string, ...string) ([]byte, []byte, error) { calls++; return nil, nil, nil })
	origSleep := sleep
	t.Cleanup(func() { sleep = origSleep })
	sleep = func(time.Duration) { t.Error("unnecessary pause") }

	if err := Stuff("1.x", 0, "", true); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("%d calls, want 1", calls)
	}
}
