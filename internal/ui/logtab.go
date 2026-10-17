package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/graph"
)

type logFocus int

const (
	lfTable logFocus = iota
	lfFilter
	lfBranches
	lfFiles
)

type LogTab struct {
	filter   git.LogFilter
	closable bool
	text     *TextInput
	commits  []*git.Commit
	rows     []graph.Row
	index    map[string]int
	sel      int
	top      int
	anchor   int
	multi    map[int]bool
	loading  bool
	err      error
	gen      int
	textGen  int
	height   int
	limit    int
	focus    logFocus

	branches *Tree
	files    *Tree
	details  *git.CommitDetails
	detFiles []git.ChangedFile
	contains []string
	detGen   int
	detHash  string
	detTop   int
	pending  string
	shownSel int
}

func NewLogTab(f git.LogFilter, closable bool) *LogTab {
	t := &LogTab{filter: f, closable: closable, text: NewInput(f.Text), index: map[string]int{}, multi: map[int]bool{}, limit: 3000, shownSel: -1,
		branches: NewTree(), files: NewTree()}
	t.text.Placeholder = "Text or hash"
	t.files.Empty = "Select commit to view changes"
	t.files.OnActivate = func(a *App, n *Node) tea.Cmd { return t.openFileDiff(a) }
	t.files.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { t.fileMenu(a, x, y); return nil }
	t.branches.OnSelect = func(a *App, n *Node) tea.Cmd {
		if r, ok := n.Data.(git.Ref); ok {
			t.jumpTo(r.Hash)
		}
		return nil
	}
	t.branches.OnActivate = func(a *App, n *Node) tea.Cmd {
		switch d := n.Data.(type) {
		case git.Ref:
			t.filter.Branches = []string{d.Name}
			a.saveLogTabs()
			return t.Reload(a)
		case string:
			if d == "HEAD" {
				t.filter.Branches = []string{"HEAD"}
				a.saveLogTabs()
				return t.Reload(a)
			}
		}
		return nil
	}
	t.branches.OnContext = func(a *App, n *Node, x, y int) tea.Cmd {
		if r, ok := n.Data.(git.Ref); ok {
			m := NewMenuAt(r.Name, BranchActions(a, r), x, y)
			m.Searchable = false
			a.Push(m)
		}
		return nil
	}
	return t
}

func (t *LogTab) Title() string {
	f := t.filter
	parts := []string{}
	if len(f.Users) > 0 {
		parts = append(parts, strings.Join(f.Users, ","))
	}
	if len(f.Branches) > 0 {
		parts = append(parts, strings.Join(f.Branches, ","))
	}
	if len(f.Paths) > 0 {
		parts = append(parts, baseOf(f.Paths[0]))
	}
	if len(parts) == 0 {
		return "Log"
	}
	return "Log: " + Truncate(strings.Join(parts, " "), 24)
}

func (t *LogTab) Closable() bool { return t.closable }
func (t *LogTab) Typing() bool {
	return t.focus == lfFilter || t.branches.Typing() || t.files.Typing()
}

func (t *LogTab) Hint() string {
	switch t.focus {
	case lfFilter:
		return "type to filter · ⏎/esc back to log"
	case lfBranches:
		return "⏎ filter by branch · . branch actions · tab next pane"
	case lfFiles:
		return "⏎ diff · H file history · tab next pane"
	}
	return "⏎ diff · . actions · / filter · tab panes · c cherry-pick · ctrl+b branches · y copy hash"
}

func (t *LogTab) SelectedPath() string {
	if t.focus == lfFiles {
		if n := t.files.Selected(); n != nil {
			if f, ok := n.Data.(git.ChangedFile); ok {
				return f.Path
			}
		}
	}
	return ""
}

func (t *LogTab) OnData(a *App) {
	t.buildBranches(a)
}

func (t *LogTab) Reload(a *App) tea.Cmd {
	t.gen++
	gen := t.gen
	t.loading = true
	f := t.filter
	f.Users = expandMe(a, f.Users)
	f.Limit = t.limit
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		commits, err := repo.Log(f)
		rows := graph.Layout(commits)
		return func(a *App) tea.Cmd {
			if gen != t.gen {
				return nil
			}
			t.loading = false
			t.err = err
			selHash := t.selectedHash()
			t.commits = commits
			t.rows = rows
			t.index = make(map[string]int, len(commits))
			for i, c := range commits {
				t.index[c.Hash] = i
			}
			t.multi = map[int]bool{}
			if selHash != "" {
				if i, ok := t.index[selHash]; ok {
					t.sel = i
				}
			}
			if t.pending != "" {
				t.jumpTo(t.pending)
				t.pending = ""
			}
			t.sel = max(0, min(t.sel, len(commits)-1))
			return t.loadDetails(a)
		}
	})
}

func expandMe(a *App, users []string) []string {
	var res []string
	for _, u := range users {
		if u == "me" {
			if a.User != "" {
				res = append(res, a.User)
			}
			if e := a.Repo.UserEmail(); e != "" {
				res = append(res, e)
			}
			continue
		}
		res = append(res, u)
	}
	return res
}

func (t *LogTab) selectedHash() string {
	if t.sel >= 0 && t.sel < len(t.commits) {
		return t.commits[t.sel].Hash
	}
	return ""
}

func (t *LogTab) selected() *git.Commit {
	if t.sel >= 0 && t.sel < len(t.commits) {
		return t.commits[t.sel]
	}
	return nil
}

func (t *LogTab) selectedCommits() []*git.Commit {
	if len(t.multi) == 0 {
		if c := t.selected(); c != nil {
			return []*git.Commit{c}
		}
		return nil
	}
	var res []*git.Commit
	for i, c := range t.commits {
		if t.multi[i] {
			res = append(res, c)
		}
	}
	return res
}

