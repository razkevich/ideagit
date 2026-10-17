package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type Field interface {
	Height() int
	Focusable() bool
	Draw(a *App, v View, focused bool, d *Dialog)
	Key(a *App, k tea.KeyMsg) bool
}

type LabelField struct {
	Text string
	Fg   Color
}

func (f *LabelField) Height() int     { return len(wrapText(f.Text, 200)) }
func (f *LabelField) Focusable() bool { return false }
func (f *LabelField) Draw(a *App, v View, _ bool, _ *Dialog) {
	fg := f.Fg
	if fg == NoColor {
		fg = a.Th.Fg
	}
	for i, l := range wrapText(f.Text, v.W()) {
		v.Text(0, i, l, Style{Fg: fg, Bg: a.Th.HeaderBg})
	}
}
func (f *LabelField) Key(*App, tea.KeyMsg) bool { return false }

type InputField struct {
	Label  string
	In     *TextInput
	LabelW int
}

func (f *InputField) Height() int     { return 1 }
func (f *InputField) Focusable() bool { return true }
func (f *InputField) Draw(a *App, v View, focused bool, d *Dialog) {
	lw := f.LabelW
	if lw == 0 {
		lw = Width(f.Label) + 1
	}
	if f.Label != "" {
		v.Text(0, 0, f.Label, Style{Fg: a.Th.Fg, Bg: a.Th.HeaderBg})
	} else {
		lw = 0
	}
	DrawField(a, v.Sub(lw, 0, v.W()-lw, 1), f.In, focused, func() { d.focusField(f) })
}
func (f *InputField) Key(_ *App, k tea.KeyMsg) bool { return f.In.Key(k) }

type CheckField struct {
	Label    string
	Val      *bool
	Disabled bool
	OnChange func(a *App, v bool)
}

func (f *CheckField) Height() int     { return 1 }
func (f *CheckField) Focusable() bool { return !f.Disabled }
func (f *CheckField) Draw(a *App, v View, focused bool, d *Dialog) {
	before := *f.Val
	DrawCheckbox(a, v, 0, 0, f.Label, f.Val, focused, f.Disabled)
	_ = before
}
func (f *CheckField) Key(a *App, k tea.KeyMsg) bool {
	if k.String() == " " {
		*f.Val = !*f.Val
		if f.OnChange != nil {
			f.OnChange(a, *f.Val)
		}
		return true
	}
	return false
}

type RadioField struct {
	Label   string
	Options []string
	Sel     *int
}

func (f *RadioField) Height() int {
	if f.Label != "" {
		return len(f.Options) + 1
	}
	return len(f.Options)
}
func (f *RadioField) Focusable() bool { return true }
func (f *RadioField) Draw(a *App, v View, focused bool, d *Dialog) {
	th := a.Th
	y := 0
	if f.Label != "" {
		v.Text(0, 0, f.Label, Style{Fg: th.Fg, Bg: th.HeaderBg})
		y++
	}
	for i, o := range f.Options {
		mark := "( )"
		fg := th.Dim
		if i == *f.Sel {
			mark, fg = "(•)", th.Accent
		}
		v.Text(2, y+i, mark, Style{Fg: fg, Bg: th.HeaderBg})
		v.Text(6, y+i, o, Style{Fg: th.Fg, Bg: th.HeaderBg, Underline: focused && i == *f.Sel})
		idx := i
		a.Hit(Rect{v.Abs().X, v.Abs().Y + y + i, v.W(), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				*f.Sel = idx
				d.focusField(f)
			}
			return nil
		})
	}
}
func (f *RadioField) Key(_ *App, k tea.KeyMsg) bool {
	switch k.String() {
	case "up", "left", "k":
		*f.Sel = (*f.Sel + len(f.Options) - 1) % len(f.Options)
		return true
	case "down", "right", "j", " ":
		*f.Sel = (*f.Sel + 1) % len(f.Options)
		return true
	}
	return false
}

type ListField struct {
	Lines []string
	Fgs   []Color
	H     int
	top   int
}

