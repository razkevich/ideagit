package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/changelist"
	"github.com/razkevich/ideagit/internal/diff"
	"github.com/razkevich/ideagit/internal/git"
)

type commitFocus int

const (
	cfTree commitFocus = iota
	cfAuthor
	cfAmend
	cfSignoff
	cfHooks
	cfTodo
	cfMessage
	cfDiff
	cfCommit
	cfCommitPush
	cfCancel
	cfCount
)

type CommitDialog struct {
	tree      *Tree
	diff      *DiffView
	msg       *TextArea
	author    *TextInput
	amend     bool
	signoff   bool
	runHooks  bool
	checkTodo bool
	hasHooks  bool
	checked   map[string]bool
	list      string
	focus     commitFocus
	files     []git.FileStatus
	savedMsg  string
	Err       string
	untracked map[string][]string
}

// untrackedUnder lists an untracked folder once per dialog; rebuild runs on every
// checkbox toggle. Folders holding only worktrees are skipped like in Local Changes.
func (d *CommitDialog) untrackedUnder(a *App, dir string) []string {
	if files, ok := d.untracked[dir]; ok {
		return files
	}
	var files []string
	wts := map[string]bool{}
	for _, w := range a.Worktrees {
		wts[filepath.Clean(w.Path)] = true
	}
	if worktreeContainer(filepath.Clean(a.Repo.Abs(dir)), wts) == 0 {
		files = a.Repo.UntrackedUnder(dir)
	}
	d.untracked[dir] = files
	return files
}

func (a *App) OpenCommit(preselect []git.FileStatus) tea.Cmd {
	if a.Status == nil {
		return nil
	}
	d := &CommitDialog{tree: NewTree(), diff: NewDiffView(), msg: NewTextArea(""), author: NewInput(""), checked: map[string]bool{}, runHooks: true, untracked: map[string][]string{}}
	d.author.Placeholder = a.Repo.UserName() + " <" + a.Repo.UserEmail() + ">"
	d.msg.Placeholder = "Commit Message"
	d.hasHooks = a.Repo.HasHooks()
	d.diff.Partial = true
	d.diff.ShowToolbar = true
	d.diff.OnToggle = func(a *App) { d.refreshTree(a) }
	d.diff.OnChange = func(a *App) tea.Cmd {
		if s := d.diff.Source(); s != nil {
			d.tree.SelectID("f:" + s.Path)
		}
		return nil
	}
	d.tree.Focused = true
	d.tree.OnSelect = func(a *App, n *Node) tea.Cmd { return d.preview(a) }
	d.tree.OnCheck = func(a *App, n *Node) tea.Cmd { d.toggle(a, n); return nil }
	d.tree.OnActivate = func(a *App, n *Node) tea.Cmd {
		if _, ok := n.Data.(git.FileStatus); ok {
			d.focus = cfDiff
			d.syncFocus()
		}
		return nil
	}
	d.list = a.CL.Active
	if len(preselect) > 0 {
		if cl := a.CLMap[preselect[0].Path]; cl != "" {
			d.list = cl
		}
		for _, f := range preselect {
			d.checked[f.Path] = true
		}
	} else {
		for _, f := range a.Status.Files {
			if f.Kind != git.Untracked && (a.CLMap[f.Path] == d.list || f.Kind == git.Conflict) {
				d.checked[f.Path] = true
			}
		}
	}
	if a.Status.State != git.StateClean {
		if m := a.Repo.MergeMessage(); m != "" {
			d.msg.SetValue(m)
		}
	}
	if d.msg.Value() == "" {
		if cl := a.CL.Get(d.list); cl != nil && cl.Comment != "" && cl.Comment != cl.Name {
			d.msg.SetValue(cl.Comment)
		}
	}
	d.rebuild(a)
	d.tree.SelectFirstLeaf()
	a.Push(d)
	if d.msg.Value() == "" {
		d.focus = cfMessage
	}
	d.syncFocus()
	return d.preview(a)
}

func (d *CommitDialog) Hint() string {
	if d.focus == cfDiff {
		return d.diff.Hint() + " · tab next"
	}
	return "space check · tab next field · ^K commit · alt+K commit and push · alt+M message history · esc cancel"
}

