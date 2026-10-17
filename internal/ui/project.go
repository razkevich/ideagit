package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

// ProjectPane is IntelliJ's Project tool window: the repository's file tree.
type ProjectPane struct {
	tree *Tree
}

type projEntry struct {
	rel string
	dir bool
}

func NewProjectPane() *ProjectPane {
	p := &ProjectPane{tree: NewTree()}
	p.tree.OnActivate = func(a *App, n *Node) tea.Cmd {
		if e, ok := n.Data.(projEntry); ok && !e.dir {
			return a.OpenInEditor(e.rel, 0)
		}
		return nil
	}
	p.tree.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { p.menu(a, x, y); return nil }
	return p
}

func (p *ProjectPane) OnData(a *App) {
	th := a.Th
	kinds := map[string]git.ChangeKind{}
	var untrackedDirs []string
	if a.Status != nil {
		for _, f := range a.Status.Files {
			kinds[f.Path] = f.Kind
			if f.Kind == git.Untracked && f.IsDir {
				untrackedDirs = append(untrackedDirs, f.Path+"/")
			}
		}
	}
	colorOf := func(rel string, dir bool) Color {
		if k, ok := kinds[rel]; ok {
			return th.KindColor(k)
		}
		for _, d := range untrackedDirs {
			if strings.HasPrefix(rel+"/", d) {
				return th.Unversioned
			}
		}
		return NoColor
	}
	var list func(rel string) []*Node
	list = func(rel string) []*Node {
		entries, err := os.ReadDir(filepath.Join(a.Repo.Root, rel))
		if err != nil {
			return nil
		}
		sort.SliceStable(entries, func(i, j int) bool {
			di, dj := entries[i].IsDir(), entries[j].IsDir()
			if di != dj {
				return di
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		var nodes []*Node
		for _, e := range entries {
			name := e.Name()
			if name == ".git" || name == ".DS_Store" || a.Cfg.ProjectHideDots && strings.HasPrefix(name, ".") {
				continue
			}
			child := filepath.ToSlash(filepath.Join(rel, name))
			isDir := e.IsDir()
			if e.Type()&os.ModeSymlink != 0 {
				if fi, err := os.Stat(filepath.Join(a.Repo.Root, child)); err == nil {
					isDir = fi.IsDir()
				}
			}
			n := &Node{ID: "p:" + child, Label: name, LabelFg: colorOf(child, isDir), Check: NoCheck, Data: projEntry{child, isDir}}
			if isDir {
				n.Icon, n.IconFg = a.dirIcon(false)
				n.IconOpen, _ = a.dirIcon(true)
				n.Collapsed = true
				c := child
				n.Lazy = func() []*Node { return list(c) }
				if strings.HasPrefix(name, ".") && n.LabelFg == NoColor {
					n.LabelFg = th.Dim
				}
			} else {
				a.setFileIcon(n, child)
			}
			nodes = append(nodes, n)
		}
		return nodes
	}
	if a.Cfg.ProjectOpenFiles {
		p.tree.SetRoots(p.openFiles(a, colorOf))
		return
	}
	root := &Node{ID: "p:", Label: a.Repo.Name, Extra: relHome(a.Repo.Root), Bold: true, Icon: "▣", Check: NoCheck, Data: projEntry{"", true}}
	if a.icons {
		root.Icon, root.IconFg = glyphRepo, th.Fg
	}
	root.Lazy = func() []*Node { return list("") }
	p.tree.SetRoots([]*Node{root})
}

// openFiles lists the editor's buffers, IntelliJ's "Open Files" view.
func (p *ProjectPane) openFiles(a *App, colorOf func(string, bool) Color) []*Node {
	if a.editor == nil || !a.editor.Running() {
		return nil
	}
	bufs, _ := a.editor.Buffers()
	var nodes []*Node
	for _, b := range bufs {
		rel, err := filepath.Rel(a.Repo.Root, b.path)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		n := &Node{ID: "o:" + rel, Label: baseOf(rel), Extra: dirOf(rel), LabelFg: colorOf(rel, false), Check: NoCheck, Data: projEntry{rel, false}}
		if b.modified {
			n.Right = "●"
		}
		a.setFileIcon(n, rel)
		nodes = append(nodes, n)
	}
	return nodes
}

// Reveal expands the parents of rel and selects it, like "Select Opened File".
func (p *ProjectPane) Reveal(a *App, rel string) {
	parts := strings.Split(rel, "/")
	p.tree.exp["p:"] = true
	for i := 1; i < len(parts); i++ {
		p.tree.exp["p:"+strings.Join(parts[:i], "/")] = true
	}
	p.OnData(a)
	p.tree.SelectID("p:" + rel)
}

func (p *ProjectPane) selected() *projEntry {
	if n := p.tree.Selected(); n != nil {
		if e, ok := n.Data.(projEntry); ok {
			return &e
		}
	}
	return nil
}

func (p *ProjectPane) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	if cmd, ok := p.tree.Key(a, k); ok {
		return cmd, true
	}
	e := p.selected()
	switch keyName(k) {
	case "f4", "e", "ctrl+down":
		if e != nil && !e.dir {
			return a.OpenInEditor(e.rel, 0), true
		}
	case "H":
		if e != nil {
			return a.OpenLog(git.LogFilter{Paths: []string{firstNonEmpty(e.rel, ".")}}), true
		}
	case "y":
		if e != nil {
			return copyToClipboard(a, a.Repo.Abs(e.rel)), true
		}
	case "alt+insert", "ctrl+n":
		p.newMenu(a, -1, -1)
	case "n":
		p.newEntry(a, false)
	case "N":
		p.newEntry(a, true)
	case "delete", "D", "ctrl+backspace":
		p.delete(a)
	case "shift+f6", "r":
		p.rename(a)
	case ".", "shift+f10", "alt+enter":
		p.menu(a, -1, -1)
	default:
		return nil, false
	}
	return nil, true
}

func (p *ProjectPane) Hint() string {
	return "⏎ open · n new file · N new directory · r rename · ⌦ delete · H history · y copy path · . menu · alt+1 hide"
}

func (p *ProjectPane) baseDir() string {
	e := p.selected()
	if e == nil {
		return ""
	}
	if e.dir {
		return e.rel
	}
	return dirOf(e.rel)
}

func (p *ProjectPane) newEntry(a *App, dir bool) {
	base := p.baseDir()
	title, label := "New File", "File name:"
	if dir {
		title, label = "New Directory", "Directory name:"
	}
	a.Push(Prompt(title, label, "", "Create", func(a *App, v string) (tea.Cmd, string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, "Name can't be empty"
		}
		rel := filepath.ToSlash(filepath.Join(base, v))
		abs := a.Repo.Abs(rel)
		if _, err := os.Stat(abs); err == nil {
			return nil, rel + " already exists"
		}
		if dir {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return nil, err.Error()
			}
			p.Reveal(a, rel)
			return a.Refresh(), ""
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err.Error()
		}
		if err := os.WriteFile(abs, nil, 0o644); err != nil {
			return nil, err.Error()
		}
		p.Reveal(a, rel)
		return tea.Batch(a.Refresh(), a.OpenInEditor(rel, 0)), ""
	}))
}

