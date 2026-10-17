package ui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

const (
	NoCheck   = -1
	Unchecked = 0
	Partial   = 1
	Checked   = 2
)

type Node struct {
	ID        string
	Label     string
	LabelFg   Color
	Bold      bool
	Strike    bool
	Extra     string
	Right     string
	RightFg   Color
	Icon      string
	IconOpen  string
	IconFg    Color
	Children  []*Node
	Lazy      func() []*Node
	Check     int
	RowBg     Color
	Data      any
	Collapsed bool
	parent    *Node
}

func (n *Node) Expandable() bool { return len(n.Children) > 0 || n.Lazy != nil }

func (n *Node) Leaves() []*Node {
	if !n.Expandable() {
		return []*Node{n}
	}
	if n.Lazy != nil && n.Children == nil {
		n.Children = n.Lazy()
		for _, c := range n.Children {
			c.parent = n
		}
	}
	var res []*Node
	for _, c := range n.Children {
		res = append(res, c.Leaves()...)
	}
	if len(res) == 0 && n.Lazy != nil {
		return []*Node{n}
	}
	return res
}

func (n *Node) Parent() *Node { return n.parent }

type trow struct {
	n     *Node
	depth int
	index int
}

type Tree struct {
	Roots      []*Node
	exp        map[string]bool
	rows       []trow
	Sel        int
	Top        int
	anchor     int
	multi      map[string]bool
	Focused    bool
	search     string
	searching  bool
	Empty      string
	EmptyLines []string
	OnSelect   func(a *App, n *Node) tea.Cmd
	OnActivate func(a *App, n *Node) tea.Cmd
	OnCheck    func(a *App, n *Node) tea.Cmd
	OnContext  func(a *App, n *Node, x, y int) tea.Cmd
	height     int
	lastSel    string
	// shownID is the node last scrolled into view; wheel scrolling may move the viewport
	// away from an unchanged selection. An ID (not a row index) so a different node that
	// lands on the same row after a rebuild still gets scrolled to.
	shownID string
	// wantID is a selection to restore once that node exists (saved session state).
	wantID   string
	hoverRow int
	hoverSeq int
}

func NewTree() *Tree { return &Tree{exp: map[string]bool{}, multi: map[string]bool{}} }

func (t *Tree) SetRoots(roots []*Node) {
	selID := ""
	if n := t.Selected(); n != nil {
		selID = n.ID
	}
	t.Roots = roots
	var link func(p *Node, ns []*Node)
	link = func(p *Node, ns []*Node) {
		for _, n := range ns {
			n.parent = p
			if n.Lazy != nil && t.isExpanded(n) && n.Children == nil {
				n.Children = n.Lazy()
			}
			link(n, n.Children)
		}
	}
	link(nil, roots)
	t.rebuild()
	if t.wantID != "" && t.SelectID(t.wantID) {
		t.wantID = ""
		selID = ""
	}
	if selID != "" {
		if !t.SelectID(selID) && t.Sel >= len(t.rows) {
			t.Sel = max(0, len(t.rows)-1)
		}
	}
	valid := map[string]bool{}
	for _, r := range t.rows {
		valid[r.n.ID] = true
	}
	for id := range t.multi {
		if !valid[id] {
			delete(t.multi, id)
		}
	}
}

func (t *Tree) isExpanded(n *Node) bool {
	if v, ok := t.exp[n.ID]; ok {
		return v
	}
	return !n.Collapsed
}

func (t *Tree) rebuild() {
	t.rows = t.rows[:0]
	var walk func(ns []*Node, depth int)
	walk = func(ns []*Node, depth int) {
		for _, n := range ns {
			t.rows = append(t.rows, trow{n, depth, len(t.rows)})
			if n.Expandable() && t.isExpanded(n) {
				if n.Lazy != nil && n.Children == nil {
					n.Children = n.Lazy()
					for _, c := range n.Children {
						c.parent = n
					}
				}
				walk(n.Children, depth+1)
			}
		}
	}
	walk(t.Roots, 0)
	if t.Sel >= len(t.rows) {
		t.Sel = max(0, len(t.rows)-1)
	}
}

