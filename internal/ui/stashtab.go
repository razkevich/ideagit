package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "Today " + t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday " + t.Format("15:04")
	}
	return t.Format("02/01/2006, 15:04")
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("02/01/2006")
}

type stashFocus int

const (
	sfList stashFocus = iota
	sfFiles
	sfDiff
)

type StashTab struct {
	list     *Tree
	files    *Tree
	diff     *DiffView
	focus    stashFocus
	cur      *git.Stash
	curFiles []git.ChangedFile
	gen      int
}

func NewStashTab() *StashTab {
	t := &StashTab{list: NewTree(), files: NewTree(), diff: NewDiffView()}
	t.list.Focused = true
	t.list.EmptyLines = []string{"No stashes", "", "Stash changes from Local Changes with z"}
	t.files.Empty = "Select a stash to view changes"
	t.list.OnSelect = func(a *App, n *Node) tea.Cmd { return t.loadFiles(a) }
	t.list.OnActivate = func(a *App, n *Node) tea.Cmd { t.setFocus(sfFiles); return nil }
	t.list.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { t.menu(a, x, y); return nil }
	t.files.OnSelect = func(a *App, n *Node) tea.Cmd { return t.preview(a, false) }
	t.files.OnActivate = func(a *App, n *Node) tea.Cmd { return t.preview(a, true) }
	return t
}

func (t *StashTab) Title() string  { return "Stash" }
func (t *StashTab) Closable() bool { return false }
func (t *StashTab) Typing() bool   { return t.list.Typing() || t.files.Typing() }

func (t *StashTab) Hint() string {
	if t.focus == sfDiff {
		return t.diff.Hint()
	}
	return "a apply · p pop · ⌦ drop · B unstash as branch · tab switch pane · . menu"
}

func (t *StashTab) SelectedPath() string {
	if n := t.files.Selected(); n != nil {
		if f, ok := n.Data.(git.ChangedFile); ok {
			return f.Path
		}
	}
	return ""
}

func (t *StashTab) OnData(a *App) {
	th := a.Th
	var nodes []*Node
	for i := range a.Stashes {
		s := a.Stashes[i]
		n := &Node{ID: "s:" + s.Hash, Label: s.Message, Extra: fmtDate(s.Date), Check: NoCheck, Data: s}
		if s.Branch != "" {
			n.Right = "◇ " + s.Branch
			n.RightFg = th.RefLocal
		}
		nodes = append(nodes, n)
	}
	t.list.SetRoots(nodes)
	if t.cur == nil || t.list.Selected() == nil || t.list.Selected().Data.(git.Stash).Hash != t.cur.Hash {
		a.pendingCmds = append(a.pendingCmds, t.list.NotifySelection(a))
	}
}

func (t *StashTab) selected() *git.Stash {
	n := t.list.Selected()
	if n == nil {
		return nil
	}
	s := n.Data.(git.Stash)
	return &s
}

func (t *StashTab) loadFiles(a *App) tea.Cmd {
	s := t.selected()
	t.cur = s
	if s == nil {
		t.files.SetRoots(nil)
		return t.diff.SetSources(a, nil, 0, false)
	}
	t.gen++
	gen := t.gen
	stash := *s
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		files, _, err := repo.StashFiles(stash)
		return func(a *App) tea.Cmd {
			if gen != t.gen {
				return nil
			}
			if err != nil {
				a.Error("Cannot load stash", err.Error())
				return nil
			}
			t.curFiles = files
			t.files.SetRoots(changedFileNodes(a, "stash", files))
			t.files.SelectFirstLeaf()
			return t.preview(a, false)
		}
	})
}

func changedFileNodes(a *App, prefix string, files []git.ChangedFile) []*Node {
	th := a.Th
	leaf := func(f git.ChangedFile) *Node {
		n := &Node{ID: "f:" + f.Path, Label: baseOf(f.Path), LabelFg: th.KindColor(f.Kind), Check: NoCheck, Data: f}
		if !a.Cfg.GroupByDir {
			n.Extra = dirOf(f.Path)
		}
		if f.OrigPath != "" {
			n.Extra = n.Extra + "  moved from " + f.OrigPath
		}
		return n
	}
	if !a.Cfg.GroupByDir {
		var res []*Node
		for _, f := range files {
			res = append(res, leaf(f))
		}
		return res
	}
	return groupByDir(a, prefix, files, leaf, func(f git.ChangedFile) string { return f.Path })
}