func (t *LogTab) jumpTo(hash string) bool {
	if i, ok := t.index[hash]; ok {
		t.sel = i
		t.multi = map[int]bool{}
		h := max(1, t.height)
		if t.sel < t.top || t.sel >= t.top+h {
			t.top = max(0, t.sel-h/3)
		}
		return true
	}
	return false
}

func (t *LogTab) loadDetails(a *App) tea.Cmd {
	c := t.selected()
	if c == nil {
		t.details, t.detFiles, t.contains = nil, nil, nil
		t.files.SetRoots(nil)
		return nil
	}
	if c.Hash == t.detHash && t.details != nil {
		return nil
	}
	t.detGen++
	gen := t.detGen
	commit := c
	repo := a.Repo
	t.detHash = c.Hash
	t.contains = nil
	return tea.Batch(Async(func() func(a *App) tea.Cmd {
		det, _ := repo.Details(commit.Hash)
		files, _ := repo.CommitFiles(commit)
		return func(a *App) tea.Cmd {
			if gen != t.detGen {
				return nil
			}
			t.details = det
			t.detFiles = files
			t.detTop = 0
			t.files.SetRoots(changedFileNodes(a, "commit", files))
			t.files.Sel = 0
			t.files.Top = 0
			return nil
		}
	}), Async(func() func(a *App) tea.Cmd {
		time.Sleep(150 * time.Millisecond)
		if gen != t.detGen {
			return func(a *App) tea.Cmd { return nil }
		}
		br := repo.BranchesContaining(commit.Hash)
		return func(a *App) tea.Cmd {
			if gen == t.detGen {
				t.contains = br
			}
			return nil
		}
	}))
}

func (t *LogTab) buildBranches(a *App) {
	th := a.Th
	head := &Node{ID: "b:HEAD", Label: "HEAD (Current Branch)", Check: NoCheck, Data: "HEAD"}
	local := &Node{ID: "b:local", Label: "Local", Check: NoCheck}
	remote := &Node{ID: "b:remote", Label: "Remote", Check: NoCheck}
	tags := &Node{ID: "b:tags", Label: "Tags", Check: NoCheck, Collapsed: true}
	remotes := map[string]*Node{}
	var remoteOrder []string
	var localRefs, tagRefs []git.Ref
	remoteRefs := map[string][]git.Ref{}
	for _, r := range a.Refs {
		switch r.Kind {
		case git.LocalBranch:
			localRefs = append(localRefs, r)
		case git.RemoteBranch:
			rm := r.Remote
			if rm == "" {
				rm, _, _ = strings.Cut(r.Name, "/")
			}
			if _, ok := remotes[rm]; !ok {
				remotes[rm] = &Node{ID: "b:remote:" + rm, Label: rm, Check: NoCheck}
				remoteOrder = append(remoteOrder, rm)
			}
			remoteRefs[rm] = append(remoteRefs[rm], r)
		case git.Tag:
			tagRefs = append(tagRefs, r)
		}
	}
	leaf := func(prefix string, strip string) func(git.Ref) *Node {
		return func(r git.Ref) *Node {
			name := strings.TrimPrefix(r.Name, strip)
			n := &Node{ID: "b:" + prefix + ":" + r.Name, Label: baseOf(name), Check: NoCheck, Data: r}
			if r.IsHead {
				n.Icon, n.IconFg = "◆", th.RefHead
				n.Bold = true
			} else if r.Kind == git.Tag {
				n.Icon, n.IconFg = "◇", th.RefTag
			} else if isFavorite(name) {
				n.Icon, n.IconFg = "★", th.RefHead
			}
			if r.Ahead > 0 || r.Behind > 0 {
				n.Right = aheadBehind(r.Ahead, r.Behind)
			}
			return n
		}
	}
	sort.SliceStable(localRefs, func(i, j int) bool {
		fi, fj := isFavorite(localRefs[i].Name), isFavorite(localRefs[j].Name)
		if fi != fj {
			return fi
		}
		return localRefs[i].Name < localRefs[j].Name
	})
	local.Children = groupByDir(a, "local", localRefs, leaf("l", ""), func(r git.Ref) string { return r.Name })
	for _, rm := range remoteOrder {
		node := remotes[rm]
		node.Children = groupByDir(a, "r:"+rm, remoteRefs[rm], leaf("r", rm+"/"), func(r git.Ref) string { return strings.TrimPrefix(r.Name, rm+"/") })
		remote.Children = append(remote.Children, node)
	}
	tags.Children = groupByDir(a, "tags", tagRefs, leaf("t", ""), func(r git.Ref) string { return r.Name })
	for _, g := range []*Node{local, remote, tags} {
		stripDirExtra(g.Children)
	}
	roots := []*Node{head, local}
	if len(remote.Children) > 0 {
		roots = append(roots, remote)
	}
	if len(tags.Children) > 0 {
		roots = append(roots, tags)
	}
	t.branches.SetRoots(roots)
}

func stripDirExtra(ns []*Node) {
	for _, n := range ns {
		if strings.HasPrefix(n.ID, "d:") {
			n.Extra = ""
			n.Icon = ""
		}
		stripDirExtra(n.Children)
	}
}

func isFavorite(name string) bool { return name == "main" || name == "master" }

func aheadBehind(ahead, behind int) string {
	var s []string
	if ahead > 0 {
		s = append(s, fmt.Sprintf("↑%d", ahead))
	}
	if behind > 0 {
		s = append(s, fmt.Sprintf("↓%d", behind))
	}
	return strings.Join(s, " ")
}

