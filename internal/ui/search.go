package ui

import (
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

// SearchEverywhere is IntelliJ's double-Shift popup: files, git objects and actions.
type SearchEverywhere struct {
	in      *TextInput
	tab     int
	items   []searchItem
	results []scored
	sel     int
	top     int
	files   []string
	loading bool
}

type searchKind int

const (
	skFile searchKind = iota
	skBranch
	skCommit
	skAction
)

type searchItem struct {
	kind   searchKind
	label  string
	detail string
	hint   string
	key    string // text matched against
	do     func(a *App) tea.Cmd
	path   string
	ref    *git.Ref
}

type scored struct {
	item  *searchItem
	score int
	hits  []int
}

var searchTabs = []string{"All", "Files", "Git", "Actions"}

func (a *App) OpenSearch(tab int) tea.Cmd {
	s := &SearchEverywhere{in: NewInput(""), tab: tab, loading: true}
	s.in.Placeholder = "Type / to see commands"
	a.Push(s)
	s.build(a)
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		files := repo.Files()
		return func(a *App) tea.Cmd {
			s.files = files
			s.loading = false
			s.build(a)
			return nil
		}
	})
}

func (s *SearchEverywhere) Dismissable() bool { return true }

func (s *SearchEverywhere) Hint() string {
	return "type to search · tab next category · ↑↓ select · ⏎ open · esc close"
}

func (s *SearchEverywhere) build(a *App) {
	s.items = s.items[:0]
	for _, f := range s.files {
		f := f
		s.items = append(s.items, searchItem{kind: skFile, label: baseOf(f), detail: dirOf(f), key: f, path: f,
			do: func(a *App) tea.Cmd { return a.OpenInEditor(f, 0) }})
	}
	for i := range a.Refs {
		r := a.Refs[i]
		kind := map[git.RefKind]string{git.LocalBranch: "local branch", git.RemoteBranch: "remote branch", git.Tag: "tag"}[r.Kind]
		s.items = append(s.items, searchItem{kind: skBranch, label: r.Name, detail: kind, key: r.Name, ref: &r})
	}
	for _, t := range a.tabs {
		if l, ok := t.(*LogTab); ok {
			for i, c := range l.commits {
				if i >= 2000 {
					break
				}
				c := c
				s.items = append(s.items, searchItem{kind: skCommit, label: c.Subject, detail: short(c.Hash) + "  " + c.Author, key: c.Subject + " " + c.Hash,
					do: func(a *App) tea.Cmd {
						for i, t := range a.tabs {
							if l, ok := t.(*LogTab); ok {
								a.SelectTab(i)
								a.focus = focusGit
								if !l.jumpTo(c.Hash) {
									l.pending = c.Hash
								}
								return l.loadDetails(a)
							}
						}
						return nil
					}})
			}
			break
		}
	}
	for _, act := range a.searchActions() {
		act := act
		s.items = append(s.items, searchItem{kind: skAction, label: act.Label, hint: act.Hint, key: act.Label, do: act.Do})
	}
	s.filter()
}

