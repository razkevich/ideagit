package ui

import (
	"testing"

	. "github.com/razkevich/ideagit/internal/canvas"
)

// Regression: a zero- or negative-width field (narrow Log pane) used to panic.
func TestTextInputDrawTinyWidths(t *testing.T) {
	a := &App{Th: DarkTheme()}
	c := New(40, 3, Style{})
	in := NewInput("abcdefg")
	in.Focused = true
	for _, w := range []int{-5, 0, 1, 2, 3, 10} {
		in.Draw(a, c.View().Sub(0, 0, w, 1), Style{})
		in.SetValue("ab")
		in.Draw(a, c.View().Sub(0, 0, w, 1), Style{})
		in.SetValue("abcdefg")
	}
}