func (t *StashTab) preview(a *App, full bool) tea.Cmd {
	if t.cur == nil {
		return nil
	}
	n := t.files.Selected()
	if n == nil {
		return nil
	}
	f, ok := n.Data.(git.ChangedFile)
	if !ok {
		return nil
	}
	var srcs []DiffSource
	idx := 0
	for _, x := range t.curFiles {
		if x.Path == f.Path {
			idx = len(srcs)
		}
		srcs = append(srcs, stashSource(a.Repo, *t.cur, x))
	}
	if full {
		w := NewDiffWindow(f.Path)
		a.Push(w)
		return w.diff.SetSources(a, srcs, idx, true)
	}
	return t.diff.SetSources(a, srcs, idx, false)
}

func (t *StashTab) setFocus(f stashFocus) {
	t.focus = f
	t.list.Focused = f == sfList
	t.files.Focused = f == sfFiles
	t.diff.Focused = f == sfDiff
}

func (t *StashTab) apply(a *App, pop, index bool) tea.Cmd {
	s := t.selected()
	if s == nil {
		return nil
	}
	stash := *s
	verb := "Apply"
	if pop {
		verb = "Pop"
	}
	return a.Op(verb+" stash", func() error { return a.Repo.StashApply(stash, pop, index) }, func(a *App) tea.Cmd {
		a.Notify("Stash "+map[bool]string{true: "popped", false: "applied"}[pop], stash.Message)
		return nil
	})
}

func (t *StashTab) drop(a *App) {
	s := t.selected()
	if s == nil {
		return
	}
	stash := *s
	a.Push(Confirm("Drop Stash", fmt.Sprintf("Drop %s \"%s\"?", stash.Ref, stash.Message), "Drop", true, func(a *App) tea.Cmd {
		return a.Op("Drop stash", func() error { return a.Repo.StashDrop(stash) }, nil)
	}))
}

func (t *StashTab) asBranch(a *App) {
	s := t.selected()
	if s == nil {
		return
	}
	stash := *s
	a.Push(Prompt("Unstash as Branch", "Branch name:", "", "Create", func(a *App, v string) (tea.Cmd, string) {
		if v == "" {
			return nil, "Branch name can't be empty"
		}
		return a.Op("Unstash as branch", func() error { return a.Repo.StashBranch(stash, v) }, nil), ""
	}))
}

func (t *StashTab) menu(a *App, x, y int) {
	none := t.selected() == nil
	items := []MenuItem{
		{Label: "Apply Stash", Hint: "a", Disabled: none, Do: func(a *App) tea.Cmd { return t.apply(a, false, false) }},
		{Label: "Apply Stash and Reinstate Index", Disabled: none, Do: func(a *App) tea.Cmd { return t.apply(a, false, true) }},
		{Label: "Pop Stash", Hint: "p", Disabled: none, Do: func(a *App) tea.Cmd { return t.apply(a, true, false) }},
		{Label: "Unstash as Branch…", Hint: "B", Disabled: none, Do: func(a *App) tea.Cmd { t.asBranch(a); return nil }},
		MenuSep(),
		{Label: "Drop Stash", Hint: "⌦", Disabled: none, Do: func(a *App) tea.Cmd { t.drop(a); return nil }},
		{Label: "Clear All Stashes…", Disabled: none, Do: func(a *App) tea.Cmd {
			a.Push(Confirm("Clear Stashes", "Remove all stashes? This can't be undone.", "Clear", true, func(a *App) tea.Cmd {
				return a.Op("Clear stashes", a.Repo.StashClear, nil)
			}))
			return nil
		}},
		MenuSep(),
		{Label: "Stash Changes…", Do: func(a *App) tea.Cmd { OpenStashDialog(a, nil); return nil }},
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (t *StashTab) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "tab":
		t.setFocus((t.focus + 1) % 3)
		return nil, true
	case "shift+tab":
		t.setFocus((t.focus + 2) % 3)
		return nil, true
	}
	switch t.focus {
	case sfList:
		if cmd, ok := t.list.Key(a, k); ok {
			return cmd, true
		}
	case sfFiles:
		if cmd, ok := t.files.Key(a, k); ok {
			return cmd, true
		}
	case sfDiff:
		if k.String() == "esc" {
			t.setFocus(sfFiles)
			return nil, true
		}
		if cmd, ok := t.diff.Key(a, k); ok {
			return cmd, true
		}
	}
	switch k.String() {
	case "a":
		return t.apply(a, false, false), true
	case "p":
		return t.apply(a, true, false), true
	case "delete", "D", "backspace":
		t.drop(a)
	case "B":
		t.asBranch(a)
	case "z":
		OpenStashDialog(a, nil)
	case ".", "shift+f10":
		t.menu(a, -1, -1)
	default:
		return nil, false
	}
	return nil, true
}