func (p *ProjectPane) rename(a *App) {
	e := p.selected()
	if e == nil || e.rel == "" {
		return
	}
	a.Push(Prompt("Rename", "New name:", baseOf(e.rel), "Rename", func(a *App, v string) (tea.Cmd, string) {
		v = strings.TrimSpace(v)
		if v == "" || strings.Contains(v, "/") {
			return nil, "Enter a name without slashes"
		}
		to := filepath.ToSlash(filepath.Join(dirOf(e.rel), v))
		if _, err := os.Lstat(a.Repo.Abs(to)); err == nil {
			return nil, to + " already exists"
		}
		if err := os.Rename(a.Repo.Abs(e.rel), a.Repo.Abs(to)); err != nil {
			return nil, err.Error()
		}
		p.Reveal(a, to)
		return a.Refresh(), ""
	}))
}

func (p *ProjectPane) delete(a *App) {
	e := p.selected()
	if e == nil || e.rel == "" {
		return
	}
	what := "file"
	if e.dir {
		what = "directory"
	}
	a.Push(Confirm("Delete", "Delete "+what+" "+e.rel+"?", "Delete", true, func(a *App) tea.Cmd {
		if err := os.RemoveAll(a.Repo.Abs(e.rel)); err != nil {
			a.Error("Delete failed", err.Error())
		}
		return a.Refresh()
	}))
}

