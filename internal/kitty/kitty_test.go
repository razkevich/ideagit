package kitty

import (
	"testing"
	"time"
)

func TestTranslate(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"\x1b[97;;97u", "a"},
		{"\x1b[97:65;2;65u", "A"},
		{"\x1b[49:33;2;33u", "!"},
		{"\x1b[97;5u", "\x01"},
		{"\x1b[107;5u", "\x0b"},
		{"\x1b[120;3u", "\x1bx"},
		{"\x1b[13u", "\r"},
		{"\x1b[9u", "\t"},
		{"\x1b[9;2u", "\x1b[Z"},
		{"\x1b[27u", "\x1b"},
		{"\x1b[127u", "\x7f"},
		{"\x1b[32;;32u", " "},
		{"\x1b[97;1:3u", ""},
		{"\x1b[A", "\x1b[A"},
		{"\x1b[1;1:3A", ""},
		{"\x1b[1;2A", "\x1b[1;2A"},
		{"\x1b[1;5C", "\x1b[1;5C"},
		{"\x1b[5~", "\x1b[5~"},
		{"\x1b[3;1:3~", ""},
		{"\x1b[P", "\x1bOP"},
		{"\x1b[<0;10;5M", "\x1b[<0;10;5M"},
		{"\x1b[200~hi\x1b[201~", "\x1b[200~hi\x1b[201~"},
		{"\x1b[57441;2u\x1b[57441;1:3u", ""},
		{"plain", "plain"},
		{"\x1b[1081;;1081u", "й"},
	}
	for _, c := range cases {
		tr := &Translator{}
		if got := string(tr.Translate([]byte(c.in))); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestChords(t *testing.T) {
	cases := map[string]string{
		"\x1b[107;6u":   "ctrl+shift+k",
		"\x1b[96;6u":    "ctrl+shift+`",
		"\x1b[27;2u":    "shift+esc",
		"\x1b[13;5u":    "ctrl+enter",
		"\x1b[109;5u":   "ctrl+m",
		"\x1b[1;5S":     "ctrl+f4",
		"\x1b[1;3P":     "alt+f1",
		"\x1b[17;2~":    "shift+f6",
		"\x1b[21;2~":    "shift+f10",
		"\x1b[2;3~":     "alt+insert",
		"\x1b[57413;5u": "ctrl+kp+",
		"\x1b[49;5u":    "ctrl+1",
		"\x1b[61;5u":    "ctrl+=",
		"\x1b[45;5u":    "ctrl+-",
		"\x1b[127;5u":   "ctrl+backspace",
		"\x1b[111;6u":   "ctrl+shift+o",
		"\x1b[93;6u":    "ctrl+shift+]",
	}
	for in, want := range cases {
		tr := &Translator{}
		out := []rune(string(tr.Translate([]byte(in))))
		if len(out) != 1 {
			t.Errorf("%q: got %q", in, string(out))
			continue
		}
		if got, ok := ChordName(out[0]); !ok || got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestSplitAfterEsc(t *testing.T) {
	tr := &Translator{Kitty: true}
	if got := tr.Translate([]byte("\x1b")); len(got) != 0 {
		t.Fatalf("lone ESC emitted early: %q", got)
	}
	if got := string(tr.Translate([]byte("[97;5u"))); got != "\x01" {
		t.Fatalf("ctrl+a after split ESC: %q", got)
	}
}

func TestSplitSequence(t *testing.T) {
	tr := &Translator{}
	if got := tr.Translate([]byte("\x1b[97;")); len(got) != 0 {
		t.Fatalf("partial emitted %q", got)
	}
	if got := string(tr.Translate([]byte(";97u"))); got != "a" {
		t.Fatalf("completed %q", got)
	}
}

func TestDoubleShift(t *testing.T) {
	now := time.Unix(0, 0)
	fired := 0
	tr := &Translator{OnDoubleShift: func() { fired++ }, now: func() time.Time { return now }}
	tap := "\x1b[57441;2u\x1b[57441;1:3u"
	tr.Translate([]byte(tap))
	now = now.Add(200 * time.Millisecond)
	tr.Translate([]byte(tap))
	if fired != 1 {
		t.Fatalf("double shift fired %d times", fired)
	}
	// Shift used as a modifier (Shift+A) is not a tap.
	tr.Translate([]byte("\x1b[57441;2u\x1b[97:65;2;65u\x1b[57441;1:3u"))
	now = now.Add(100 * time.Millisecond)
	tr.Translate([]byte(tap))
	if fired != 1 {
		t.Fatalf("shift+key counted as tap: %d", fired)
	}
	// Too slow.
	now = now.Add(time.Second)
	tr.Translate([]byte(tap))
	if fired != 1 {
		t.Fatalf("slow taps fired: %d", fired)
	}
}

func TestMalformedInputDoesNotPanic(t *testing.T) {
	for _, in := range []string{"\x1b[55296u", "\x1b[-5u", "\x1b[97:1114112;2u", "\x1b[97;;55296u", "\x1b[;;u", "\x1b[::u", "\x1b[99999999999999999999u"} {
		(&Translator{}).Translate([]byte(in))
	}
}