func (t *LogTab) moveSel(a *App, d int, extend bool) tea.Cmd {
	if len(t.commits) == 0 {
		return nil
	}
	if extend {
		if len(t.multi) == 0 {
			t.anchor = t.sel
		}
	} else {
		t.multi = map[int]bool{}
	}
	t.sel = max(0, min(len(t.commits)-1, t.sel+d))
	if extend {
		t.multi = map[int]bool{}
		lo, hi := min(t.anchor, t.sel), max(t.anchor, t.sel)
		for i := lo; i <= hi; i++ {
			t.multi[i] = true
		}
	}
	var cmds []tea.Cmd
	if t.sel > len(t.commits)-50 && len(t.commits) >= t.limit {
		t.limit += 3000
		cmds = append(cmds, t.Reload(a))
	}
	cmds = append(cmds, t.loadDetails(a))
	return tea.Batch(cmds...)
}

func (t *LogTab) setFocus(f logFocus) {
	t.focus = f
	t.text.Focused = f == lfFilter
	t.branches.Focused = f == lfBranches
	t.files.Focused = f == lfFiles
}

func (t *LogTab) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	s := k.String()
	if t.focus == lfFilter {
		switch s {
		case "esc", "enter", "down", "tab":
			t.setFocus(lfTable)
			return t.applyText(a), true
		}
		if t.text.Key(k) {
			t.textGen++
			gen := t.textGen
			return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg {
				return apply(func(a *App) tea.Cmd {
					if gen != t.textGen {
						return nil
					}
					return t.applyText(a)
				})
			}), true
		}
		return nil, true
	}
	switch s {
	case "tab":
		next := map[logFocus]logFocus{lfTable: lfFiles, lfFiles: lfTable}
		if a.Cfg.BranchesPane {
			next = map[logFocus]logFocus{lfBranches: lfTable, lfTable: lfFiles, lfFiles: lfBranches}
		}
		t.setFocus(next[t.focus])
		return nil, true
	case "shift+tab":
		prev := map[logFocus]logFocus{lfTable: lfFiles, lfFiles: lfTable}
		if a.Cfg.BranchesPane {
			prev = map[logFocus]logFocus{lfBranches: lfFiles, lfTable: lfBranches, lfFiles: lfTable}
		}
		t.setFocus(prev[t.focus])
		return nil, true
	case "/", "ctrl+f":
		t.setFocus(lfFilter)
		return nil, true
	case "ctrl+b", "B":
		a.Cfg.BranchesPane = !a.Cfg.BranchesPane
		a.Cfg.Save()
		if a.Cfg.BranchesPane {
			t.setFocus(lfBranches)
		} else if t.focus == lfBranches {
			t.setFocus(lfTable)
		}
		return nil, true
	}
	switch t.focus {
	case lfBranches:
		if cmd, ok := t.branches.Key(a, k); ok {
			return cmd, true
		}
		if s == "." || s == "shift+f10" {
			if n := t.branches.Selected(); n != nil {
				if r, ok := n.Data.(git.Ref); ok {
					m := NewMenu(r.Name, BranchActions(a, r))
					m.Searchable = false
					a.Push(m)
				}
			}
			return nil, true
		}
		if s == "esc" {
			t.setFocus(lfTable)
			return nil, true
		}
		return nil, false
	case lfFiles:
		if cmd, ok := t.files.Key(a, k); ok {
			return cmd, true
		}
		switch s {
		case "esc":
			t.setFocus(lfTable)
			return nil, true
		case "H":
			if p := t.SelectedPath(); p != "" {
				return a.OpenLog(git.LogFilter{Paths: []string{p}}), true
			}
		case ".", "shift+f10":
			t.fileMenu(a, -1, -1)
			return nil, true
		case "ctrl+d":
			return t.openFileDiff(a), true
		}
		return nil, false
	}
	h := max(1, t.height)
	switch s {
	case "up", "k":
		return t.moveSel(a, -1, false), true
	case "down", "j":
		return t.moveSel(a, 1, false), true
	case "shift+up", "K":
		return t.moveSel(a, -1, true), true
	case "shift+down", "J":
		return t.moveSel(a, 1, true), true
	case "pgup", "ctrl+u":
		return t.moveSel(a, -h+1, false), true
	case "pgdown", "ctrl+d":
		return t.moveSel(a, h-1, false), true
	case "home", "g":
		return t.moveSel(a, -len(t.commits), false), true
	case "end", "G":
		return t.moveSel(a, len(t.commits), false), true
	case "enter", "o":
		return t.openCommitDiff(a), true
	case ".", "shift+f10", "alt+enter":
		t.commitMenu(a, -1, -1)
		return nil, true
	case "y":
		if c := t.selected(); c != nil {
			return copyToClipboard(a, c.Hash), true
		}
	case "c":
		return t.cherryPick(a), true
	case "left", "h":
		if c := t.selected(); c != nil && len(c.Parents) > 0 {
			t.jumpTo(c.Parents[0])
			return t.loadDetails(a), true
		}
	case "right", "l":
		if c := t.selected(); c != nil {
			for i := t.sel - 1; i >= 0; i-- {
				for _, p := range t.commits[i].Parents {
					if p == c.Hash {
						t.jumpTo(t.commits[i].Hash)
						return t.loadDetails(a), true
					}
				}
			}
		}
		return nil, true
	case "esc":
		if t.hasFilters() {
			t.clearFilters(a)
			return t.Reload(a), true
		}
		return nil, false
	}
	return nil, false
}

