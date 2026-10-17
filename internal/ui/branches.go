package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

func (a *App) OpenBranches() {
	th := a.Th
	cur := a.CurrentBranch()
	items := []MenuItem{
		{Label: "Update Project…", Icon: "⟳", Hint: "^T", Do: func(a *App) tea.Cmd { return a.OpenUpdate() }},
		{Label: "Commit…", Icon: "✓", Hint: "^K", Do: func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
		{Label: "Push…", Icon: "⇡", Hint: "P", Do: func(a *App) tea.Cmd { return a.OpenPush() }},
		{Label: "Fetch", Icon: "⇣", Hint: "F", Do: func(a *App) tea.Cmd {
			return a.Op("Fetch", a.Repo.Fetch, func(a *App) tea.Cmd { a.Notify("Fetch successful", ""); return nil })
		}},
		MenuSep(),
		{Label: "New Branch…", Icon: "+", Do: func(a *App) tea.Cmd { OpenNewBranch(a, "", a.HeadLabel()); return nil }},
		{Label: "Checkout Tag or Revision…", Icon: "⎇", Do: func(a *App) tea.Cmd {
			a.Push(Prompt("Checkout", "Tag or revision:", "", "Checkout", func(a *App, v string) (tea.Cmd, string) {
				if strings.TrimSpace(v) == "" {
					return nil, "Enter a revision"
				}
				return a.Op("Checkout", func() error { return a.Repo.Checkout(strings.TrimSpace(v)) }, nil), ""
			}))
			return nil
		}},
	}
	refItem := func(r git.Ref) MenuItem {
		icon, fg := "◇", th.Dim
		if r.IsHead {
			icon, fg = "◆", th.RefHead
		} else if isFavorite(r.Name) {
			icon, fg = "★", th.RefHead
		}
		hint := aheadBehind(r.Ahead, r.Behind)
		if r.Kind == git.LocalBranch && r.Upstream != "" && hint == "" {
			hint = ""
		}
		rr := r
		return MenuItem{Label: r.Name, Icon: icon, IconFg: fg, Hint: hint, Sub: func(a *App) []MenuItem { return BranchActions(a, rr) }}
	}
	if len(a.Recent) > 0 {
		var recent []MenuItem
		for _, name := range a.Recent {
			if name == cur {
				continue
			}
			if r := a.Ref(name, git.LocalBranch); r != nil && len(recent) < 5 {
				recent = append(recent, refItem(*r))
			}
		}
		if len(recent) > 0 {
			items = append(items, MenuHeader("Recent"))
			items = append(items, recent...)
		}
	}
	items = append(items, MenuHeader("Local"))
	for _, r := range a.Refs {
		if r.Kind == git.LocalBranch {
			items = append(items, refItem(r))
		}
	}
	var remote, tags []MenuItem
	for _, r := range a.Refs {
		switch r.Kind {
		case git.RemoteBranch:
			remote = append(remote, refItem(r))
		case git.Tag:
			tags = append(tags, refItem(r))
		}
	}
	if len(remote) > 0 {
		items = append(items, MenuHeader("Remote"))
		items = append(items, remote...)
	}
	if len(tags) > 0 {
		items = append(items, MenuHeader("Tags"))
		items = append(items, tags...)
	}
	title := "Git Branches"
	if cur != "" {
		title = "Git Branches · " + cur
	}
	m := NewMenu(title, items)
	m.MinWidth = 52
	a.Push(m)
}

// BranchActions is the IntelliJ branch submenu, shared by the popup and the log's branch tree.
func BranchActions(a *App, r git.Ref) []MenuItem {
	cur := a.CurrentBranch()
	curLabel := cur
	if curLabel == "" {
		curLabel = "HEAD"
	}
	isCur := r.Kind == git.LocalBranch && r.Name == cur
	repo := a.Repo
	var items []MenuItem
	switch r.Kind {
	case git.LocalBranch, git.Tag:
		items = append(items, MenuItem{Label: "Checkout", Disabled: isCur, Do: func(a *App) tea.Cmd {
			return a.Op("Checkout "+r.Name, func() error { return repo.Checkout(r.Name) }, nil)
		}})
	case git.RemoteBranch:
		local := strings.TrimPrefix(r.Name, r.Remote+"/")
		items = append(items, MenuItem{Label: "Checkout", Do: func(a *App) tea.Cmd {
			if a.Ref(local, git.LocalBranch) != nil {
				return a.Op("Checkout "+local, func() error { return repo.Checkout(local) }, nil)
			}
			return a.Op("Checkout "+r.Name, func() error { return repo.CheckoutRemote(r.Name, local) }, nil)
		}})
	}
	items = append(items,
		MenuItem{Label: fmt.Sprintf("New Branch from '%s'…", r.Name), Do: func(a *App) tea.Cmd { OpenNewBranch(a, r.Name, r.Name); return nil }},
		MenuItem{Label: fmt.Sprintf("Checkout and Rebase onto '%s'", curLabel), Disabled: isCur || r.Kind != git.LocalBranch, Do: func(a *App) tea.Cmd {
			return a.Op("Checkout and rebase", func() error { return repo.CheckoutAndRebase(r.Name, curLabel) }, nil)
		}},
		MenuSep(),
		MenuItem{Label: fmt.Sprintf("Compare with '%s'", curLabel), Disabled: isCur, Do: func(a *App) tea.Cmd {
			return a.OpenLog(git.LogFilter{Branches: []string{"HEAD.." + r.Name}})
		}},
		MenuItem{Label: "Show Diff with Working Tree", Do: func(a *App) tea.Cmd { return OpenCompare(a, r.Name, r.Name) }},
		MenuItem{Label: "Show in Log", Do: func(a *App) tea.Cmd { return a.OpenLog(git.LogFilter{Branches: []string{r.Name}}) }},
		MenuSep(),
		MenuItem{Label: fmt.Sprintf("Rebase '%s' onto '%s'", curLabel, r.Name), Disabled: isCur, Do: func(a *App) tea.Cmd {
			return a.Op("Rebase", func() error { return repo.Rebase(r.Name) }, nil)
		}},
		MenuItem{Label: fmt.Sprintf("Merge '%s' into '%s'", r.Name, curLabel), Disabled: isCur, Do: func(a *App) tea.Cmd {
			return a.Op("Merge", func() error { return repo.Merge(r.Name) }, func(a *App) tea.Cmd {
				a.Notify("Merged "+r.Name+" into "+curLabel, "")
				return nil
			})
		}},
	)
	if r.Kind == git.LocalBranch {
		items = append(items, MenuSep())
		if isCur {
			items = append(items, MenuItem{Label: "Update", Hint: "^T", Do: func(a *App) tea.Cmd { return a.OpenUpdate() }})
		} else {
			items = append(items, MenuItem{Label: "Update", Disabled: r.Upstream == "", Do: func(a *App) tea.Cmd {
				return a.Op("Update "+r.Name, func() error { return repo.UpdateBranch(r.Name, r.Upstream) }, nil)
			}})
		}
		items = append(items,
			MenuItem{Label: "Push…", Disabled: !isCur, Do: func(a *App) tea.Cmd { return a.OpenPush() }},
			MenuSep(),
			MenuItem{Label: "Rename…", Do: func(a *App) tea.Cmd {
				a.Push(Prompt("Rename Branch", "New name:", r.Name, "Rename", func(a *App, v string) (tea.Cmd, string) {
					v = strings.TrimSpace(v)
					if v == "" {
						return nil, "Name can't be empty"
					}
					return a.Op("Rename branch", func() error { return repo.RenameBranch(r.Name, v) }, nil), ""
				}))
				return nil
			}},
			MenuItem{Label: "Delete", Disabled: isCur, Do: func(a *App) tea.Cmd { return deleteBranch(a, r) }},
		)
	}
	if r.Kind == git.RemoteBranch {
		items = append(items, MenuSep(), MenuItem{Label: "Delete", Do: func(a *App) tea.Cmd {
			a.Push(Confirm("Delete Remote Branch", fmt.Sprintf("Delete remote branch %s?", r.Name), "Delete", true, func(a *App) tea.Cmd {
				return a.Op("Delete remote branch", func() error { return repo.DeleteRemoteBranch(r.Name) }, nil)
			}))
			return nil
		}})
	}
	if r.Kind == git.Tag {
		items = append(items, MenuSep(), MenuItem{Label: "Delete Tag", Do: func(a *App) tea.Cmd {
			return a.Op("Delete tag", func() error { return repo.DeleteTag(r.Name) }, nil)
		}})
	}
	return items
}

func deleteBranch(a *App, r git.Ref) tea.Cmd {
	repo := a.Repo
	return a.Op("Delete branch "+r.Name, func() error { return repo.DeleteBranch(r.Name, false) }, func(a *App) tea.Cmd {
		a.NotifyActions("Deleted branch "+r.Name, "", noteAction{"Restore", func(a *App) tea.Cmd {
			return a.Op("Restore branch", func() error { return repo.CreateBranch(r.Name, r.Hash, false) }, nil)
		}})
		return nil
	})
}

func OpenNewBranch(a *App, from, fromLabel string) {
	name := NewInput("")
	checkout := true
	overwrite := false
	d := &Dialog{Title: "Create New Branch from " + fromLabel, Width: 64}
	d.Fields = []Field{
		&InputField{Label: "New branch name:", In: name},
		&CheckField{Label: "Checkout branch", Val: &checkout},
		&CheckField{Label: "Overwrite existing branch", Val: &overwrite},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Create", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		n := strings.TrimSpace(name.Value())
		if n == "" {
			d.Err = "Branch name can't be empty"
			return nil, false
		}
		if strings.ContainsAny(n, " ~^:?*[\\") {
			d.Err = "Invalid branch name"
			return nil, false
		}
		if a.Ref(n, git.LocalBranch) != nil && !overwrite {
			d.Err = "Branch '" + n + "' already exists"
			return nil, false
		}
		repo := a.Repo
		return a.Op("Create branch", func() error {
			if overwrite && a.Ref(n, git.LocalBranch) != nil {
				if checkout {
					_, err := repo.Exec("checkout", "-B", n, firstNonEmpty(from, "HEAD"))
					return err
				}
				_, err := repo.Exec("branch", "-f", n, firstNonEmpty(from, "HEAD"))
				return err
			}
			return repo.CreateBranch(n, from, checkout)
		}, nil), true
	}}}
	a.Push(d)
}

