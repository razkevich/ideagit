package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

// drawFilesToolbar is the toolbar every changed-files tree shares: an optional label,
// then on the right IntelliJ's view toggle (grouped by directory or a flat list) and,
// in tree view, expand/collapse all.
func drawFilesToolbar(a *App, v View, label string, tree *Tree) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	if label != "" {
		v.TextFit(1, 0, label, v.W()-16, Style{Fg: th.Dim, Bg: th.Bg})
	}
	treeIcon, listIcon := "▤", "☰"
	if a.icons {
		treeIcon, listIcon = "\U000f0645", "\uf03a"
	}
	x := v.W() - 1
	button := func(glyph, tip string, on bool, fn func()) {
		x -= Width(glyph)
		st := Style{Fg: th.Dim, Bg: th.Bg}
		if on {
			st = Style{Fg: th.Fg, Bg: th.Sel}
		}
		v.Text(x, 0, glyph, st)
		a.Hit(Rect{v.Abs().X + x, v.Abs().Y, Width(glyph), 1}, func(m Mouse) tea.Cmd {
			if m.Action == tea.MouseActionMotion {
				a.tip = tip
			}
			if m.LeftPress() {
				fn()
			}
			return nil
		})
		x -= 2
	}
	if a.Cfg.GroupByDir {
		button("⊟", "Collapse All (ctrl+-)", false, tree.CollapseAll)
		button("⊞", "Expand All (ctrl+=)", false, tree.ExpandAll)
		v.Text(x, 0, "│", Style{Fg: th.Border, Bg: th.Bg})
		x -= 2
	}
	button(listIcon, "Show as List (g)", !a.Cfg.GroupByDir, func() { a.setGroupByDir(false) })
	button(treeIcon, "Group by Directory (g)", a.Cfg.GroupByDir, func() { a.setGroupByDir(true) })
}

// setGroupByDir switches every file tree between grouped and flat at once, since it is
// one setting.
func (a *App) setGroupByDir(group bool) {
	if a.Cfg.GroupByDir == group {
		return
	}
	a.Cfg.GroupByDir = group
	a.Cfg.Save()
	a.regroupFiles()
}

func (a *App) regroupFiles() {
	for _, t := range a.tabs {
		if r, ok := t.(interface{ regroup(*App) }); ok {
			r.regroup(a)
		}
	}
	for _, o := range a.overlays {
		if r, ok := o.(interface{ regroup(*App) }); ok {
			r.regroup(a)
		}
	}
}

const filesHint = "⏎ diff · F4 source · H history · y copy path · g tree/list · . menu · tab next pane"

// fileItems is the context menu every changed-files tree starts with; callers append
// their own revision-specific actions.
func fileItems(a *App, path string, diff func(a *App) tea.Cmd) []MenuItem {
	return []MenuItem{
		{Label: "Show Diff", Hint: "ctrl+d", Do: diff},
		{Label: "Jump to Source", Hint: "F4", Do: func(a *App) tea.Cmd { return a.JumpToSource(path, 0) }},
		{Label: "Show History", Hint: "H", Do: func(a *App) tea.Cmd { return a.OpenLog(git.LogFilter{Paths: []string{path}}) }},
		MenuSep(),
		{Label: "Copy Absolute Path", Hint: "y", Do: func(a *App) tea.Cmd { return copyToClipboard(a, a.Repo.Abs(path)) }},
		{Label: "Copy Path From Repository Root", Do: func(a *App) tea.Cmd { return copyToClipboard(a, path) }},
	}
}

func showFileMenu(a *App, items []MenuItem, x, y int) {
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

// fileKey handles the keys every changed-files tree shares. path is the selected file
// ("" for a folder); menu opens the tree's context menu.
func fileKey(a *App, key, path string, diff func(a *App) tea.Cmd, menu func()) (tea.Cmd, bool) {
	switch key {
	case "g":
		a.setGroupByDir(!a.Cfg.GroupByDir)
		return nil, true
	case ".", "x", "shift+f10", "alt+enter":
		menu()
		return nil, true
	case "ctrl+d":
		return diff(a), true
	}
	if path == "" {
		return nil, false
	}
	switch key {
	case "f4", "e", "ctrl+down":
		return a.JumpToSource(path, 0), true
	case "H":
		return a.OpenLog(git.LogFilter{Paths: []string{path}}), true
	case "y", "ctrl+shift+c":
		return copyToClipboard(a, a.Repo.Abs(path)), true
	}
	return nil, false
}

func selectedFilePath(t *Tree) string {
	if n := t.Selected(); n != nil {
		if f, ok := n.Data.(git.ChangedFile); ok {
			return f.Path
		}
	}
	return ""
}