func (t *LogTab) hasFilters() bool {
	f := t.filter
	return f.Text != "" || len(f.Branches) > 0 || len(f.Users) > 0 || f.Since != "" || len(f.Paths) > 0
}

func (t *LogTab) clearFilters(a *App) {
	t.filter = git.LogFilter{}
	t.text.SetValue("")
	a.saveLogTabs()
}

func (t *LogTab) applyText(a *App) tea.Cmd {
	if t.text.Value() == t.filter.Text {
		return nil
	}
	t.filter.Text = t.text.Value()
	a.saveLogTabs()
	return t.Reload(a)
}

func (t *LogTab) openCommitDiff(a *App) tea.Cmd {
	c := t.selected()
	if c == nil || len(t.detFiles) == 0 || t.detHash != c.Hash {
		return nil
	}
	return t.diffFiles(a, c.Hash, t.detFiles, 0)
}

func (t *LogTab) diffFiles(a *App, hash string, files []git.ChangedFile, idx int) tea.Cmd {
	var srcs []DiffSource
	parent := t.parentOf(a, hash)
	for _, f := range files {
		srcs = append(srcs, commitSource(a.Repo, hash, parent, f))
	}
	if len(srcs) == 0 {
		return nil
	}
	w := NewDiffWindow(srcs[idx].Path)
	a.Push(w)
	return w.diff.SetSources(a, srcs, idx, true)
}

// parentOf uses the parents already loaded with the commit details instead of asking git.
func (t *LogTab) parentOf(a *App, hash string) string {
	if t.details != nil && t.details.Hash == hash {
		if len(t.details.Parents) == 0 {
			return git.EmptyTree
		}
		return t.details.Parents[0]
	}
	return a.Repo.ParentRev(hash)
}

func (t *LogTab) openFileDiff(a *App) tea.Cmd {
	n := t.files.Selected()
	if n == nil {
		return nil
	}
	f, ok := n.Data.(git.ChangedFile)
	if !ok {
		return nil
	}
	idx := 0
	for i, x := range t.detFiles {
		if x.Path == f.Path {
			idx = i
		}
	}
	return t.diffFiles(a, t.detHash, t.detFiles, idx)
}

