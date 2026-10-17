package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/shelf"
)

type shelfFile struct {
	s *shelf.Shelf
	f *shelf.File
}

type ShelfTab struct {
	tree      *Tree
	diff      *DiffView
	preview   bool
	focusDiff bool
}

func NewShelfTab() *ShelfTab {
	t := &ShelfTab{tree: NewTree(), diff: NewDiffView()}
	t.tree.Focused = true
	t.tree.EmptyLines = []string{"No shelved changes", "", "Shelve changes from Local Changes with s.", "Shelves are shared with IntelliJ (.idea/shelf)."}
	t.tree.OnSelect = func(a *App, n *Node) tea.Cmd { return t.updatePreview(a) }
	t.tree.OnActivate = func(a *App, n *Node) tea.Cmd {
		if _, ok := n.Data.(shelfFile); ok {
			return t.openDiff(a)
		}
		return nil
	}
	t.tree.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { t.menu(a, x, y); return nil }
	return t
}

func (t *ShelfTab) Title() string  { return "Shelf" }
func (t *ShelfTab) Closable() bool { return false }
func (t *ShelfTab) Typing() bool   { return t.tree.Typing() }

func (t *ShelfTab) Hint() string {
	if t.focusDiff {
		return t.diff.Hint()
	}
	return "u unshelve · ⏎ diff · v preview · r rename · ⌦ delete · R restore · . menu"
}

func (t *ShelfTab) SelectedPath() string {
	if n := t.tree.Selected(); n != nil {
		if sf, ok := n.Data.(shelfFile); ok {
			return sf.f.Path
		}
	}
	return ""
}

func (t *ShelfTab) OnData(a *App) {
	th := a.Th
	var roots, deleted []*Node
	for _, s := range a.Shelves {
		n := &Node{ID: "s:" + s.Name, Label: s.Description, Extra: fmt.Sprintf("%s, %s", plural(len(s.Files), "file"), s.Date.Format("02/01/2006, 15:04")),
			Check: NoCheck, Data: s, Collapsed: true}
		for _, f := range s.Files {
			fn := &Node{ID: "f:" + s.Name + ":" + f.Path, Label: baseOf(f.Path), Extra: dirOf(f.Path),
				LabelFg: th.KindColor(f.Kind), Check: NoCheck, Data: shelfFile{s, f}}
			a.setFileIcon(fn, f.Path)
			n.Children = append(n.Children, fn)
		}
		if s.Recycled || s.Deleted {
			deleted = append(deleted, n)
		} else {
			roots = append(roots, n)
		}
	}
	if len(deleted) > 0 || len(roots) > 0 {
		roots = append(roots, &Node{ID: "recycled", Label: "Recently Deleted", Check: NoCheck, Children: deleted, Collapsed: true})
	}
	t.tree.SetRoots(roots)
}

func (t *ShelfTab) selectedShelf() *shelf.Shelf {
	n := t.tree.Selected()
	for n != nil {
		if s, ok := n.Data.(*shelf.Shelf); ok {
			return s
		}
		n = n.Parent()
	}
	return nil
}

func (t *ShelfTab) sources(a *App) ([]DiffSource, int) {
	n := t.tree.Selected()
	if n == nil {
		return nil, 0
	}
	sf, ok := n.Data.(shelfFile)
	if !ok {
		return nil, 0
	}
	var srcs []DiffSource
	idx := 0
	for _, f := range sf.s.Files {
		if f == sf.f {
			idx = len(srcs)
		}
		srcs = append(srcs, shelfSource(a.Repo, sf.s, f))
	}
	return srcs, idx
}

func (t *ShelfTab) updatePreview(a *App) tea.Cmd {
	if !t.preview {
		return nil
	}
	srcs, idx := t.sources(a)
	return t.diff.SetSources(a, srcs, idx, false)
}

func (t *ShelfTab) openDiff(a *App) tea.Cmd {
	srcs, idx := t.sources(a)
	if len(srcs) == 0 {
		return nil
	}
	title := "Shelf"
	if s := t.selectedShelf(); s != nil {
		title = "Shelf: " + s.Description
	}
	return a.OpenChanges(title, nil, srcs, idx, nil)
}

