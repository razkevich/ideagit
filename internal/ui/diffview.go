package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/diff"
	"github.com/razkevich/ideagit/internal/git"
)

type DiffSource struct {
	Path      string
	Kind      git.ChangeKind
	Header    string
	LeftTitle string
	// LeftPath is shown next to LeftTitle unless the diff window's title already has it.
	LeftPath   string
	RightTitle string
	// LeftEditable/RightEditable mark the side that is the working-tree file.
	LeftEditable  bool
	RightEditable bool
	Load          func() (old, new []byte, err error)
}

type rowKind int

const (
	rowEqual rowKind = iota
	rowBlock
	rowOld
	rowNew
	rowFold
)

type drow struct {
	kind      rowKind
	o, n      int
	block     int
	foldStart int
	foldLen   int
	first     bool
}

type DiffView struct {
	sources []DiffSource
	idx     int
	res     *diff.Result
	err     error
	loading bool
	gen     int

	hlOld, hlNew  [][]tokSpan
	wordOld       map[int][]diff.Span
	wordNew       map[int][]diff.Span
	rows          []drow
	foldsOpen     map[int]bool
	top, left     int
	cur           int
	Focused       bool
	ShowToolbar   bool
	HidePath      bool
	height        int
	wantBlock     int
	nextFileArmed int

	// Partial commit support: Excluded[path][block] means "don't commit this chunk".
	Partial  bool
	Excluded map[string]map[int]bool
	// Shown is the working-tree content each partial-commit diff was computed from.
	Shown    map[string]string
	OnToggle func(a *App)
	OnChange func(a *App) tea.Cmd
}

func NewDiffView() *DiffView {
	return &DiffView{ShowToolbar: true, foldsOpen: map[int]bool{}, Excluded: map[string]map[int]bool{}, Shown: map[string]string{}, wantBlock: -1}
}

func (d *DiffView) Source() *DiffSource {
	if d.idx < 0 || d.idx >= len(d.sources) {
		return nil
	}
	return &d.sources[d.idx]
}

func (d *DiffView) Result() *diff.Result { return d.res }

func (d *DiffView) Empty() bool { return len(d.sources) == 0 }

// SetSources replaces the file list; it reloads only if the shown file changed unless force is set.
func (d *DiffView) SetSources(a *App, srcs []DiffSource, idx int, force bool) tea.Cmd {
	prev := ""
	if s := d.Source(); s != nil {
		prev = s.Path + "\x00" + s.Header
	}
	d.sources = srcs
	d.idx = idx
	if len(srcs) == 0 {
		d.res = nil
		d.err = nil
		d.gen++
		return nil
	}
	cur := d.sources[d.idx].Path + "\x00" + d.sources[d.idx].Header
	if cur == prev && !force && d.res != nil {
		return d.reload(a, true)
	}
	d.top, d.left, d.cur = 0, 0, 0
	d.foldsOpen = map[int]bool{}
	d.wantBlock = 0
	return d.reload(a, false)
}

func (d *DiffView) Reload(a *App) tea.Cmd { return d.reload(a, true) }

func (d *DiffView) reload(a *App, keepPos bool) tea.Cmd {
	s := d.Source()
	if s == nil {
		return nil
	}
	d.gen++
	gen := d.gen
	d.loading = true
	src := *s
	ignore := diff.IgnoreMode(a.Cfg.IgnoreWS)
	if d.Partial {
		// Chunk selection maps onto the exact diff the commit is built from.
		ignore = diff.IgnoreNone
	}
	th := a.Th
	words := !a.Cfg.NoWordHighlight
	return Async(func() func(a *App) tea.Cmd {
		old, nw, err := src.Load()
		var res *diff.Result
		var hlo, hln [][]tokSpan
		wo, wn := map[int][]diff.Span{}, map[int][]diff.Span{}
		if err == nil {
			res = diff.Compute(old, nw, ignore)
			res.OldMissing = old == nil
			res.NewMissing = nw == nil
			if !res.Binary && !res.TooLarge {
				hlo = highlightLines(th, src.Path, res.Old)
				hln = highlightLines(th, src.Path, res.New)
				if words {
					for bi := range res.Blocks {
						o, n := res.WordDiff(bi)
						for k, v := range o {
							wo[k] = v
						}
						for k, v := range n {
							wn[k] = v
						}
					}
				}
			}
		}
		return func(a *App) tea.Cmd {
			if gen != d.gen {
				return nil
			}
			d.loading = false
			d.err = err
			d.res = res
			if d.Partial && res != nil && !res.Binary && !res.TooLarge {
				if prev, ok := d.Shown[src.Path]; ok && prev != string(diff.JoinLines(res.New, res.NewNoEOL)) {
					// The file changed, so chunk numbers no longer mean the same chunks.
					delete(d.Excluded, src.Path)
				}
				d.Shown[src.Path] = string(diff.JoinLines(res.New, res.NewNoEOL))
			}
			d.hlOld, d.hlNew = hlo, hln
			d.wordOld, d.wordNew = wo, wn
			if !keepPos {
				d.top, d.left = 0, 0
			}
			d.buildRows(a)
			if d.wantBlock >= 0 && res != nil && len(res.Blocks) > 0 {
				d.gotoBlock(min(d.wantBlock, len(res.Blocks)-1))
			}
			d.wantBlock = -1
			return nil
		}
	})
}

