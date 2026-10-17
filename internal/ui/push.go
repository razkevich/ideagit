package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

type PushDialog struct {
	remote   string
	local    string
	target   *TextInput
	commits  []*git.Commit
	list     *Tree
	files    *Tree
	tags     bool
	setUp    bool
	loading  bool
	editing  bool
	focus    int
	gen      int
	filesGen int
	err      string
}

func (a *App) OpenPush() tea.Cmd {
	st := a.Status
	if st == nil {
		return nil
	}
	if st.Detached || st.Branch == "" {
		a.Error("Cannot push", "HEAD is detached. Check out a branch first.")
		return nil
	}
	remotes := a.Repo.Remotes()
	if len(remotes) == 0 {
		a.Error("Cannot push", "No remotes are configured for this repository")
		return nil
	}
	p := &PushDialog{local: st.Branch, list: NewTree(), files: NewTree()}
	p.list.Focused = true
	p.files.Empty = "Select a commit"
	if st.Upstream != "" {
		rm, br, _ := strings.Cut(st.Upstream, "/")
		p.remote, p.target = rm, NewInput(br)
	} else {
		p.remote = remotes[0]
		for _, r := range remotes {
			if r == "origin" {
				p.remote = r
			}
		}
		p.target = NewInput(st.Branch)
		p.setUp = true
	}
	p.list.OnSelect = func(a *App, n *Node) tea.Cmd { return p.loadFiles(a) }
	a.Push(p)
	return p.load(a)
}

func (p *PushDialog) Dismissable() bool { return false }

func (p *PushDialog) Hint() string {
	return "⏎ push · alt+f force push · e edit target · t push tags · esc cancel"
}

func (p *PushDialog) load(a *App) tea.Cmd {
	p.loading = true
	p.gen++
	gen := p.gen
	repo := a.Repo
	target := p.remote + "/" + p.target.Value()
	return Async(func() func(a *App) tea.Cmd {
		cs, err := repo.Outgoing(target)
		return func(a *App) tea.Cmd {
			if gen != p.gen {
				return nil
			}
			p.loading = false
			if err != nil {
				p.err = err.Error()
			}
			p.commits = cs
			var nodes []*Node
			for _, c := range cs {
				nodes = append(nodes, &Node{ID: c.Hash, Label: c.Subject, Right: short(c.Hash), Check: NoCheck, Data: c})
			}
			p.list.SetRoots(nodes)
			return p.loadFiles(a)
		}
	})
}

func (p *PushDialog) loadFiles(a *App) tea.Cmd {
	n := p.list.Selected()
	if n == nil {
		p.files.SetRoots(nil)
		return nil
	}
	c := n.Data.(*git.Commit)
	repo := a.Repo
	p.filesGen++
	gen := p.filesGen
	return Async(func() func(a *App) tea.Cmd {
		files, _ := repo.CommitFiles(c)
		return func(a *App) tea.Cmd {
			if gen != p.filesGen {
				return nil
			}
			p.files.SetRoots(changedFileNodes(a, "push", files))
			return nil
		}
	})
}

func (p *PushDialog) push(a *App, force bool) tea.Cmd {
	opts := git.PushOptions{Remote: p.remote, Local: p.local, Target: strings.TrimSpace(p.target.Value()), Force: force, Tags: p.tags, SetUpstream: p.setUp}
	if opts.Target == "" {
		p.err = "Target branch can't be empty"
		return nil
	}
	n := len(p.commits)
	a.Pop(p)
	return a.Op("Push", func() error { return a.Repo.Push(opts) }, func(a *App) tea.Cmd {
		a.Notify("Push successful", fmt.Sprintf("Pushed %s to %s/%s", plural(n, "commit"), opts.Remote, opts.Target))
		return nil
	})
}