func (p *ProjectPane) menu(a *App, x, y int) {
	e := p.selected()
	none := e == nil
	file := e != nil && !e.dir
	var changed *git.FileStatus
	if file && a.Status != nil {
		for _, f := range a.Status.Files {
			if f.Path == e.rel {
				f := f
				changed = &f
			}
		}
	}
	items := []MenuItem{
		{Label: "Open", Hint: "⏎", Disabled: !file, Do: func(a *App) tea.Cmd { return a.OpenInEditor(e.rel, 0) }},
		{Label: "Open in External Editor", Disabled: !file, Do: func(a *App) tea.Cmd { return a.ExternalEdit(e.rel, 0) }},
		MenuSep(),
		{Label: "New File…", Hint: "n", Do: func(a *App) tea.Cmd { p.newEntry(a, false); return nil }},
		{Label: "New Directory…", Hint: "N", Do: func(a *App) tea.Cmd { p.newEntry(a, true); return nil }},
		{Label: "Rename…", Hint: "r", Disabled: none || e.rel == "", Do: func(a *App) tea.Cmd { p.rename(a); return nil }},
		{Label: "Delete…", Hint: "⌦", Disabled: none || e.rel == "", Do: func(a *App) tea.Cmd { p.delete(a); return nil }},
		MenuSep(),
		{Label: "Show Diff", Disabled: changed == nil, Do: func(a *App) tea.Cmd {
			w := NewDiffWindow(e.rel)
			a.Push(w)
			return w.diff.SetSources(a, []DiffSource{localSource(a.Repo, a.headHash(), *changed)}, 0, true)
		}},
		{Label: "Rollback…", Disabled: changed == nil || changed.Kind == git.Untracked, Do: func(a *App) tea.Cmd {
			OpenRollback(a, []git.FileStatus{*changed})
			return nil
		}},
		{Label: "Show History", Hint: "H", Disabled: none, Do: func(a *App) tea.Cmd {
			return a.OpenLog(git.LogFilter{Paths: []string{firstNonEmpty(e.rel, ".")}})
		}},
		MenuSep(),
		{Label: "Copy Absolute Path", Hint: "y", Disabled: none, Do: func(a *App) tea.Cmd { return copyToClipboard(a, a.Repo.Abs(e.rel)) }},
		{Label: "Copy Path From Repository Root", Disabled: none, Do: func(a *App) tea.Cmd { return copyToClipboard(a, e.rel) }},
		{Label: "Open Shell Here", Disabled: none, Do: func(a *App) tea.Cmd {
			dir := e.rel
			if file {
				dir = dirOf(e.rel)
			}
			return a.OpenShell(a.Repo.Abs(dir))
		}},
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (p *ProjectPane) Draw(a *App, v View, focused bool) {
	th := a.Th
	hdr := v.Sub(0, 0, v.W(), 1)
	hdr.Fill(Style{Bg: th.Bg})
	title := "Project"
	if a.Cfg.ProjectOpenFiles {
		title = "Open Files"
	}
	tx := hdr.Text(1, 0, title, Style{Fg: th.Fg, Bg: th.Bg, Bold: true})
	tx = hdr.Text(tx+1, 0, "⌄", Style{Fg: th.Dim, Bg: th.Bg})
	a.Hit(Rect{hdr.Abs().X + 1, hdr.Abs().Y, tx, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			p.viewMenu(a, hdr.Abs().X+1, hdr.Abs().Y+1)
		}
		return nil
	})
	// IntelliJ's Project toolbar, right to left: hide, options, collapse, expand, select opened, new.
	x := v.W() - 1
	button := func(glyph, tip string, fn func() tea.Cmd) {
		x -= Width(glyph)
		bx := x
		hdr.Text(bx, 0, glyph, Style{Fg: th.Fg, Bg: th.Bg})
		a.Hit(Rect{hdr.Abs().X + bx, hdr.Abs().Y, Width(glyph), 1}, func(m Mouse) tea.Cmd {
			if m.Action == tea.MouseActionMotion {
				a.tip = tip
				return nil
			}
			if m.LeftPress() {
				return fn()
			}
			return nil
		})
		x -= 2
	}
	button("–", "Hide (alt+1, shift+esc)", func() tea.Cmd { a.toggleProject(); return nil })
	button("⋮", "Options", func() tea.Cmd { p.optionsMenu(a, hdr.Abs().X+x-20, hdr.Abs().Y+1); return nil })
	button("⊟", "Collapse All (ctrl+-)", func() tea.Cmd { p.tree.CollapseAll(); p.tree.SetExpanded(p.tree.Roots[0], true); return nil })
	button("⊞", "Expand Selected (ctrl+=)", func() tea.Cmd { p.expandSelected(a); return nil })
	button("⌖", "Select Opened File (alt+f1)", func() tea.Cmd { a.selectOpenedFile(true); return nil })
	if !a.Cfg.ProjectOpenFiles {
		hdr.Text(x+1, 0, "│", Style{Fg: th.Border, Bg: th.Bg})
		x--
		button("+", "New… (alt+insert)", func() tea.Cmd { p.newMenu(a, hdr.Abs().X+x, hdr.Abs().Y+1); return nil })
	}
	p.tree.Focused = focused
	p.tree.Draw(a, v.Sub(0, 1, v.W(), v.H()-1))
}

func (p *ProjectPane) newMenu(a *App, x, y int) {
	m := NewMenuAt("New", []MenuItem{
		{Label: "File", Icon: "▪", Do: func(a *App) tea.Cmd { p.newEntry(a, false); return nil }},
		{Label: "Directory", Icon: "▤", Do: func(a *App) tea.Cmd { p.newEntry(a, true); return nil }},
	}, x, y)
	m.Searchable = false
	a.Push(m)
}

func (p *ProjectPane) viewMenu(a *App, x, y int) {
	set := func(open bool) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			a.Cfg.ProjectOpenFiles = open
			a.Cfg.Save()
			p.OnData(a)
			return nil
		}
	}
	m := NewMenuAt("", []MenuItem{
		{Label: "Project", Checked: !a.Cfg.ProjectOpenFiles, Do: set(false)},
		{Label: "Open Files", Checked: a.Cfg.ProjectOpenFiles, Do: set(true)},
	}, x, y)
	m.Searchable = false
	a.Push(m)
}

