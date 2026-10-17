package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/changelist"
	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/shelf"
)

type LocalChanges struct {
	tree      *Tree
	diff      *DiffView
	focusDiff bool
	hiddenWT  int
	showWT    bool
	splitX    int
}

func NewLocalChanges() *LocalChanges {
	l := &LocalChanges{tree: NewTree(), diff: NewDiffView()}
	l.tree.Focused = true
	l.tree.OnSelect = func(a *App, n *Node) tea.Cmd { return l.updatePreview(a) }
	l.tree.OnActivate = func(a *App, n *Node) tea.Cmd {
		if _, ok := n.Data.(git.FileStatus); ok {
			return l.openDiff(a)
		}
		return nil
	}
	l.tree.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { l.contextMenu(a, x, y); return nil }
	l.diff.OnChange = func(a *App) tea.Cmd {
		if s := l.diff.Source(); s != nil {
			l.tree.SelectID("f:" + s.Path)
		}
		return nil
	}
	return l
}

func (l *LocalChanges) Title() string  { return "Local Changes" }
func (l *LocalChanges) Closable() bool { return false }
func (l *LocalChanges) Typing() bool   { return l.tree.Typing() }

func (l *LocalChanges) Hint() string {
	if l.focusDiff {
		return l.diff.Hint() + " · tab tree"
	}
	return "⏎ diff · F4 source · space stage · c commit · d rollback · F6 move · s shelve · z stash · g tree/list · v preview · x menu"
}

func (l *LocalChanges) SelectedPath() string {
	if n := l.tree.Selected(); n != nil {
		if f, ok := n.Data.(git.FileStatus); ok {
			return f.Path
		}
	}
	return ""
}

func (l *LocalChanges) OnData(a *App) {
	l.rebuild(a)
}

func (l *LocalChanges) worktreePaths(a *App) map[string]bool {
	res := map[string]bool{}
	for _, w := range a.Worktrees {
		res[filepath.Clean(w.Path)] = true
	}
	return res
}

func (l *LocalChanges) rebuild(a *App) {
	st := a.Status
	if st == nil {
		return
	}
	th := a.Th
	var conflicts, untracked []git.FileStatus
	byList := map[string][]git.FileStatus{}
	wts := l.worktreePaths(a)
	l.hiddenWT = 0
	for _, f := range st.Files {
		switch f.Kind {
		case git.Conflict:
			conflicts = append(conflicts, f)
		case git.Untracked:
			if f.IsDir && !l.showWT {
				if n := worktreeContainer(filepath.Clean(a.Repo.Abs(f.Path)), wts); n > 0 {
					l.hiddenWT += n
					continue
				}
			}
			untracked = append(untracked, f)
		default:
			cl := a.CLMap[f.Path]
			if cl == "" {
				cl = a.CL.Active
			}
			byList[cl] = append(byList[cl], f)
		}
	}
	var roots []*Node
	if len(conflicts) > 0 {
		g := &Node{ID: "g:conflicts", Label: "Merge Conflicts", LabelFg: th.Conflict, Bold: true, Extra: plural(len(conflicts), "file"), Check: NoCheck,
			Right: "Resolve…", RightFg: th.Link}
		g.Children = l.fileNodes(a, conflicts, "conflicts")
		roots = append(roots, g)
	}
	for _, cl := range a.CL.Lists {
		files := byList[cl.Name]
		g := &Node{ID: "g:cl:" + cl.Name, Label: cl.Name, Bold: cl.Name == a.CL.Active, Extra: plural(len(files), "file"), Check: NoCheck, Data: cl}
		if cl.Comment != "" && cl.Comment != cl.Name && len(a.CL.Lists) > 1 {
			g.Extra += "  " + firstLine(cl.Comment)
		}
		g.Children = l.fileNodes(a, files, "cl")
		roots = append(roots, g)
	}
	if len(untracked) > 0 || l.hiddenWT > 0 {
		dirs, files := 0, 0
		for _, f := range untracked {
			if f.IsDir {
				dirs++
			} else {
				files++
			}
		}
		var parts []string
		if dirs > 0 {
			parts = append(parts, plural(dirs, "directory"))
		}
		if files > 0 || dirs == 0 {
			parts = append(parts, plural(files, "file"))
		}
		g := &Node{ID: "g:unversioned", Label: "Unversioned Files", Extra: strings.Join(parts, " and "), Check: NoCheck, RowBg: th.UnversionedRowBg}
		if l.hiddenWT > 0 {
			g.Right = fmt.Sprintf("%d worktrees hidden", l.hiddenWT)
		}
		g.Children = l.fileNodes(a, untracked, "unv")
		for _, c := range g.Children {
			tintTree(c, th.UnversionedRowBg)
		}
		roots = append(roots, g)
	}
	l.tree.SetRoots(roots)
	if n := l.tree.Selected(); n == nil || n.Expandable() && l.diff.Empty() {
		l.tree.SelectFirstLeaf()
	}
	// Re-reads the previewed file so external edits show up after a refresh.
	a.pendingCmds = append(a.pendingCmds, l.tree.NotifySelection(a))
}