func (t *Tree) SetExpanded(n *Node, v bool) {
	t.exp[n.ID] = v
	t.rebuild()
}

func (t *Tree) ExpandAll() {
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			if n.Expandable() {
				t.exp[n.ID] = true
				if n.Lazy == nil {
					walk(n.Children)
				}
			}
		}
	}
	walk(t.Roots)
	t.rebuild()
}

func (t *Tree) CollapseAll() {
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			if n.Expandable() {
				t.exp[n.ID] = false
				walk(n.Children)
			}
		}
	}
	walk(t.Roots)
	t.rebuild()
}

func (t *Tree) Len() int { return len(t.rows) }

func (t *Tree) Selected() *Node {
	if t.Sel < 0 || t.Sel >= len(t.rows) {
		return nil
	}
	return t.rows[t.Sel].n
}

func (t *Tree) SelectID(id string) bool {
	for i, r := range t.rows {
		if r.n.ID == id {
			t.Sel = i
			return true
		}
	}
	return false
}

func (t *Tree) SelectFirstLeaf() {
	for i, r := range t.rows {
		if !r.n.Expandable() {
			t.Sel = i
			return
		}
	}
}

// SelectedNodes returns the multi-selection, or the single selected node.
func (t *Tree) SelectedNodes() []*Node {
	var res []*Node
	if len(t.multi) > 0 {
		for _, r := range t.rows {
			if t.multi[r.n.ID] {
				res = append(res, r.n)
			}
		}
		return res
	}
	if n := t.Selected(); n != nil {
		res = append(res, n)
	}
	return res
}

// SelectedLeaves expands group/directory selections into their leaf nodes.
func (t *Tree) SelectedLeaves() []*Node {
	seen := map[string]bool{}
	var res []*Node
	for _, n := range t.SelectedNodes() {
		for _, l := range n.Leaves() {
			if !seen[l.ID] {
				seen[l.ID] = true
				res = append(res, l)
			}
		}
	}
	return res
}

func (t *Tree) ClearMulti() { t.multi = map[string]bool{} }

func (t *Tree) selectRange(from, to int) {
	t.multi = map[string]bool{}
	if from > to {
		from, to = to, from
	}
	for i := from; i <= to && i < len(t.rows); i++ {
		t.multi[t.rows[i].n.ID] = true
	}
}

func (t *Tree) Typing() bool { return t.searching }

func (t *Tree) move(a *App, d int, extend bool) tea.Cmd {
	if len(t.rows) == 0 {
		return nil
	}
	if !extend {
		t.multi = map[string]bool{}
		t.anchor = -1
	} else if t.anchor < 0 || len(t.multi) == 0 {
		t.anchor = t.Sel
	}
	t.Sel = max(0, min(len(t.rows)-1, t.Sel+d))
	if extend {
		t.selectRange(t.anchor, t.Sel)
	}
	return t.selChanged(a)
}

func (t *Tree) selChanged(a *App) tea.Cmd {
	n := t.Selected()
	if n == nil || t.OnSelect == nil {
		return nil
	}
	if n.ID == t.lastSel {
		return nil
	}
	t.lastSel = n.ID
	return t.OnSelect(a, n)
}

// NotifySelection re-fires OnSelect even if the selection didn't move.
func (t *Tree) NotifySelection(a *App) tea.Cmd {
	t.lastSel = ""
	return t.selChanged(a)
}