func (d *CommitDialog) OnData(a *App) {
	d.rebuild(a)
}

func (d *CommitDialog) rebuild(a *App) {
	th := a.Th
	d.files = nil
	var inList, unversioned []git.FileStatus
	for _, f := range a.Status.Files {
		switch {
		case f.Kind == git.Untracked:
			if !f.IsDir {
				unversioned = append(unversioned, f)
				break
			}
			for _, p := range d.untrackedUnder(a, f.Path) {
				if len(unversioned) < 500 {
					unversioned = append(unversioned, git.FileStatus{Path: p, Kind: git.Untracked, X: '?', Y: '?'})
				}
			}
		case a.CLMap[f.Path] == d.list || f.Kind == git.Conflict:
			inList = append(inList, f)
		}
	}
	d.files = append(append(d.files, inList...), unversioned...)
	mk := func(f git.FileStatus) *Node {
		n := &Node{ID: "f:" + f.Path, Label: baseOf(f.Path), Extra: dirOf(f.Path), LabelFg: th.KindColor(f.Kind), Data: f, Check: Unchecked}
		if d.checked[f.Path] {
			n.Check = Checked
			if ex := d.diff.Excluded[f.Path]; len(ex) > 0 {
				for _, v := range ex {
					if v {
						n.Check = Partial
						break
					}
				}
			}
		}
		return n
	}
	group := func(id, label string, files []git.FileStatus, collapsed bool) *Node {
		g := &Node{ID: id, Label: label, Bold: true, Collapsed: collapsed}
		on := 0
		for _, f := range files {
			n := mk(f)
			if n.Check != Unchecked {
				on++
			}
			g.Children = append(g.Children, n)
		}
		g.Extra = fmt.Sprintf("%d of %s", on, plural(len(files), "file"))
		switch {
		case on == 0:
			g.Check = Unchecked
		case on == len(files):
			g.Check = Checked
		default:
			g.Check = Partial
		}
		return g
	}
	roots := []*Node{group("g:list", d.list, inList, false)}
	if len(unversioned) > 0 {
		anyChecked := false
		for _, f := range unversioned {
			anyChecked = anyChecked || d.checked[f.Path]
		}
		roots = append(roots, group("g:unv", "Unversioned Files", unversioned, !anyChecked))
	}
	d.tree.SetRoots(roots)
}

func (d *CommitDialog) refreshTree(a *App) { d.rebuild(a) }

func (d *CommitDialog) toggle(a *App, n *Node) {
	leaves := n.Leaves()
	target := n.Check != Checked
	for _, l := range leaves {
		if f, ok := l.Data.(git.FileStatus); ok {
			d.checked[f.Path] = target
			if target {
				delete(d.diff.Excluded, f.Path)
			}
		}
	}
	d.rebuild(a)
}

func (d *CommitDialog) preview(a *App) tea.Cmd {
	n := d.tree.Selected()
	if n == nil {
		return nil
	}
	f, ok := n.Data.(git.FileStatus)
	if !ok {
		return nil
	}
	var srcs []DiffSource
	idx := 0
	for _, x := range d.files {
		if x.Path == f.Path {
			idx = len(srcs)
		}
		srcs = append(srcs, localSource(a.Repo, a.headHash(), x))
	}
	return d.diff.SetSources(a, srcs, idx, false)
}

func (d *CommitDialog) syncFocus() {
	d.tree.Focused = d.focus == cfTree
	d.diff.Focused = d.focus == cfDiff
	d.msg.Focused = d.focus == cfMessage
	d.author.Focused = d.focus == cfAuthor
}

func (d *CommitDialog) cycle(dir int) {
	for {
		d.focus = (d.focus + commitFocus(dir) + cfCount) % cfCount
		if d.focus == cfHooks && !d.hasHooks {
			continue
		}
		break
	}
	d.syncFocus()
}