func (t *LogTab) fileMenu(a *App, x, y int) {
	n := t.files.Selected()
	if n == nil {
		return
	}
	f, ok := n.Data.(git.ChangedFile)
	if !ok {
		return
	}
	hash := t.detHash
	items := []MenuItem{
		{Label: "Show Diff", Hint: "⏎", Do: func(a *App) tea.Cmd { return t.openFileDiff(a) }},
		{Label: "Compare with Local", Do: func(a *App) tea.Cmd {
			w := NewDiffWindow(f.Path)
			a.Push(w)
			return w.diff.SetSources(a, []DiffSource{compareSource(a.Repo, hash, short(hash), f)}, 0, true)
		}},
		{Label: "Show History for File", Hint: "H", Do: func(a *App) tea.Cmd { return a.OpenLog(git.LogFilter{Paths: []string{f.Path}}) }},
		{Label: "Revert Selected Changes", Do: func(a *App) tea.Cmd {
			return a.Op("Revert file changes", func() error {
				parent := a.Repo.ParentRev(hash)
				if b, ok := a.Repo.Show(parent, firstNonEmpty(f.OrigPath, f.Path)); ok {
					return writeFile(a.Repo.Abs(f.Path), b)
				}
				return removeFile(a.Repo.Abs(f.Path))
			}, nil)
		}},
		{Label: "Get from Revision", Do: func(a *App) tea.Cmd {
			return a.Op("Get from revision", func() error {
				if b, ok := a.Repo.Show(hash, f.Path); ok {
					return writeFile(a.Repo.Abs(f.Path), b)
				}
				return removeFile(a.Repo.Abs(f.Path))
			}, nil)
		}},
		{Label: "Copy Path", Do: func(a *App) tea.Cmd { return copyToClipboard(a, f.Path) }},
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (t *LogTab) cherryPick(a *App) tea.Cmd {
	cs := t.selectedCommits()
	if len(cs) == 0 {
		return nil
	}
	return a.Op("Cherry-pick", func() error {
		for i := len(cs) - 1; i >= 0; i-- {
			if err := a.Repo.CherryPick(cs[i].Hash); err != nil {
				return err
			}
		}
		return nil
	}, func(a *App) tea.Cmd { a.Notify("Cherry-picked "+plural(len(cs), "commit"), ""); return nil })
}

func (t *LogTab) commitMenu(a *App, x, y int) {
	c := t.selected()
	if c == nil {
		return
	}
	head := a.Repo.HeadHash()
	isHead := c.Hash == head
	branch := a.CurrentBranch()
	pushed := isHead && a.Repo.IsPushed(c.Hash)
	items := []MenuItem{
		{Label: "Copy Revision Number", Hint: "y", Do: func(a *App) tea.Cmd { return copyToClipboard(a, c.Hash) }},
		{Label: "Show Diff", Hint: "⏎", Do: func(a *App) tea.Cmd { return t.openCommitDiff(a) }},
		{Label: "Compare with Local", Do: func(a *App) tea.Cmd { return OpenCompare(a, c.Hash, short(c.Hash)) }},
		MenuSep(),
		{Label: "Cherry-Pick", Hint: "c", Do: func(a *App) tea.Cmd { return t.cherryPick(a) }},
		{Label: "Checkout Revision '" + short(c.Hash) + "'", Do: func(a *App) tea.Cmd {
			return a.Op("Checkout revision", func() error { return a.Repo.Checkout(c.Hash) }, nil)
		}},
		{Label: "Reset Current Branch to Here…", Disabled: branch == "" && a.Status != nil && !a.Status.Detached, Do: func(a *App) tea.Cmd { OpenReset(a, c); return nil }},
		{Label: "Revert Commit", Do: func(a *App) tea.Cmd {
			return a.Op("Revert commit", func() error { return a.Repo.Revert(c.Hash) }, nil)
		}},
		{Label: "Undo Commit…", Disabled: !isHead || pushed || len(c.Parents) == 0, Do: func(a *App) tea.Cmd {
			return a.Op("Undo commit", func() error { return a.Repo.Reset("HEAD~1", "soft") }, func(a *App) tea.Cmd {
				a.Notify("Commit undone", "Changes are back in Local Changes")
				return nil
			})
		}},
		{Label: "Edit Commit Message…", Disabled: !isHead || pushed, Do: func(a *App) tea.Cmd { OpenEditMessage(a); return nil }},
		MenuSep(),
		{Label: "New Branch…", Do: func(a *App) tea.Cmd { OpenNewBranch(a, c.Hash, short(c.Hash)); return nil }},
		{Label: "New Tag…", Do: func(a *App) tea.Cmd { OpenNewTag(a, c.Hash); return nil }},
		MenuSep(),
		{Label: "Go to Parent Commit", Hint: "←", Disabled: len(c.Parents) == 0, Do: func(a *App) tea.Cmd {
			t.jumpTo(c.Parents[0])
			return t.loadDetails(a)
		}},
		{Label: "Go to Child Commit", Hint: "→", Do: func(a *App) tea.Cmd {
			cmd, _ := t.Key(a, tea.KeyMsg{Type: tea.KeyRight})
			return cmd
		}},
	}
	m := NewMenuAt(Truncate(c.Subject, 50), items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (t *LogTab) Draw(a *App, v View) {
	th := a.Th
	x := 0
	if a.Cfg.BranchesPane {
		bw := max(18, min(a.Cfg.BranchesWidth, v.W()/3))
		bv := v.Sub(0, 0, bw, v.H())
		hdr := bv.Sub(0, 0, bw, 1)
		hdr.Fill(Style{Bg: th.Bg})
		hdr.Text(1, 0, "‹", Style{Fg: th.Dim, Bg: th.Bg})
		hdr.Text(3, 0, "Branches", Style{Fg: th.Fg, Bg: th.Bg, Bold: true})
		a.Hit(Rect{hdr.Abs().X, hdr.Abs().Y, 2, 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.Cfg.BranchesPane = false
				a.Cfg.Save()
				t.setFocus(lfTable)
			}
			return nil
		})
		tv := bv.Sub(0, 1, bw, v.H()-1)
		t.branches.Draw(a, tv)
		a.HitPass(tv.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() || m.RightPress() {
				t.setFocus(lfBranches)
			}
			return nil
		})
		v.VLine(bw, 0, v.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
		abs := v.Abs()
		a.Hit(Rect{abs.X + bw, abs.Y, 1, v.H()}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.Drag(func(m Mouse) tea.Cmd {
					a.Cfg.BranchesWidth = max(18, m.X-abs.X)
					return nil
				})
			}
			return nil
		})
		x = bw + 1
	} else {
		strip := v.Sub(0, 0, 2, v.H())
		strip.Fill(Style{Bg: th.Bg})
		strip.Text(0, 0, "›", Style{Fg: th.Dim, Bg: th.Bg})
		for i, r := range "Branches" {
			if 2+i < v.H() {
				strip.Set(0, 2+i, r, Style{Fg: th.Dim, Bg: th.Bg})
			}
		}
		a.Hit(strip.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.Cfg.BranchesPane = true
				a.Cfg.Save()
				t.setFocus(lfBranches)
			}
			return nil
		})
		v.VLine(2, 0, v.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
		x = 3
	}
	dw := max(30, min(a.Cfg.LogDetailsWidth, (v.W()-x)/2))
	mainW := v.W() - x - dw - 1
	main := v.Sub(x, 0, mainW, v.H())
	t.drawFilterBar(a, main.Sub(0, 0, mainW, 1))
	t.drawTable(a, main.Sub(0, 1, mainW, v.H()-1))
	v.VLine(x+mainW, 0, v.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
	abs := v.Abs()
	a.Hit(Rect{abs.X + x + mainW, abs.Y, 1, v.H()}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.Drag(func(m Mouse) tea.Cmd {
				a.Cfg.LogDetailsWidth = max(30, abs.X+v.W()-m.X)
				return nil
			})
		}
		return nil
	})
	t.drawDetails(a, v.Sub(x+mainW+1, 0, dw, v.H()))
}