func (d *DiffView) unified(a *App) bool { return a.Cfg.DiffUnified }

func (d *DiffView) buildRows(a *App) {
	d.rows = d.rows[:0]
	r := d.res
	if r == nil || r.Binary || r.TooLarge {
		return
	}
	uni := d.unified(a)
	collapse := a.Cfg.CollapseUnchanged
	const ctx = 4
	emitEqual := func(o0, n0, count int, atStart, atEnd bool) {
		lead, trail := ctx, ctx
		if atStart {
			lead = 0
		}
		if atEnd {
			trail = 0
		}
		if collapse && count > lead+trail+2 && !d.foldsOpen[o0+lead] {
			for i := 0; i < lead; i++ {
				d.rows = append(d.rows, drow{kind: rowEqual, o: o0 + i, n: n0 + i, block: -1})
			}
			d.rows = append(d.rows, drow{kind: rowFold, o: -1, n: -1, block: -1, foldStart: o0 + lead, foldLen: count - lead - trail})
			for i := count - trail; i < count; i++ {
				d.rows = append(d.rows, drow{kind: rowEqual, o: o0 + i, n: n0 + i, block: -1})
			}
			return
		}
		for i := 0; i < count; i++ {
			d.rows = append(d.rows, drow{kind: rowEqual, o: o0 + i, n: n0 + i, block: -1})
		}
	}
	po, pn := 0, 0
	for bi, b := range r.Blocks {
		emitEqual(po, pn, b.O0-po, bi == 0 && po == 0, false)
		oc, nc := b.O1-b.O0, b.N1-b.N0
		if uni {
			first := true
			for i := 0; i < oc; i++ {
				d.rows = append(d.rows, drow{kind: rowOld, o: b.O0 + i, n: -1, block: bi, first: first})
				first = false
			}
			for i := 0; i < nc; i++ {
				d.rows = append(d.rows, drow{kind: rowNew, o: -1, n: b.N0 + i, block: bi, first: first})
				first = false
			}
		} else {
			for i := 0; i < max(oc, nc); i++ {
				row := drow{kind: rowBlock, o: -1, n: -1, block: bi, first: i == 0}
				if i < oc {
					row.o = b.O0 + i
				}
				if i < nc {
					row.n = b.N0 + i
				}
				d.rows = append(d.rows, row)
			}
		}
		po, pn = b.O1, b.N1
	}
	emitEqual(po, pn, len(r.Old)-po, len(r.Blocks) == 0, true)
}

func (d *DiffView) blockRow(bi int) int {
	for i, r := range d.rows {
		if r.block == bi {
			return i
		}
	}
	return 0
}

func (d *DiffView) gotoBlock(bi int) {
	if d.res == nil || bi < 0 || bi >= len(d.res.Blocks) {
		return
	}
	d.cur = bi
	row := d.blockRow(bi)
	h := max(1, d.height)
	d.top = max(0, row-h/3)
}

func (d *DiffView) blocks() int {
	if d.res == nil {
		return 0
	}
	return len(d.res.Blocks)
}

func (d *DiffView) Next(a *App) tea.Cmd {
	n := d.blocks()
	if n > 0 && d.cur < n-1 {
		d.nextFileArmed = 0
		d.gotoBlock(d.cur + 1)
		return nil
	}
	if d.idx < len(d.sources)-1 {
		if d.nextFileArmed == 1 {
			d.nextFileArmed = 0
			return d.SelectFile(a, d.idx+1, 0)
		}
		d.nextFileArmed = 1
		a.Notify("Press F7 again to go to the next file", "")
		return nil
	}
	if n > 0 {
		d.gotoBlock(n - 1)
	}
	return nil
}