func OpenNewTag(a *App, rev string) {
	name := NewInput("")
	msg := NewInput("")
	d := &Dialog{Title: "Create New Tag on " + short(rev), Width: 64}
	d.Fields = []Field{
		&InputField{Label: "Tag name:", In: name, LabelW: 10},
		&InputField{Label: "Message:", In: msg, LabelW: 10},
		&LabelField{Text: "Leave the message empty for a lightweight tag."},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Create Tag", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		n := strings.TrimSpace(name.Value())
		if n == "" {
			d.Err = "Tag name can't be empty"
			return nil, false
		}
		m := msg.Value()
		return a.Op("Create tag", func() error { return a.Repo.CreateTag(n, rev, m) }, nil), true
	}}}
	a.Push(d)
}

func OpenReset(a *App, c *git.Commit) {
	mode := 1
	modes := []string{"soft", "mixed", "hard", "keep"}
	d := &Dialog{Title: "Git Reset", Width: 76}
	d.Fields = []Field{
		&LabelField{Text: fmt.Sprintf("Reset %s to %s \"%s\"", a.HeadLabel(), short(c.Hash), Truncate(c.Subject, 40))},
		&RadioField{Options: []string{
			"Soft — files won't change, differences will be staged for commit",
			"Mixed — files won't change, differences won't be staged",
			"Hard — files will be reverted to the state of the selected commit",
			"Keep — files will be reverted, local changes will be kept",
		}, Sel: &mode},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Reset", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		m := modes[mode]
		return a.Op("Reset", func() error { return a.Repo.Reset(c.Hash, m) }, nil), true
	}}}
	a.Push(d)
}