func (a *App) searchActions() []MenuItem {
	toggle := func(label string, v *bool, after func(a *App)) MenuItem {
		return MenuItem{Label: label, Checked: *v, Do: func(a *App) tea.Cmd {
			*v = !*v
			a.Cfg.Save()
			if after != nil {
				after(a)
			}
			return a.Refresh()
		}}
	}
	return []MenuItem{
		{Label: "Commit…", Hint: "ctrl+k", Do: func(a *App) tea.Cmd { return a.OpenCommit(nil) }},
		{Label: "Push…", Hint: "P", Do: func(a *App) tea.Cmd { return a.OpenPush() }},
		{Label: "Update Project…", Hint: "ctrl+t", Do: func(a *App) tea.Cmd { return a.OpenUpdate() }},
		{Label: "Fetch", Hint: "F", Do: func(a *App) tea.Cmd {
			return a.Op("Fetch", a.Repo.Fetch, func(a *App) tea.Cmd { a.Notify("Fetch successful", ""); return nil })
		}},
		{Label: "Branches…", Hint: "b", Do: func(a *App) tea.Cmd { a.OpenBranches(); return nil }},
		{Label: "New Branch…", Do: func(a *App) tea.Cmd { OpenNewBranch(a, "", a.HeadLabel()); return nil }},
		{Label: "Worktrees…", Hint: "W", Do: func(a *App) tea.Cmd { a.OpenWorktrees(); return nil }},
		{Label: "Open Project / Directory…", Hint: "O", Do: func(a *App) tea.Cmd { a.OpenProjects(); return nil }},
		{Label: "Stash Changes…", Hint: "S", Do: func(a *App) tea.Cmd { OpenStashDialog(a, nil); return nil }},
		{Label: "VCS Operations…", Hint: "`", Do: func(a *App) tea.Cmd { a.OpenVCSOperations(); return nil }},
		{Label: "Show Git Log", Hint: "4", Do: func(a *App) tea.Cmd { a.SelectTab(3); a.focus = focusGit; return nil }},
		{Label: "Local Changes", Hint: "1", Do: func(a *App) tea.Cmd { a.SelectTab(0); a.focus = focusGit; return nil }},
		{Label: "Show Console", Do: func(a *App) tea.Cmd { a.ShowConsole(); a.focus = focusGit; return nil }},
		{Label: "Project Tool Window", Hint: "alt+1", Do: func(a *App) tea.Cmd { a.toggleProject(); return nil }},
		{Label: "Git Tool Window", Hint: "alt+9", Do: func(a *App) tea.Cmd { a.toggleGit(); return nil }},
		{Label: "Open Shell Here", Hint: "ctrl+o", Do: func(a *App) tea.Cmd { return a.OpenShell(a.Repo.Root) }},
		{Label: "Open in IntelliJ", Do: func(a *App) tea.Cmd { return a.OpenInIDE(a.Repo.Root) }},
		{Label: "Refresh", Hint: "ctrl+r", Do: func(a *App) tea.Cmd { return tea.Batch(a.Refresh(), a.reloadLogs()) }},
		{Label: "Keyboard Shortcuts", Hint: "?", Do: func(a *App) tea.Cmd { a.Push(NewHelp()); return nil }},
		toggle("Group by Directory", &a.Cfg.GroupByDir, (*App).regroupFiles),
		toggle("Unified Diff Viewer", &a.Cfg.DiffUnified, nil),
		toggle("Collapse Unchanged Fragments", &a.Cfg.CollapseUnchanged, nil),
		{Label: "Quit", Hint: "q", Do: func(a *App) tea.Cmd { return a.Quit() }},
	}
}

func (s *SearchEverywhere) inTab(it *searchItem) bool {
	switch s.tab {
	case 1:
		return it.kind == skFile
	case 2:
		return it.kind == skBranch || it.kind == skCommit
	case 3:
		return it.kind == skAction
	}
	return true
}

func (s *SearchEverywhere) filter() {
	q := strings.TrimSpace(s.in.Value())
	tab := s.tab
	if strings.HasPrefix(q, "/") {
		q = strings.TrimPrefix(q, "/")
		tab = 3
	}
	s.results = s.results[:0]
	for i := range s.items {
		it := &s.items[i]
		saved := s.tab
		s.tab = tab
		ok := s.inTab(it)
		s.tab = saved
		if !ok {
			continue
		}
		if q == "" {
			if it.kind == skAction || it.kind == skBranch && it.ref.Kind == git.LocalBranch || s.tab == 1 && it.kind == skFile {
				s.results = append(s.results, scored{item: it})
			}
			continue
		}
		target := it.key
		if it.kind == skFile && !strings.Contains(q, "/") {
			// Match the file name first; fall back to the whole path.
			if sc, hits := fuzzyScore(q, it.label); sc > 0 {
				s.results = append(s.results, scored{it, sc + 50, hits})
				continue
			}
		}
		if sc, hits := fuzzyScore(q, target); sc > 0 {
			if it.kind == skFile && strings.Contains(q, "/") {
				hits = nil
			}
			s.results = append(s.results, scored{it, sc, hits})
		}
	}
	sort.SliceStable(s.results, func(i, j int) bool {
		if s.results[i].score != s.results[j].score {
			return s.results[i].score > s.results[j].score
		}
		return len(s.results[i].item.key) < len(s.results[j].item.key)
	})
	if len(s.results) > 300 {
		s.results = s.results[:300]
	}
	// In "All", group by kind like IntelliJ's sections while keeping relevance inside each.
	if tab == 0 {
		sort.SliceStable(s.results, func(i, j int) bool { return s.results[i].item.kind < s.results[j].item.kind })
	}
	s.sel, s.top = 0, 0
}