func (d *DiffView) Prev(a *App) tea.Cmd {
	if d.blocks() > 0 && d.cur > 0 {
		d.nextFileArmed = 0
		d.gotoBlock(d.cur - 1)
		return nil
	}
	if d.idx > 0 {
		if d.nextFileArmed == -1 {
			d.nextFileArmed = 0
			return d.SelectFile(a, d.idx-1, 1<<30)
		}
		d.nextFileArmed = -1
		a.Notify("Press Shift+F7 again to go to the previous file", "")
	}
	return nil
}

func (d *DiffView) SelectFile(a *App, idx, block int) tea.Cmd {
	if idx < 0 || idx >= len(d.sources) {
		return nil
	}
	d.idx = idx
	d.top, d.left, d.cur = 0, 0, 0
	d.foldsOpen = map[int]bool{}
	d.wantBlock = block
	if d.OnChange != nil {
		d.OnChange(a)
	}
	return d.reload(a, false)
}

func (d *DiffView) currentLine() int {
	for i := d.top; i < len(d.rows); i++ {
		if d.rows[i].n >= 0 {
			return d.rows[i].n + 1
		}
	}
	return 1
}

func (d *DiffView) editable() (left, right bool) {
	s := d.Source()
	if s == nil || d.res == nil || d.res.Binary {
		return false, false
	}
	return s.LeftEditable, s.RightEditable
}

// ApplyBlock moves a chunk from the read-only side into the working-tree file.
func (d *DiffView) ApplyBlock(a *App, bi int) tea.Cmd {
	s := d.Source()
	le, re := d.editable()
	// In the commit dialog chunks are picked, not applied; editing would shift them.
	if s == nil || d.Partial || (!le && !re) || bi < 0 || bi >= d.blocks() {
		return nil
	}
	path := a.Repo.Abs(s.Path)
	// The diff may be stale (another process edited the file since it was loaded);
	// rebuilding the file from it would silently drop those edits.
	shown := diff.JoinLines(d.res.New, d.res.NewNoEOL)
	if le {
		shown = diff.JoinLines(d.res.Old, d.res.OldNoEOL)
	}
	if cur, ok := a.Repo.ReadWorktree(s.Path); !ok || string(cur) != string(shown) {
		a.Notify("File changed on disk", s.Path+" was modified since the diff was shown; reloaded, try again")
		return d.reload(a, true)
	}
	var content []byte
	if re {
		content = d.res.RevertInNew(bi)
	} else {
		content = d.res.ApplyToOld(bi)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		a.Error("Cannot write "+s.Path, err.Error())
		return nil
	}
	d.wantBlock = bi
	return tea.Batch(d.reload(a, true), a.Refresh())
}

func (d *DiffView) toggleChunk(a *App, bi int) {
	s := d.Source()
	if s == nil || !d.Partial {
		return
	}
	m := d.Excluded[s.Path]
	if m == nil {
		m = map[int]bool{}
		d.Excluded[s.Path] = m
	}
	m[bi] = !m[bi]
	if d.OnToggle != nil {
		d.OnToggle(a)
	}
}

func (d *DiffView) chunkIncluded(bi int) bool {
	s := d.Source()
	if s == nil {
		return true
	}
	return !d.Excluded[s.Path][bi]
}

func (d *DiffView) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	h := max(1, d.height)
	switch keyName(k) {
	case "up", "k":
		d.top = max(0, d.top-1)
	case "down", "j":
		d.top = min(max(0, len(d.rows)-h), d.top+1)
	case "pgup":
		d.top = max(0, d.top-h+1)
	case "pgdown":
		d.top = min(max(0, len(d.rows)-h), d.top+h-1)
	case "home", "g":
		d.top = 0
	case "end", "G":
		d.top = max(0, len(d.rows)-h)
	case "left", "h":
		d.left = max(0, d.left-4)
	case "right", "l":
		d.left += 4
	case "f7", "n":
		return d.Next(a), true
	case "f19", "shift+f7", "N":
		return d.Prev(a), true
	case "]", "alt+right", "alt+down":
		return d.SelectFile(a, d.idx+1, 0), true
	case "[", "alt+left", "alt+up":
		return d.SelectFile(a, d.idx-1, 0), true
	case "ctrl+shift+right", "ctrl+shift+left":
		return d.ApplyBlock(a, d.cur), true
	case "v":
		a.Cfg.DiffUnified = !a.Cfg.DiffUnified
		a.Cfg.Save()
		d.buildRows(a)
		d.gotoBlock(d.cur)
	case "c":
		a.Cfg.CollapseUnchanged = !a.Cfg.CollapseUnchanged
		a.Cfg.Save()
		d.buildRows(a)
		d.gotoBlock(d.cur)
	case "w":
		a.Cfg.IgnoreWS = (a.Cfg.IgnoreWS + 1) % 3
		a.Cfg.Save()
		return d.reload(a, true), true
	case "x":
		a.Cfg.NoWordHighlight = !a.Cfg.NoWordHighlight
		a.Cfg.Save()
		return d.reload(a, true), true
	case "e", "f4", "ctrl+down":
		if s := d.Source(); s != nil {
			return a.JumpToSource(s.Path, d.currentLine()), true
		}
	case ">", "<", "a":
		return d.ApplyBlock(a, d.cur), true
	case " ":
		if d.Partial {
			d.toggleChunk(a, d.cur)
			return nil, true
		}
		return nil, false
	default:
		return nil, false
	}
	return nil, true
}

