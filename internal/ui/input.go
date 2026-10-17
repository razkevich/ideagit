package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type TextInput struct {
	val         []rune
	cur         int
	off         int
	Placeholder string
	Focused     bool
}

func NewInput(v string) *TextInput {
	t := &TextInput{val: []rune(v)}
	t.cur = len(t.val)
	return t
}

func (t *TextInput) Value() string { return string(t.val) }

func (t *TextInput) SetValue(v string) {
	t.val = []rune(v)
	t.cur = len(t.val)
}

func (t *TextInput) SelectAllOnType() {}

// Key handles editing keys and reports whether the key was consumed.
func (t *TextInput) Key(k tea.KeyMsg) bool {
	switch k.String() {
	case "left", "ctrl+b":
		t.cur = max(0, t.cur-1)
	case "right", "ctrl+f":
		t.cur = min(len(t.val), t.cur+1)
	case "home", "ctrl+a":
		t.cur = 0
	case "end", "ctrl+e":
		t.cur = len(t.val)
	case "alt+left", "alt+b":
		t.cur = wordLeft(t.val, t.cur)
	case "alt+right", "alt+f":
		t.cur = wordRight(t.val, t.cur)
	case "backspace", "ctrl+h":
		if t.cur > 0 {
			t.val = append(t.val[:t.cur-1], t.val[t.cur:]...)
			t.cur--
		}
	case "delete", "ctrl+d":
		if t.cur < len(t.val) {
			t.val = append(t.val[:t.cur], t.val[t.cur+1:]...)
		}
	case "ctrl+w", "alt+backspace":
		p := wordLeft(t.val, t.cur)
		t.val = append(t.val[:p], t.val[t.cur:]...)
		t.cur = p
	case "ctrl+u":
		t.val = t.val[t.cur:]
		t.cur = 0
	case "ctrl+k":
		t.val = t.val[:t.cur]
	default:
		if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
			rs := k.Runes
			if k.Type == tea.KeySpace {
				rs = []rune{' '}
			}
			rs = []rune(strings.ReplaceAll(string(rs), "\n", " "))
			t.val = append(t.val[:t.cur], append(append([]rune{}, rs...), t.val[t.cur:]...)...)
			t.cur += len(rs)
			return true
		}
		return false
	}
	return true
}

func wordLeft(v []rune, cur int) int {
	i := cur
	for i > 0 && !isWordRune(v[i-1]) {
		i--
	}
	for i > 0 && isWordRune(v[i-1]) {
		i--
	}
	return i
}

func wordRight(v []rune, cur int) int {
	i := cur
	for i < len(v) && !isWordRune(v[i]) {
		i++
	}
	for i < len(v) && isWordRune(v[i]) {
		i++
	}
	return i
}

// Draw renders the input as a bordered-less field of width w at row 0 of v.
func (t *TextInput) Draw(a *App, v View, st Style) {
	th := a.Th
	w := v.W()
	v.Fill(st)
	if len(t.val) == 0 && t.Placeholder != "" {
		v.TextFit(0, 0, t.Placeholder, w, st.WithFg(th.Faint))
	}
	if t.cur < t.off {
		t.off = t.cur
	}
	for runewidth.StringWidth(string(t.val[t.off:t.cur])) >= w {
		t.off++
	}
	v.Text(0, 0, string(t.val[t.off:]), st)
	if t.Focused {
		cx := runewidth.StringWidth(string(t.val[t.off:t.cur]))
		ch := ' '
		if t.cur < len(t.val) {
			ch = t.val[t.cur]
		} else if len(t.val) == 0 && t.Placeholder != "" {
			ch = []rune(t.Placeholder)[0]
		}
		v.Set(cx, 0, ch, Style{Fg: st.Bg, Bg: th.Fg})
	}
	abs := v.Abs()
	a.Hit(abs, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			x := m.X - abs.X
			pos := t.off
			acc := 0
			for pos < len(t.val) && acc+runewidth.RuneWidth(t.val[pos]) <= x {
				acc += runewidth.RuneWidth(t.val[pos])
				pos++
			}
			t.cur = pos
		}
		return nil
	})
}

