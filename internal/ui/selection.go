package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

// textRegion is an area of read-only (or input) text the mouse can select. In plain
// regions a drag selects; elsewhere clicks and drags already mean something, so only
// shift+drag does.
type textRegion struct {
	r     Rect
	plain bool
}

// textSel is a stream selection in screen cells, kept inside the region it began in.
type textSel struct {
	r              Rect
	ax, ay, bx, by int
	dragging       bool
	// active once the pointer has moved: a plain click in a plain region stays a click.
	active bool
	// press is a shift+click held back from a list; without a drag it is replayed.
	press *Mouse
}

// Selectable marks r as selectable text for this frame.
func (a *App) Selectable(r Rect, plain bool) {
	if r.W > 0 && r.H > 0 {
		a.regions = append(a.regions, textRegion{r, plain})
	}
}

func (a *App) regionAt(x, y int) (textRegion, bool) {
	for i := len(a.regions) - 1; i >= 0; i-- {
		if a.regions[i].r.Contains(x, y) {
			return a.regions[i], true
		}
	}
	return textRegion{}, false
}

// selectMouse runs before the regular hit handlers; ok means the event was consumed.
func (a *App) selectMouse(m Mouse) (tea.Cmd, bool) {
	s := a.textSel
	if s != nil && s.dragging {
		if m.Action == tea.MouseActionMotion && m.Button != tea.MouseButtonLeft {
			// The release happened outside the window.
			m.Action = tea.MouseActionRelease
		}
		switch m.Action {
		case tea.MouseActionMotion:
			s.bx = max(s.r.X, min(m.X, s.r.X+s.r.W-1))
			s.by = max(s.r.Y, min(m.Y, s.r.Y+s.r.H-1))
			if s.bx != s.ax || s.by != s.ay {
				s.active = true
			}
			return nil, s.active
		case tea.MouseActionRelease:
			s.dragging = false
			if !s.active {
				a.textSel = nil
				if s.press != nil {
					return a.dispatch(*s.press), true
				}
				return nil, false
			}
			return a.copySelection(), true
		}
	}
	if m.Action == tea.MouseActionPress && (m.Button == tea.MouseButtonWheelUp || m.Button == tea.MouseButtonWheelDown) {
		a.textSel = nil
		return nil, false
	}
	if !m.LeftPress() {
		return nil, false
	}
	a.textSel = nil
	r, ok := a.regionAt(m.X, m.Y)
	if !ok || (!r.plain && !m.Shift) {
		return nil, false
	}
	a.textSel = &textSel{r: r.r, ax: m.X, ay: m.Y, bx: m.X, by: m.Y, dragging: true}
	if r.plain {
		// A plain press still focuses and clicks.
		return nil, false
	}
	a.textSel.press = &m
	return nil, true
}

// selectKey copies an active selection on the usual copy keys and drops it on any
// other key.
func (a *App) selectKey(k tea.KeyMsg) (tea.Cmd, bool) {
	s := a.textSel
	if s == nil || !s.active {
		return nil, false
	}
	switch keyName(k) {
	case "ctrl+c", "ctrl+shift+c", "ctrl+insert":
		return a.copySelection(), true
	case "esc":
		a.textSel = nil
		return nil, true
	}
	a.textSel = nil
	return nil, false
}

func (s *textSel) ordered() (x0, y0, x1, y1 int) {
	if s.ay < s.by || (s.ay == s.by && s.ax <= s.bx) {
		return s.ax, s.ay, s.bx, s.by
	}
	return s.bx, s.by, s.ax, s.ay
}

// span is the selected column range of screen row y.
func (s *textSel) span(y int) (int, int) {
	x0, y0, x1, y1 := s.ordered()
	from, to := s.r.X, s.r.X+s.r.W-1
	if y == y0 {
		from = x0
	}
	if y == y1 {
		to = x1
	}
	return from, to
}

func (a *App) drawTextSel(v View) {
	s := a.textSel
	if s == nil || !s.active {
		return
	}
	_, y0, _, y1 := s.ordered()
	for y := y0; y <= y1; y++ {
		from, to := s.span(y)
		v.Tint(from, y, to-from+1, a.Th.Sel)
	}
}

func (a *App) selectedText() string {
	s, c := a.textSel, a.frame
	if s == nil || c == nil {
		return ""
	}
	_, y0, _, y1 := s.ordered()
	var lines []string
	for y := y0; y <= y1 && y < c.H; y++ {
		from, to := s.span(y)
		var b strings.Builder
		for x := from; x <= to && x < c.W; x++ {
			if cell := c.At(x, y); !cell.Cont() {
				b.WriteRune(cell.R)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Trim(strings.Join(dedent(lines), "\n"), "\n")
}

// dedent drops the indentation all lines share (tree depth, panel padding) but keeps
// relative indentation, e.g. of code.
func dedent(lines []string) []string {
	common := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if common < 0 || n < common {
			common = n
		}
	}
	for i, l := range lines {
		lines[i] = l[min(max(common, 0), len(l)):]
	}
	return lines
}

func (a *App) copySelection() tea.Cmd {
	text := a.selectedText()
	if text == "" {
		return nil
	}
	if a.snapshot {
		a.Notify("Copied to clipboard", text)
		return nil
	}
	return copyToClipboard(a, text)
}