// worktreeContainer reports how many worktrees dir holds if it is a worktree itself or
// a folder containing nothing but worktrees (like .worktrees/), else 0.
func worktreeContainer(dir string, wts map[string]bool) int {
	if wts[dir] {
		return 1
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.Name() == ".DS_Store" {
			continue
		}
		if !e.IsDir() || !wts[filepath.Join(dir, e.Name())] {
			return 0
		}
		n++
	}
	return n
}

func tintTree(n *Node, bg Color) {
	n.RowBg = bg
	for _, c := range n.Children {
		tintTree(c, bg)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	for _, suf := range []string{"ch", "sh", "s", "x"} {
		if strings.HasSuffix(word, suf) {
			return fmt.Sprintf("%d %ses", n, word)
		}
	}
	if strings.HasSuffix(word, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

func (l *LocalChanges) fileNode(a *App, f git.FileStatus, flat bool) *Node {
	th := a.Th
	n := &Node{ID: "f:" + f.Path, Label: baseOf(f.Path), LabelFg: th.KindColor(f.Kind), Check: NoCheck, Data: f}
	if flat {
		n.Extra = dirOf(f.Path)
	}
	if f.Kind == git.Renamed && f.OrigPath != "" {
		n.Extra = strings.TrimSpace(n.Extra + "  moved from " + f.OrigPath)
	}
	if !a.Cfg.HideStagedMarks {
		switch f.Staged() {
		case 2:
			n.Right = "staged"
		case 1:
			n.Right = "partially staged"
		}
	}
	if !f.IsDir {
		a.setFileIcon(n, f.Path)
	}
	if f.IsDir {
		n.Icon, _ = a.dirIcon(false)
		n.IconOpen, _ = a.dirIcon(true)
		n.IconFg = th.Unversioned
		repo := a.Repo
		dir := f.Path
		n.Lazy = func() []*Node {
			var kids []*Node
			for _, p := range repo.UntrackedUnder(dir) {
				rel := strings.TrimPrefix(p, dir+"/")
				kid := &Node{ID: "f:" + p, Label: baseOf(rel), Extra: dirOf(rel), LabelFg: th.Unversioned, Check: NoCheck,
					Data: git.FileStatus{Path: p, Kind: git.Untracked, X: '?', Y: '?'}, RowBg: th.UnversionedRowBg}
				a.setFileIcon(kid, p)
				kids = append(kids, kid)
			}
			return kids
		}
		n.Collapsed = true
	}
	return n
}

func (l *LocalChanges) fileNodes(a *App, files []git.FileStatus, prefix string) []*Node {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if !a.Cfg.GroupByDir {
		var res []*Node
		for _, f := range files {
			res = append(res, l.fileNode(a, f, true))
		}
		return res
	}
	return groupByDir(a, prefix, files, func(f git.FileStatus) *Node { return l.fileNode(a, f, false) }, func(f git.FileStatus) string { return f.Path })
}

// groupByDir builds a directory tree with single-child chains compacted ("src/main/java").
func groupByDir[T any](a *App, prefix string, items []T, leaf func(T) *Node, pathOf func(T) string) []*Node {
	type dnode struct {
		name  string
		dirs  map[string]*dnode
		order []string
		files []*Node
	}
	root := &dnode{dirs: map[string]*dnode{}}
	for _, it := range items {
		p := pathOf(it)
		parts := strings.Split(dirOf(p), "/")
		cur := root
		if dirOf(p) != "" {
			for _, part := range parts {
				nx, ok := cur.dirs[part]
				if !ok {
					nx = &dnode{name: part, dirs: map[string]*dnode{}}
					cur.dirs[part] = nx
					cur.order = append(cur.order, part)
				}
				cur = nx
			}
		}
		cur.files = append(cur.files, leaf(it))
	}
	var conv func(d *dnode, path string) []*Node
	conv = func(d *dnode, path string) []*Node {
		var res []*Node
		sort.Strings(d.order)
		for _, name := range d.order {
			sub := d.dirs[name]
			label := name
			full := path + "/" + name
			for len(sub.files) == 0 && len(sub.order) == 1 {
				nn := sub.order[0]
				label += "/" + nn
				full += "/" + nn
				sub = sub.dirs[nn]
			}
			kids := conv(sub, full)
			count := countLeaves(kids)
			icon, fg := a.dirIcon(false)
			open, _ := a.dirIcon(true)
			res = append(res, &Node{ID: "d:" + prefix + ":" + full, Label: label, Icon: icon, IconOpen: open, IconFg: fg, Extra: plural(count, "file"), Check: NoCheck, Children: kids})
		}
		res = append(res, d.files...)
		return res
	}
	return conv(root, "")
}

func countLeaves(ns []*Node) int {
	n := 0
	for _, x := range ns {
		if x.Expandable() && x.Lazy == nil {
			n += countLeaves(x.Children)
		} else {
			n++
		}
	}
	return n
}

// groupFiles returns all files in the same top-level group as the selection, for diff paging.
func (l *LocalChanges) groupFiles() ([]git.FileStatus, int) {
	n := l.tree.Selected()
	if n == nil {
		return nil, 0
	}
	f, ok := n.Data.(git.FileStatus)
	if !ok {
		return nil, 0
	}
	g := n
	for g.Parent() != nil {
		g = g.Parent()
	}
	var files []git.FileStatus
	idx := 0
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, c := range ns {
			if cf, ok := c.Data.(git.FileStatus); ok && !cf.IsDir {
				if cf.Path == f.Path {
					idx = len(files)
				}
				files = append(files, cf)
			}
			walk(c.Children)
		}
	}
	walk(g.Children)
	return files, idx
}

func (l *LocalChanges) sources(a *App) ([]DiffSource, int) {
	files, idx := l.groupFiles()
	var srcs []DiffSource
	head := a.headHash()
	for _, f := range files {
		srcs = append(srcs, localSource(a.Repo, head, f))
	}
	return srcs, idx
}

func (l *LocalChanges) updatePreview(a *App) tea.Cmd {
	if a.Cfg.HidePreview {
		return nil
	}
	srcs, idx := l.sources(a)
	if len(srcs) == 0 {
		return l.diff.SetSources(a, nil, 0, false)
	}
	return l.diff.SetSources(a, srcs, idx, false)
}

func (l *LocalChanges) openDiff(a *App) tea.Cmd {
	srcs, idx := l.sources(a)
	if len(srcs) == 0 {
		return nil
	}
	title := "Local Changes"
	if cl := l.selectedChangelist(a); cl != nil {
		title = cl.Name
	}
	return a.OpenChanges(title, nil, srcs, idx, nil)
}

func (l *LocalChanges) selectedFiles() []git.FileStatus {
	var res []git.FileStatus
	for _, n := range l.tree.SelectedLeaves() {
		if f, ok := n.Data.(git.FileStatus); ok {
			res = append(res, f)
		}
	}
	return res
}

func (l *LocalChanges) selectedChangelist(a *App) *changelist.List {
	n := l.tree.Selected()
	for n != nil {
		if cl, ok := n.Data.(*changelist.List); ok {
			return cl
		}
		n = n.Parent()
	}
	return a.CL.Get(a.CL.Active)
}

func (l *LocalChanges) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	if keyName(k) == "tab" || keyName(k) == "shift+tab" {
		if !a.Cfg.HidePreview {
			l.focusDiff = !l.focusDiff
			l.tree.Focused = !l.focusDiff
			l.diff.Focused = l.focusDiff
		}
		return nil, true
	}
	if l.focusDiff {
		if keyName(k) == "esc" {
			l.focusDiff = false
			l.tree.Focused, l.diff.Focused = true, false
			return nil, true
		}
		if cmd, ok := l.diff.Key(a, k); ok {
			return cmd, true
		}
	}
	if !l.focusDiff {
		if cmd, ok := l.tree.Key(a, k); ok {
			return cmd, true
		}
	}
	switch keyName(k) {
	case "ctrl+d", "o":
		return l.openDiff(a), true
	case "ctrl+alt+z", "ctrl+z", "alt+z", "d":
		l.rollback(a)
	case " ":
		return l.toggleStage(a), true
	case "a", "ctrl+alt+a":
		return l.stage(a), true
	case "f6", "m":
		l.moveToChangelist(a)
	case "n":
		l.newChangelist(a, nil)
	case "s":
		l.shelve(a)
	case "z":
		l.stash(a)
	case "delete", "D", "ctrl+backspace":
		l.deleteFiles(a)
	case "i":
		return l.ignore(a), true
	case "g":
		a.setGroupByDir(!a.Cfg.GroupByDir)
	case "v":
		a.Cfg.HidePreview = !a.Cfg.HidePreview
		a.Cfg.Save()
		if a.Cfg.HidePreview {
			l.focusDiff = false
			l.tree.Focused = true
		}
		return l.updatePreview(a), true
	case "H":
		if p := l.SelectedPath(); p != "" {
			return a.OpenLog(git.LogFilter{Paths: []string{p}}), true
		}
	case "e", "f4", "ctrl+down":
		if p := l.SelectedPath(); p != "" {
			return a.JumpToSource(p, 0), true
		}
	case "y":
		if p := l.SelectedPath(); p != "" {
			return copyToClipboard(a, a.Repo.Abs(p)), true
		}
	case ".", "shift+f10", "alt+enter":
		l.contextMenu(a, -1, -1)
	default:
		return nil, false
	}
	return nil, true
}

func (l *LocalChanges) contextMenu(a *App, x, y int) {
	files := l.selectedFiles()
	cl := l.selectedChangelist(a)
	var hasUnv, hasTracked, hasConflict bool
	for _, f := range files {
		switch f.Kind {
		case git.Untracked:
			hasUnv = true
		case git.Conflict:
			hasConflict = true
		default:
			hasTracked = true
		}
	}
	none := len(files) == 0
	items := []MenuItem{
		{Label: "Commit Files…", Hint: "ctrl+k", Disabled: none || !hasTracked, Do: func(a *App) tea.Cmd { return a.OpenCommit(files) }},
		{Label: "Rollback…", Hint: "ctrl+alt+z", Disabled: none, Do: func(a *App) tea.Cmd { l.rollback(a); return nil }},
		{Label: "Show Diff", Hint: "⏎", Disabled: none, Do: func(a *App) tea.Cmd { return l.openDiff(a) }},
		{Label: "Jump to Source", Hint: "F4", Disabled: none, Do: func(a *App) tea.Cmd { return a.JumpToSource(files[0].Path, 0) }},
		MenuSep(),
	}
	if hasConflict {
		items = append(items,
			MenuItem{Label: "Resolve Conflicts…", Do: func(a *App) tea.Cmd { a.OpenConflicts(); return nil }},
			MenuItem{Label: "Accept Yours", Do: func(a *App) tea.Cmd { return l.resolve(a, files, false) }},
			MenuItem{Label: "Accept Theirs", Do: func(a *App) tea.Cmd { return l.resolve(a, files, true) }},
			MenuItem{Label: "Mark as Resolved", Do: func(a *App) tea.Cmd {
				return a.Op("Mark resolved", func() error {
					for _, f := range files {
						if err := a.Repo.MarkResolved(f.Path); err != nil {
							return err
						}
					}
					return nil
				}, nil)
			}},
			MenuSep())
	}
	items = append(items,
		MenuItem{Label: "Move to Another Changelist…", Hint: "F6", Disabled: !hasTracked, Do: func(a *App) tea.Cmd { l.moveToChangelist(a); return nil }},
		MenuItem{Label: "New Changelist…", Do: func(a *App) tea.Cmd { l.newChangelist(a, nil); return nil }},
		MenuItem{Label: "Edit Changelist…", Disabled: cl == nil, Do: func(a *App) tea.Cmd { l.editChangelist(a, cl); return nil }},
		MenuItem{Label: "Set Active Changelist", Disabled: cl == nil || cl.Name == a.CL.Active, Do: func(a *App) tea.Cmd {
			a.CL.Active = cl.Name
			a.CL.Save()
			l.rebuild(a)
			return nil
		}},
		MenuItem{Label: "Delete Changelist", Disabled: cl == nil || len(a.CL.Lists) < 2, Do: func(a *App) tea.Cmd {
			a.CL.Remove(cl.Name)
			a.CL.Save()
			a.assignChangelists()
			l.rebuild(a)
			return nil
		}},
		MenuSep(),
		MenuItem{Label: "Shelve Changes…", Hint: "s", Disabled: !hasTracked && !hasUnv, Do: func(a *App) tea.Cmd { l.shelve(a); return nil }},
		MenuItem{Label: "Stash Changes…", Hint: "z", Do: func(a *App) tea.Cmd { l.stash(a); return nil }},
		MenuSep(),
		MenuItem{Label: "Add to VCS", Hint: "ctrl+alt+a", Disabled: !hasUnv, Do: func(a *App) tea.Cmd { return l.addToVCS(a) }},
		MenuItem{Label: "Stage / Unstage", Hint: "space", Disabled: none, Do: func(a *App) tea.Cmd { return l.toggleStage(a) }},
		MenuItem{Label: "Add to .gitignore", Hint: "i", Disabled: !hasUnv, Do: func(a *App) tea.Cmd { return l.ignore(a) }},
		MenuItem{Label: "Delete…", Hint: "⌦", Disabled: none, Do: func(a *App) tea.Cmd { l.deleteFiles(a); return nil }},
		MenuSep(),
		MenuItem{Label: "Show History", Hint: "H", Disabled: none, Do: func(a *App) tea.Cmd {
			return a.OpenLog(git.LogFilter{Paths: []string{files[0].Path}})
		}},
		MenuItem{Label: "Copy Absolute Path", Hint: "y", Disabled: none, Do: func(a *App) tea.Cmd { return copyToClipboard(a, a.Repo.Abs(files[0].Path)) }},
		MenuItem{Label: "Copy Path From Repository Root", Disabled: none, Do: func(a *App) tea.Cmd { return copyToClipboard(a, files[0].Path) }},
		MenuSep(),
		MenuItem{Label: "Group by Directory", Hint: "g", Checked: a.Cfg.GroupByDir, Do: func(a *App) tea.Cmd {
			a.setGroupByDir(!a.Cfg.GroupByDir)
			return nil
		}},
		MenuItem{Label: "Show Diff Preview", Hint: "v", Checked: !a.Cfg.HidePreview, Do: func(a *App) tea.Cmd {
			a.Cfg.HidePreview = !a.Cfg.HidePreview
			a.Cfg.Save()
			return l.updatePreview(a)
		}},
		MenuItem{Label: "Show Staged Markers", Checked: !a.Cfg.HideStagedMarks, Do: func(a *App) tea.Cmd {
			a.Cfg.HideStagedMarks = !a.Cfg.HideStagedMarks
			a.Cfg.Save()
			l.rebuild(a)
			return nil
		}},
		MenuItem{Label: "Show Nested Worktrees in Unversioned", Checked: l.showWT, Do: func(a *App) tea.Cmd {
			l.showWT = !l.showWT
			l.rebuild(a)
			return nil
		}},
		MenuItem{Label: "Refresh", Hint: "ctrl+r", Do: func(a *App) tea.Cmd { return a.Refresh() }},
	)
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (l *LocalChanges) resolve(a *App, files []git.FileStatus, theirs bool) tea.Cmd {
	return a.Op("Resolve conflicts", func() error {
		for _, f := range files {
			if f.Kind != git.Conflict {
				continue
			}
			if err := a.Repo.ResolveConflict(f.Path, theirs); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}

func (l *LocalChanges) rollback(a *App) {
	files := l.selectedFiles()
	var tracked []git.FileStatus
	for _, f := range files {
		if f.Kind != git.Untracked && f.Kind != git.Conflict {
			tracked = append(tracked, f)
		}
	}
	if len(tracked) == 0 {
		if cl := l.selectedChangelist(a); cl != nil && l.tree.Selected() != nil && l.tree.Selected().Data == cl {
			tracked = files
		}
	}
	if len(tracked) == 0 {
		a.Notify("Nothing to roll back", "Select modified files or a changelist")
		return
	}
	OpenRollback(a, tracked)
}

func OpenRollback(a *App, files []git.FileStatus) {
	deleteAdded := false
	var lines, icons []string
	var fgs, iconFgs []Color
	hasAdded := false
	for _, f := range files {
		lines = append(lines, f.Path)
		fgs = append(fgs, a.Th.KindColor(f.Kind))
		ic, ifg := a.fileIcon(f.Path)
		icons, iconFgs = append(icons, ic), append(iconFgs, ifg)
		if f.Kind == git.Added {
			hasAdded = true
		}
	}
	d := &Dialog{Title: "Rollback Changes", Width: 72}
	d.Fields = []Field{
		&LabelField{Text: fmt.Sprintf("%s will be reverted to the last committed state:", plural(len(files), "file"))},
		&ListField{Lines: lines, Fgs: fgs, Icons: icons, IconFgs: iconFgs, H: 10},
	}
	if hasAdded {
		d.Fields = append(d.Fields, &CheckField{Label: "Delete local copies of added files", Val: &deleteAdded})
	}
	d.Buttons = []Button{
		{Label: "Cancel"},
		{Label: "Rollback", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
			unborn := a.Status != nil && a.Status.Unborn
			return a.Op("Rollback", func() error { return a.Repo.Rollback(files, deleteAdded, unborn) }, func(a *App) tea.Cmd {
				a.Notify("Rolled back", plural(len(files), "file"))
				return nil
			}), true
		}},
	}
	a.Push(d)
}

// toggleStage is lazygit's space: stage the selection, or unstage it if fully staged.
func (l *LocalChanges) toggleStage(a *App) tea.Cmd {
	files := l.selectedFiles()
	if len(files) == 0 {
		return nil
	}
	allStaged := true
	var paths []string
	for _, f := range files {
		if f.Kind == git.Conflict {
			continue
		}
		paths = append(paths, f.Path)
		if f.OrigPath != "" {
			paths = append(paths, f.OrigPath)
		}
		if f.Staged() != 2 {
			allStaged = false
		}
	}
	if len(paths) == 0 {
		return nil
	}
	if allStaged {
		unborn := a.Status != nil && a.Status.Unborn
		return a.Op("Unstage", func() error { return a.Repo.Unstage(paths, unborn) }, nil)
	}
	return a.Op("Stage", func() error { return a.Repo.Stage(paths) }, nil)
}

func (l *LocalChanges) stage(a *App) tea.Cmd {
	var paths []string
	for _, f := range l.selectedFiles() {
		if f.Kind != git.Conflict {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return a.Op("Stage", func() error { return a.Repo.Stage(paths) }, nil)
}

func (l *LocalChanges) addToVCS(a *App) tea.Cmd {
	var paths []string
	for _, f := range l.selectedFiles() {
		if f.Kind == git.Untracked {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return a.Op("Add to VCS", func() error { return a.Repo.Add(paths) }, nil)
}

func (l *LocalChanges) ignore(a *App) tea.Cmd {
	var paths []string
	for _, n := range l.tree.SelectedNodes() {
		if f, ok := n.Data.(git.FileStatus); ok && f.Kind == git.Untracked {
			p := "/" + f.Path
			if f.IsDir {
				p += "/"
			}
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return a.Op("Add to .gitignore", func() error {
		gi := a.Repo.Abs(".gitignore")
		b, _ := os.ReadFile(gi)
		s := string(b)
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += strings.Join(paths, "\n") + "\n"
		return os.WriteFile(gi, []byte(s), 0o644)
	}, nil)
}

func (l *LocalChanges) deleteFiles(a *App) {
	files := l.selectedFiles()
	if len(files) == 0 {
		return
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Path)
	}
	msg := fmt.Sprintf("Delete %s from disk?\n\n%s", plural(len(files), "file"), strings.Join(names[:min(8, len(names))], "\n"))
	if len(names) > 8 {
		msg += fmt.Sprintf("\n… and %d more", len(names)-8)
	}
	a.Push(Confirm("Delete", msg, "Delete", true, func(a *App) tea.Cmd {
		return a.Op("Delete files", func() error {
			for _, f := range files {
				if err := os.RemoveAll(a.Repo.Abs(f.Path)); err != nil {
					return err
				}
			}
			return nil
		}, nil)
	}))
}

func (l *LocalChanges) moveToChangelist(a *App) {
	var paths []string
	for _, f := range l.selectedFiles() {
		if f.Kind != git.Untracked {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return
	}
	var items []MenuItem
	for _, name := range a.CL.Names() {
		name := name
		items = append(items, MenuItem{Label: name, Checked: name == a.CLMap[paths[0]], Do: func(a *App) tea.Cmd {
			a.CL.Move(paths, name)
			a.CL.Save()
			a.assignChangelists()
			l.rebuild(a)
			return nil
		}})
	}
	items = append(items, MenuSep(), MenuItem{Label: "New Changelist…", Do: func(a *App) tea.Cmd {
		l.newChangelist(a, paths)
		return nil
	}})
	a.Push(NewMenu("Move to Another Changelist", items))
}

func (l *LocalChanges) newChangelist(a *App, move []string) {
	name := NewInput("")
	comment := NewInput("")
	active := false
	d := &Dialog{Title: "New Changelist", Width: 64}
	d.Fields = []Field{
		&InputField{Label: "Name:   ", In: name},
		&InputField{Label: "Comment:", In: comment},
		&CheckField{Label: "Set active", Val: &active},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "OK", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		n := strings.TrimSpace(name.Value())
		if n == "" {
			d.Err = "Name can't be empty"
			return nil, false
		}
		if a.CL.Get(n) != nil && move == nil {
			d.Err = "Changelist already exists"
			return nil, false
		}
		a.CL.Add(n, comment.Value())
		if len(move) > 0 {
			a.CL.Move(move, n)
		}
		if active {
			a.CL.Active = n
		}
		a.CL.Save()
		a.assignChangelists()
		l.rebuild(a)
		return nil, true
	}}}
	a.Push(d)
}

func (l *LocalChanges) editChangelist(a *App, cl *changelist.List) {
	name := NewInput(cl.Name)
	comment := NewInput(cl.Comment)
	active := cl.Name == a.CL.Active
	d := &Dialog{Title: "Edit Changelist", Width: 64}
	d.Fields = []Field{
		&InputField{Label: "Name:   ", In: name},
		&InputField{Label: "Comment:", In: comment},
		&CheckField{Label: "Set active", Val: &active},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "OK", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		n := strings.TrimSpace(name.Value())
		if n == "" {
			d.Err = "Name can't be empty"
			return nil, false
		}
		a.CL.Rename(cl.Name, n, comment.Value())
		if active {
			a.CL.Active = n
		}
		a.CL.Save()
		a.assignChangelists()
		l.rebuild(a)
		return nil, true
	}}}
	a.Push(d)
}

func (l *LocalChanges) shelve(a *App) {
	files := l.selectedFiles()
	if len(files) == 0 || (len(files) == 1 && l.tree.Selected() != nil && l.tree.Selected().Data != nil) {
		if cl := l.selectedChangelist(a); cl != nil && len(files) == 0 {
			files = nil
			for _, f := range a.Status.Files {
				if a.CLMap[f.Path] == cl.Name && f.Kind != git.Untracked {
					files = append(files, f)
				}
			}
		}
	}
	if len(files) == 0 {
		a.Notify("Nothing to shelve", "")
		return
	}
	OpenShelveDialog(a, files, l.selectedChangelist(a))
}

func OpenShelveDialog(a *App, files []git.FileStatus, cl *changelist.List) {
	desc := ""
	if cl != nil {
		desc = cl.Comment
		if desc == "" {
			desc = cl.Name
		}
	}
	in := NewInput(desc)
	keep := false
	d := &Dialog{Title: "Shelve Changes", Width: 70}
	d.Fields = []Field{
		&LabelField{Text: fmt.Sprintf("Shelve %s. Shelves are stored in %s, shared with IntelliJ.", plural(len(files), "file"), relHome(shelf.Dir(a.Repo)))},
		&InputField{Label: "Description:", In: in},
		&CheckField{Label: "Keep changes in the working tree (shelve silently copies)", Val: &keep},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Shelve Changes", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		name := strings.TrimSpace(in.Value())
		if name == "" {
			d.Err = "Description can't be empty"
			return nil, false
		}
		repo := a.Repo
		unborn := a.Status != nil && a.Status.Unborn
		var skippedFiles []string
		return a.Op("Shelve", func() error {
			var srcs []shelf.Source
			for _, f := range files {
				src := shelf.Source{Path: f.Path, OrigPath: f.OrigPath, Kind: f.Kind}
				if f.Kind != git.Untracked && f.Kind != git.Added {
					from := firstNonEmpty(f.OrigPath, f.Path)
					src.Old, _ = repo.Show("HEAD", from)
				}
				if f.Kind != git.Deleted {
					src.New, _ = repo.ReadWorktree(f.Path)
				}
				srcs = append(srcs, src)
			}
			_, skipped, err := shelf.Create(repo, name, srcs)
			if err != nil {
				return err
			}
			skippedFiles = skipped
			if keep {
				return nil
			}
			var shelved []git.FileStatus
			for _, f := range files {
				if !slices.Contains(skipped, f.Path) {
					shelved = append(shelved, f)
				}
			}
			return repo.Rollback(shelved, true, unborn)
		}, func(a *App) tea.Cmd {
			if len(skippedFiles) > 0 {
				a.Error("Some files were not shelved", "Binary or very large files can't be shelved and were left untouched:\n"+strings.Join(skippedFiles, "\n"))
			}
			a.Notify("Changes shelved", name)
			return nil
		}), true
	}}}
	a.Push(d)
}

func (l *LocalChanges) stash(a *App) {
	OpenStashDialog(a, nil)
}

func OpenStashDialog(a *App, paths []string) {
	in := NewInput("")
	keepIndex := false
	untracked := false
	d := &Dialog{Title: "Stash", Width: 64}
	d.Fields = []Field{
		&InputField{Label: "Message:", In: in},
		&CheckField{Label: "Keep index", Val: &keepIndex},
		&CheckField{Label: "Include untracked files", Val: &untracked},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Create Stash", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		msg := in.Value()
		return a.Op("Stash", func() error { return a.Repo.StashPush(msg, keepIndex, untracked, paths) }, func(a *App) tea.Cmd {
			a.Notify("Changes stashed", msg)
			return nil
		}), true
	}}}
	a.Push(d)
}

func relHome(p string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return p
	}
	// git reports resolved paths, so also try the home dir with symlinks resolved.
	homes := []string{home}
	if real, err := filepath.EvalSymlinks(home); err == nil && real != home {
		homes = append(homes, real)
	}
	for _, h := range homes {
		if p == h || strings.HasPrefix(p, h+"/") {
			return "~" + strings.TrimPrefix(p, h)
		}
	}
	return p
}

func (l *LocalChanges) Draw(a *App, v View) {
	th := a.Th
	y := 0
	if banner := a.stateBanner(v.Sub(0, 0, v.W(), 1)); banner {
		y = 1
	}
	body := v.Sub(0, y, v.W(), v.H()-y)
	if a.Cfg.HidePreview {
		drawFilesToolbar(a, body.Sub(0, 0, body.W(), 1), "", l.tree)
		l.tree.Draw(a, body.Sub(0, 1, body.W(), body.H()-1))
		return
	}
	split := int(float64(body.W()) * a.Cfg.LocalSplit)
	split = max(20, min(body.W()-30, split))
	drawFilesToolbar(a, body.Sub(0, 0, split, 1), "", l.tree)
	l.tree.Draw(a, body.Sub(0, 1, split, body.H()-1))
	body.VLine(split, 0, body.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
	abs := body.Abs()
	a.Hit(Rect{abs.X + split, abs.Y, 1, body.H()}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.Drag(func(m Mouse) tea.Cmd {
				a.Cfg.LocalSplit = max(0.15, min(0.85, float64(m.X-abs.X)/float64(max(1, body.W()))))
				if m.Action == tea.MouseActionRelease {
					a.Cfg.Save()
				}
				return nil
			})
		}
		return nil
	})
	dv := body.Sub(split+1, 0, body.W()-split-1, body.H())
	l.diff.Draw(a, dv)
	a.HitPass(dv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			l.focusDiff = true
			l.tree.Focused, l.diff.Focused = false, true
		}
		return nil
	})
	a.HitPass(body.Sub(0, 0, split, body.H()).Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() || m.RightPress() {
			l.focusDiff = false
			l.tree.Focused, l.diff.Focused = true, false
		}
		return nil
	})
}