func (t *Tree) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	if t.searching {
		switch keyName(k) {
		case "esc":
			t.searching = false
			t.search = ""
			return nil, true
		case "enter":
			t.searching = false
			if n := t.Selected(); n != nil && t.OnActivate != nil {
				return t.OnActivate(a, n), true
			}
			return nil, true
		case "backspace":
			if t.search != "" {
				rs := []rune(t.search)
				t.search = string(rs[:len(rs)-1])
			}
			return nil, true
		case "up", "down":
			return t.searchStep(a, keyName(k) == "down"), true
		}
		if (k.Type == tea.KeyRunes || k.Type == tea.KeySpace) && !isChord(k) {
			t.search += string(k.Runes)
			if k.Type == tea.KeySpace {
				t.search += " "
			}
			return t.searchFrom(a, t.Sel), true
		}
		t.searching = false
		t.search = ""
	}
	switch keyName(k) {
	case "up", "k":
		return t.move(a, -1, false), true
	case "down", "j":
		return t.move(a, 1, false), true
	case "shift+up", "K":
		return t.move(a, -1, true), true
	case "shift+down", "J":
		return t.move(a, 1, true), true
	case "pgup":
		return t.move(a, -max(1, t.height-1), false), true
	case "pgdown":
		return t.move(a, max(1, t.height-1), false), true
	case "home":
		return t.move(a, -len(t.rows), false), true
	case "end":
		return t.move(a, len(t.rows), false), true
	case "left", "h":
		if n := t.Selected(); n != nil {
			if n.Expandable() && t.isExpanded(n) {
				t.SetExpanded(n, false)
			} else if n.parent != nil {
				t.SelectID(n.parent.ID)
				return t.selChanged(a), true
			}
		}
		return nil, true
	case "right", "l":
		if n := t.Selected(); n != nil && n.Expandable() {
			if !t.isExpanded(n) {
				t.SetExpanded(n, true)
			} else {
				return t.move(a, 1, false), true
			}
		}
		return nil, true
	case "enter":
		if n := t.Selected(); n != nil {
			if t.OnActivate != nil {
				if cmd := t.OnActivate(a, n); cmd != nil || !n.Expandable() {
					return cmd, true
				}
			}
			if n.Expandable() {
				t.SetExpanded(n, !t.isExpanded(n))
			}
		}
		return nil, true
	case " ":
		if n := t.Selected(); n != nil && n.Check != NoCheck && t.OnCheck != nil {
			return t.OnCheck(a, n), true
		}
		return nil, false
	case "/":
		t.searching = true
		t.search = ""
		return nil, true
	case "*", "ctrl+kp+", "ctrl+=":
		t.ExpandAll()
		return nil, true
	case "-", "ctrl+kp-", "ctrl+-":
		t.CollapseAll()
		return nil, true
	}
	return nil, false
}

func (t *Tree) matches(n *Node) bool {
	if t.search == "" {
		return false
	}
	_, hits := fuzzyScore(t.search, n.Label)
	return len(hits) > 0 && hits[len(hits)-1]-hits[0] == len(hits)-1
}

func (t *Tree) searchFrom(a *App, from int) tea.Cmd {
	for i := 0; i < len(t.rows); i++ {
		idx := (from + i) % len(t.rows)
		if t.matches(t.rows[idx].n) {
			t.Sel = idx
			t.multi = map[string]bool{}
			return t.selChanged(a)
		}
	}
	return nil
}

func (t *Tree) searchStep(a *App, down bool) tea.Cmd {
	n := len(t.rows)
	for i := 1; i < n; i++ {
		idx := t.Sel + i
		if !down {
			idx = t.Sel - i
		}
		idx = (idx%n + n) % n
		if t.matches(t.rows[idx].n) {
			t.Sel = idx
			return t.selChanged(a)
		}
	}
	return nil
}

func (t *Tree) ensureVisible(h int) {
	id := ""
	if n := t.Selected(); n != nil {
		id = n.ID
	}
	if id != t.shownID {
		t.shownID = id
		if t.Sel < t.Top {
			t.Top = t.Sel
		}
		if t.Sel >= t.Top+h {
			t.Top = t.Sel - h + 1
		}
	}
	if t.Top > len(t.rows)-h {
		t.Top = max(0, len(t.rows)-h)
	}
	if t.Top < 0 {
		t.Top = 0
	}
}