func (t *ShelfTab) unshelve(a *App) {
	s := t.selectedShelf()
	if s == nil {
		return
	}
	files := s.Files
	if n := t.tree.Selected(); n != nil {
		var picked []*shelf.File
		for _, x := range t.tree.SelectedNodes() {
			if sf, ok := x.Data.(shelfFile); ok {
				picked = append(picked, sf.f)
			}
		}
		if len(picked) > 0 {
			files = picked
		}
	}
	remove := true
	target := NewInput(a.CL.Active)
	d := &Dialog{Title: "Unshelve Changes", Width: 70}
	d.Fields = []Field{
		&LabelField{Text: fmt.Sprintf("Unshelve %s from \"%s\".", plural(len(files), "file"), s.Description)},
		&InputField{Label: "Changelist:", In: target},
		&CheckField{Label: "Remove successfully applied files from the shelf", Val: &remove},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Unshelve", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		list := strings.TrimSpace(target.Value())
		repo := a.Repo
		var res shelf.UnshelveResult
		return a.Op("Unshelve", func() error {
			var err error
			res, err = s.Unshelve(repo, files)
			if err != nil {
				return err
			}
			if remove && len(files) == len(s.Files) && len(res.Conflicts) == 0 {
				return s.SetRecycled(repo, true)
			}
			return nil
		}, func(a *App) tea.Cmd {
			if list != "" {
				a.CL.Add(list, "")
				a.CL.Move(res.Applied, list)
				a.CL.Save()
			}
			if len(res.Conflicts) > 0 {
				a.Error("Unshelved with conflicts", "These files changed since shelving; check them (conflict markers, or left as they were):\n"+strings.Join(res.Conflicts, "\n"))
			} else {
				a.Notify("Changes unshelved", s.Description)
			}
			return nil
		}), true
	}}}
	a.Push(d)
}

func (t *ShelfTab) delete(a *App) {
	s := t.selectedShelf()
	if s == nil {
		return
	}
	repo := a.Repo
	if s.Recycled || s.Deleted {
		a.Push(Confirm("Delete Permanently", fmt.Sprintf("Permanently delete shelf \"%s\"?", s.Description), "Delete", true, func(a *App) tea.Cmd {
			return a.Op("Delete shelf", s.Remove, nil)
		}))
		return
	}
	a.Push(Confirm("Delete Shelf", fmt.Sprintf("Move \"%s\" to Recently Deleted?", s.Description), "Delete", false, func(a *App) tea.Cmd {
		return a.Op("Delete shelf", func() error { return s.SetRecycled(repo, true) }, nil)
	}))
}