func (f *ListField) Height() int     { return min(f.H, max(1, len(f.Lines))) }
func (f *ListField) Focusable() bool { return false }
func (f *ListField) Draw(a *App, v View, _ bool, _ *Dialog) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	for i := 0; i < v.H() && f.top+i < len(f.Lines); i++ {
		fg := th.Fg
		if f.top+i < len(f.Fgs) && f.Fgs[f.top+i] != NoColor {
			fg = f.Fgs[f.top+i]
		}
		v.TextFit(1, i, f.Lines[f.top+i], v.W()-2, Style{Fg: fg, Bg: th.Bg})
	}
	if len(f.Lines) > v.H() {
		DrawScrollbar(a, v.Sub(v.W()-1, 0, 1, v.H()), f.top, v.H(), len(f.Lines), func(t int) { f.top = t })
	}
	a.Hit(v.Abs(), func(m Mouse) tea.Cmd {
		if d := m.Wheel(); d != 0 {
			f.top = max(0, min(len(f.Lines)-v.H(), f.top+d))
		}
		return nil
	})
}
func (f *ListField) Key(*App, tea.KeyMsg) bool { return false }

type Button struct {
	Label   string
	Primary bool
	Do      func(a *App, d *Dialog) (tea.Cmd, bool)
}

type Dialog struct {
	Title    string
	Fields   []Field
	Buttons  []Button
	Width    int
	Err      string
	focus    int
	OnCancel func(a *App)
	// BeforeKey/AfterKey let callers react to edits (e.g. keep dependent fields in sync).
	BeforeKey func(a *App, d *Dialog, k tea.KeyMsg)
	AfterKey  func(a *App, d *Dialog)
}

func (d *Dialog) Dismissable() bool { return false }

func (d *Dialog) focusables() int {
	n := 0
	for _, f := range d.Fields {
		if f.Focusable() {
			n++
		}
	}
	return n + len(d.Buttons)
}

func (d *Dialog) focusedField() Field {
	i := 0
	for _, f := range d.Fields {
		if f.Focusable() {
			if i == d.focus {
				return f
			}
			i++
		}
	}
	return nil
}

func (d *Dialog) focusedButton() int {
	nf := d.focusables() - len(d.Buttons)
	if d.focus >= nf {
		return d.focus - nf
	}
	return -1
}

func (d *Dialog) focusField(target Field) {
	i := 0
	for _, f := range d.Fields {
		if f.Focusable() {
			if f == target {
				d.focus = i
				return
			}
			i++
		}
	}
}

func (d *Dialog) primary() int {
	for i, b := range d.Buttons {
		if b.Primary {
			return i
		}
	}
	return 0
}

func (d *Dialog) press(a *App, i int) tea.Cmd {
	if i < 0 || i >= len(d.Buttons) {
		return nil
	}
	b := d.Buttons[i]
	if b.Do == nil {
		a.Pop(d)
		if d.OnCancel != nil {
			d.OnCancel(a)
		}
		return nil
	}
	cmd, closeIt := b.Do(a, d)
	if closeIt {
		a.Pop(d)
	}
	return cmd
}

func (d *Dialog) Key(a *App, k tea.KeyMsg) tea.Cmd {
	if d.BeforeKey != nil {
		d.BeforeKey(a, d, k)
	}
	cmd := d.key(a, k)
	if d.AfterKey != nil {
		d.AfterKey(a, d)
	}
	return cmd
}

