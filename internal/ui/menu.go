package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type MenuItem struct {
	Label    string
	Extra    string
	Hint     string
	Icon     string
	IconFg   Color
	Fg       Color
	Disabled bool
	Checked  bool
	Header   bool
	Sep      bool
	Do       func(a *App) tea.Cmd
	Sub      func(a *App) []MenuItem
	// Keep lets an action leave the menu open (e.g. toggles).
	Keep bool
}

func MenuSep() MenuItem            { return MenuItem{Sep: true} }
func MenuHeader(s string) MenuItem { return MenuItem{Header: true, Label: s} }

type Menu struct {
	Title      string
	items      []MenuItem
	vis        []int
	sel        int
	top        int
	X, Y       int
	search     *TextInput
	Searchable bool
	MinWidth   int
	MaxHeight  int
	parent     *Menu
	child      *Menu
	rect       Rect
	Footer     string
	onClose    func(a *App)
	shownSel   int
}

func NewMenu(title string, items []MenuItem) *Menu {
	m := &Menu{Title: title, items: items, X: -1, Y: -1, search: NewInput(""), Searchable: true, shownSel: -1}
	m.filter()
	return m
}

func NewMenuAt(title string, items []MenuItem, x, y int) *Menu {
	m := NewMenu(title, items)
	m.X, m.Y = x, y
	return m
}

func (m *Menu) Dismissable() bool { return true }

func (m *Menu) SetItems(items []MenuItem) {
	m.items = items
	m.filter()
}

func (m *Menu) filter() {
	q := strings.ToLower(m.search.Value())
	m.vis = m.vis[:0]
	for i, it := range m.items {
		if q != "" {
			if it.Sep || it.Header {
				continue
			}
			if !fuzzy(strings.ToLower(it.Label+" "+it.Extra), q) {
				continue
			}
		}
		m.vis = append(m.vis, i)
	}
	// Drop leading/trailing/double separators left after filtering.
	var cleaned []int
	for _, i := range m.vis {
		it := m.items[i]
		if it.Sep && (len(cleaned) == 0 || m.items[cleaned[len(cleaned)-1]].Sep) {
			continue
		}
		cleaned = append(cleaned, i)
	}
	for len(cleaned) > 0 && m.items[cleaned[len(cleaned)-1]].Sep {
		cleaned = cleaned[:len(cleaned)-1]
	}
	m.vis = cleaned
	m.sel = 0
	m.top = 0
	m.skip(1)
}

func fuzzy(s, q string) bool {
	if strings.Contains(s, q) {
		return true
	}
	j := 0
	for _, r := range s {
		if j < len(q) && r == rune(q[j]) {
			j++
		}
	}
	return j == len(q)
}

func (m *Menu) selectable(vi int) bool {
	if vi < 0 || vi >= len(m.vis) {
		return false
	}
	it := m.items[m.vis[vi]]
	return !it.Sep && !it.Header
}

func (m *Menu) skip(d int) {
	for i := 0; i < len(m.vis) && !m.selectable(m.sel); i++ {
		m.sel = (m.sel + d + len(m.vis)) % len(m.vis)
	}
}

func (m *Menu) move(d int) {
	if len(m.vis) == 0 {
		return
	}
	m.sel = (m.sel + d + len(m.vis)) % len(m.vis)
	m.skip(d)
}

func (m *Menu) current() *MenuItem {
	if !m.selectable(m.sel) {
		return nil
	}
	return &m.items[m.vis[m.sel]]
}

func (m *Menu) root() *Menu {
	r := m
	for r.parent != nil {
		r = r.parent
	}
	return r
}

func (m *Menu) closeAll(a *App) {
	r := m.root()
	for x := r; x != nil; x = x.child {
		a.Pop(x)
	}
	if r.onClose != nil {
		r.onClose(a)
	}
}