// Field draws a labelled input box IntelliJ-style with an outline when focused.
func DrawField(a *App, v View, in *TextInput, focused bool, onClick func()) {
	th := a.Th
	in.Focused = focused
	border := th.InputBorder
	if focused {
		border = th.InputFocus
	}
	st := Style{Fg: th.Fg, Bg: th.InputBg}
	v.Fill(st)
	v.Set(0, 0, '▏', Style{Fg: border, Bg: th.InputBg})
	v.Set(v.W()-1, 0, '▕', Style{Fg: border, Bg: th.InputBg})
	in.Draw(a, v.Sub(1, 0, v.W()-2, 1), st)
	if onClick != nil {
		a.Hit(v.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				onClick()
				in.Draw(a, v.Sub(1, 0, v.W()-2, 1), st)
			}
			return nil
		})
		in.Draw(a, v.Sub(1, 0, v.W()-2, 1), st)
	}
}

type TextArea struct {
	lines       [][]rune
	row         int
	col         int
	top         int
	left        int
	Focused     bool
	Placeholder string
}

func NewTextArea(v string) *TextArea {
	t := &TextArea{}
	t.SetValue(v)
	return t
}

func (t *TextArea) SetValue(v string) {
	t.lines = nil
	for _, l := range strings.Split(v, "\n") {
		t.lines = append(t.lines, []rune(l))
	}
	t.row = len(t.lines) - 1
	t.col = len(t.lines[t.row])
	t.top = 0
}

func (t *TextArea) Value() string {
	parts := make([]string, len(t.lines))
	for i, l := range t.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (t *TextArea) MoveToStart() { t.row, t.col, t.top = 0, 0, 0 }

func (t *TextArea) insert(rs []rune) {
	for _, r := range rs {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			line := t.lines[t.row]
			rest := append([]rune{}, line[t.col:]...)
			t.lines[t.row] = line[:t.col]
			t.lines = append(t.lines[:t.row+1], append([][]rune{rest}, t.lines[t.row+1:]...)...)
			t.row++
			t.col = 0
			continue
		}
		line := t.lines[t.row]
		line = append(line[:t.col], append([]rune{r}, line[t.col:]...)...)
		t.lines[t.row] = line
		t.col++
	}
}

func (t *TextArea) Key(k tea.KeyMsg) bool {
	line := t.lines[t.row]
	switch k.String() {
	case "left":
		if t.col > 0 {
			t.col--
		} else if t.row > 0 {
			t.row--
			t.col = len(t.lines[t.row])
		}
	case "right":
		if t.col < len(line) {
			t.col++
		} else if t.row < len(t.lines)-1 {
			t.row++
			t.col = 0
		}
	case "up":
		if t.row > 0 {
			t.row--
			t.col = min(t.col, len(t.lines[t.row]))
		}
	case "down":
		if t.row < len(t.lines)-1 {
			t.row++
			t.col = min(t.col, len(t.lines[t.row]))
		}
	case "home", "ctrl+a":
		t.col = 0
	case "end", "ctrl+e":
		t.col = len(line)
	case "alt+left", "alt+b":
		t.col = wordLeft(line, t.col)
	case "alt+right", "alt+f":
		t.col = wordRight(line, t.col)
	case "enter":
		t.insert([]rune{'\n'})
	case "backspace", "ctrl+h":
		if t.col > 0 {
			t.lines[t.row] = append(line[:t.col-1], line[t.col:]...)
			t.col--
		} else if t.row > 0 {
			prev := t.lines[t.row-1]
			t.col = len(prev)
			t.lines[t.row-1] = append(prev, line...)
			t.lines = append(t.lines[:t.row], t.lines[t.row+1:]...)
			t.row--
		}
	case "delete", "ctrl+d":
		if t.col < len(line) {
			t.lines[t.row] = append(line[:t.col], line[t.col+1:]...)
		} else if t.row < len(t.lines)-1 {
			t.lines[t.row] = append(line, t.lines[t.row+1]...)
			t.lines = append(t.lines[:t.row+1], t.lines[t.row+2:]...)
		}
	case "ctrl+w", "alt+backspace":
		p := wordLeft(line, t.col)
		t.lines[t.row] = append(line[:p], line[t.col:]...)
		t.col = p
	case "ctrl+u":
		t.lines[t.row] = line[t.col:]
		t.col = 0
	default:
		if k.Type == tea.KeyRunes {
			t.insert(k.Runes)
			return true
		}
		if k.Type == tea.KeySpace {
			t.insert([]rune{' '})
			return true
		}
		return false
	}
	return true
}

