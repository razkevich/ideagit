package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

type Help struct{ top int }

func NewHelp() *Help { return &Help{} }

func (h *Help) Dismissable() bool { return true }
func (h *Help) Hint() string      { return "↑↓ scroll · esc close" }

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Global", [][2]string{
		{"1…7, alt+1…7", "Switch tab (Local Changes, Shelf, Stash, Log, Console)"},
		{"{ }, alt+← →", "Previous / next tab"},
		{"ctrl+k", "Commit… (modal Commit Changes dialog)"},
		{"P, alt+k", "Push…"},
		{"U, ctrl+t", "Update Project…"},
		{"F", "Fetch"},
		{"b", "Git Branches popup"},
		{"W", "Worktrees (with Claude session activity)"},
		{"O", "Projects / open directory"},
		{"ctrl+o", "Open shell in the current worktree"},
		{"`", "VCS Operations popup"},
		{"ctrl+r, F5", "Refresh"},
		{"ctrl+w", "Close tab"},
		{"q, ctrl+c", "Quit"},
	}},
	{"Trees & lists", [][2]string{
		{"↑↓ j k", "Move"},
		{"shift+↑↓, shift/ctrl+click", "Extend selection"},
		{"← →", "Collapse / expand"},
		{"/", "Speed search"},
		{"* -", "Expand / collapse all"},
		{". shift+F10, right-click", "Context menu"},
		{"tab", "Next pane"},
	}},
	{"Local Changes", [][2]string{
		{"⏎, double-click, o", "Show diff"},
		{"ctrl+z", "Rollback…"},
		{"a", "Add to VCS"},
		{"F6, m / n", "Move to another changelist / new changelist"},
		{"s / z", "Shelve / stash changes"},
		{"i", "Add to .gitignore"},
		{"g / p", "Group by directory / toggle diff preview"},
		{"H", "Show history for file"},
		{"e, F4", "Jump to source ($EDITOR)"},
	}},
	{"Diff viewer", [][2]string{
		{"F7 / shift+F7, n / N", "Next / previous difference"},
		{"[ ]", "Previous / next file"},
		{"a, click » « ↶", "Apply chunk to the working file (revert / take)"},
		{"space", "Include / exclude chunk (Commit dialog)"},
		{"v", "Side-by-side ⇄ unified"},
		{"c / w / x", "Collapse unchanged / whitespace mode / word highlight"},
		{"← → , shift+wheel", "Scroll horizontally"},
	}},
	{"Log", [][2]string{
		{"/ , ctrl+f", "Text or hash filter"},
		{"⏎, double-click", "Show diff of commit"},
		{"c / y", "Cherry-pick / copy hash"},
		{"← →", "Go to parent / child commit"},
		{"B, ctrl+b", "Toggle Branches pane"},
		{"esc", "Clear filters"},
	}},
	{"Commit dialog", [][2]string{
		{"space", "Toggle file / chunk inclusion"},
		{"ctrl+k / alt+k", "Commit / Commit and Push"},
		{"alt+a", "Toggle amend"},
		{"alt+m", "Commit message history"},
	}},
}

func (h *Help) lines(a *App) []struct {
	text  string
	key   string
	title bool
} {
	var res []struct {
		text  string
		key   string
		title bool
	}
	for _, s := range helpSections {
		res = append(res, struct {
			text  string
			key   string
			title bool
		}{s.title, "", true})
		for _, k := range s.keys {
			res = append(res, struct {
				text  string
				key   string
				title bool
			}{k[1], k[0], false})
		}
		res = append(res, struct {
			text  string
			key   string
			title bool
		}{"", "", false})
	}
	return res
}

func (h *Help) Key(a *App, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "up", "k":
		h.top = max(0, h.top-1)
	case "down", "j":
		h.top++
	case "pgdown", " ":
		h.top += 10
	case "pgup":
		h.top = max(0, h.top-10)
	default:
		a.Pop(h)
	}
	return nil
}

func (h *Help) Draw(a *App, screen View) {
	th := a.Th
	w := min(screen.W()-4, 84)
	hh := screen.H() - 4
	v := screen.Sub((screen.W()-w)/2, 2, w, hh)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.Border, Bg: bg}, "Keyboard & Mouse", Style{Fg: th.Fg, Bg: bg, Bold: true})
	lines := h.lines(a)
	inner := hh - 2
	h.top = max(0, min(h.top, len(lines)-inner))
	for i := 0; i < inner && h.top+i < len(lines); i++ {
		l := lines[h.top+i]
		if l.title {
			v.Text(2, 1+i, l.text, Style{Fg: th.Accent, Bg: bg, Bold: true})
			continue
		}
		v.TextFit(4, 1+i, l.key, 28, Style{Fg: th.Fg, Bg: bg, Bold: true})
		v.TextFit(33, 1+i, l.text, w-35, Style{Fg: th.Dim, Bg: bg})
	}
	a.Hit(v.Abs(), func(m Mouse) tea.Cmd {
		if d := m.Wheel(); d != 0 {
			h.top = max(0, h.top+d*2)
		}
		return nil
	})
}