func (d *CommitDialog) Key(a *App, k tea.KeyMsg) tea.Cmd {
	s := k.String()
	switch s {
	case "esc":
		if d.focus == cfTree && d.tree.Typing() {
			break
		}
		a.Pop(d)
		return nil
	case "tab":
		d.cycle(1)
		return nil
	case "shift+tab":
		d.cycle(-1)
		return nil
	case "ctrl+k", "ctrl+s":
		return d.commit(a, false)
	case "alt+k":
		return d.commit(a, true)
	case "alt+m", "ctrl+h":
		d.history(a)
		return nil
	case "alt+a":
		d.setAmend(a, !d.amend)
		return nil
	}
	switch d.focus {
	case cfTree:
		cmd, _ := d.tree.Key(a, k)
		return cmd
	case cfDiff:
		cmd, _ := d.diff.Key(a, k)
		return cmd
	case cfMessage:
		d.msg.Key(k)
	case cfAuthor:
		if s == "enter" {
			d.cycle(1)
			return nil
		}
		d.author.Key(k)
	case cfAmend, cfSignoff, cfHooks, cfTodo:
		if s == " " || s == "enter" {
			switch d.focus {
			case cfAmend:
				d.setAmend(a, !d.amend)
			case cfSignoff:
				d.signoff = !d.signoff
			case cfHooks:
				d.runHooks = !d.runHooks
			case cfTodo:
				d.checkTodo = !d.checkTodo
			}
		}
	case cfCommit, cfCommitPush, cfCancel:
		switch s {
		case "left":
			if d.focus > cfCommit {
				d.focus--
			}
		case "right":
			if d.focus < cfCancel {
				d.focus++
			}
		case "enter", " ":
			switch d.focus {
			case cfCommit:
				return d.commit(a, false)
			case cfCommitPush:
				return d.commit(a, true)
			default:
				a.Pop(d)
			}
		}
	}
	return nil
}

func (d *CommitDialog) setAmend(a *App, v bool) {
	d.amend = v
	if v {
		d.savedMsg = d.msg.Value()
		if strings.TrimSpace(d.savedMsg) == "" {
			d.msg.SetValue(a.Repo.LastCommitMessage())
		}
	} else if strings.TrimSpace(d.savedMsg) == "" {
		d.msg.SetValue("")
	}
}

func (d *CommitDialog) history(a *App) {
	if len(a.Cfg.CommitHistory) == 0 {
		a.Notify("No commit message history yet", "")
		return
	}
	var items []MenuItem
	for _, m := range a.Cfg.CommitHistory {
		m := m
		items = append(items, MenuItem{Label: firstLine(m), Do: func(a *App) tea.Cmd {
			d.msg.SetValue(m)
			d.focus = cfMessage
			d.syncFocus()
			return nil
		}})
	}
	menu := NewMenu("Commit Message History", items)
	menu.MinWidth = 60
	a.Push(menu)
}

var todoRe = regexp.MustCompile(`\b(TODO|FIXME)\b`)