func (t *StashTab) Draw(a *App, v View) {
	th := a.Th
	split := int(float64(v.W()) * a.Cfg.StashSplit)
	split = max(24, min(v.W()-30, split))
	left := v.Sub(0, 0, split, v.H())
	listH := max(3, min(len(a.Stashes)+1, left.H()*2/5))
	if len(a.Stashes) == 0 {
		listH = left.H() / 2
	}
	t.list.Draw(a, left.Sub(0, 0, split, listH))
	a.HitPass(left.Sub(0, 0, split, listH).Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() || m.RightPress() {
			t.setFocus(sfList)
		}
		return nil
	})
	left.HLine(0, listH, split, '─', Style{Fg: th.Border, Bg: th.Bg})
	tb := left.Sub(0, listH+1, split, 1)
	tb.Fill(Style{Bg: th.Bg})
	x := 1
	tool := func(icon, tip string, fn func() tea.Cmd) {
		tb.Text(x, 0, icon, Style{Fg: th.Fg, Bg: th.Bg})
		a.Hit(Rect{tb.Abs().X + x, tb.Abs().Y, Width(icon), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				return fn()
			}
			return nil
		})
		x += Width(icon) + 2
	}
	tool("⇱ Unstash", "", func() tea.Cmd { return t.apply(a, false, false) })
	tool("▤", "", func() tea.Cmd {
		a.Cfg.GroupByDir = !a.Cfg.GroupByDir
		a.Cfg.Save()
		t.files.SetRoots(changedFileNodes(a, "stash", t.curFiles))
		return nil
	})
	tool("⌄", "", func() tea.Cmd { t.files.ExpandAll(); return nil })
	tool("⌃", "", func() tea.Cmd { t.files.CollapseAll(); return nil })
	filesY := listH + 2
	filesH := left.H() - filesY - 2
	fv := left.Sub(0, filesY, split, filesH)
	t.files.Draw(a, fv)
	a.HitPass(fv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			t.setFocus(sfFiles)
		}
		return nil
	})
	by := left.H() - 1
	left.FillRect(0, by-1, split, 2, Style{Bg: th.Bg})
	bx := 1
	bx += DrawButton(a, left, bx, by, "Apply", true, false, func() tea.Cmd { return t.apply(a, false, false) }) + 1
	bx += DrawButton(a, left, bx, by, "Pop", false, false, func() tea.Cmd { return t.apply(a, true, false) }) + 1
	DrawButton(a, left, bx, by, "Drop", false, false, func() tea.Cmd { t.drop(a); return nil })

	v.VLine(split, 0, v.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
	abs := v.Abs()
	a.Hit(Rect{abs.X + split, abs.Y, 1, v.H()}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.Drag(func(m Mouse) tea.Cmd {
				a.Cfg.StashSplit = max(0.2, min(0.8, float64(m.X-abs.X)/float64(max(1, v.W()))))
				return nil
			})
		}
		return nil
	})
	dv := v.Sub(split+1, 0, v.W()-split-1, v.H())
	if t.cur == nil {
		dv.Fill(Style{Bg: th.Bg})
		dv.Text(max(0, (dv.W()-28)/2), dv.H()/3, "Select a stash to view changes", Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	t.diff.Draw(a, dv)
	a.HitPass(dv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			t.setFocus(sfDiff)
		}
		return nil
	})
}
