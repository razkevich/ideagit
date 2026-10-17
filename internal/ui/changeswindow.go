package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
)

// ChangesWindow is IntelliJ's multi-file diff window: a tree of changed files on the
// left (about a quarter of the width) and the diff of the selected file on the right.
type ChangesWindow struct {
	title     string
	header    []headerPart
	swap      func(a *App) tea.Cmd
	tree      *Tree
	diff      *DiffView
	srcs      []DiffSource
	focusTree bool
}

type headerPart struct {
	text string
	bold bool
}

// OpenChanges shows srcs with srcs[idx] selected; single files get the plain diff window.
func (a *App) OpenChanges(title string, header []headerPart, srcs []DiffSource, idx int, swap func(a *App) tea.Cmd) tea.Cmd {
	if len(srcs) == 0 {
		a.Notify("No differences", "")
		return nil
	}
	if len(srcs) == 1 && header == nil {
		w := NewDiffWindow(srcs[0].Path)
		a.Push(w)
		return w.diff.SetSources(a, srcs, 0, true)
	}
	w := &ChangesWindow{title: title, header: header, swap: swap, tree: NewTree(), diff: NewDiffView(), srcs: srcs, focusTree: true}
	w.diff.HidePath = true
	w.tree.Focused = true
	w.tree.OnSelect = func(a *App, n *Node) tea.Cmd {
		if i, ok := n.Data.(int); ok && i != w.diff.idx {
			return w.diff.SelectFile(a, i, 0)
		}
		return nil
	}
	w.tree.OnActivate = func(a *App, n *Node) tea.Cmd {
		if _, ok := n.Data.(int); ok {
			w.setFocus(false)
		}
		return nil
	}
	w.tree.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { w.fileMenu(a, x, y); return nil }
	w.diff.OnChange = func(a *App) tea.Cmd {
		w.tree.SelectID(fmt.Sprintf("c:%d", w.diff.idx))
		return nil
	}
	w.buildTree(a)
	w.tree.SelectID(fmt.Sprintf("c:%d", idx))
	a.Push(w)
	return w.diff.SetSources(a, srcs, idx, true)
}

func (w *ChangesWindow) buildTree(a *App) {
	type item struct {
		i   int
		src DiffSource
	}
	items := make([]item, len(w.srcs))
	for i, s := range w.srcs {
		items[i] = item{i, s}
	}
	th := a.Th
	leaf := func(it item) *Node {
		n := &Node{ID: fmt.Sprintf("c:%d", it.i), Label: baseOf(it.src.Path), LabelFg: th.KindColor(it.src.Kind), Check: NoCheck, Data: it.i}
		a.setFileIcon(n, it.src.Path)
		if !a.Cfg.GroupByDir {
			n.Extra = dirOf(it.src.Path)
		}
		return n
	}
	var kids []*Node
	if a.Cfg.GroupByDir {
		kids = groupByDir(a, "changes", items, leaf, func(it item) string { return it.src.Path })
	} else {
		for _, it := range items {
			kids = append(kids, leaf(it))
		}
	}
	root := &Node{ID: "c:root", Label: a.Repo.Name, Extra: plural(len(w.srcs), "file"), Bold: true, Check: NoCheck, Children: kids}
	if a.icons {
		root.Icon, root.IconFg = glyphRepo, th.Fg
	}
	w.tree.SetRoots([]*Node{root})
}

func (w *ChangesWindow) regroup(a *App) {
	w.buildTree(a)
	w.tree.SelectID(fmt.Sprintf("c:%d", w.diff.idx))
}

func (w *ChangesWindow) setFocus(tree bool) {
	w.focusTree = tree
	w.tree.Focused = tree
	w.diff.Focused = !tree
}

func (w *ChangesWindow) Hint() string {
	if w.focusTree {
		return "↑↓ file · ⏎/tab diff · F7 next change · F4 source · H history · y copy path · g tree/list · . menu · esc close"
	}
	return w.diff.Hint() + " · tab tree · esc close"
}