func (d *CommitDialog) commit(a *App, push bool) tea.Cmd {
	msg := strings.TrimSpace(d.msg.Value())
	if msg == "" {
		d.Err = "Commit message is empty"
		d.focus = cfMessage
		d.syncFocus()
		return nil
	}
	var req git.CommitRequest
	req.Message = msg
	req.Amend = d.amend
	req.Author = strings.TrimSpace(d.author.Value())
	req.SignOff = d.signoff
	req.NoVerify = d.hasHooks && !d.runHooks
	repo := a.Repo
	todos := 0
	for _, f := range d.files {
		if !d.checked[f.Path] {
			continue
		}
		if f.Kind == git.Conflict {
			if b, ok := repo.ReadWorktree(f.Path); ok && strings.Contains(string(b), "<<<<<<<") {
				d.Err = f.Path + " still contains conflict markers"
				return nil
			}
		}
		cf := git.CommitFile{Path: f.Path, OrigPath: f.OrigPath, Deleted: f.Kind == git.Deleted}
		ex := d.diff.Excluded[f.Path]
		partial := false
		for _, v := range ex {
			partial = partial || v
		}
		if partial && !cf.Deleted {
			old, _ := repo.Show("HEAD", firstNonEmpty(f.OrigPath, f.Path))
			cur, _ := repo.ReadWorktree(f.Path)
			r := diff.Compute(old, cur, diff.IgnoreNone)
			if !r.Binary {
				cf.Content = r.Merge(func(bi int) bool { return !ex[bi] })
			}
		}
		if d.checkTodo && !cf.Deleted {
			if b, ok := repo.ReadWorktree(f.Path); ok {
				old, _ := repo.Show("HEAD", firstNonEmpty(f.OrigPath, f.Path))
				r := diff.Compute(old, b, diff.IgnoreNone)
				for _, blk := range r.Blocks {
					for _, line := range r.New[blk.N0:blk.N1] {
						if todoRe.MatchString(line) {
							todos++
						}
					}
				}
			}
		}
		req.Files = append(req.Files, cf)
	}
	if len(req.Files) == 0 && !d.amend && (a.Status == nil || a.Status.State != git.StateMerging) {
		d.Err = "No files selected for commit"
		return nil
	}
	run := func(a *App) tea.Cmd {
		a.Pop(d)
		a.Cfg.RememberMessage(msg)
		count := len(req.Files)
		return a.Op("Commit", func() error {
			_, err := repo.Commit(req)
			return err
		}, func(a *App) tea.Cmd {
			first := firstLine(msg)
			a.Notify(fmt.Sprintf("%s committed", plural(count, "file")), first)
			if push {
				return a.OpenPush()
			}
			return nil
		})
	}
	if todos > 0 {
		a.Push(Confirm("TODO", fmt.Sprintf("%s found in the changed code. Commit anyway?", plural(todos, "TODO item")), "Commit", false, run))
		return nil
	}
	return run(a)
}