// fuzzyScore matches q as a subsequence of s, rewarding consecutive runs and word starts.
// It returns 0 when q doesn't match, plus the matched rune positions in s.
func fuzzyScore(q, s string) (int, []int) {
	qr := []rune(strings.ToLower(q))
	sr := []rune(s)
	// Lowercase rune by rune so positions line up with sr (strings.ToLower can change
	// byte lengths, e.g. for invalid UTF-8).
	lr := make([]rune, len(sr))
	for i, r := range sr {
		lr[i] = unicode.ToLower(r)
	}
	if len(qr) == 0 {
		return 1, nil
	}
	if start := runeIndex(lr, qr); start >= 0 {
		hits := make([]int, len(qr))
		for i := range hits {
			hits[i] = start + i
		}
		score := 100 + 10*len(qr) - min(start, 30)
		if start == 0 || isBoundary(sr, start) {
			score += 40
		}
		return score, hits
	}
	var hits []int
	score := 0
	j := 0
	prev := -2
	for i := 0; i < len(lr) && j < len(qr); i++ {
		if lr[i] != qr[j] {
			continue
		}
		bonus := 1
		if i == prev+1 {
			bonus += 5
		}
		if isBoundary(sr, i) {
			bonus += 8
		}
		score += bonus
		hits = append(hits, i)
		prev = i
		j++
	}
	if j < len(qr) {
		return 0, nil
	}
	return score, hits
}