func (p *ProjectPane) optionsMenu(a *App, x, y int) {
	toggle := func(label string, v *bool) MenuItem {
		return MenuItem{Label: label, Checked: *v, Do: func(a *App) tea.Cmd {
			*v = !*v
			a.Cfg.Save()
			p.OnData(a)
			if a.Cfg.ProjectAutoscroll {
				a.selectOpenedFile(false)
			}
			return nil
		}}
	}
	m := NewMenuAt("", []MenuItem{
		toggle("Always Select Opened File", &a.Cfg.ProjectAutoscroll),
		toggle("Hide Dot Files", &a.Cfg.ProjectHideDots),
		MenuSep(),
		{Label: "Hide", Hint: "alt+1", Do: func(a *App) tea.Cmd { a.toggleProject(); return nil }},
	}, x, y)
	m.Searchable = false
	a.Push(m)
}

// repoRel makes an absolute path repo-relative, also when either side goes through a
// symlink; "" means the file isn't in this repository.
func (a *App) repoRel(abs string) string {
	if abs == "" {
		return ""
	}
	try := func(root, p string) string {
		if r, err := filepath.Rel(root, p); err == nil && r != "." && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return ""
	}
	if r := try(a.Repo.Root, abs); r != "" {
		return r
	}
	root, err1 := filepath.EvalSymlinks(a.Repo.Root)
	p, err2 := filepath.EvalSymlinks(abs)
	if err1 == nil && err2 == nil {
		return try(root, p)
	}
	return ""
}