func (t *TextArea) Draw(a *App, v View) {
	th := a.Th
	st := Style{Fg: th.Fg, Bg: th.InputBg}
	v.Fill(st)
	h, w := v.H(), v.W()
	if t.row < t.top {
		t.top = t.row
	}
	if t.row >= t.top+h {
		t.top = t.row - h + 1
	}
	curX := runewidth.StringWidth(string(t.lines[t.row][:t.col]))
	if curX < t.left {
		t.left = curX
	}
	if curX >= t.left+w {
		t.left = curX - w + 1
	}
	if len(t.lines) == 1 && len(t.lines[0]) == 0 && t.Placeholder != "" {
		v.TextFit(0, 0, t.Placeholder, w, st.WithFg(th.Faint))
	}
	for i := 0; i < h && t.top+i < len(t.lines); i++ {
		line := string(t.lines[t.top+i])
		x := -t.left
		for _, r := range line {
			rw := runewidth.RuneWidth(r)
			if x >= 0 && x+rw <= w {
				v.Set(x, i, r, st)
			}
			x += rw
		}
		if t.top+i == 0 && w > 72 {
			v.Tint(72-t.left, i, 0, st.Bg)
		}
	}
	if t.Focused {
		cy := t.row - t.top
		cx := curX - t.left
		ch := ' '
		if t.col < len(t.lines[t.row]) {
			ch = t.lines[t.row][t.col]
		} else if len(t.lines) == 1 && len(t.lines[0]) == 0 && t.Placeholder != "" {
			ch = []rune(t.Placeholder)[0]
		}
		v.Set(cx, cy, ch, Style{Fg: st.Bg, Bg: th.Fg})
	}
	abs := v.Abs()
	a.Hit(abs, func(m Mouse) tea.Cmd {
		if d := m.Wheel(); d != 0 {
			t.top = max(0, min(len(t.lines)-1, t.top+d))
			return nil
		}
		if m.LeftPress() {
			row := min(len(t.lines)-1, t.top+m.Y-abs.Y)
			t.row = row
			x := m.X - abs.X + t.left
			col, acc := 0, 0
			for col < len(t.lines[row]) && acc+runewidth.RuneWidth(t.lines[row][col]) <= x {
				acc += runewidth.RuneWidth(t.lines[row][col])
				col++
			}
			t.col = col
		}
		return nil
	})
}

// DrawButton draws an IntelliJ-style button and returns its width.
func DrawButton(a *App, v View, x, y int, label string, primary, focused bool, onClick func() tea.Cmd) int {
	th := a.Th
	w := Width(label) + 4
	st := Style{Fg: th.ButtonFg, Bg: th.ButtonBg}
	if primary {
		st = Style{Fg: Hex("#FFFFFF"), Bg: th.ButtonPrimary, Bold: true}
	}
	if focused {
		st.Underline = true
	}
	v.FillRect(x, y, w, 1, st)
	v.Text(x+2, y, label, st)
	if !primary {
		v.Set(x, y, '▏', Style{Fg: th.InputBorder, Bg: th.ButtonBg})
		v.Set(x+w-1, y, '▕', Style{Fg: th.InputBorder, Bg: th.ButtonBg})
		if focused {
			v.Set(x, y, '▏', Style{Fg: th.InputFocus, Bg: th.ButtonBg})
			v.Set(x+w-1, y, '▕', Style{Fg: th.InputFocus, Bg: th.ButtonBg})
		}
	}
	abs := v.Abs()
	a.Hit(Rect{abs.X + x, abs.Y + y, w, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			return onClick()
		}
		return nil
	})
	return w
}

// DrawCheckbox draws "[x] label" and toggles on click.
func DrawCheckbox(a *App, v View, x, y int, label string, val *bool, focused bool, disabled bool) int {
	th := a.Th
	box := "[ ]"
	if *val {
		box = "[x]"
	}
	fg := th.Fg
	if disabled {
		fg = th.Faint
	}
	bfg := th.Dim
	if *val {
		bfg = th.Accent
	}
	if disabled {
		bfg = th.Faint
	}
	bg := v.Canvas().At(min(v.Canvas().W-1, v.Abs().X+x), min(v.Canvas().H-1, v.Abs().Y+y)).S.Bg
	st := Style{Fg: fg, Bg: bg, Underline: focused}
	v.Text(x, y, box, Style{Fg: bfg, Bg: bg})
	end := v.Text(x+4, y, label, st)
	abs := v.Abs()
	if !disabled {
		a.Hit(Rect{abs.X + x, abs.Y + y, end - x, 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				*val = !*val
			}
			return nil
		})
	}
	return end - x
}
