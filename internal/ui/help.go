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
	{"VCS — IntelliJ keymap (Windows/Linux) · git-style key", [][2]string{
		{"ctrl+k · c", "Commit… (A: amend; in the dialog ctrl+alt+k: Commit and Push)"},
		{"ctrl+shift+k · P", "Push…"},
		{"ctrl+t · p · U", "Update Project (pull)"},
		{"f · F", "Fetch"},
		{"ctrl+shift+` · b", "Git Branches popup"},
		{"alt+` · `", "VCS Operations popup"},
		{"ctrl+alt+z · d", "Rollback (works from the editor too)"},
		{"space · a", "Stage / unstage selection · stage (Local Changes)"},
		{"ctrl+alt+a", "Add to VCS"},
		{"ctrl+d · ⏎", "Show Diff"},
		{"F4 · e", "Jump to Source"},
		{"F6 · m", "Move to Another Changelist"},
		{"M · R", "Merge… · Rebase…"},
		{"S · z / s", "Stash all… · stash / shelve the selection (Local Changes)"},
		{"w · W", "Worktrees (with Claude Code sessions)"},
	}},
	{"Navigation", [][2]string{
		{"Shift Shift · ctrl+p", "Search Everywhere"},
		{"ctrl+shift+a", "Find Action"},
		{"ctrl+shift+n · ctrl+e", "Go to File"},
		{"ctrl+1 / ctrl+9 / ctrl+0", "Project / Git tool window / Commit (⌘1/⌘9/⌘0 in IntelliJ; alt+ works too)"},
		{"ctrl+shift+o · ctrl+↓", "Go to File · Jump to Source"},
		{"ctrl+backspace", "Delete"},
		{"shift+esc · esc", "Hide tool window · back to editor"},
		{"alt+f1", "Select Opened File in Project"},
		{"ctrl+tab", "Switch between Project, editor and Git"},
		{"1…7 · alt+← →", "Git tool window tabs"},
		{"ctrl+f4 · ctrl+w", "Close tab"},
		{"shift+f10 · . · x", "Context menu / actions"},
		{"ctrl+r · F5 · ctrl+alt+y", "Refresh"},
		{"ctrl+shift+c", "Copy path"},
		{"O · ctrl+o", "Projects · shell here"},
		{"ctrl+q · q", "Quit"},
	}},
	{"Trees & lists", [][2]string{
		{"↑↓ j k · ← →", "Move · collapse / expand"},
		{"shift+↑↓, shift/ctrl+click", "Extend selection"},
		{"/", "Speed search"},
		{"ctrl+= / ctrl+- · ctrl+numpad + / -", "Expand / collapse all"},
		{"shift+f6 · ⌦", "Rename · delete"},
		{"alt+insert · n / N", "New file / directory (Project)"},
	}},
	{"Diff viewer", [][2]string{
		{"F7 / shift+F7 · n / N", "Next / previous difference"},
		{"alt+→ / alt+← · ] [", "Next / previous file"},
		{"ctrl+shift+→ · > < a", "Apply chunk to the working file (or click » « ↶)"},
		{"space", "Include / exclude chunk (Commit dialog)"},
		{"v / c / w / x", "Unified view / collapse unchanged / whitespace / word highlight"},
	}},
	{"Log", [][2]string{
		{"ctrl+f · /", "Text or hash filter"},
		{"⏎ · ctrl+d", "Show diff of commit"},
		{"c", "Cherry-pick"},
		{"ctrl+alt+shift+c · y", "Copy revision number"},
		{"← →", "Parent / child commit"},
		{"B · ctrl+b", "Branches pane"},
	}},
	{"Commit dialog", [][2]string{
		{"ctrl+enter · ctrl+k", "Commit"},
		{"ctrl+alt+k", "Commit and Push"},
		{"alt+m", "Toggle amend"},
		{"ctrl+m · alt+h", "Commit message history"},
	}},
	{"Editor (nvim)", [][2]string{
		{"(all keys)", "Go to nvim; ctrl+k / ctrl+t stay vim's"},
		{"ctrl+shift+k, ctrl+shift+`, alt+`", "Push, Branches, VCS Operations work from the editor"},
		{"alt+1 / alt+9 / Shift Shift", "Leave the editor for Project / Git / Search"},
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
	switch keyName(k) {
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
	w := min(screen.W()-4, 100)
	hh := screen.H() - 4
	v := screen.Sub((screen.W()-w)/2, 2, w, hh)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.PopupBorder, Bg: bg}, "Keyboard & Mouse", Style{Fg: th.Fg, Bg: bg, Bold: true})
	lines := h.lines(a)
	inner := hh - 2
	a.Selectable(v.Sub(1, 1, w-2, inner).Abs(), true)
	h.top = max(0, min(h.top, len(lines)-inner))
	for i := 0; i < inner && h.top+i < len(lines); i++ {
		l := lines[h.top+i]
		if l.title {
			v.Text(2, 1+i, l.text, Style{Fg: th.Accent, Bg: bg, Bold: true})
			continue
		}
		v.TextFit(4, 1+i, l.key, 30, Style{Fg: th.Fg, Bg: bg, Bold: true})
		v.TextFit(35, 1+i, l.text, w-37, Style{Fg: th.Dim, Bg: bg})
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
		{Label: "Commit…", Hint: "ctrl+k", Do: func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
		{Label: "Push…", Hint: "P", Do: func(a *App) tea.Cmd { return a.OpenPush() }},
		{Label: "Update Project…", Hint: "ctrl+t", Do: func(a *App) tea.Cmd { return a.OpenUpdate() }},
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
		{Label: "Stash Changes…", Hint: "S", Do: func(a *App) tea.Cmd { OpenStashDialog(a, nil); return nil }},
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