func (t *Tree) Draw(a *App, v View) {
	th := a.Th
	h := v.H()
	t.height = h
	a.Selectable(v.Abs(), false)
	t.ensureVisible(h)
	base := Style{Fg: th.Fg, Bg: th.PanelBg}
	if len(t.rows) == 0 {
		lines := t.EmptyLines
		if len(lines) == 0 && t.Empty != "" {
			lines = []string{t.Empty}
		}
		for i, l := range lines {
			y := h/3 + i
			v.Text(max(1, (v.W()-Width(l))/2), y, l, base.WithFg(th.Dim))
		}
	}
	sb := len(t.rows) > h
	w := v.W()
	if sb {
		w--
	}
	for i := 0; i < h && t.Top+i < len(t.rows); i++ {
		r := t.rows[t.Top+i]
		idx := t.Top + i
		t.drawRow(a, v.Sub(0, i, w, 1), r, idx == t.Sel || t.multi[r.n.ID])
	}
	if sb {
		DrawScrollbar(a, v.Sub(v.W()-1, 0, 1, h), t.Top, h, len(t.rows), func(top int) { t.Top = top })
	}
	if t.searching {
		label := " Search for: " + t.search + " "
		sw := min(v.W(), Width(label)+1)
		v.FillRect(0, 0, sw, 1, Style{Bg: th.HeaderBg})
		v.Text(0, 0, label, Style{Fg: th.Fg, Bg: th.HeaderBg})
	}
	abs := v.Abs()
	a.Hit(Rect{abs.X, abs.Y, w, h}, func(m Mouse) tea.Cmd {
		if m.Action == tea.MouseActionMotion {
			t.hoverRow, t.hoverSeq = t.Top+(m.Y-abs.Y), a.motionSeq
			return nil
		}
		if d := m.Wheel(); d != 0 {
			t.Top = max(0, min(len(t.rows)-h, t.Top+d*3))
			return nil
		}
		row := t.Top + (m.Y - abs.Y)
		if row < 0 || row >= len(t.rows) {
			return nil
		}
		r := t.rows[row]
		x := m.X - abs.X
		arrowX := 1 + r.depth*2
		if m.LeftPress() {
			if r.n.Expandable() && x >= arrowX && x <= arrowX+1 {
				t.SetExpanded(r.n, !t.isExpanded(r.n))
				return nil
			}
			checkX := arrowX + 2
			if r.n.Check != NoCheck && x >= checkX && x <= checkX+2 && t.OnCheck != nil {
				t.Sel = row
				return t.OnCheck(a, r.n)
			}
			switch {
			case m.Shift:
				if t.anchor < 0 || len(t.multi) == 0 {
					t.anchor = t.Sel
				}
				t.Sel = row
				t.selectRange(t.anchor, row)
			case m.Ctrl || m.Alt:
				if len(t.multi) == 0 {
					if cur := t.Selected(); cur != nil {
						t.multi[cur.ID] = true
					}
				}
				if t.multi[r.n.ID] {
					delete(t.multi, r.n.ID)
				} else {
					t.multi[r.n.ID] = true
				}
				t.Sel = row
			default:
				t.multi = map[string]bool{}
				t.anchor = -1
				t.Sel = row
			}
			cmd := t.selChanged(a)
			if m.Double {
				if t.OnActivate != nil {
					if c2 := t.OnActivate(a, r.n); c2 != nil || !r.n.Expandable() {
						return tea.Batch(cmd, c2)
					}
				}
				if r.n.Expandable() {
					t.SetExpanded(r.n, !t.isExpanded(r.n))
				}
			}
			return cmd
		}
		if m.RightPress() {
			if !t.multi[r.n.ID] {
				t.multi = map[string]bool{}
				t.Sel = row
			}
			cmd := t.selChanged(a)
			if t.OnContext != nil {
				return tea.Batch(cmd, t.OnContext(a, r.n, m.X, m.Y))
			}
			return cmd
		}
		return nil
	})
}