// expandSelected expands the selected directory and its subdirectories, capped so a
// huge tree (node_modules, worktrees) can't freeze the UI.
func (p *ProjectPane) expandSelected(a *App) {
	n := p.tree.Selected()
	if n == nil {
		return
	}
	budget := 400
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		if !n.Expandable() || budget <= 0 || depth > 8 {
			return
		}
		budget--
		p.tree.exp[n.ID] = true
		if n.Lazy != nil && n.Children == nil {
			n.Children = n.Lazy()
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	p.tree.rebuild()
}

// selectOpenedFile reveals the editor's current file in the Project tree (IntelliJ's ⌖).
func (a *App) selectOpenedFile(focus bool) {
	rel := ""
	if a.editor != nil && a.editor.Running() {
		_, cur := a.editor.Buffers()
		rel = a.repoRel(cur)
	} else if a.viewer != nil {
		rel = a.viewer.path
	}
	if rel == "" {
		if focus {
			a.Notify("No file is open in the editor", "")
		}
		return
	}
	if !a.showProject() {
		a.setProjectShown(true)
	}
	a.lastRevealed = rel
	if a.Cfg.ProjectOpenFiles {
		a.project.OnData(a)
		a.project.tree.SelectID("o:" + rel)
	} else {
		a.project.Reveal(a, rel)
	}
	if focus {
		a.focus = focusProject
	}
}

func (p *ProjectPane) SelectedPath() string {
	if e := p.selected(); e != nil {
		return e.rel
	}
	return ""
}

// Viewer is the read-only fallback editor when nvim isn't installed.
type Viewer struct {
	path  string
	lines []string
	hl    [][]tokSpan
	top   int
	left  int
	h     int
	err   string
}

func (vw *Viewer) Load(a *App, rel string, line int) {
	vw.path = rel
	vw.err = ""
	b, err := os.ReadFile(a.Repo.Abs(rel))
	if err != nil {
		vw.err = err.Error()
		vw.lines = nil
		return
	}
	if len(b) > 8<<20 {
		vw.err = "File is too large to show"
		vw.lines = nil
		return
	}
	vw.lines = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	vw.hl = highlightLines(a.Th, rel, vw.lines)
	vw.top = max(0, line-1-5)
	vw.left = 0
}

func (vw *Viewer) Key(a *App, k tea.KeyMsg) bool {
	switch keyName(k) {
	case "up", "k":
		vw.top = max(0, vw.top-1)
	case "down", "j":
		vw.top = min(max(0, len(vw.lines)-vw.h), vw.top+1)
	case "pgup", "ctrl+b":
		vw.top = max(0, vw.top-vw.h)
	case "pgdown", "ctrl+f", " ":
		vw.top = min(max(0, len(vw.lines)-vw.h), vw.top+vw.h)
	case "home", "g":
		vw.top = 0
	case "end", "G":
		vw.top = max(0, len(vw.lines)-vw.h)
	case "left", "h":
		vw.left = max(0, vw.left-4)
	case "right", "l":
		vw.left += 4
	default:
		return false
	}
	return true
}

func (vw *Viewer) Draw(a *App, v View) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	vw.h = v.H()
	if vw.err != "" {
		v.Text(2, 1, vw.err, Style{Fg: th.Error, Bg: th.Bg})
		return
	}
	nw := numWidth(len(vw.lines))
	d := &DiffView{left: vw.left}
	for i := 0; i < v.H() && vw.top+i < len(vw.lines); i++ {
		n := vw.top + i
		v.TextRight(nw, i, itoa(n+1), Style{Fg: th.DiffGutterNum, Bg: th.Bg})
		d.drawText(v.Sub(nw+2, i, v.W()-nw-2, 1), vw.lines[n], hl(vw.hl, n), nil, th.Fg, th.Bg, th.Bg)
	}
	a.Hit(v.Abs(), func(m Mouse) tea.Cmd {
		if dd := m.Wheel(); dd != 0 {
			vw.top = max(0, min(len(vw.lines)-vw.h, vw.top+dd*3))
		}
		return nil
	})
}