func (a *App) OpenVCSOperations() {
	st := a.Status
	items := []MenuItem{
		{Label: "Commit…", Hint: "^K", Do: func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
		{Label: "Push…", Hint: "P", Do: func(a *App) tea.Cmd { return a.OpenPush() }},
		{Label: "Update Project…", Hint: "^T", Do: func(a *App) tea.Cmd { return a.OpenUpdate() }},
		{Label: "Fetch", Hint: "F", Do: func(a *App) tea.Cmd {
			return a.Op("Fetch", a.Repo.Fetch, func(a *App) tea.Cmd { a.Notify("Fetch successful", ""); return nil })
		}},
		{Label: "Pull…", Do: func(a *App) tea.Cmd { return a.OpenUpdate() }},
		MenuSep(),
		{Label: "Branches…", Hint: "b", Do: func(a *App) tea.Cmd { a.OpenBranches(); return nil }},
		{Label: "New Branch…", Do: func(a *App) tea.Cmd { OpenNewBranch(a, "", a.HeadLabel()); return nil }},
		{Label: "New Tag…", Do: func(a *App) tea.Cmd { OpenNewTag(a, "HEAD"); return nil }},
		{Label: "Merge…", Do: func(a *App) tea.Cmd {
			a.pickRef("Merge into "+a.HeadLabel(), func(a *App, r git.Ref) tea.Cmd {
				return a.Op("Merge", func() error { return a.Repo.Merge(r.Name) }, nil)
			})
			return nil
		}},
		{Label: "Rebase…", Do: func(a *App) tea.Cmd {
			a.pickRef("Rebase "+a.HeadLabel()+" onto", func(a *App, r git.Ref) tea.Cmd {
				return a.Op("Rebase", func() error { return a.Repo.Rebase(r.Name) }, nil)
			})
			return nil
		}},
		MenuSep(),
		{Label: "Stash Changes…", Do: func(a *App) tea.Cmd { OpenStashDialog(a, nil); return nil }},
		{Label: "Unstash Changes…", Do: func(a *App) tea.Cmd { a.SelectTab(2); return nil }},
		{Label: "Shelve Changes…", Do: func(a *App) tea.Cmd {
			if st != nil {
				var files []git.FileStatus
				for _, f := range st.Files {
					if f.Kind != git.Untracked {
						files = append(files, f)
					}
				}
				OpenShelveDialog(a, files, a.CL.Get(a.CL.Active))
			}
			return nil
		}},
		MenuSep(),
		{Label: "Worktrees…", Hint: "W", Do: func(a *App) tea.Cmd { a.OpenWorktrees(); return nil }},
		{Label: "Show Git Log", Do: func(a *App) tea.Cmd { a.SelectTab(3); return nil }},
		{Label: "Show Console", Do: func(a *App) tea.Cmd { a.ShowConsole(); return nil }},
	}
	if st != nil && st.State == git.StateRebasing {
		items = append([]MenuItem{
			{Label: "Continue Rebase", Do: func(a *App) tea.Cmd { return a.Op("Continue rebase", a.Repo.RebaseContinue, nil) }},
			{Label: "Abort Rebase", Do: func(a *App) tea.Cmd { return a.Op("Abort rebase", a.Repo.RebaseAbort, nil) }},
			MenuSep(),
		}, items...)
	}
	if st != nil && st.State == git.StateMerging {
		items = append([]MenuItem{
			{Label: "Resolve Conflicts…", Do: func(a *App) tea.Cmd { a.OpenConflicts(); return nil }},
			{Label: "Abort Merge", Do: func(a *App) tea.Cmd { return a.Op("Abort merge", a.Repo.MergeAbort, nil) }},
			MenuSep(),
		}, items...)
	}
	m := NewMenu("VCS Operations", items)
	m.MinWidth = 40
	a.Push(m)
}

func (a *App) pickRef(title string, fn func(a *App, r git.Ref) tea.Cmd) {
	var items []MenuItem
	for _, r := range a.Refs {
		if r.Kind == git.Tag || r.IsHead {
			continue
		}
		r := r
		items = append(items, MenuItem{Label: r.Name, Do: func(a *App) tea.Cmd { return fn(a, r) }})
	}
	m := NewMenu(title, items)
	m.MinWidth = 40
	a.Push(m)
}