func (d *DiffView) Hint() string {
	h := "F7/⇧F7 next/prev change · [ ] file · v view · c collapse · w whitespace · F4 source"
	if le, re := d.editable(); le || re {
		h += " · a apply chunk"
	}
	if d.Partial {
		h += " · space include chunk"
	}
	return h
}

func (d *DiffView) leftTitle(s *DiffSource) string {
	if d.HidePath || s.LeftPath == "" {
		return s.LeftTitle
	}
	return s.LeftTitle + "  " + s.LeftPath
}

func (d *DiffView) Draw(a *App, v View) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	y := 0
	if d.ShowToolbar {
		d.drawToolbar(a, v.Sub(0, 0, v.W(), 1))
		y++
	}
	s := d.Source()
	if s == nil {
		v.Text(max(0, (v.W()-22)/2), v.H()/3, "No file selected", Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	uni := d.unified(a)
	header := v.Sub(0, y, v.W(), 1)
	header.Fill(Style{Bg: th.Bg})
	if uni {
		header.TextFit(1, 0, d.leftTitle(s)+"  →  "+s.RightTitle, v.W()-2, Style{Fg: th.Fg, Bg: th.Bg})
	} else {
		half := (v.W() - 1) / 2
		header.TextFit(1, 0, d.leftTitle(s), half-2, Style{Fg: th.Fg, Bg: th.Bg})
		header.TextFit(half+2, 0, s.RightTitle, v.W()-half-3, Style{Fg: th.Fg, Bg: th.Bg})
	}
	y++
	v.HLine(0, y, v.W(), '─', Style{Fg: th.Border, Bg: th.Bg})
	y++
	body := v.Sub(0, y, v.W(), v.H()-y)
	d.height = body.H()
	switch {
	case d.loading && d.res == nil:
		body.Text(2, 1, "Loading…", Style{Fg: th.Dim, Bg: th.Bg})
		return
	case d.err != nil:
		body.Text(2, 1, d.err.Error(), Style{Fg: th.Error, Bg: th.Bg})
		return
	case d.res == nil:
		return
	case d.res.TooLarge:
		body.Text(2, 1, "File is too large to compare", Style{Fg: th.Dim, Bg: th.Bg})
		return
	case d.res.Binary:
		msg := "Binary files differ"
		if d.res.Identical {
			msg = "Binary files are identical"
		}
		body.Text(2, 1, msg, Style{Fg: th.Dim, Bg: th.Bg})
		return
	case d.res.Identical && len(d.res.Old) == len(d.res.New) && !d.res.OldMissing && !d.res.NewMissing:
		body.Text(2, 0, "Contents are identical", Style{Fg: th.Dim, Bg: th.Bg})
		body = body.Sub(0, 1, body.W(), body.H()-1)
		d.height = body.H()
	}
	d.top = max(0, min(d.top, len(d.rows)-d.height))
	w := body.W() - 1
	if uni {
		d.drawUnified(a, body.Sub(0, 0, w, body.H()))
	} else {
		d.drawSide(a, body.Sub(0, 0, w, body.H()))
	}
	d.drawScroll(a, body.Sub(w, 0, 1, body.H()))
	abs := body.Abs()
	// Pass-through: chunk buttons and checkboxes drawn above must still get their clicks.
	a.HitPass(Rect{abs.X, abs.Y, w, body.H()}, func(m Mouse) tea.Cmd {
		if dd := m.Wheel(); dd != 0 {
			if m.Shift {
				d.left = max(0, d.left+dd*4)
			} else {
				d.top = max(0, min(len(d.rows)-d.height, d.top+dd*3))
			}
			return nil
		}
		if dd := m.HWheel(); dd != 0 {
			d.left = max(0, d.left+dd*4)
			return nil
		}
		if m.LeftPress() {
			row := d.top + m.Y - abs.Y
			if row >= 0 && row < len(d.rows) {
				r := d.rows[row]
				if r.kind == rowFold {
					d.foldsOpen[r.foldStart] = true
					d.buildRows(a)
					return nil
				}
				if r.block >= 0 {
					d.cur = r.block
				}
				if m.Double {
					line := d.currentLine()
					if r.n >= 0 {
						line = r.n + 1
					}
					if s := d.Source(); s != nil {
						if _, err := os.Stat(a.Repo.Abs(s.Path)); err == nil {
							return a.EditFile(s.Path, line)
						}
					}
				}
			}
		}
		return nil
	})
}

func (d *DiffView) drawToolbar(a *App, v View) {
	th := a.Th
	st := Style{Fg: th.Fg, Bg: th.ToolbarBg}
	v.Fill(st)
	x := 1
	btn := func(label string, enabled bool, fn func() tea.Cmd) {
		fg := th.Fg
		if !enabled {
			fg = th.Faint
		}
		v.Text(x, 0, label, st.WithFg(fg))
		if enabled {
			a.Hit(Rect{v.Abs().X + x, v.Abs().Y, Width(label), 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					return fn()
				}
				return nil
			})
		}
		x += Width(label) + 1
	}
	sep := func() {
		v.Text(x, 0, "│", st.WithFg(th.Border))
		x += 2
	}
	n := d.blocks()
	btn("↑", n > 0 || d.idx > 0, func() tea.Cmd { return d.Prev(a) })
	btn("↓", n > 0 || d.idx < len(d.sources)-1, func() tea.Cmd { return d.Next(a) })
	sep()
	s := d.Source()
	btn("✎", s != nil, func() tea.Cmd {
		if s != nil {
			return a.EditFile(s.Path, d.currentLine())
		}
		return nil
	})
	sep()
	btn("←", d.idx > 0, func() tea.Cmd { return d.SelectFile(a, d.idx-1, 0) })
	files := fmt.Sprintf("%d/%d files", d.idx+1, len(d.sources))
	if len(d.sources) <= 1 {
		files = fmt.Sprintf("%d file", len(d.sources))
	}
	filesFg := th.Faint
	if len(d.sources) > 1 {
		filesFg = th.Link
	}
	v.Text(x, 0, files, st.WithFg(filesFg))
	x += Width(files) + 1
	btn("→", d.idx < len(d.sources)-1, func() tea.Cmd { return d.SelectFile(a, d.idx+1, 0) })
	sep()
	collapse := "⤫ collapse"
	if a.Cfg.CollapseUnchanged {
		collapse = "⤧ expand"
	}
	btn(collapse, true, func() tea.Cmd {
		a.Cfg.CollapseUnchanged = !a.Cfg.CollapseUnchanged
		a.Cfg.Save()
		d.buildRows(a)
		return nil
	})

	right := v.W() - 1
	gear := "⚙"
	gx := right - 1
	v.Text(gx, 0, gear, st)
	a.Hit(Rect{v.Abs().X + gx, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			d.openSettings(a, v.Abs().X+gx-30, v.Abs().Y+1)
		}
		return nil
	})
	modeX := gx - 9
	side := Style{Fg: th.Fg, Bg: th.ToolbarBg}
	uniSt := side
	if a.Cfg.DiffUnified {
		uniSt.Bg = th.TabSelBg
	} else {
		side.Bg = th.TabSelBg
	}
	v.Text(modeX, 0, " ▥ ", side)
	v.Text(modeX+4, 0, " □ ", uniSt)
	a.Hit(Rect{v.Abs().X + modeX, v.Abs().Y, 3, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() && a.Cfg.DiffUnified {
			a.Cfg.DiffUnified = false
			a.Cfg.Save()
			d.buildRows(a)
		}
		return nil
	})
	a.Hit(Rect{v.Abs().X + modeX + 4, v.Abs().Y, 3, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() && !a.Cfg.DiffUnified {
			a.Cfg.DiffUnified = true
			a.Cfg.Save()
			d.buildRows(a)
		}
		return nil
	})
	label := "No differences"
	switch {
	case d.res == nil || d.loading:
		label = ""
	case n == 1:
		label = "1 difference"
	case n > 1:
		label = fmt.Sprintf("%d differences", n)
	}
	if n > 1 && d.Focused {
		label = fmt.Sprintf("%d of %d differences", d.cur+1, n)
	}
	if a.Cfg.IgnoreWS != 0 {
		label += " (ws ignored)"
	}
	if x < modeX-Width(label)-2 {
		v.TextRight(modeX-2, 0, label, st)
	}
}

