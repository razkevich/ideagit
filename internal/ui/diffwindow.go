package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type DiffWindow struct {
	title string
	diff  *DiffView
}

func NewDiffWindow(title string) *DiffWindow {
	d := NewDiffView()
	d.Focused = true
	return &DiffWindow{title: title, diff: d}
}

func (w *DiffWindow) Hint() string { return w.diff.Hint() + " · esc close" }

func (w *DiffWindow) Key(a *App, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q", "ctrl+w":
		a.Pop(w)
		return nil
	}
	cmd, _ := w.diff.Key(a, k)
	return cmd
}

func (w *DiffWindow) OnData(a *App) {}

func (w *DiffWindow) Draw(a *App, screen View) {
	th := a.Th
	v := screen.Sub(0, 0, screen.W(), screen.H()-1)
	v.Fill(Style{Bg: th.Bg})
	title := v.Sub(0, 0, v.W(), 1)
	title.Fill(Style{Bg: th.HeaderBg})
	if s := w.diff.Source(); s != nil {
		w.title = s.Path
	}
	title.Text(1, 0, "Diff", Style{Fg: th.Dim, Bg: th.HeaderBg})
	title.TextFit(6, 0, w.title, v.W()-12, Style{Fg: th.Fg, Bg: th.HeaderBg, Bold: true})
	cx := v.W() - 3
	title.Text(cx, 0, "×", Style{Fg: th.Fg, Bg: th.HeaderBg})
	a.Hit(Rect{title.Abs().X + cx - 1, title.Abs().Y, 3, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.Pop(w)
		}
		return nil
	})
	w.diff.Draw(a, v.Sub(0, 1, v.W(), v.H()-1))
}
