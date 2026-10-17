package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/razkevich/ideagit/internal/kitty"
)

// keyName is the canonical, IntelliJ-style name of a key: chords from the kitty
// translator ("ctrl+shift+k"), and legacy aliases normalized ("alt+ctrl+z" →
// "ctrl+alt+z", "f18" → "shift+f6").
func keyName(k tea.KeyMsg) string {
	if name, ok := chordOf(k); ok {
		return name
	}
	s := k.String()
	if strings.HasPrefix(s, "alt+ctrl+") {
		s = "ctrl+alt+" + strings.TrimPrefix(s, "alt+ctrl+")
	}
	// xterm-style terminals report Shift+F1…F12 as F13…F24.
	if strings.HasPrefix(s, "f") {
		if n, err := strconv.Atoi(s[1:]); err == nil && n >= 13 && n <= 24 {
			return "shift+f" + strconv.Itoa(n-12)
		}
	}
	return s
}

func chordOf(k tea.KeyMsg) (string, bool) {
	if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
		return "", false
	}
	return kitty.ChordName(k.Runes[0])
}

// isChord reports keys that must never be inserted as text.
func isChord(k tea.KeyMsg) bool {
	_, ok := chordOf(k)
	return ok
}

// splitChords splits a multi-rune message into text runs and single chord runes.
func splitChords(k tea.KeyMsg) []tea.KeyMsg {
	var out []tea.KeyMsg
	var run []rune
	flush := func() {
		if len(run) > 0 {
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: run, Alt: k.Alt})
			run = nil
		}
	}
	for _, r := range k.Runes {
		if _, ok := kitty.ChordName(r); ok {
			flush()
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			continue
		}
		run = append(run, r)
	}
	flush()
	return out
}
