package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

func selectionApp(t *testing.T) (*App, *Tree, func()) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}, snapshot: true}
	tr := NewTree()
	tr.Focused = true
	var kids []*Node
	for _, n := range []string{"a.go", "b.go", "c.go", "d.go"} {
		kids = append(kids, &Node{ID: n, Label: n, Check: NoCheck})
	}
	tr.SetRoots(kids)
	draw := func() {
		c := New(40, 10, Style{})
		a.hits, a.regions, a.frame = a.hits[:0], a.regions[:0], c
		tr.Draw(a, c.View())
		a.drawTextSel(c.View())
	}
	draw()
	return a, tr, draw
}

func mouse(a *App, x, y int, act tea.MouseAction, shift bool) {
	a.handleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: act, Shift: shift})
}

func TestShiftClickInListStillMultiSelects(t *testing.T) {
	a, tr, draw := selectionApp(t)
	mouse(a, 5, 0, tea.MouseActionPress, false)
	mouse(a, 5, 0, tea.MouseActionRelease, false)
	draw()
	mouse(a, 5, 2, tea.MouseActionPress, true)
	mouse(a, 5, 2, tea.MouseActionRelease, true)
	if n := len(tr.SelectedNodes()); n != 3 {
		t.Fatalf("shift+click selected %d rows, want 3", n)
	}
	if a.textSel != nil {
		t.Fatal("a click left a text selection behind")
	}
}

func TestShiftDragInListSelectsText(t *testing.T) {
	a, tr, draw := selectionApp(t)
	mouse(a, 0, 1, tea.MouseActionPress, true)
	mouse(a, 30, 2, tea.MouseActionMotion, true)
	draw()
	if got := a.selectedText(); got == "" || len(tr.SelectedNodes()) > 1 {
		t.Fatalf("text=%q multi=%d", got, len(tr.SelectedNodes()))
	}
	mouse(a, 30, 2, tea.MouseActionRelease, true)
	if got := a.selectedText(); got != "b.go\nc.go" {
		t.Fatalf("copied %q", got)
	}
}

func TestPlainDragInTextRegion(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}, snapshot: true}
	c := New(30, 4, Style{})
	v := c.View()
	v.Text(0, 0, "first line here", Style{})
	v.Text(0, 1, "second line", Style{})
	a.frame = c
	a.Selectable(v.Abs(), true)
	clicked := 0
	a.Hit(v.Abs(), func(m Mouse) tea.Cmd { clicked++; return nil })
	mouse(a, 6, 0, tea.MouseActionPress, false)
	mouse(a, 5, 1, tea.MouseActionMotion, false)
	mouse(a, 5, 1, tea.MouseActionRelease, false)
	if clicked != 1 {
		t.Fatalf("press reached the pane %d times, want 1", clicked)
	}
	if got := a.selectedText(); got != "line here\nsecond" {
		t.Fatalf("selected %q", got)
	}
}