func (p *PushDialog) Key(a *App, k tea.KeyMsg) tea.Cmd {
	if p.editing {
		switch keyName(k) {
		case "enter", "esc", "tab":
			p.editing = false
			return p.load(a)
		}
		p.target.Key(k)
		return nil
	}
	switch keyName(k) {
	case "esc":
		a.Pop(p)
		return nil
	case "enter", "ctrl+enter", "ctrl+k", "alt+k":
		return p.push(a, false)
	case "ctrl+shift+enter", "alt+f", "ctrl+f":
		return p.push(a, true)
	case "e":
		p.editing = true
		return nil
	case "t":
		p.tags = !p.tags
		return nil
	case "r":
		remotes := a.Repo.Remotes()
		var items []MenuItem
		for _, r := range remotes {
			r := r
			items = append(items, MenuItem{Label: r, Checked: r == p.remote, Do: func(a *App) tea.Cmd { p.remote = r; return p.load(a) }})
		}
		a.Push(NewMenu("Remote", items))
		return nil
	}
	cmd, _ := p.list.Key(a, k)
	return cmd
}

func (p *PushDialog) Draw(a *App, screen View) {
	th := a.Th
	w := min(screen.W()-4, 120)
	h := min(screen.H()-4, 30)
	v := screen.Sub((screen.W()-w)/2, max(1, (screen.H()-h)/3), w, h)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.PopupBorder, Bg: bg}, "Push Commits to "+a.Repo.Name, Style{Fg: th.Fg, Bg: bg, Bold: true})
	x := 2
	x = v.Text(x, 1, "⎇ "+p.local, Style{Fg: th.Fg, Bg: bg, Bold: true})
	x = v.Text(x, 1, " → ", Style{Fg: th.Dim, Bg: bg})
	rx := x
	x = v.Text(x, 1, p.remote, Style{Fg: th.Link, Bg: bg})
	a.Hit(Rect{v.Abs().X + rx, v.Abs().Y + 1, Width(p.remote), 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			return p.Key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
		}
		return nil
	})
	x = v.Text(x, 1, " : ", Style{Fg: th.Dim, Bg: bg})
	if p.editing {
		DrawField(a, v.Sub(x, 1, 30, 1), p.target, true, nil)
	} else {
		label := p.target.Value()
		if p.setUp {
			label += " (new)"
		}
		tx := x
		v.Text(x, 1, label, Style{Fg: th.Link, Bg: bg})
		a.Hit(Rect{v.Abs().X + tx, v.Abs().Y + 1, Width(label), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				p.editing = true
			}
			return nil
		})
	}
	listW := w * 3 / 5
	body := v.Sub(1, 3, w-2, h-6)
	if p.loading {
		body.Text(2, 0, "Loading…", Style{Fg: th.Dim, Bg: bg})
	} else if len(p.commits) == 0 {
		body.Text(2, 0, "Nothing to push", Style{Fg: th.Dim, Bg: bg})
	}
	p.list.Draw(a, body.Sub(0, 0, listW, body.H()))
	body.VLine(listW, 0, body.H(), '│', Style{Fg: th.Border, Bg: bg})
	p.files.Draw(a, body.Sub(listW+1, 0, body.W()-listW-1, body.H()))
	by := h - 2
	DrawCheckbox(a, v, 2, by, "Push tags", &p.tags, false, false)
	if p.err != "" {
		v.TextFit(18, by, p.err, w/2-18, Style{Fg: th.Error, Bg: bg})
	}
	bx := w - 2
	for _, b := range []struct {
		label   string
		primary bool
		do      func() tea.Cmd
	}{
		{"Push", true, func() tea.Cmd { return p.push(a, false) }},
		{"Force Push", false, func() tea.Cmd { return p.push(a, true) }},
		{"Cancel", false, func() tea.Cmd { a.Pop(p); return nil }},
	} {
		bx -= Width(b.label) + 4
		DrawButton(a, v, bx, by, b.label, b.primary, false, b.do)
		bx--
	}
}

func (a *App) OpenUpdate() tea.Cmd {
	sel := 0
	if a.Cfg.UpdateRebase {
		sel = 1
	}
	d := &Dialog{Title: "Update Project", Width: 60}
	d.Fields = []Field{
		&RadioField{Label: "Update type:", Options: []string{"Merge incoming changes into the current branch", "Rebase the current branch on top of incoming changes"}, Sel: &sel},
		&LabelField{Text: "Local changes are stashed and restored automatically.", Fg: a.Th.Dim},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "OK", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		rebase := sel == 1
		a.Cfg.UpdateRebase = rebase
		a.Cfg.Save()
		before := a.Repo.HeadHash()
		return a.Op("Update project", func() error { return a.Repo.Pull(rebase) }, func(a *App) tea.Cmd {
			after := a.Repo.HeadHash()
			if before == after {
				a.Notify("All files are up-to-date", "")
				return nil
			}
			files, _ := a.Repo.DiffNameStatus(before, after)
			out, _ := a.Repo.Read("rev-list", "--count", before+".."+after)
			a.Notify("Project updated", fmt.Sprintf("%s updated in %s commits", plural(len(files), "file"), strings.TrimSpace(out)))
			return nil
		}), true
	}}}
	d.focus = 1
	a.Push(d)
	return nil
}

