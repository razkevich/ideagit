// Package kitty turns on the kitty keyboard protocol to see bare modifier presses (for
// IntelliJ's double-Shift) and translates everything else back to the legacy encoding
// Bubble Tea v1 understands.
package kitty

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Flags: disambiguate (1) | report event types (2) | alternate keys (4) |
// report all keys as escape codes (8) | associated text (16).
const (
	Enable  = "\x1b[>31u"
	Disable = "\x1b[<u"
)

const doubleTapWindow = 450 * time.Millisecond

// ChordBase starts the private code points used for chords the legacy encoding can't
// express (Ctrl+Shift+K, Shift+Esc, Ctrl+Enter, modified F-keys). Each chord is sent
// as one rune; ChordName maps it back to a name like "ctrl+shift+k".
const ChordBase = 0x10FC00

var (
	chordMu    sync.Mutex
	chordNames []string
	chordIndex = map[string]int{}
)

func chordRune(name string) []byte {
	chordMu.Lock()
	defer chordMu.Unlock()
	i, ok := chordIndex[name]
	if !ok {
		i = len(chordNames)
		chordNames = append(chordNames, name)
		chordIndex[name] = i
	}
	return []byte(string(rune(ChordBase + i)))
}

func ChordName(r rune) (string, bool) {
	chordMu.Lock()
	defer chordMu.Unlock()
	i := int(r) - ChordBase
	if i < 0 || i >= len(chordNames) {
		return "", false
	}
	return chordNames[i], true
}

func modPrefix(mods int) string {
	p := ""
	if mods&modCtrl != 0 {
		p += "ctrl+"
	}
	if mods&modAlt != 0 {
		p += "alt+"
	}
	if mods&modShift != 0 {
		p += "shift+"
	}
	return p
}

var fkeyTilde = map[int]int{11: 1, 12: 2, 13: 3, 14: 4, 15: 5, 17: 6, 18: 7, 19: 8, 20: 9, 21: 10, 23: 11, 24: 12}

// Supported reports whether the terminal is known to implement the protocol.
func Supported() bool {
	if v := os.Getenv("IDEAGIT_KITTY_KEYS"); v != "" {
		return v != "0"
	}
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	t := os.Getenv("TERM")
	return strings.Contains(t, "ghostty") || strings.Contains(t, "kitty")
}

// Input wraps the terminal's stdin. It keeps the file descriptor so Bubble Tea still
// puts the terminal into raw mode and polls it, but rewrites what Read returns.
type Input struct {
	f *os.File
	t *Translator
}

// NewInput wraps f; kittyMode means the protocol is on, so the Esc key never arrives as
// a bare ESC byte and a trailing ESC is the start of a sequence split across reads.
func NewInput(f *os.File, kittyMode bool, onDoubleShift func()) *Input {
	return &Input{f: f, t: &Translator{OnDoubleShift: onDoubleShift, Kitty: kittyMode}}
}

func (in *Input) Read(p []byte) (int, error) {
	// One read per call: the caller has already waited for the fd to be readable.
	buf := make([]byte, max(16, len(p)-len(in.t.pending)))
	n, err := in.f.Read(buf)
	out := in.t.Translate(buf[:n])
	return copy(p, out), err
}

func (in *Input) Write(p []byte) (int, error) { return in.f.Write(p) }
func (in *Input) Close() error                { return in.f.Close() }
func (in *Input) Fd() uintptr                 { return in.f.Fd() }
func (in *Input) Name() string                { return in.f.Name() }

type Translator struct {
	OnDoubleShift func()
	Kitty         bool
	pending       []byte
	shiftDown     bool
	lastTap       time.Time
	now           func() time.Time
}

const (
	modShift = 1 << iota
	modAlt
	modCtrl
	modSuper
)