func (t *ShelfTab) menu(a *App, x, y int) {
	s := t.selectedShelf()
	none := s == nil
	recycled := s != nil && (s.Recycled || s.Deleted)
	items := []MenuItem{
		{Label: "Unshelve…", Hint: "u", Disabled: none, Do: func(a *App) tea.Cmd { t.unshelve(a); return nil }},
		{Label: "Show Diff", Hint: "⏎", Disabled: none, Do: func(a *App) tea.Cmd { return t.openDiff(a) }},
		{Label: "Jump to Source", Hint: "F4", Disabled: t.SelectedPath() == "", Do: func(a *App) tea.Cmd { return a.JumpToSource(t.SelectedPath(), 0) }},
		{Label: "Rename…", Hint: "r", Disabled: none, Do: func(a *App) tea.Cmd { t.rename(a); return nil }},
		MenuSep(),
		{Label: "Restore", Hint: "R", Disabled: !recycled, Do: func(a *App) tea.Cmd {
			return a.Op("Restore shelf", func() error { return s.SetRecycled(a.Repo, false) }, nil)
		}},
		{Label: "Delete", Hint: "⌦", Disabled: none, Do: func(a *App) tea.Cmd { t.delete(a); return nil }},
		MenuSep(),
		{Label: "Show Diff Preview", Hint: "v", Checked: t.preview, Do: func(a *App) tea.Cmd { t.preview = !t.preview; return t.updatePreview(a) }},
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (t *ShelfTab) rename(a *App) {
	s := t.selectedShelf()
	if s == nil {
		return
	}
	a.Push(Prompt("Rename Shelf", "Description:", s.Description, "Rename", func(a *App, v string) (tea.Cmd, string) {
		if strings.TrimSpace(v) == "" {
			return nil, "Description can't be empty"
		}
		return a.Op("Rename shelf", func() error { return s.Rename(a.Repo, v) }, nil), ""
	}))
}

func (t *ShelfTab) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	if keyName(k) == "tab" && t.preview {
		t.focusDiff = !t.focusDiff
		t.tree.Focused, t.diff.Focused = !t.focusDiff, t.focusDiff
		return nil, true
	}
	if t.focusDiff {
		if keyName(k) == "esc" {
			t.focusDiff = false
			t.tree.Focused, t.diff.Focused = true, false
			return nil, true
		}
		if cmd, ok := t.diff.Key(a, k); ok {
			return cmd, true
		}
	} else if cmd, ok := t.tree.Key(a, k); ok {
		return cmd, true
	}
	switch keyName(k) {
	case "f4", "e":
		if p := t.SelectedPath(); p != "" {
			return a.JumpToSource(p, 0), true
		}
	case "u", "ctrl+u":
		t.unshelve(a)
	case "v":
		t.preview = !t.preview
		if !t.preview {
			t.focusDiff = false
			t.tree.Focused = true
		}
		return t.updatePreview(a), true
	case "r", "shift+f6":
		t.rename(a)
	case "R":
		if s := t.selectedShelf(); s != nil && (s.Recycled || s.Deleted) {
			return a.Op("Restore shelf", func() error { return s.SetRecycled(a.Repo, false) }, nil), true
		}
	case "delete", "D", "backspace", "ctrl+backspace":
		t.delete(a)
	case ".", "shift+f10", "alt+enter":
		t.menu(a, -1, -1)
	default:
		return nil, false
	}
	return nil, true
}

func (t *ShelfTab) Draw(a *App, v View) {
	th := a.Th
	tb := v.Sub(0, 0, v.W(), 1)
	tb.Fill(Style{Bg: th.Bg})
	x := 1
	tool := func(icon string, on bool, fn func() tea.Cmd) {
		st := Style{Fg: th.Fg, Bg: th.Bg}
		if on {
			st.Bg = th.TabSelBg
		}
		tb.Text(x, 0, icon, st)
		a.Hit(Rect{tb.Abs().X + x, tb.Abs().Y, Width(icon), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				return fn()
			}
			return nil
		})
		x += Width(icon) + 2
	}
	tool("⇱ Unshelve", false, func() tea.Cmd { t.unshelve(a); return nil })
	tool("◉ Preview", t.preview, func() tea.Cmd { t.preview = !t.preview; return t.updatePreview(a) })
	tool("⊞", false, func() tea.Cmd { t.tree.ExpandAll(); return nil })
	tool("⊟", false, func() tea.Cmd { t.tree.CollapseAll(); return nil })
	// Only where it fits: the toolbar buttons come first.
	if loc := TruncateLeft(relHome(shelf.Dir(a.Repo)), v.W()-x-2); v.W()-x-2 > 12 {
		tb.TextRight(v.W()-1, 0, loc, Style{Fg: th.Faint, Bg: th.Bg})
	}
	body := v.Sub(0, 1, v.W(), v.H()-1)
	if !t.preview {
		t.tree.Draw(a, body)
		return
	}
	split := body.W() / 2
	tv := body.Sub(0, 0, split, body.H())
	t.tree.Draw(a, tv)
	body.VLine(split, 0, body.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
	dv := body.Sub(split+1, 0, body.W()-split-1, body.H())
	t.diff.Draw(a, dv)
	a.HitPass(tv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			t.focusDiff = false
			t.tree.Focused, t.diff.Focused = true, false
		}
		return nil
	})
	a.HitPass(dv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			t.focusDiff = true
			t.tree.Focused, t.diff.Focused = false, true
		}
		return nil
	})
}

var _ = git.Modified