type ConflictsDialog struct {
	tree *Tree
}

func (a *App) OpenConflicts() {
	c := &ConflictsDialog{tree: NewTree()}
	c.tree.Focused = true
	c.OnData(a)
	a.Push(c)
}

func (c *ConflictsDialog) Dismissable() bool { return false }

func (c *ConflictsDialog) OnData(a *App) {
	th := a.Th
	var nodes []*Node
	if a.Status != nil {
		for _, f := range a.Status.Files {
			if f.Kind != git.Conflict {
				continue
			}
			n := &Node{ID: f.Path, Label: baseOf(f.Path), Extra: dirOf(f.Path), LabelFg: th.Conflict, Right: conflictKind(f), Check: NoCheck, Data: f}
			a.setFileIcon(n, f.Path)
			nodes = append(nodes, n)
		}
	}
	c.tree.SetRoots(nodes)
	if len(nodes) == 0 {
		a.Pop(c)
		a.Notify("All conflicts resolved", "")
	}
}

func conflictKind(f git.FileStatus) string {
	side := func(b byte) string {
		switch b {
		case 'A':
			return "Added"
		case 'D':
			return "Deleted"
		}
		return "Modified"
	}
	return fmt.Sprintf("Yours: %s · Theirs: %s", side(f.X), side(f.Y))
}

func (c *ConflictsDialog) Hint() string {
	return "y accept yours · t accept theirs · m merge in editor · esc close"
}

func (c *ConflictsDialog) selected() []git.FileStatus {
	var res []git.FileStatus
	for _, n := range c.tree.SelectedNodes() {
		res = append(res, n.Data.(git.FileStatus))
	}
	return res
}

func (c *ConflictsDialog) accept(a *App, theirs bool) tea.Cmd {
	files := c.selected()
	return a.Op("Resolve conflicts", func() error {
		for _, f := range files {
			if err := a.Repo.ResolveConflict(f.Path, theirs); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}

func (c *ConflictsDialog) Key(a *App, k tea.KeyMsg) tea.Cmd {
	switch keyName(k) {
	case "esc":
		a.Pop(c)
		return nil
	case "y":
		return c.accept(a, false)
	case "t":
		return c.accept(a, true)
	case "m", "enter", "f4":
		if fs := c.selected(); len(fs) > 0 {
			return a.JumpToSource(fs[0].Path, 0)
		}
		return nil
	}
	cmd, _ := c.tree.Key(a, k)
	return cmd
}

func (c *ConflictsDialog) Draw(a *App, screen View) {
	th := a.Th
	w := min(screen.W()-4, 100)
	h := min(screen.H()-4, 20)
	v := screen.Sub((screen.W()-w)/2, max(1, (screen.H()-h)/3), w, h)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.PopupBorder, Bg: bg}, "Conflicts", Style{Fg: th.Fg, Bg: bg, Bold: true})
	tw := w - 22
	c.tree.Draw(a, v.Sub(1, 1, tw, h-2))
	by := 1
	for _, b := range []struct {
		label string
		fn    func() tea.Cmd
	}{
		{"Accept Yours", func() tea.Cmd { return c.accept(a, false) }},
		{"Accept Theirs", func() tea.Cmd { return c.accept(a, true) }},
		{"Merge…", func() tea.Cmd {
			if fs := c.selected(); len(fs) > 0 {
				return a.EditFile(fs[0].Path, 0)
			}
			return nil
		}},
		{"Close", func() tea.Cmd { a.Pop(c); return nil }},
	} {
		DrawButton(a, v, tw+3, by, b.label, by == 1, false, b.fn)
		by += 2
	}
}