func (t *Translator) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// Translate converts a chunk of kitty-encoded input. Incomplete trailing sequences are
// kept until the next chunk.
func (t *Translator) Translate(in []byte) []byte {
	data := append(t.pending, in...)
	t.pending = nil
	var out []byte
	for i := 0; i < len(data); {
		if data[i] == 0x1b && i+1 == len(data) && t.Kitty {
			t.pending = []byte{0x1b}
			break
		}
		if data[i] != 0x1b || i+1 >= len(data) || data[i+1] != '[' {
			// Legacy bytes (or a lone ESC from a terminal without the protocol) pass through.
			out = append(out, data[i])
			t.otherKey()
			i++
			continue
		}
		end := i + 2
		for end < len(data) && (data[end] < 0x40 || data[end] > 0x7e) {
			end++
		}
		if end >= len(data) {
			t.pending = append([]byte(nil), data[i:]...)
			break
		}
		seq := data[i : end+1]
		out = append(out, t.csi(seq)...)
		i = end + 1
	}
	return out
}

func (t *Translator) otherKey() {
	t.shiftDown = false
	t.lastTap = time.Time{}
}

func isShiftKey(code int) bool { return code == 57441 || code == 57447 }

func isModifierKey(code int) bool {
	return code >= 57441 && code <= 57452 || code >= 57358 && code <= 57363
}

// csi translates one CSI sequence (ESC [ params final).
func (t *Translator) csi(seq []byte) []byte {
	final := seq[len(seq)-1]
	params := string(seq[2 : len(seq)-1])
	if strings.HasPrefix(params, "<") || strings.HasPrefix(params, "?") || strings.HasPrefix(params, ">") || final == 'I' || final == 'O' && params == "" {
		// Mouse (SGR), focus, and protocol replies are untouched.
		return seq
	}
	fields := strings.Split(params, ";")
	field := func(i int) []int {
		if i >= len(fields) || fields[i] == "" {
			return nil
		}
		var res []int
		for _, s := range strings.Split(fields[i], ":") {
			n, err := strconv.Atoi(s)
			if err != nil {
				n = 0
			}
			res = append(res, n)
		}
		return res
	}
	mods, event := 0, 1
	if m := field(1); len(m) > 0 {
		mods = max(0, m[0]-1)
		if len(m) > 1 {
			event = m[1]
		}
	}
	switch final {
	case 'u':
		key := field(0)
		if len(key) == 0 {
			return seq
		}
		code := key[0]
		if isModifierKey(code) {
			t.modifierEvent(code, event)
			return nil
		}
		if event == 3 {
			return nil
		}
		t.otherKey()
		shifted := 0
		if len(key) > 1 {
			shifted = key[1]
		}
		var text []rune
		for _, c := range field(2) {
			if c > 0 {
				text = append(text, rune(c))
			}
		}
		if name := chord(code, mods); name != "" {
			return chordRune(name)
		}
		return legacyKey(code, shifted, text, mods)
	case '~':
		if event == 3 {
			return nil
		}
		t.otherKey()
		num := "0"
		if k := field(0); len(k) > 0 {
			num = strconv.Itoa(k[0])
		}
		if num == "200" || num == "201" {
			return seq
		}
		if n, _ := strconv.Atoi(num); mods&^modSuper != 0 {
			if f, ok := fkeyTilde[n]; ok {
				return chordRune(modPrefix(mods) + "f" + strconv.Itoa(f))
			}
			if n == 2 {
				return chordRune(modPrefix(mods) + "insert")
			}
		}
		return []byte(csiWithMods(num, mods, "~"))
	case 'A', 'B', 'C', 'D', 'H', 'F', 'P', 'Q', 'R', 'S', 'E':
		if event == 3 {
			return nil
		}
		t.otherKey()
		if mods == 0 {
			if final >= 'P' && final <= 'S' {
				return []byte("\x1bO" + string(final))
			}
			return []byte("\x1b[" + string(final))
		}
		if final >= 'P' && final <= 'S' && mods&^modSuper != 0 {
			return chordRune(modPrefix(mods) + "f" + strconv.Itoa(int(final-'P')+1))
		}
		return []byte(csiWithMods("1", mods, string(final)))
	}
	return seq
}