func (d *CommitDialog) Draw(a *App, screen View) {
	th := a.Th
	W := min(screen.W()-2, 150)
	H := screen.H() - 2
	x0 := (screen.W() - W) / 2
	v := screen.Sub(x0, 1, W, H)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.Border, Bg: bg}, "Commit Changes", Style{Fg: th.Fg, Bg: bg, Bold: true})
	inner := v.Sub(1, 1, W-2, H-2)
	iw, ih := inner.W(), inner.H()

	optW := min(36, iw/3)
	topH := max(6, (ih-4)*2/5)
	msgH := max(3, min(8, ih/6))
	diffH := ih - topH - msgH - 4

	// Changelist selector + file tree.
	row := inner.Sub(0, 0, iw-optW-1, 1)
	row.Fill(Style{Bg: bg})
	row.Text(1, 0, "Changelist:", Style{Fg: th.Fg, Bg: bg})
	label := d.list + " ▾"
	row.Text(13, 0, label, Style{Fg: th.Link, Bg: bg})
	a.Hit(Rect{row.Abs().X + 13, row.Abs().Y, Width(label), 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			d.chooseList(a, row.Abs().X+13, row.Abs().Y+1)
		}
		return nil
	})
	tv := inner.Sub(0, 1, iw-optW-1, topH-1)
	d.tree.Draw(a, tv)
	a.HitPass(tv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			d.focus = cfTree
			d.syncFocus()
		}
		return nil
	})
	inner.VLine(iw-optW-1, 0, topH, '│', Style{Fg: th.Border, Bg: bg})

	// Options panel.
	ov := inner.Sub(iw-optW, 0, optW, topH)
	ov.Fill(Style{Bg: bg})
	ov.Text(1, 0, "Git", Style{Fg: th.Fg, Bg: bg, Bold: true})
	ov.Text(1, 1, "Author:", Style{Fg: th.Fg, Bg: bg})
	DrawField(a, ov.Sub(9, 1, optW-10, 1), d.author, d.focus == cfAuthor, func() { d.focus = cfAuthor; d.syncFocus() })
	amend := d.amend
	DrawCheckbox(a, ov, 1, 2, "Amend commit", &amend, d.focus == cfAmend, false)
	if amend != d.amend {
		d.setAmend(a, amend)
	}
	DrawCheckbox(a, ov, 1, 3, "Sign-off commit", &d.signoff, d.focus == cfSignoff, false)
	y := 4
	if d.hasHooks {
		DrawCheckbox(a, ov, 1, y, "Run Git hooks", &d.runHooks, d.focus == cfHooks, false)
		y++
	}
	y++
	if y < topH {
		ov.Text(1, y, "Before Commit", Style{Fg: th.Fg, Bg: bg, Bold: true})
		y++
	}
	if y < topH {
		DrawCheckbox(a, ov, 1, y, "Check TODO", &d.checkTodo, d.focus == cfTodo, false)
	}

	// Commit message.
	my := topH
	inner.HLine(0, my, iw, '─', Style{Fg: th.Border, Bg: bg})
	inner.Text(1, my, " Commit Message ", Style{Fg: th.Fg, Bg: bg, Bold: true})
	hist := " ⟲ History "
	hx := iw - Width(hist) - 1
	inner.Text(hx, my, hist, Style{Fg: th.Link, Bg: bg})
	a.Hit(Rect{inner.Abs().X + hx, inner.Abs().Y + my, Width(hist), 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			d.history(a)
		}
		return nil
	})
	mv := inner.Sub(1, my+1, iw-2, msgH)
	d.msg.Draw(a, mv)
	a.HitPass(mv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			d.focus = cfMessage
			d.syncFocus()
		}
		return nil
	})

	// Diff.
	dy := my + 1 + msgH
	inner.HLine(0, dy, iw, '─', Style{Fg: th.Border, Bg: bg})
	inner.Text(1, dy, " Diff ", Style{Fg: th.Fg, Bg: bg, Bold: true})
	if diffH > 3 {
		dv := inner.Sub(0, dy+1, iw, diffH)
		d.diff.Draw(a, dv)
		a.HitPass(dv.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				d.focus = cfDiff
				d.syncFocus()
			}
			return nil
		})
	}

	// Buttons.
	by := ih - 1
	inner.FillRect(0, by, iw, 1, Style{Bg: bg})
	if d.Err != "" {
		inner.TextFit(1, by, "⚠ "+d.Err, iw/2, Style{Fg: th.Error, Bg: bg})
	} else {
		n := 0
		for _, f := range d.files {
			if d.checked[f.Path] {
				n++
			}
		}
		inner.TextFit(1, by, fmt.Sprintf("%d of %s selected", n, plural(len(d.files), "file")), iw/2, Style{Fg: th.Dim, Bg: bg})
	}
	bx := iw - 1
	labels := []struct {
		f       commitFocus
		label   string
		primary bool
		do      func() tea.Cmd
	}{
		{cfCancel, "Cancel", false, func() tea.Cmd { a.Pop(d); return nil }},
		{cfCommitPush, "Commit and Push…", false, func() tea.Cmd { return d.commit(a, true) }},
		{cfCommit, "Commit", true, func() tea.Cmd { return d.commit(a, false) }},
	}
	for _, b := range labels {
		bx -= Width(b.label) + 4
		DrawButton(a, inner, bx, by, b.label, b.primary, d.focus == b.f, b.do)
		bx--
	}
}

func (d *CommitDialog) chooseList(a *App, x, y int) {
	var items []MenuItem
	for _, cl := range a.CL.Lists {
		cl := cl
		items = append(items, MenuItem{Label: cl.Name, Checked: cl.Name == d.list, Do: func(a *App) tea.Cmd {
			d.selectList(a, cl)
			return d.preview(a)
		}})
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (d *CommitDialog) selectList(a *App, cl *changelist.List) {
	d.list = cl.Name
	d.checked = map[string]bool{}
	for _, f := range a.Status.Files {
		if a.CLMap[f.Path] == cl.Name {
			d.checked[f.Path] = true
		}
	}
	if strings.TrimSpace(d.msg.Value()) == "" && cl.Comment != "" && cl.Comment != cl.Name {
		d.msg.SetValue(cl.Comment)
	}
	d.rebuild(a)
	d.tree.SelectFirstLeaf()
}

func copyToClipboard(a *App, s string) tea.Cmd {
	return func() tea.Msg {
		for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
			if err := runWithInput(c, s); err == nil {
				return apply(func(a *App) tea.Cmd { a.Notify("Copied to clipboard", s); return nil })
			}
		}
		// OSC 52 works over SSH and in most modern terminals.
		fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", b64(s))
		return apply(func(a *App) tea.Cmd { a.Notify("Copied to clipboard", s); return nil })
	}
}