func (d *Dialog) key(a *App, k tea.KeyMsg) tea.Cmd {
	n := d.focusables()
	if wt, ok := d.focusedField().(interface{ WantsTab() bool }); ok && wt.WantsTab() && k.String() == "tab" {
		d.focusedField().Key(a, k)
		return nil
	}
	switch k.String() {
	case "esc":
		a.Pop(d)
		if d.OnCancel != nil {
			d.OnCancel(a)
		}
		return nil
	case "tab", "down":
		if k.String() == "down" {
			if _, ok := d.focusedField().(*RadioField); ok {
				break
			}
		}
		d.focus = (d.focus + 1) % n
		return nil
	case "shift+tab", "up":
		if k.String() == "up" {
			if _, ok := d.focusedField().(*RadioField); ok {
				break
			}
		}
		d.focus = (d.focus + n - 1) % n
		return nil
	case "enter":
		if b := d.focusedButton(); b >= 0 {
			return d.press(a, b)
		}
		return d.press(a, d.primary())
	case "left", "right":
		if b := d.focusedButton(); b >= 0 {
			nf := n - len(d.Buttons)
			if k.String() == "left" {
				d.focus = nf + (b+len(d.Buttons)-1)%len(d.Buttons)
			} else {
				d.focus = nf + (b+1)%len(d.Buttons)
			}
			return nil
		}
	case " ":
		if b := d.focusedButton(); b >= 0 {
			return d.press(a, b)
		}
	}
	if f := d.focusedField(); f != nil {
		f.Key(a, k)
	}
	return nil
}

func (d *Dialog) Hint() string { return "tab next field · ⏎ confirm · esc cancel" }

func (d *Dialog) Draw(a *App, screen View) {
	th := a.Th
	w := d.Width
	if w == 0 {
		w = 64
	}
	w = min(w, screen.W()-2)
	inner := w - 4
	h := 2
	for _, f := range d.Fields {
		if lf, ok := f.(*LabelField); ok {
			h += len(wrapText(lf.Text, inner))
		} else {
			h += f.Height()
		}
	}
	h += len(d.Fields) - 1
	if d.Err != "" {
		h += len(wrapText(d.Err, inner)) + 1
	}
	h += 3
	h = min(h, screen.H()-1)
	x := (screen.W() - w) / 2
	y := max(1, (screen.H()-h)/3)
	v := screen.Sub(x, y, w, h)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.Border, Bg: bg}, d.Title, Style{Fg: th.Fg, Bg: bg, Bold: true})
	row := 2
	fi := 0
	for _, f := range d.Fields {
		fh := f.Height()
		if lf, ok := f.(*LabelField); ok {
			fh = len(wrapText(lf.Text, inner))
		}
		if row+fh > h-3 {
			fh = max(0, h-3-row)
		}
		focused := false
		if f.Focusable() {
			focused = fi == d.focus
			fi++
		}
		f.Draw(a, v.Sub(2, row, inner, fh), focused, d)
		row += fh + 1
	}
	if d.Err != "" {
		for _, l := range wrapText(d.Err, inner) {
			v.Text(2, row-1, l, Style{Fg: th.Error, Bg: bg})
			row++
		}
	}
	bx := w - 2
	fb := d.focusedButton()
	for i := len(d.Buttons) - 1; i >= 0; i-- {
		b := d.Buttons[i]
		bw := Width(b.Label) + 4
		bx -= bw
		idx := i
		DrawButton(a, v, bx, h-2, b.Label, b.Primary, fb == i, func() tea.Cmd { return d.press(a, idx) })
		bx--
	}
}

func Prompt(title, label, initial, ok string, fn func(a *App, val string) (tea.Cmd, string)) *Dialog {
	in := NewInput(initial)
	d := &Dialog{Title: title, Width: 60}
	d.Fields = []Field{&InputField{Label: label, In: in}}
	d.Buttons = []Button{
		{Label: "Cancel"},
		{Label: ok, Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
			cmd, err := fn(a, in.Value())
			if err != "" {
				d.Err = err
				return nil, false
			}
			return cmd, true
		}},
	}
	return d
}

func Confirm(title, message, ok string, danger bool, fn func(a *App) tea.Cmd) *Dialog {
	d := &Dialog{Title: title, Width: 64}
	fg := NoColor
	_ = danger
	d.Fields = []Field{&LabelField{Text: message, Fg: fg}}
	d.Buttons = []Button{
		{Label: "Cancel"},
		{Label: ok, Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) { return fn(a), true }},
	}
	d.focus = 1
	return d
}
