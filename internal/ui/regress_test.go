package ui

import (
	"testing"

	. "github.com/razkevich/ideagit/internal/canvas"
)

// Lowercasing can change byte lengths (invalid UTF-8, Ⱥ); positions must stay in runes.
func TestFuzzyScoreOddNames(t *testing.T) {
	for _, c := range [][2]string{{"pdf", "r\xe9sum\xe9.pdf"}, {"ab", "ȺȺab"}, {"ȺA", "xȺa"}} {
		if sc, hits := fuzzyScore(c[0], c[1]); sc == 0 || len(hits) == 0 {
			t.Errorf("%q in %q: no match", c[0], c[1])
		}
	}
}

func TestTreeSearchHighlightOddNames(t *testing.T) {
	a := &App{Th: DarkTheme()}
	tr := NewTree()
	tr.SetRoots([]*Node{{ID: "x", Label: "ȺȺab", Check: NoCheck}, {ID: "y", Label: "r\xe9sum\xe9.pdf", Check: NoCheck}})
	tr.searching = true
	for _, q := range []string{"ab", "pdf", "Ⱥ"} {
		tr.search = q
		tr.Draw(a, New(40, 5, Style{}).View())
	}
}
