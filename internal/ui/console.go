package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type consoleLine struct {
	text string
	fg   Color
	bold bool
}

type ConsoleTab struct {
	top    int
	follow bool
	height int
	seen   int
	lines  []consoleLine
}

func NewConsoleTab() *ConsoleTab { return &ConsoleTab{follow: true} }

func (c *ConsoleTab) Title() string  { return "Console" }
func (c *ConsoleTab) Closable() bool { return true }
func (c *ConsoleTab) Typing() bool   { return false }
func (c *ConsoleTab) Hint() string   { return "↑↓ scroll · G follow · x clear view" }
func (c *ConsoleTab) OnData(a *App)  {}

func (c *ConsoleTab) refresh(a *App) {
	th := a.Th
	entries := a.Repo.Console()
	if len(entries) == c.seen {
		return
	}
	for _, e := range entries[c.seen:] {
		c.lines = append(c.lines, consoleLine{text: e.Time.Format("15:04:05.000") + ": [" + a.Repo.Name + "] " + e.Cmd, fg: th.Fg, bold: true})
		for _, l := range strings.Split(e.Output, "\n") {
			if l == "" {
				continue
			}
			fg := th.Dim
			if e.Failed {
				fg = th.Error
			}
			c.lines = append(c.lines, consoleLine{text: l, fg: fg})
		}
	}
	c.seen = len(entries)
}

func (c *ConsoleTab) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	h := max(1, c.height)
	switch k.String() {
	case "up", "k":
		c.top = max(0, c.top-1)
		c.follow = false
	case "down", "j":
		c.top++
	case "pgup":
		c.top = max(0, c.top-h)
		c.follow = false
	case "pgdown":
		c.top += h
	case "home", "g":
		c.top = 0
		c.follow = false
	case "end", "G":
		c.follow = true
	case "x":
		c.lines = nil
		c.top = 0
	default:
		return nil, false
	}
	return nil, true
}

func (c *ConsoleTab) Draw(a *App, v View) {
	th := a.Th
	c.refresh(a)
	v.Fill(Style{Bg: th.Bg})
	h := v.H()
	c.height = h
	maxTop := max(0, len(c.lines)-h)
	if c.follow || c.top > maxTop {
		c.top = maxTop
		if c.top == maxTop {
			c.follow = true
		}
	}
	if len(c.lines) == 0 {
		v.Text(2, 1, "Git commands run by ideagit will appear here.", Style{Fg: th.Dim, Bg: th.Bg})
	}
	for i := 0; i < h && c.top+i < len(c.lines); i++ {
		l := c.lines[c.top+i]
		v.TextFit(1, i, l.text, v.W()-3, Style{Fg: l.fg, Bg: th.Bg, Bold: l.bold})
	}
	DrawScrollbar(a, v.Sub(v.W()-1, 0, 1, h), c.top, h, len(c.lines), func(t int) { c.top = t; c.follow = false })
	a.Hit(v.Sub(0, 0, v.W()-1, h).Abs(), func(m Mouse) tea.Cmd {
		if d := m.Wheel(); d != 0 {
			c.top = max(0, c.top+d*3)
			c.follow = d > 0 && c.top >= maxTop
		}
		return nil
	})
}