// stateBanner shows an IntelliJ-like notification bar during merge/rebase.
func (a *App) stateBanner(v View) bool {
	st := a.Status
	if st == nil || st.State == git.StateClean {
		return false
	}
	th := a.Th
	bg := blend(th.Bg, th.Warning, 0.18)
	v.Fill(Style{Bg: bg})
	text := ""
	var actions []noteAction
	switch st.State {
	case git.StateRebasing:
		text = "Rebasing " + a.Repo.RebaseBranch()
		actions = []noteAction{
			{"Continue", func(a *App) tea.Cmd { return a.Op("Continue rebase", a.Repo.RebaseContinue, nil) }},
			{"Skip", func(a *App) tea.Cmd { return a.Op("Skip commit", a.Repo.RebaseSkip, nil) }},
			{"Abort", func(a *App) tea.Cmd { return a.Op("Abort rebase", a.Repo.RebaseAbort, nil) }},
		}
	case git.StateMerging:
		text = "Merging into " + st.Branch
		actions = []noteAction{
			{"Commit…", func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
			{"Abort", func(a *App) tea.Cmd { return a.Op("Abort merge", a.Repo.MergeAbort, nil) }},
		}
	case git.StateCherryPicking:
		text = "Cherry-pick in progress"
		actions = []noteAction{
			{"Commit…", func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
			{"Abort", func(a *App) tea.Cmd { return a.Op("Abort cherry-pick", a.Repo.CherryPickAbort, nil) }},
		}
	case git.StateReverting:
		text = "Revert in progress"
		actions = []noteAction{
			{"Commit…", func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
			{"Abort", func(a *App) tea.Cmd { return a.Op("Abort revert", a.Repo.RevertAbort, nil) }},
		}
	}
	conflicts := 0
	for _, f := range st.Files {
		if f.Kind == git.Conflict {
			conflicts++
		}
	}
	if conflicts > 0 {
		text += fmt.Sprintf(" · %s with conflicts", plural(conflicts, "file"))
		actions = append([]noteAction{{"Resolve…", func(a *App) tea.Cmd { a.OpenConflicts(); return nil }}}, actions...)
	}
	x := v.Text(1, 0, "⚠ "+text, Style{Fg: th.Fg, Bg: bg}) + 3
	for _, act := range actions {
		act := act
		w := Width(act.label)
		v.Text(x, 0, act.label, Style{Fg: th.Link, Bg: bg})
		a.Hit(Rect{v.Abs().X + x, v.Abs().Y, w, 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				return act.fn(a)
			}
			return nil
		})
		x += w + 3
	}
	return true
}

func (l *LocalChanges) regroup(a *App) { l.rebuild(a) }