func (t *Tree) drawRow(a *App, v View, r trow, selected bool) {
	th := a.Th
	n := r.n
	bg := th.PanelBg
	if n.RowBg != NoColor {
		bg = n.RowBg
	}
	if selected {
		if t.Focused {
			bg = th.Sel
		} else {
			bg = th.SelInactive
		}
	} else if r.index == t.hoverRow && a.hovered(t.hoverSeq) {
		bg = th.Hover
	}
	st := Style{Fg: th.Fg, Bg: bg}
	v.Fill(st)
	x := 1 + r.depth*2
	if n.Expandable() {
		arrow := "▸"
		if t.isExpanded(n) {
			arrow = "▾"
		}
		v.Text(x, 0, arrow, st.WithFg(th.Dim))
	}
	x += 2
	if n.Check != NoCheck {
		box := a.checkGlyph(n.Check)
		fg := th.Dim
		if n.Check != Unchecked {
			fg = th.Accent
		}
		if selected && !a.icons {
			fg = th.Fg
		}
		v.Text(x, 0, box, st.WithFg(fg))
		x += Width(box) + 1
	}
	if n.Icon != "" {
		fg := n.IconFg
		if fg == NoColor {
			fg = th.Dim
		}
		glyph := n.Icon
		if n.IconOpen != "" && n.Expandable() && t.isExpanded(n) {
			glyph = n.IconOpen
		}
		x = v.Text(x, 0, glyph, st.WithFg(fg)) + 1
	}
	rightW := 0
	if n.Right != "" {
		rightW = Width(n.Right) + 2
	}
	labelFg := n.LabelFg
	if labelFg == NoColor {
		labelFg = th.Fg
	}
	ls := st.WithFg(labelFg)
	ls.Bold = n.Bold
	ls.Strike = n.Strike
	avail := v.W() - x - rightW
	label := Truncate(n.Label, max(1, avail))
	lx := x
	x = v.Text(x, 0, label, ls)
	if t.searching && t.search != "" {
		lr := []rune(label)
		low := make([]rune, len(lr))
		for i, r := range lr {
			low[i] = unicode.ToLower(r)
		}
		q := []rune(strings.ToLower(t.search))
		if i := runeIndex(low, q); i >= 0 {
			col := lx + Width(string(lr[:i]))
			v.Text(col, 0, string(lr[i:i+len(q)]), Style{Fg: Hex("#000000"), Bg: th.Warning})
		}
	}
	if n.Extra != "" && x+2 < v.W()-rightW {
		x = v.TextFit(x+1, 0, n.Extra, v.W()-rightW-x-1, st.WithFg(th.Dim))
	}
	if n.Right != "" {
		fg := n.RightFg
		if fg == NoColor {
			fg = th.Dim
		}
		v.TextRight(v.W()-1, 0, n.Right, st.WithFg(fg))
	}
}

// DrawScrollbar draws a 1-column scrollbar and makes it clickable/draggable.
func DrawScrollbar(a *App, v View, top, visible, total int, set func(int)) {
	th := a.Th
	h := v.H()
	if total <= visible || h <= 0 {
		return
	}
	thumbH := max(1, h*visible/total)
	thumbY := (h - thumbH) * top / max(1, total-visible)
	v.Fill(Style{Bg: th.ScrollTrack})
	v.FillRect(0, thumbY, 1, thumbH, Style{Bg: th.ScrollThumb})
	abs := v.Abs()
	jump := func(y int) {
		rel := y - abs.Y - thumbH/2
		t := rel * max(1, total-visible) / max(1, h-thumbH)
		set(max(0, min(total-visible, t)))
	}
	a.Hit(abs, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			jump(m.Y)
			a.Drag(func(m Mouse) tea.Cmd { jump(m.Y); return nil })
		}
		return nil
	})
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