func OpenEditMessage(a *App) {
	ta := NewTextArea(a.Repo.LastCommitMessage())
	d := &Dialog{Title: "Edit Commit Message", Width: 80}
	f := &areaField{ta: ta, h: 8}
	d.Fields = []Field{f}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "OK", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		msg := strings.TrimSpace(ta.Value())
		if msg == "" {
			d.Err = "Message can't be empty"
			return nil, false
		}
		return a.Op("Edit commit message", func() error { return a.Repo.AmendMessage(msg) }, nil), true
	}}}
	a.Push(d)
}

type areaField struct {
	ta *TextArea
	h  int
}

func (f *areaField) Height() int     { return f.h }
func (f *areaField) Focusable() bool { return true }
func (f *areaField) Draw(a *App, v View, focused bool, d *Dialog) {
	f.ta.Focused = focused
	f.ta.Draw(a, v)
}
func (f *areaField) Key(a *App, k tea.KeyMsg) bool {
	if k.String() == "enter" {
		return false
	}
	return f.ta.Key(k)
}

// OpenCompare shows files that differ between rev and the working tree.
func OpenCompare(a *App, rev, label string) tea.Cmd {
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		files, err := repo.DiffNameStatus(rev, "")
		return func(a *App) tea.Cmd {
			if err != nil {
				a.Error("Compare failed", err.Error())
				return nil
			}
			if len(files) == 0 {
				a.Notify("No differences", "Working tree matches "+label)
				return nil
			}
			var srcs []DiffSource
			for _, f := range files {
				srcs = append(srcs, compareSource(repo, rev, label, f))
			}
			w := NewDiffWindow(srcs[0].Path)
			a.Push(w)
			return w.diff.SetSources(a, srcs, 0, true)
		}
	})
}