func runeIndex(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func isBoundary(s []rune, i int) bool {
	if i == 0 {
		return true
	}
	p := s[i-1]
	if p == '/' || p == '_' || p == '-' || p == '.' || p == ' ' {
		return true
	}
	return unicode.IsLower(p) && unicode.IsUpper(s[i])
}

func (s *SearchEverywhere) activate(a *App) tea.Cmd {
	if s.sel < 0 || s.sel >= len(s.results) {
		return nil
	}
	it := s.results[s.sel].item
	a.Pop(s)
	if it.kind == skBranch {
		m := NewMenu(it.label, BranchActions(a, *it.ref))
		m.Searchable = false
		a.Push(m)
		return nil
	}
	if it.do != nil {
		return it.do(a)
	}
	return nil
}

func (s *SearchEverywhere) Key(a *App, k tea.KeyMsg) tea.Cmd {
	switch keyName(k) {
	case "esc":
		a.Pop(s)
		return nil
	case "tab":
		s.tab = (s.tab + 1) % len(searchTabs)
		s.filter()
		return nil
	case "shift+tab":
		s.tab = (s.tab + len(searchTabs) - 1) % len(searchTabs)
		s.filter()
		return nil
	case "up", "ctrl+p":
		s.sel = max(0, s.sel-1)
		return nil
	case "down", "ctrl+n":
		s.sel = min(len(s.results)-1, s.sel+1)
		return nil
	case "pgup":
		s.sel = max(0, s.sel-10)
		return nil
	case "pgdown":
		s.sel = min(len(s.results)-1, s.sel+10)
		return nil
	case "enter":
		return s.activate(a)
	case "f4":
		if s.sel >= 0 && s.sel < len(s.results) && s.results[s.sel].item.kind == skFile {
			a.Pop(s)
			return a.JumpToSource(s.results[s.sel].item.path, 0)
		}
		return s.activate(a)
	}
	if s.in.Key(k) {
		s.filter()
	}
	return nil
}

func (s *SearchEverywhere) Draw(a *App, screen View) {
	th := a.Th
	w := min(screen.W()-4, 110)
	h := min(screen.H()-4, 26)
	v := screen.Sub((screen.W()-w)/2, 2, w, h)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	v.Box(Style{Fg: th.PopupBorder, Bg: bg}, "", Style{})
	x := 2
	for i, t := range searchTabs {
		st := Style{Fg: th.Fg, Bg: bg}
		if i == s.tab {
			st = Style{Fg: th.Fg, Bg: th.TabSelBg, Bold: true}
		}
		label := " " + t + " "
		v.Text(x, 1, label, st)
		idx := i
		a.Hit(Rect{v.Abs().X + x, v.Abs().Y + 1, Width(label), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				s.tab = idx
				s.filter()
			}
			return nil
		})
		x += Width(label) + 1
	}
	if s.loading {
		v.TextRight(w-3, 1, "indexing files…", Style{Fg: th.Dim, Bg: bg})
	}
	in := v.Sub(2, 2, w-4, 1)
	in.Fill(Style{Bg: th.InputBg})
	in.Text(0, 0, "⌕", Style{Fg: th.Dim, Bg: th.InputBg})
	s.in.Focused = true
	s.in.Draw(a, in.Sub(2, 0, in.W()-2, 1), Style{Fg: th.Fg, Bg: th.InputBg})
	listY, listH := 4, h-5
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+listH {
		s.top = s.sel - listH + 1
	}
	if len(s.results) == 0 {
		msg := "Nothing found"
		if s.loading {
			msg = "Searching…"
		}
		v.Text(4, listY, msg, Style{Fg: th.Dim, Bg: bg})
	}
	lastKind := searchKind(-1)
	if s.top > 0 {
		lastKind = s.results[s.top-1].item.kind
	}
	row := 0
	for i := s.top; i < len(s.results) && row < listH; i++ {
		r := s.results[i]
		it := r.item
		if s.tab == 0 && it.kind != lastKind && strings.TrimSpace(s.in.Value()) != "" {
			lastKind = it.kind
			if row > 0 || i > 0 {
				name := map[searchKind]string{skFile: "Files", skBranch: "Branches", skCommit: "Commits", skAction: "Actions"}[it.kind]
				v.Text(3, listY+row, name, Style{Fg: th.Dim, Bg: bg})
				row++
				if row >= listH {
					break
				}
			}
		}
		lastKind = it.kind
		rv := v.Sub(1, listY+row, w-2, 1)
		st := Style{Fg: th.Fg, Bg: bg}
		if i == s.sel {
			st.Bg = th.Sel
		}
		rv.Fill(st)
		cx := 2
		icon, ifg := "", th.Dim
		switch it.kind {
		case skFile:
			icon, ifg = a.fileIcon(it.path)
			if icon == "" {
				icon = "▪"
			}
			if ifg == NoColor {
				ifg = th.Dim
			}
		case skBranch:
			icon, ifg = "⎇", th.RefLocal
			if it.ref.Kind == git.RemoteBranch {
				ifg = th.RefRemote
			} else if it.ref.Kind == git.Tag {
				icon, ifg = "◇", th.RefTag
			}
		case skCommit:
			icon, ifg = "●", th.Graph[0]
		case skAction:
			icon = "›"
		}
		cx = rv.Text(cx, 0, icon, st.WithFg(ifg)) + 1
		lx := cx
		cx = rv.TextFit(cx, 0, it.label, rv.W()-cx-20, st)
		if r.hits != nil && (it.kind != skFile || len(r.hits) <= len([]rune(it.label))) {
			rs := []rune(it.label)
			for _, hpos := range r.hits {
				if hpos < len(rs) {
					col := lx + Width(string(rs[:hpos]))
					if col < cx {
						rv.Set(col, 0, rs[hpos], st.WithFg(th.Link).WithBold())
					}
				}
			}
		}
		if it.detail != "" {
			rv.TextFit(cx+2, 0, it.detail, rv.W()-cx-4-Width(it.hint), st.WithFg(th.Dim))
		}
		if it.hint != "" {
			rv.TextRight(rv.W()-2, 0, it.hint, st.WithFg(th.Dim))
		}
		idx := i
		a.Hit(rv.Abs(), func(m Mouse) tea.Cmd {
			if m.Action == tea.MouseActionMotion {
				s.sel = idx
				return nil
			}
			if m.LeftPress() {
				s.sel = idx
				return s.activate(a)
			}
			return nil
		})
		row++
	}
	a.HitPass(v.Abs(), func(m Mouse) tea.Cmd {
		if d := m.Wheel(); d != 0 {
			s.sel = max(0, min(len(s.results)-1, s.sel+d))
		}
		return nil
	})
}