func (d *DiffView) openSettings(a *App, x, y int) {
	ws := func(i int, label string) MenuItem {
		return MenuItem{Label: label, Checked: a.Cfg.IgnoreWS == i, Do: func(a *App) tea.Cmd {
			a.Cfg.IgnoreWS = i
			a.Cfg.Save()
			return d.reload(a, true)
		}}
	}
	items := []MenuItem{
		MenuHeader("Ignore"),
		ws(0, "Do not ignore"),
		ws(1, "Trim whitespaces"),
		ws(2, "Ignore whitespaces"),
		MenuSep(),
		{Label: "Highlight words", Checked: !a.Cfg.NoWordHighlight, Do: func(a *App) tea.Cmd {
			a.Cfg.NoWordHighlight = !a.Cfg.NoWordHighlight
			a.Cfg.Save()
			return d.reload(a, true)
		}},
		{Label: "Collapse unchanged fragments", Checked: a.Cfg.CollapseUnchanged, Do: func(a *App) tea.Cmd {
			a.Cfg.CollapseUnchanged = !a.Cfg.CollapseUnchanged
			a.Cfg.Save()
			d.buildRows(a)
			return nil
		}},
		MenuSep(),
		{Label: "Side-by-side viewer", Checked: !a.Cfg.DiffUnified, Do: func(a *App) tea.Cmd {
			a.Cfg.DiffUnified = false
			a.Cfg.Save()
			d.buildRows(a)
			return nil
		}},
		{Label: "Unified viewer", Checked: a.Cfg.DiffUnified, Do: func(a *App) tea.Cmd {
			a.Cfg.DiffUnified = true
			a.Cfg.Save()
			d.buildRows(a)
			return nil
		}},
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (d *DiffView) blockColors(a *App, bi int) (line, word, filler Color) {
	th := a.Th
	switch d.res.Blocks[bi].Type {
	case diff.BlockInserted:
		return th.DiffIns, th.DiffInsWord, blend(th.Bg, th.DiffIns, 0.35)
	case diff.BlockDeleted:
		return th.DiffDel, th.DiffDelWord, blend(th.Bg, th.DiffDel, 0.35)
	}
	return th.DiffMod, th.DiffModWord, blend(th.Bg, th.DiffMod, 0.35)
}

func numWidth(n int) int { return max(3, len(fmt.Sprint(n))) }

func (d *DiffView) drawSide(a *App, v View) {
	th := a.Th
	r := d.res
	nw := numWidth(max(len(r.Old), len(r.New)))
	mid := 3
	textW := (v.W() - 2*nw - mid - 2) / 2
	if textW < 4 {
		textW = 4
	}
	leftX := 0
	numLX := textW
	midX := numLX + nw + 1
	numRX := midX + mid
	rightX := numRX + nw + 1
	rightW := v.W() - rightX
	le, re := d.editable()
	abs := v.Abs()
	a.Selectable(Rect{abs.X + leftX, abs.Y, textW, v.H()}, true)
	a.Selectable(Rect{abs.X + rightX, abs.Y, rightW, v.H()}, true)
	for i := 0; i < v.H(); i++ {
		ri := d.top + i
		base := Style{Fg: th.Fg, Bg: th.Bg}
		if ri >= len(d.rows) {
			break
		}
		row := d.rows[ri]
		if row.kind == rowFold {
			st := Style{Fg: th.Dim, Bg: th.DiffFold}
			v.FillRect(0, i, v.W(), 1, st)
			label := fmt.Sprintf("⋯  %d unchanged lines  ⋯", row.foldLen)
			v.Text(max(0, (v.W()-Width(label))/2), i, label, st)
			continue
		}
		lineBg, wordBg, filler := th.Bg, th.Bg, th.Bg
		if row.block >= 0 {
			lineBg, wordBg, filler = d.blockColors(a, row.block)
		}
		lbg, rbg := lineBg, lineBg
		if row.block >= 0 && row.o < 0 {
			lbg = filler
		}
		if row.block >= 0 && row.n < 0 {
			rbg = filler
		}
		if row.kind == rowEqual {
			lbg, rbg = th.Bg, th.Bg
		}
		v.FillRect(leftX, i, textW, 1, base.WithBg(lbg))
		v.FillRect(rightX, i, rightW, 1, base.WithBg(rbg))
		if row.o >= 0 {
			d.drawText(v.Sub(leftX, i, textW, 1), r.Old[row.o], hl(d.hlOld, row.o), d.wordOld[row.o], th.Fg, lbg, wordBg)
			v.TextRight(numLX+nw, i, fmt.Sprint(row.o+1), base.WithFg(th.DiffGutterNum))
		}
		if row.n >= 0 {
			d.drawText(v.Sub(rightX, i, rightW, 1), r.New[row.n], hl(d.hlNew, row.n), d.wordNew[row.n], th.Fg, rbg, wordBg)
			v.TextRight(numRX+nw, i, fmt.Sprint(row.n+1), base.WithFg(th.DiffGutterNum))
		}
		if row.block >= 0 {
			// Thin change marker in the gutters, like IntelliJ's divider polygons.
			v.Set(numLX+nw, i, '▕', Style{Fg: lineBg, Bg: th.Bg})
			v.Set(numRX-1, i, '▏', Style{Fg: lineBg, Bg: th.Bg})
			if row.first {
				bi := row.block
				switch {
				case d.Partial:
					box := a.checkGlyph(Checked)
					if !d.chunkIncluded(bi) {
						box = a.checkGlyph(Unchecked)
					}
					v.Text(midX+1, i, box, Style{Fg: th.Accent, Bg: th.Bg})
					a.Hit(Rect{abs.X + midX, abs.Y + i, 3, 1}, func(m Mouse) tea.Cmd {
						if m.LeftPress() {
							d.toggleChunk(a, bi)
						}
						return nil
					})
				case re:
					v.Text(midX, i, " » ", Style{Fg: th.Fg, Bg: th.Bg, Bold: true})
					a.Hit(Rect{abs.X + midX, abs.Y + i, 3, 1}, func(m Mouse) tea.Cmd {
						if m.LeftPress() {
							return d.ApplyBlock(a, bi)
						}
						return nil
					})
				case le:
					v.Text(midX, i, " « ", Style{Fg: th.Fg, Bg: th.Bg, Bold: true})
					a.Hit(Rect{abs.X + midX, abs.Y + i, 3, 1}, func(m Mouse) tea.Cmd {
						if m.LeftPress() {
							return d.ApplyBlock(a, bi)
						}
						return nil
					})
				}
				if bi == d.cur && d.Focused && !d.Partial && !le && !re {
					v.Text(midX+1, i, "▸", Style{Fg: th.Accent, Bg: th.Bg})
				}
			}
		}
	}
}

func (d *DiffView) drawUnified(a *App, v View) {
	th := a.Th
	r := d.res
	nw := numWidth(max(len(r.Old), len(r.New)))
	textX := 2*nw + 5
	_, re := d.editable()
	abs := v.Abs()
	a.Selectable(Rect{abs.X + textX, abs.Y, v.W() - textX, v.H()}, true)
	for i := 0; i < v.H(); i++ {
		ri := d.top + i
		if ri >= len(d.rows) {
			break
		}
		row := d.rows[ri]
		base := Style{Fg: th.Fg, Bg: th.Bg}
		if row.kind == rowFold {
			st := Style{Fg: th.Dim, Bg: th.DiffFold}
			v.FillRect(0, i, v.W(), 1, st)
			label := fmt.Sprintf("⋯  %d unchanged lines  ⋯", row.foldLen)
			v.Text(max(0, (v.W()-Width(label))/2), i, label, st)
			continue
		}
		bg, word := th.Bg, th.Bg
		var text string
		var toks []tokSpan
		var words []diff.Span
		switch row.kind {
		case rowOld:
			bg, word = th.DiffDel, th.DiffDelWord
			text, toks, words = r.Old[row.o], hl(d.hlOld, row.o), d.wordOld[row.o]
		case rowNew:
			bg, word = th.DiffIns, th.DiffInsWord
			text, toks, words = r.New[row.n], hl(d.hlNew, row.n), d.wordNew[row.n]
		default:
			text, toks = r.New[row.n], hl(d.hlNew, row.n)
		}
		v.FillRect(0, i, v.W(), 1, Style{Bg: th.Bg})
		if row.o >= 0 {
			v.TextRight(nw, i, fmt.Sprint(row.o+1), base.WithFg(th.DiffGutterNum))
		}
		if row.n >= 0 {
			v.TextRight(2*nw+1, i, fmt.Sprint(row.n+1), base.WithFg(th.DiffGutterNum))
		}
		v.FillRect(textX-1, i, v.W()-textX+1, 1, Style{Bg: bg})
		if row.first && row.block >= 0 {
			bi := row.block
			switch {
			case d.Partial:
				box := a.checkGlyph(Checked)
				if !d.chunkIncluded(bi) {
					box = a.checkGlyph(Unchecked)
				}
				v.Text(2*nw+3, i, box, Style{Fg: th.Accent, Bg: th.Bg})
				a.Hit(Rect{abs.X + 2*nw + 2, abs.Y + i, 3, 1}, func(m Mouse) tea.Cmd {
					if m.LeftPress() {
						d.toggleChunk(a, bi)
					}
					return nil
				})
			case re:
				v.Text(2*nw+3, i, "↶", Style{Fg: th.Fg, Bg: th.Bg, Bold: true})
				a.Hit(Rect{abs.X + 2*nw + 2, abs.Y + i, 3, 1}, func(m Mouse) tea.Cmd {
					if m.LeftPress() {
						return d.ApplyBlock(a, bi)
					}
					return nil
				})
			}
		}
		d.drawText(v.Sub(textX, i, v.W()-textX, 1), text, toks, words, th.Fg, bg, word)
	}
}

func hl(h [][]tokSpan, i int) []tokSpan {
	if i < 0 || i >= len(h) {
		return nil
	}
	return h[i]
}

// drawText renders one source line with syntax colours, word highlights, tab expansion
// and the shared horizontal scroll offset.
func (d *DiffView) drawText(v View, line string, toks []tokSpan, words []diff.Span, fg, bg, wordBg Color) {
	line = strings.TrimSuffix(line, "\r")
	x := -d.left
	ti, wi := 0, 0
	for idx, r := range []rune(line) {
		for ti < len(toks) && toks[ti].end <= idx {
			ti++
		}
		for wi < len(words) && words[wi].End <= idx {
			wi++
		}
		st := Style{Fg: NoColor, Bg: bg}
		if ti < len(toks) && toks[ti].start <= idx {
			st.Fg = toks[ti].fg
			st.Italic = toks[ti].italic
		}
		if wi < len(words) && words[wi].Start <= idx {
			st.Bg = wordBg
		}
		if st.Fg == NoColor {
			st.Fg = fg
		}
		if r == '\t' {
			n := 4 - ((x + d.left) % 4)
			for k := 0; k < n; k++ {
				if x >= 0 && x < v.W() {
					v.Set(x, 0, ' ', st)
				}
				x++
			}
			continue
		}
		rw := RuneWidth(r)
		if rw == 0 {
			continue
		}
		if x >= 0 && x+rw <= v.W() {
			v.Set(x, 0, r, st)
		}
		x += rw
		if x >= v.W() {
			break
		}
	}
}

func (d *DiffView) drawScroll(a *App, v View) {
	th := a.Th
	h := v.H()
	total := len(d.rows)
	v.Fill(Style{Bg: th.ScrollTrack})
	if total == 0 {
		return
	}
	if total > h {
		thumbH := max(1, h*h/total)
		thumbY := (h - thumbH) * d.top / max(1, total-h)
		v.FillRect(0, thumbY, 1, thumbH, Style{Bg: th.ScrollThumb})
	}
	for ri, r := range d.rows {
		if !r.first || r.block < 0 {
			continue
		}
		y := ri * h / max(1, total)
		if total <= h {
			y = ri
		}
		lineBg, _, _ := d.blockColors(a, r.block)
		marker := blend(lineBg, th.Fg, 0.25)
		cell := v.Canvas().At(v.Abs().X, min(v.Canvas().H-1, v.Abs().Y+y))
		v.Set(0, y, '━', Style{Fg: marker, Bg: cell.S.Bg})
	}
	abs := v.Abs()
	jump := func(y int) {
		rel := y - abs.Y
		d.top = max(0, min(total-h, rel*total/max(1, h)-h/2))
	}
	a.Hit(abs, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			jump(m.Y)
			a.Drag(func(m Mouse) tea.Cmd { jump(m.Y); return nil })
		}
		return nil
	})
}