func (w *ChangesWindow) Key(a *App, k tea.KeyMsg) tea.Cmd {
	switch keyName(k) {
	case "esc", "q", "ctrl+w", "ctrl+f4":
		if w.focusTree && w.tree.Typing() {
			break
		}
		a.Pop(w)
		return nil
	case "tab", "shift+tab":
		w.setFocus(!w.focusTree)
		return nil
	case "f7", "shift+f7", "n", "N":
		cmd, _ := w.diff.Key(a, k)
		return cmd
	case "f4", "e":
		if s := w.diff.Source(); s != nil {
			return a.JumpToSource(s.Path, w.diff.currentLine())
		}
	}
	if w.focusTree {
		if cmd, ok := w.tree.Key(a, k); ok {
			return cmd
		}
		cmd, _ := fileKey(a, keyName(k), w.selectedPath(), w.showDiff, func() { w.fileMenu(a, -1, -1) })
		return cmd
	}
	cmd, _ := w.diff.Key(a, k)
	return cmd
}

func (w *ChangesWindow) Draw(a *App, screen View) {
	th := a.Th
	v := screen.Sub(0, 0, screen.W(), screen.H()-1)
	v.Fill(Style{Bg: th.Bg})
	title := v.Sub(0, 0, v.W(), 1)
	title.Fill(Style{Bg: th.HeaderBg})
	title.TextFit(1, 0, w.title, v.W()-6, Style{Fg: th.Fg, Bg: th.HeaderBg, Bold: true})
	title.Text(v.W()-3, 0, "×", Style{Fg: th.Fg, Bg: th.HeaderBg})
	a.Hit(Rect{title.Abs().X + v.W() - 4, title.Abs().Y, 3, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.Pop(w)
		}
		return nil
	})
	y := 1
	if len(w.header) > 0 {
		hv := v.Sub(0, y, v.W(), 1)
		hv.Fill(Style{Bg: th.Bg})
		x := 1
		for _, p := range w.header {
			x = hv.Text(x, 0, p.text, Style{Fg: th.Fg, Bg: th.Bg, Bold: p.bold})
		}
		if w.swap != nil {
			x += 3
			label := "Swap branches"
			hv.Text(x, 0, label, Style{Fg: th.Link, Bg: th.Bg})
			a.Hit(Rect{hv.Abs().X + x, hv.Abs().Y, Width(label), 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					a.Pop(w)
					return w.swap(a)
				}
				return nil
			})
		}
		y++
	}
	body := v.Sub(0, y, v.W(), v.H()-y)
	tw := max(24, min(body.W()/4, 60))
	tv := body.Sub(0, 0, tw, body.H())
	drawFilesToolbar(a, tv.Sub(0, 0, tw, 1), "", w.tree)
	w.tree.Draw(a, tv.Sub(0, 1, tw, tv.H()-1))
	a.HitPass(tv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() || m.RightPress() {
			w.setFocus(true)
		}
		return nil
	})
	body.VLine(tw, 0, body.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
	dv := body.Sub(tw+1, 0, body.W()-tw-1, body.H())
	w.diff.Draw(a, dv)
	a.HitPass(dv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			w.setFocus(false)
		}
		return nil
	})
}

func (w *ChangesWindow) selectedPath() string {
	if n := w.tree.Selected(); n != nil {
		if i, ok := n.Data.(int); ok && i < len(w.srcs) {
			return w.srcs[i].Path
		}
	}
	return ""
}

func (w *ChangesWindow) showDiff(a *App) tea.Cmd {
	w.setFocus(false)
	return nil
}

func (w *ChangesWindow) fileMenu(a *App, x, y int) {
	if p := w.selectedPath(); p != "" {
		showFileMenu(a, fileItems(a, p, w.showDiff), x, y)
	}
}