func (t *LogTab) drawFilterBar(a *App, v View) {
	th := a.Th
	st := Style{Fg: th.Fg, Bg: th.Bg}
	v.Fill(st)
	fw := min(34, v.W()/3)
	box := v.Sub(0, 0, fw, 1)
	box.Fill(Style{Bg: th.InputBg})
	box.Text(1, 0, "⌕", Style{Fg: th.Dim, Bg: th.InputBg})
	t.text.Focused = t.focus == lfFilter
	inW := fw - 10
	t.text.Draw(a, box.Sub(3, 0, inW, 1), Style{Fg: th.Fg, Bg: th.InputBg})
	a.HitPass(box.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			t.setFocus(lfFilter)
		}
		return nil
	})
	toggle := func(x int, label string, on *bool) {
		s := Style{Fg: th.Dim, Bg: th.InputBg}
		if *on {
			s = Style{Fg: th.Fg, Bg: th.TabSelBg, Bold: true}
		}
		box.Text(x, 0, label, s)
		a.Hit(Rect{box.Abs().X + x, box.Abs().Y, Width(label), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				*on = !*on
				a.saveLogTabs()
				if t.filter.Text != "" {
					return t.Reload(a)
				}
			}
			return nil
		})
	}
	toggle(fw-6, ".*", &t.filter.Regex)
	toggle(fw-3, "Cc", &t.filter.MatchCase)
	if t.focus == lfFilter {
		box.Tint(0, 0, 1, th.InputFocus)
	}
	x := fw + 2
	chip := func(label, value string, open func(x int), clear func()) {
		text := label
		s := st.WithFg(th.Dim)
		if value != "" {
			text = label + ": " + value
			s = st.WithFg(th.Fg)
		}
		text = Truncate(text, 28)
		end := v.Text(x, 0, text, s)
		cx := x
		if value != "" {
			end = v.Text(end+1, 0, "×", st.WithFg(th.Dim))
			a.Hit(Rect{v.Abs().X + end - 1, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					clear()
					a.saveLogTabs()
					return t.Reload(a)
				}
				return nil
			})
		} else {
			end = v.Text(end, 0, " ⌄", st.WithFg(th.Dim))
		}
		a.Hit(Rect{v.Abs().X + cx, v.Abs().Y, Width(text), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				open(v.Abs().X + cx)
			}
			return nil
		})
		x = end + 2
	}
	y := v.Abs().Y + 1
	chip("Branch", strings.Join(t.filter.Branches, ","), func(x int) { t.branchFilterMenu(a, x, y) }, func() { t.filter.Branches = nil })
	chip("User", strings.Join(t.filter.Users, ","), func(x int) { t.userFilterMenu(a, x, y) }, func() { t.filter.Users = nil })
	chip("Date", t.filter.Since, func(x int) { t.dateFilterMenu(a, x, y) }, func() { t.filter.Since = "" })
	chip("Paths", strings.Join(t.filter.Paths, ","), func(x int) { t.pathFilterMenu(a, x, y) }, func() { t.filter.Paths = nil })
	right := v.W() - 2
	icon := "⟳"
	if t.loading {
		icon = string([]rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")[a.spin%10])
	}
	v.Text(right, 0, icon, st)
	a.Hit(Rect{v.Abs().X + right, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			return t.Reload(a)
		}
		return nil
	})
}

func (t *LogTab) branchFilterMenu(a *App, x, y int) {
	set := func(b []string) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			t.filter.Branches = b
			a.saveLogTabs()
			return t.Reload(a)
		}
	}
	items := []MenuItem{
		{Label: "All", Checked: len(t.filter.Branches) == 0, Do: set(nil)},
		{Label: "HEAD", Checked: strings.Join(t.filter.Branches, ",") == "HEAD", Do: set([]string{"HEAD"})},
		MenuHeader("Local"),
	}
	for _, r := range a.Refs {
		if r.Kind == git.LocalBranch {
			items = append(items, MenuItem{Label: r.Name, Checked: strings.Join(t.filter.Branches, ",") == r.Name, Do: set([]string{r.Name})})
		}
	}
	items = append(items, MenuHeader("Remote"))
	for _, r := range a.Refs {
		if r.Kind == git.RemoteBranch {
			items = append(items, MenuItem{Label: r.Name, Checked: strings.Join(t.filter.Branches, ",") == r.Name, Do: set([]string{r.Name})})
		}
	}
	m := NewMenuAt("Branch", items, x, y)
	m.MinWidth = 36
	a.Push(m)
}

func (t *LogTab) userFilterMenu(a *App, x, y int) {
	set := func(u []string) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			t.filter.Users = u
			a.saveLogTabs()
			return t.Reload(a)
		}
	}
	items := []MenuItem{
		{Label: "All", Checked: len(t.filter.Users) == 0, Do: set(nil)},
		{Label: "me", Extra: a.User, Checked: strings.Join(t.filter.Users, ",") == "me", Do: set([]string{"me"})},
		{Label: "Select…", Do: func(a *App) tea.Cmd {
			a.Push(Prompt("Select Users", "Users (comma-separated):", strings.Join(t.filter.Users, ","), "OK", func(a *App, v string) (tea.Cmd, string) {
				var us []string
				for _, u := range strings.Split(v, ",") {
					if u = strings.TrimSpace(u); u != "" {
						us = append(us, u)
					}
				}
				return set(us)(a), ""
			}))
			return nil
		}},
		MenuSep(),
	}
	seen := map[string]bool{}
	for _, c := range t.commits {
		if !seen[c.Author] && len(seen) < 60 {
			seen[c.Author] = true
			au := c.Author
			items = append(items, MenuItem{Label: au, Checked: strings.Join(t.filter.Users, ",") == au, Do: set([]string{au})})
		}
	}
	m := NewMenuAt("User", items, x, y)
	m.MinWidth = 30
	a.Push(m)
}