func (m *Menu) activate(a *App) tea.Cmd {
	it := m.current()
	if it == nil || it.Disabled {
		return nil
	}
	if it.Sub != nil {
		m.openSub(a, it)
		return nil
	}
	if it.Do == nil {
		return nil
	}
	if !it.Keep {
		m.closeAll(a)
	}
	return it.Do(a)
}

func (m *Menu) openSub(a *App, it *MenuItem) {
	sub := NewMenuAt(it.Label, it.Sub(a), m.rect.X+m.rect.W-1, m.rect.Y+1+(m.sel-m.top))
	sub.Searchable = false
	sub.parent = m
	m.child = sub
	a.Push(sub)
}

func (m *Menu) Key(a *App, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		if m.Searchable && m.search.Value() != "" {
			m.search.SetValue("")
			m.filter()
			return nil
		}
		a.Pop(m)
		if m.parent != nil {
			m.parent.child = nil
		} else if m.onClose != nil {
			m.onClose(a)
		}
		return nil
	case "left":
		if m.parent != nil {
			a.Pop(m)
			m.parent.child = nil
			return nil
		}
	case "up", "ctrl+p":
		m.move(-1)
		return nil
	case "down", "ctrl+n", "tab":
		m.move(1)
		return nil
	case "pgup":
		for i := 0; i < 10; i++ {
			m.move(-1)
		}
		return nil
	case "pgdown":
		for i := 0; i < 10; i++ {
			m.move(1)
		}
		return nil
	case "right":
		if it := m.current(); it != nil && it.Sub != nil && !it.Disabled {
			m.openSub(a, it)
		}
		return nil
	case "enter":
		return m.activate(a)
	}
	if m.Searchable && m.search.Key(k) {
		m.filter()
		return nil
	}
	if !m.Searchable && k.Type == tea.KeyRunes && len(k.Runes) == 1 {
		// Non-searchable submenus jump to the next item starting with the typed letter.
		r := strings.ToLower(string(k.Runes))
		for i := 1; i <= len(m.vis); i++ {
			vi := (m.sel + i) % len(m.vis)
			if m.selectable(vi) && strings.HasPrefix(strings.ToLower(m.items[m.vis[vi]].Label), r) {
				m.sel = vi
				break
			}
		}
	}
	return nil
}

func (m *Menu) Hint() string {
	if m.Searchable {
		return "Type to search · ↑↓ navigate · ⏎ select · → submenu · esc close"
	}
	return "↑↓ navigate · ⏎ select · ← back · esc close"
}