func (t *Translator) modifierEvent(code, event int) {
	if !isShiftKey(code) {
		if event != 3 {
			t.otherKey()
		}
		return
	}
	switch event {
	case 1:
		t.shiftDown = true
	case 3:
		if !t.shiftDown {
			return
		}
		t.shiftDown = false
		now := t.clock()
		if !t.lastTap.IsZero() && now.Sub(t.lastTap) <= doubleTapWindow {
			t.lastTap = time.Time{}
			if t.OnDoubleShift != nil {
				t.OnDoubleShift()
			}
			return
		}
		t.lastTap = now
	}
}

// chord names key combinations that IntelliJ uses but the legacy encoding loses.
func chord(code, mods int) string {
	mods &^= modSuper
	ctrl, shift := mods&modCtrl != 0, mods&modShift != 0
	switch {
	case code == 27 && shift:
		return "shift+esc"
	case code == 13 && (ctrl || shift):
		return modPrefix(mods) + "enter"
	case code == 9 && ctrl:
		return modPrefix(mods) + "tab"
	case (code == 57413 || code == 57412) && ctrl:
		return map[int]string{57413: "ctrl+kp+", 57412: "ctrl+kp-"}[code]
	case code == 'm' && ctrl && !shift:
		return "ctrl+m"
	case code == 127 && ctrl:
		return modPrefix(mods) + "backspace"
	// IntelliJ's ⌘1…⌘0, ⌘+, ⌘- mapped to Ctrl; legacy encoding has no Ctrl+digit.
	case ctrl && !shift && mods&modAlt == 0 && (code >= '0' && code <= '9' || code == '=' || code == '-' || code == ',' || code == '.'):
		return "ctrl+" + string(rune(code))
	case ctrl && shift && code >= 32 && code < 127:
		r := rune(code)
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		return modPrefix(mods) + string(r)
	}
	return ""
}

func csiWithMods(num string, mods int, final string) string {
	if mods == 0 {
		return "\x1b[" + num + final
	}
	return "\x1b[" + num + ";" + strconv.Itoa(mods+1) + final
}

func legacyKey(code, shifted int, text []rune, mods int) []byte {
	if !utf8.ValidRune(rune(code)) {
		return nil
	}
	if !utf8.ValidRune(rune(shifted)) {
		shifted = 0
	}
	for _, r := range text {
		if !utf8.ValidRune(r) {
			text = nil
			break
		}
	}
	alt := mods&modAlt != 0
	ctrl := mods&modCtrl != 0
	shift := mods&modShift != 0
	prefix := func(b []byte) []byte {
		if alt {
			return append([]byte{0x1b}, b...)
		}
		return b
	}
	switch code {
	case 13, 57414:
		return prefix([]byte{'\r'})
	case 9:
		if shift {
			return []byte("\x1b[Z")
		}
		return prefix([]byte{'\t'})
	case 27:
		return prefix([]byte{0x1b})
	case 127:
		if ctrl {
			return prefix([]byte{0x08})
		}
		return prefix([]byte{0x7f})
	case 32:
		if ctrl {
			return prefix([]byte{0})
		}
		return prefix([]byte{' '})
	}
	if code >= 57399 && code <= 57408 {
		return prefix([]byte{byte('0' + code - 57399)})
	}
	if code >= 57376 && code <= 57398 {
		// F13–F35 have no legacy encoding worth emitting.
		return nil
	}
	if code >= 57344 {
		return nil
	}
	if len(text) > 0 && !ctrl && !alt {
		return []byte(string(text))
	}
	r := rune(code)
	if shift && shifted > 0 {
		r = rune(shifted)
	} else if shift && r >= 'a' && r <= 'z' {
		r -= 'a' - 'A'
	}
	if ctrl {
		switch {
		case code >= 'a' && code <= 'z':
			return prefix([]byte{byte(code - 'a' + 1)})
		case code == '[':
			return prefix([]byte{0x1b})
		case code == '\\':
			return prefix([]byte{0x1c})
		case code == ']':
			return prefix([]byte{0x1d})
		case code == '6' || code == '^':
			return prefix([]byte{0x1e})
		case code == '-' || code == '_' || code == '/':
			return prefix([]byte{0x1f})
		case code == '2' || code == '@':
			return prefix([]byte{0})
		}
	}
	buf := make([]byte, utf8.RuneLen(r))
	utf8.EncodeRune(buf, r)
	return prefix(buf)
}