func (t *LogTab) dateFilterMenu(a *App, x, y int) {
	set := func(s string) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			t.filter.Since = s
			a.saveLogTabs()
			return t.Reload(a)
		}
	}
	items := []MenuItem{
		{Label: "All", Checked: t.filter.Since == "", Do: set("")},
		{Label: "Last 24 hours", Checked: t.filter.Since == "24 hours ago", Do: set("24 hours ago")},
		{Label: "Last 7 days", Checked: t.filter.Since == "7 days ago", Do: set("7 days ago")},
		{Label: "Last 30 days", Checked: t.filter.Since == "30 days ago", Do: set("30 days ago")},
		{Label: "Select…", Do: func(a *App) tea.Cmd {
			a.Push(Prompt("Date Filter", "Since (e.g. 2026-08-01 or '2 weeks ago'):", t.filter.Since, "OK", func(a *App, v string) (tea.Cmd, string) {
				return set(strings.TrimSpace(v))(a), ""
			}))
			return nil
		}},
	}
	m := NewMenuAt("Date", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (t *LogTab) pathFilterMenu(a *App, x, y int) {
	set := func(p []string) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			t.filter.Paths = p
			a.saveLogTabs()
			return t.Reload(a)
		}
	}
	items := []MenuItem{
		{Label: "All", Checked: len(t.filter.Paths) == 0, Do: set(nil)},
		{Label: "Select…", Do: func(a *App) tea.Cmd {
			a.Push(Prompt("Paths Filter", "Paths (comma-separated, relative to repo root):", strings.Join(t.filter.Paths, ","), "OK", func(a *App, v string) (tea.Cmd, string) {
				var ps []string
				for _, p := range strings.Split(v, ",") {
					if p = strings.TrimSpace(p); p != "" {
						ps = append(ps, p)
					}
				}
				return set(ps)(a), ""
			}))
			return nil
		}},
	}
	m := NewMenuAt("Paths", items, x, y)
	m.Searchable = false
	a.Push(m)
}

type refLabel struct {
	text string
	fg   Color
}

func (t *LogTab) refLabels(a *App, c *git.Commit) []refLabel {
	th := a.Th
	var res []refLabel
	locals := map[string]bool{}
	for _, r := range c.Refs {
		if strings.HasPrefix(r, "HEAD -> ") {
			locals[strings.TrimPrefix(r, "HEAD -> ")] = true
		} else if a.Ref(r, git.LocalBranch) != nil {
			locals[r] = true
		}
	}
	for _, r := range c.Refs {
		switch {
		case r == "HEAD":
			res = append(res, refLabel{"◆ HEAD", th.RefHead})
		case strings.HasPrefix(r, "HEAD -> "):
			name := strings.TrimPrefix(r, "HEAD -> ")
			res = append(res, refLabel{"◆ " + name, th.RefHead})
		case strings.HasPrefix(r, "tag: "):
			res = append(res, refLabel{"◇ " + strings.TrimPrefix(r, "tag: "), th.RefTag})
		case locals[r]:
			res = append(res, refLabel{"◇ " + r, th.RefLocal})
		default:
			// Hide origin/x when local x sits on the same commit, IntelliJ-style.
			if _, rest, ok := strings.Cut(r, "/"); ok && locals[rest] {
				for i := range res {
					if strings.HasSuffix(res[i].text, " "+rest) {
						res[i].text = strings.Replace(res[i].text, "◇ ", "◇◇ ", 1)
						res[i].text = strings.Replace(res[i].text, "◆ ", "◆◇ ", 1)
					}
				}
				continue
			}
			res = append(res, refLabel{"◇ " + r, th.RefRemote})
		}
	}
	return res
}