func (m *Menu) Draw(a *App, screen View) {
	th := a.Th
	w := max(m.MinWidth, Width(m.Title)+6)
	for _, it := range m.items {
		iw := Width(it.Label) + 6
		if it.Icon != "" {
			iw += Width(it.Icon) + 1
		}
		if it.Extra != "" {
			iw += Width(it.Extra) + 2
		}
		if it.Hint != "" {
			iw += Width(it.Hint) + 3
		}
		if it.Sub != nil {
			iw += 2
		}
		w = max(w, iw)
	}
	w = min(w, screen.W()-4)
	headerH := 0
	if m.Title != "" {
		headerH++
	}
	if m.Searchable {
		headerH++
	}
	footerH := 0
	if m.Footer != "" {
		footerH = 1
	}
	maxH := m.MaxHeight
	if maxH == 0 {
		maxH = screen.H() - 4
	}
	listH := min(len(m.vis), max(3, maxH-headerH-footerH-2))
	if len(m.vis) == 0 {
		listH = 1
	}
	h := listH + headerH + footerH + 2
	x, y := m.X, m.Y
	if x < 0 {
		x = (screen.W() - w) / 2
	}
	if y < 0 {
		y = max(1, (screen.H()-h)/4)
	}
	x = max(0, min(x, screen.W()-w))
	y = max(0, min(y, screen.H()-h))
	m.rect = Rect{x, y, w, h}
	v := screen.Sub(x, y, w, h)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.Border, Bg: bg}, "", Style{})
	row := 1
	if m.Title != "" {
		v.TextFit(2, row, m.Title, w-4, Style{Fg: th.Fg, Bg: bg, Bold: true})
		row++
	}
	if m.Searchable {
		v.Text(2, row, "⌕", Style{Fg: th.Dim, Bg: bg})
		m.search.Focused = true
		m.search.Placeholder = "Search"
		m.search.Draw(a, v.Sub(4, row, w-6, 1), Style{Fg: th.Fg, Bg: bg})
		row++
	}
	if m.sel != m.shownSel {
		m.shownSel = m.sel
		if m.sel < m.top {
			m.top = m.sel
		}
		if m.sel >= m.top+listH {
			m.top = m.sel - listH + 1
		}
	}
	m.top = max(0, min(m.top, len(m.vis)-listH))
	if len(m.vis) == 0 {
		v.Text(3, row, "Nothing found", Style{Fg: th.Dim, Bg: bg})
	}
	for i := 0; i < listH && m.top+i < len(m.vis); i++ {
		vi := m.top + i
		it := m.items[m.vis[vi]]
		rv := v.Sub(1, row+i, w-2, 1)
		st := Style{Fg: th.Fg, Bg: bg}
		if it.Fg != NoColor {
			st.Fg = it.Fg
		}
		if it.Disabled {
			st.Fg = th.Faint
		}
		if vi == m.sel && !it.Disabled {
			st.Bg = th.Sel
		}
		rv.Fill(Style{Bg: st.Bg})
		switch {
		case it.Sep:
			rv.HLine(1, 0, w-4, '─', Style{Fg: th.Border, Bg: bg})
			continue
		case it.Header:
			rv.TextFit(1, 0, it.Label, w-4, Style{Fg: th.Dim, Bg: bg})
			continue
		}
		cx := 1
		if it.Checked {
			rv.Text(cx, 0, "✓", st.WithFg(th.Accent))
		}
		cx += 2
		if it.Icon != "" {
			fg := it.IconFg
			if fg == NoColor {
				fg = th.Dim
			}
			cx = rv.Text(cx, 0, it.Icon, Style{Fg: fg, Bg: st.Bg}) + 1
		}
		rightW := 1
		if it.Hint != "" {
			rightW += Width(it.Hint) + 1
		}
		if it.Sub != nil {
			rightW += 2
		}
		cx = rv.TextFit(cx, 0, it.Label, rv.W()-cx-rightW, st)
		if it.Extra != "" {
			rv.TextFit(cx+1, 0, it.Extra, rv.W()-cx-rightW-2, Style{Fg: th.Dim, Bg: st.Bg})
		}
		rx := rv.W() - 1
		if it.Sub != nil {
			rv.Text(rx-1, 0, "›", Style{Fg: th.Dim, Bg: st.Bg})
			rx -= 2
		}
		if it.Hint != "" {
			rv.TextRight(rx, 0, it.Hint, Style{Fg: th.Dim, Bg: st.Bg})
		}
		idx := vi
		a.Hit(rv.Abs(), func(mm Mouse) tea.Cmd {
			if mm.Action == tea.MouseActionMotion && !it.Disabled {
				m.sel = idx
				return nil
			}
			if mm.LeftPress() {
				m.sel = idx
				if m.child != nil {
					a.Pop(m.child)
					m.child = nil
				}
				return m.activate(a)
			}
			return nil
		})
	}
	if len(m.vis) > listH {
		DrawScrollbar(a, v.Sub(w-1, row, 1, listH), m.top, listH, len(m.vis), func(t int) { m.top = t })
	}
	if m.Footer != "" {
		v.TextFit(2, h-2, m.Footer, w-4, Style{Fg: th.Dim, Bg: bg})
	}
	abs := v.Abs()
	a.HitPass(abs, func(mm Mouse) tea.Cmd {
		if d := mm.Wheel(); d != 0 {
			m.top = max(0, min(len(m.vis)-listH, m.top+d))
		}
		return nil
	})
	if m.child != nil {
		// The child draws itself as the next overlay; only keep our hits clickable.
		return
	}
}