func (t *LogTab) drawTable(a *App, v View) {
	th := a.Th
	h := v.H()
	t.height = h
	v.Fill(Style{Bg: th.Bg})
	if t.err != nil {
		v.Text(2, 1, t.err.Error(), Style{Fg: th.Error, Bg: th.Bg})
		return
	}
	if len(t.commits) == 0 {
		msg := "No commits"
		if t.loading {
			msg = "Loading…"
		} else if t.hasFilters() {
			msg = "No commits matching filters"
		}
		v.Text(max(0, (v.W()-Width(msg))/2), h/3, msg, Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	// Follow the selection only when it moved, so the wheel can scroll past it.
	if t.sel != t.shownSel {
		t.shownSel = t.sel
		if t.sel < t.top {
			t.top = t.sel
		}
		if t.sel >= t.top+h {
			t.top = t.sel - h + 1
		}
	}
	t.top = max(0, min(t.top, len(t.commits)-h))
	w := v.W() - 1
	dateW := 18
	authorW := min(16, max(8, w/8))
	subjW := w - dateW - authorW - 2
	head := ""
	if a.Status != nil {
		head = a.Status.OID
	}
	for i := 0; i < h && t.top+i < len(t.commits); i++ {
		idx := t.top + i
		c := t.commits[idx]
		row := t.rows[idx]
		bg := th.Bg
		selected := idx == t.sel || t.multi[idx]
		if selected {
			bg = th.SelInactive
			if t.focus == lfTable {
				bg = th.Sel
			}
		}
		rv := v.Sub(0, i, w, 1)
		rv.Fill(Style{Bg: bg})
		x := 1
		for _, cell := range row.Cells {
			if x >= subjW/2 {
				break
			}
			if cell.R != ' ' {
				rv.Set(x, 0, cell.R, Style{Fg: th.GraphColor(cell.Color), Bg: bg})
			}
			x++
		}
		x = max(x+1, 4)
		labels := t.refLabels(a, c)
		labelW := 0
		var parts []string
		for _, l := range labels {
			parts = append(parts, l.text)
		}
		if len(parts) > 0 {
			labelW = min(Width(strings.Join(parts, "  "))+2, subjW/3)
		}
		subjSt := Style{Fg: th.Fg, Bg: bg}
		if c.Hash == head {
			subjSt.Bold = true
		}
		rv.TextFit(x, 0, c.Subject, subjW-x-labelW, subjSt)
		if labelW > 0 {
			lx := subjW - labelW + 1
			avail := labelW - 1
			for li, l := range labels {
				if avail <= 1 {
					break
				}
				text := l.text
				if li == len(labels)-1 || Width(text) > avail {
					icon, name, _ := strings.Cut(text, " ")
					text = icon + " " + TruncateLeft(name, max(1, avail-Width(icon)-1))
				}
				lx = rv.Text(lx, 0, text, Style{Fg: l.fg, Bg: bg}) + 2
				avail = subjW - lx
			}
		}
		author := c.Author
		if c.Committer != "" && c.Committer != c.Author {
			author += "*"
		}
		rv.TextFit(subjW+1, 0, author, authorW, Style{Fg: th.Fg, Bg: bg})
		rv.TextFit(subjW+authorW+2, 0, fmtDate(c.Date), dateW, Style{Fg: th.Fg, Bg: bg})
	}
	if len(t.commits) > h {
		DrawScrollbar(a, v.Sub(w, 0, 1, h), t.top, h, len(t.commits), func(top int) { t.top = top })
	}
	abs := v.Abs()
	a.Hit(Rect{abs.X, abs.Y, w, h}, func(m Mouse) tea.Cmd {
		if d := m.Wheel(); d != 0 {
			t.top = max(0, min(len(t.commits)-h, t.top+d*3))
			if t.top+h > len(t.commits)-20 && len(t.commits) >= t.limit {
				t.limit += 3000
				return t.Reload(a)
			}
			return nil
		}
		idx := t.top + m.Y - abs.Y
		if idx < 0 || idx >= len(t.commits) {
			return nil
		}
		if m.LeftPress() || m.RightPress() {
			t.setFocus(lfTable)
			if m.LeftPress() && m.Shift {
				if len(t.multi) == 0 {
					t.anchor = t.sel
				}
				t.sel = idx
				t.multi = map[int]bool{}
				for i := min(t.anchor, idx); i <= max(t.anchor, idx); i++ {
					t.multi[i] = true
				}
			} else if m.LeftPress() && (m.Ctrl || m.Alt) {
				if len(t.multi) == 0 {
					t.multi[t.sel] = true
				}
				t.multi[idx] = !t.multi[idx]
				t.sel = idx
			} else if !(m.RightPress() && t.multi[idx]) {
				t.multi = map[int]bool{}
				t.sel = idx
			}
			cmd := t.loadDetails(a)
			if m.RightPress() {
				t.commitMenu(a, m.X, m.Y)
			}
			if m.Double {
				return tea.Batch(cmd, func() tea.Msg {
					return apply(func(a *App) tea.Cmd { return t.openCommitDiff(a) })
				})
			}
			return cmd
		}
		return nil
	})
}

func (t *LogTab) drawDetails(a *App, v View) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	c := t.selected()
	filesH := v.H() / 2
	if c == nil {
		v.Text(max(0, (v.W()-28)/2), filesH/2, "Select commit to view changes", Style{Fg: th.Dim, Bg: th.Bg})
		v.HLine(0, filesH, v.W(), '─', Style{Fg: th.Border, Bg: th.Bg})
		v.Text(max(0, (v.W()-14)/2), filesH+(v.H()-filesH)/2, "Commit details", Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	fv := v.Sub(0, 0, v.W(), filesH)
	t.files.Draw(a, fv)
	a.HitPass(fv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() || m.RightPress() {
			t.setFocus(lfFiles)
		}
		return nil
	})
	v.HLine(0, filesH, v.W(), '─', Style{Fg: th.Border, Bg: th.Bg})
	dv := v.Sub(1, filesH+1, v.W()-2, v.H()-filesH-1)
	d := t.details
	if d == nil || d.Hash != c.Hash {
		dv.Text(0, 0, "Loading…", Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	var lines []refLabel
	add := func(s string, fg Color) {
		for _, l := range wrapText(s, dv.W()) {
			lines = append(lines, refLabel{l, fg})
		}
	}
	for i, l := range strings.Split(d.Message, "\n") {
		if i == 0 {
			add(l, th.Fg)
			continue
		}
		add(l, th.Fg)
	}
	lines = append(lines, refLabel{"", th.Fg})
	add(fmt.Sprintf("%s %s <%s> on %s at %s", short(d.Hash), d.Author, d.Email, d.Date.Format("02/01/2006"), d.Date.Format("15:04")), th.Dim)
	if d.Committer != d.Author || d.CDate.Sub(d.Date) > time.Minute || d.Date.Sub(d.CDate) > time.Minute {
		add(fmt.Sprintf("committed by %s <%s> on %s at %s", d.Committer, d.CEmail, d.CDate.Format("02/01/2006"), d.CDate.Format("15:04")), th.Dim)
	}
	if len(d.Parents) > 1 {
		var ps []string
		for _, p := range d.Parents {
			ps = append(ps, short(p))
		}
		add("Merge of "+strings.Join(ps, ", "), th.Dim)
	}
	if t.contains != nil {
		lines = append(lines, refLabel{"", th.Fg})
		shown := t.contains
		more := ""
		if len(shown) > 12 {
			more = fmt.Sprintf(" and %d more", len(shown)-12)
			shown = shown[:12]
		}
		add(fmt.Sprintf("In %s: %s%s", plural(len(t.contains), "branch"), strings.Join(shown, ", "), more), th.Link)
	}
	t.detTop = max(0, min(t.detTop, len(lines)-dv.H()))
	for i := 0; i < dv.H() && t.detTop+i < len(lines); i++ {
		l := lines[t.detTop+i]
		st := Style{Fg: l.fg, Bg: th.Bg}
		if t.detTop+i == 0 {
			st.Bold = true
		}
		dv.Text(0, i, l.text, st)
	}
	a.Hit(dv.Abs(), func(m Mouse) tea.Cmd {
		if dd := m.Wheel(); dd != 0 {
			t.detTop = max(0, t.detTop+dd)
		}
		return nil
	})
}
