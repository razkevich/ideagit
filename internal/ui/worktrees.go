package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/claude"
	"github.com/razkevich/ideagit/internal/git"
)

// SwitchMsg asks the root model to reopen the app on another repository or worktree.
type SwitchMsg struct{ Dir string }

func (a *App) SwitchTo(dir string) tea.Cmd {
	return func() tea.Msg { return SwitchMsg{Dir: dir} }
}

// scanClaude refreshes Claude Code session activity for every worktree.
func (a *App) scanClaude() tea.Cmd {
	if a.claudeScanning || len(a.Worktrees) == 0 {
		return nil
	}
	a.claudeScanning = true
	a.lastClaudeScan = time.Now()
	paths := make([]string, len(a.Worktrees))
	for i, w := range a.Worktrees {
		paths[i] = w.Path
	}
	if a.Repo.Root != "" && !contains(paths, a.Repo.Root) {
		paths = append(paths, a.Repo.Root)
	}
	return Async(func() func(a *App) tea.Cmd {
		sessions := claude.Running()
		res := make(map[string]claude.Activity, len(paths))
		for _, p := range paths {
			res[p] = claude.ActivityFor(p, sessions, paths)
		}
		return func(a *App) tea.Cmd {
			a.claudeScanning = false
			a.Claude = res
			return nil
		}
	})
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// loadWorktreeStatus fills dirty/ahead/merged for worktrees in small batches so the
// window fills in progressively.
func (a *App) loadWorktreeStatus() tea.Cmd {
	if a.wtStatusLoading {
		return nil
	}
	a.wtStatusLoading = true
	repo := a.Repo
	base := repo.DefaultBranch()
	wts := append([]*git.Worktree(nil), a.Worktrees...)
	var step func(from int) tea.Cmd
	step = func(from int) tea.Cmd {
		return Async(func() func(a *App) tea.Cmd {
			to := min(len(wts), from+12)
			results := make([]git.Worktree, to-from)
			done := make(chan struct{}, to-from)
			for i, w := range wts[from:to] {
				// Copies: the UI keeps drawing the originals meanwhile.
				results[i] = *w
				go func(cp *git.Worktree) {
					repo.WorktreeStatus(cp, base)
					done <- struct{}{}
				}(&results[i])
			}
			for i := from; i < to; i++ {
				<-done
			}
			return func(a *App) tea.Cmd {
				for _, r := range results {
					for _, w := range a.Worktrees {
						if w.Path == r.Path && w.Head == r.Head {
							w.Dirty, w.Ahead, w.Behind, w.Merged, w.StatusOK = r.Dirty, r.Ahead, r.Behind, r.Merged, r.StatusOK
						}
					}
				}
				if to < len(wts) {
					return step(to)
				}
				a.wtStatusLoading = false
				a.wtStatusAt = time.Now()
				return nil
			}
		})
	}
	return step(0)
}

func (a *App) currentWorktree() *git.Worktree {
	for _, w := range a.Worktrees {
		if w.Current {
			return w
		}
	}
	return nil
}

// claudeSummary counts open sessions and how many worktrees have one mid-turn.
func (a *App) claudeSummary() (sessions, worktrees, working int) {
	for _, act := range a.Claude {
		if act.Open == 0 {
			continue
		}
		sessions += act.Open
		worktrees++
		if act.Working() {
			working++
		}
	}
	return
}

func (a *App) drawTitleBar(v View) {
	th := a.Th
	bg := th.Frame
	st := Style{Fg: th.Fg, Bg: bg}
	v.Fill(st)
	x := 1
	item := func(text string, fg Color, bold bool, fn func()) {
		s := Style{Fg: fg, Bg: bg, Bold: bold}
		end := v.Text(x, 0, text, s)
		a.Hit(Rect{v.Abs().X + x, v.Abs().Y, end - x, 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				fn()
			}
			return nil
		})
		x = end + 1
	}
	project := filepath.Base(a.Repo.MainRootCached())
	item("▣ "+project+" ⌄", th.Fg, true, func() { a.OpenProjects() })
	x++
	if wt := a.currentWorktree(); wt != nil && wt.Origin != git.OriginMain {
		label := "⌥ " + Truncate(wt.Name(), 28)
		item(label+" ⌄", th.Fg, false, func() { a.OpenWorktrees() })
		if tag := wt.Origin.Tag(); tag != "" {
			v.Text(x, 0, tag, Style{Fg: th.Dim, Bg: bg})
			x += Width(tag) + 2
		}
	} else {
		n := len(a.Worktrees)
		label := "⌥ worktrees"
		if n > 1 {
			label = fmt.Sprintf("⌥ %d worktrees", n)
		}
		item(label+" ⌄", th.Fg, false, func() { a.OpenWorktrees() })
		x++
	}
	right := v.W() - 1
	path := relHome(a.Repo.Root)
	if right-x > 12 && !a.showProject() {
		p := TruncateLeft(path, right-x)
		px := right - Width(p)
		v.Text(px, 0, p, Style{Fg: th.Faint, Bg: bg})
		a.Hit(Rect{v.Abs().X + px, v.Abs().Y, Width(p), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				return copyToClipboard(a, a.Repo.Root)
			}
			return nil
		})
	}
}

func (a *App) drawClaudeBadge(v View, x, y int, act claude.Activity, bg Color, long bool) int {
	th := a.Th
	if act.Open == 0 {
		if long || act.LastWrite.IsZero() {
			return x
		}
		return v.Text(x, y, "claude "+ago(act.LastWrite), Style{Fg: th.Faint, Bg: bg})
	}
	if act.Working() {
		frames := []rune("◐◓◑◒")
		label := "claude working"
		if act.Open > 1 {
			label = fmt.Sprintf("claude working (%d sessions)", act.Open)
		}
		x = v.Text(x, y, string(frames[(a.spin/3)%len(frames)])+" ", Style{Fg: th.Warning, Bg: bg})
		return v.Text(x, y, label, Style{Fg: th.Warning, Bg: bg})
	}
	label := "claude idle"
	if act.Open > 1 {
		label = fmt.Sprintf("claude idle (%d sessions)", act.Open)
	}
	x = v.Text(x, y, "○ ", Style{Fg: th.Success, Bg: bg})
	return v.Text(x, y, label, Style{Fg: th.Success, Bg: bg})
}

// --- Worktrees window ---

type WorktreesWindow struct {
	tree   *Tree
	sortBy int
	filter *TextInput
	typing bool
}

func (a *App) OpenWorktrees() {
	w := &WorktreesWindow{tree: NewTree(), filter: NewInput("")}
	w.filter.Placeholder = "Filter worktrees"
	w.tree.Focused = true
	w.tree.OnActivate = func(a *App, n *Node) tea.Cmd {
		if wt, ok := n.Data.(*git.Worktree); ok {
			return w.switchTo(a, wt)
		}
		return nil
	}
	w.tree.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { w.menu(a, x, y); return nil }
	a.Push(w)
	w.OnData(a)
	if wt := a.currentWorktree(); wt != nil {
		w.tree.SelectID("w:" + wt.Path)
	}
	a.pendingCmds = append(a.pendingCmds, a.loadWorktreeStatus(), a.scanClaude())
}

func (w *WorktreesWindow) Dismissable() bool { return true }

func (w *WorktreesWindow) Hint() string {
	return "⏎ switch · n new · ⌦ remove · C cleanup merged · s shell · i IntelliJ · y copy path · o sort · / filter · esc close"
}

func (w *WorktreesWindow) OnData(a *App) {
	th := a.Th
	groups := map[git.WorktreeOrigin]*Node{}
	var order []git.WorktreeOrigin
	q := strings.ToLower(w.filter.Value())
	wts := append([]*git.Worktree(nil), a.Worktrees...)
	sort.SliceStable(wts, func(i, j int) bool {
		if w.sortBy == 1 {
			return wts[i].Name() < wts[j].Name()
		}
		ai, aj := a.Claude[wts[i].Path], a.Claude[wts[j].Path]
		ti, tj := latest(wts[i].Activity, ai.LastWrite), latest(wts[j].Activity, aj.LastWrite)
		if (ai.Open > 0) != (aj.Open > 0) {
			return ai.Open > 0
		}
		return ti.After(tj)
	})
	for _, wt := range wts {
		if q != "" && !strings.Contains(strings.ToLower(wt.Path+" "+wt.Branch), q) {
			continue
		}
		g, ok := groups[wt.Origin]
		if !ok {
			g = &Node{ID: "g:" + wt.Origin.String(), Label: wt.Origin.String(), Bold: true, Check: NoCheck}
			groups[wt.Origin] = g
			order = append(order, wt.Origin)
		}
		act := a.Claude[wt.Path]
		n := &Node{ID: "w:" + wt.Path, Label: wt.Name(), Check: NoCheck, Data: wt}
		branch := wt.Branch
		if wt.Detached {
			branch = "detached " + short(wt.Head)
		}
		n.Extra = "⎇ " + branch
		var right []string
		switch {
		case act.Working():
			right = append(right, "● claude working")
			n.RightFg = th.Warning
		case act.Open > 0:
			right = append(right, "○ claude idle")
			n.RightFg = th.Success
		}
		if wt.StatusOK {
			if wt.Dirty > 0 {
				right = append(right, plural(wt.Dirty, "change"))
			}
			if ab := aheadBehind(wt.Ahead, wt.Behind); ab != "" {
				right = append(right, ab)
			}
			if wt.Merged {
				right = append(right, "merged")
			}
		}
		if wt.Prunable {
			right = append(right, "missing")
			n.LabelFg = th.Faint
			n.Strike = true
		}
		if wt.Locked {
			right = append(right, "locked")
		}
		right = append(right, ago(latest(wt.Activity, act.LastWrite)))
		n.Right = strings.Join(right, " · ")
		if wt.Current {
			n.Icon, n.IconFg = "◆", th.RefHead
			n.Bold = true
		}
		g.Children = append(g.Children, n)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	var roots []*Node
	for _, o := range order {
		g := groups[o]
		g.Extra = fmt.Sprintf("%d", len(g.Children))
		roots = append(roots, g)
	}
	w.tree.SetRoots(roots)
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (w *WorktreesWindow) selected() *git.Worktree {
	if n := w.tree.Selected(); n != nil {
		if wt, ok := n.Data.(*git.Worktree); ok {
			return wt
		}
	}
	return nil
}

func (w *WorktreesWindow) switchTo(a *App, wt *git.Worktree) tea.Cmd {
	if wt.Prunable {
		a.Error("Worktree is missing", wt.Path+" no longer exists. Prune it with p.")
		return nil
	}
	a.Pop(w)
	if wt.Current {
		return nil
	}
	return a.SwitchTo(wt.Path)
}

func (w *WorktreesWindow) Key(a *App, k tea.KeyMsg) tea.Cmd {
	if w.typing {
		switch keyName(k) {
		case "esc", "enter", "down":
			w.typing = false
			return nil
		}
		if w.filter.Key(k) {
			w.OnData(a)
		}
		return nil
	}
	wt := w.selected()
	switch keyName(k) {
	case "esc", "q", "W":
		a.Pop(w)
		return nil
	case "/":
		w.typing = true
		return nil
	case "n":
		OpenNewWorktree(a)
		return nil
	case "delete", "backspace", "d":
		if wt != nil {
			OpenRemoveWorktree(a, wt)
		}
		return nil
	case "C":
		return OpenCleanup(a)
	case "p":
		return a.Op("Prune worktrees", a.Repo.PruneWorktrees, nil)
	case "s":
		if wt != nil && !wt.Prunable {
			return a.OpenShell(wt.Path)
		}
	case "i":
		if wt != nil {
			return a.OpenInIDE(wt.Path)
		}
	case "y":
		if wt != nil {
			return copyToClipboard(a, wt.Path)
		}
	case "l":
		if wt != nil && wt.Origin != git.OriginMain {
			return a.Op(map[bool]string{false: "Lock worktree", true: "Unlock worktree"}[wt.Locked], func() error { return a.Repo.SetWorktreeLock(wt.Path, !wt.Locked) }, nil)
		}
	case "o":
		w.sortBy = 1 - w.sortBy
		w.OnData(a)
		return nil
	case "r", "ctrl+r":
		return tea.Batch(a.Refresh(), a.loadWorktreeStatus(), a.scanClaude())
	case ".", "shift+f10", "alt+enter":
		w.menu(a, -1, -1)
		return nil
	case "L":
		if wt != nil {
			a.Pop(w)
			return a.OpenLog(git.LogFilter{Branches: []string{firstNonEmpty(wt.Branch, wt.Head)}})
		}
	}
	cmd, _ := w.tree.Key(a, k)
	return cmd
}

func (w *WorktreesWindow) menu(a *App, x, y int) {
	wt := w.selected()
	none := wt == nil
	items := []MenuItem{
		{Label: "Switch to Worktree", Hint: "⏎", Disabled: none || wt.Current, Do: func(a *App) tea.Cmd { return w.switchTo(a, wt) }},
		{Label: "Open Shell Here", Hint: "s", Disabled: none, Do: func(a *App) tea.Cmd { return a.OpenShell(wt.Path) }},
		{Label: "Open in IntelliJ", Hint: "i", Disabled: none, Do: func(a *App) tea.Cmd { return a.OpenInIDE(wt.Path) }},
		{Label: "Show in Log", Hint: "L", Disabled: none, Do: func(a *App) tea.Cmd {
			a.Pop(w)
			return a.OpenLog(git.LogFilter{Branches: []string{firstNonEmpty(wt.Branch, wt.Head)}})
		}},
		{Label: "Copy Path", Hint: "y", Disabled: none, Do: func(a *App) tea.Cmd { return copyToClipboard(a, wt.Path) }},
		MenuSep(),
		{Label: "New Worktree…", Hint: "n", Do: func(a *App) tea.Cmd { OpenNewWorktree(a); return nil }},
		{Label: "Remove Worktree…", Hint: "⌦", Disabled: none || wt.Origin == git.OriginMain, Do: func(a *App) tea.Cmd { OpenRemoveWorktree(a, wt); return nil }},
		{Label: lockLabel(wt), Hint: "l", Disabled: none || wt.Origin == git.OriginMain, Do: func(a *App) tea.Cmd {
			return a.Op(map[bool]string{false: "Lock worktree", true: "Unlock worktree"}[wt.Locked], func() error { return a.Repo.SetWorktreeLock(wt.Path, !wt.Locked) }, nil)
		}},
		MenuSep(),
		{Label: "Clean Up Merged Worktrees…", Hint: "C", Do: func(a *App) tea.Cmd { return OpenCleanup(a) }},
		{Label: "Prune Missing Worktrees", Hint: "p", Do: func(a *App) tea.Cmd { return a.Op("Prune worktrees", a.Repo.PruneWorktrees, nil) }},
	}
	m := NewMenuAt("", items, x, y)
	m.Searchable = false
	a.Push(m)
}

func lockLabel(wt *git.Worktree) string {
	if wt != nil && wt.Locked {
		return "Unlock Worktree"
	}
	return "Lock Worktree"
}

func (w *WorktreesWindow) Draw(a *App, screen View) {
	th := a.Th
	W := min(screen.W()-4, 150)
	H := min(screen.H()-3, 40)
	v := screen.Sub((screen.W()-W)/2, 1, W, H)
	bg := th.HeaderBg
	v.Fill(Style{Bg: bg})
	title := "Worktrees · " + filepath.Base(a.Repo.MainRootCached())
	v.Box(Style{Fg: th.PopupBorder, Bg: bg}, title, Style{Fg: th.Fg, Bg: bg, Bold: true})
	// Per-group counts are in the tree; the header only adds what isn't shown there.
	var status []string
	if a.wtStatusLoading {
		status = append(status, "loading status…")
	}
	if sessions, wts, working := a.claudeSummary(); sessions > 0 {
		status = append(status, fmt.Sprintf("claude: %s in %s, %d working", plural(sessions, "session"), plural(wts, "worktree"), working))
	}
	if s := strings.Join(status, " · "); s != "" && W-Width(title)-10 > Width(s) {
		v.TextRight(W-3, 0, " "+s+" ", Style{Fg: th.Dim, Bg: bg})
	}
	fv := v.Sub(2, 1, W-4, 1)
	fv.Fill(Style{Bg: th.InputBg})
	fv.Text(0, 0, "⌕", Style{Fg: th.Dim, Bg: th.InputBg})
	w.filter.Focused = w.typing
	w.filter.Draw(a, fv.Sub(2, 0, fv.W()-2, 1), Style{Fg: th.Fg, Bg: th.InputBg})
	a.HitPass(fv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			w.typing = true
		}
		return nil
	})
	// The details section gives way first in short windows.
	detailH := 5
	if H < 14 {
		detailH = 0
	}
	tv := v.Sub(1, 3, W-2, max(1, H-4-detailH))
	w.tree.Draw(a, tv)
	if detailH == 0 {
		return
	}
	v.HLine(1, H-1-detailH, W-2, '─', Style{Fg: th.Border, Bg: bg})
	dv := v.Sub(2, H-detailH, W-4, detailH-1)
	wt := w.selected()
	if wt == nil {
		dv.Text(0, 1, "n new worktree · C clean up merged · p prune missing", Style{Fg: th.Dim, Bg: bg})
		return
	}
	dv.TextFit(0, 0, relHome(wt.Path), dv.W(), Style{Fg: th.Fg, Bg: bg, Bold: true})
	info := []string{}
	if wt.Branch != "" {
		info = append(info, "branch "+wt.Branch)
	}
	info = append(info, "HEAD "+short(wt.Head))
	if wt.StatusOK {
		if wt.Dirty == 0 {
			info = append(info, "clean")
		} else {
			info = append(info, plural(wt.Dirty, "uncommitted change"))
		}
		if wt.Merged {
			info = append(info, "merged into "+a.Repo.DefaultBranch())
		}
	}
	dv.TextFit(0, 1, strings.Join(info, " · "), dv.W(), Style{Fg: th.Dim, Bg: bg})
	act := a.Claude[wt.Path]
	x := a.drawClaudeBadge(dv, 0, 2, act, bg, false)
	if act.Open == 0 && act.LastWrite.IsZero() {
		x = dv.Text(0, 2, "No Claude sessions", Style{Fg: th.Faint, Bg: bg})
	} else if act.Open > 0 {
		x = dv.Text(x, 2, " · last activity "+ago(act.LastWrite), Style{Fg: th.Dim, Bg: bg})
	}
	if act.Transcripts > 0 {
		dv.Text(x, 2, fmt.Sprintf(" · %s", plural(act.Transcripts, "transcript")), Style{Fg: th.Faint, Bg: bg})
	}
	if act.LastPrompt != "" {
		dv.TextFit(0, 3, "› "+act.LastPrompt, dv.W(), Style{Fg: th.Fg, Bg: bg, Italic: true})
	}
}

var branchSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func OpenNewWorktree(a *App) {
	branch := NewInput("")
	base := NewInput(firstNonEmpty(a.CurrentBranch(), "HEAD"))
	mainRoot := a.Repo.MainRootCached()
	loc := NewInput("")
	locTouched := false
	switchAfter := true
	d := &Dialog{Title: "New Worktree", Width: 80}
	locField := &InputField{Label: "Location:   ", In: loc}
	d.Fields = []Field{
		&InputField{Label: "Branch:     ", In: branch},
		&InputField{Label: "Based on:   ", In: base},
		locField,
		&LabelField{Text: "An existing branch is checked out; otherwise a new branch is created from 'Based on'. The default location is a sibling folder like your other checkouts.", Fg: a.Th.Dim},
		&CheckField{Label: "Switch to the new worktree", Val: &switchAfter},
	}
	defaultLoc := func() string {
		name := branchSafe.ReplaceAllString(strings.ReplaceAll(branch.Value(), "/", "-"), "-")
		if name == "" {
			return ""
		}
		return filepath.Join(filepath.Dir(mainRoot), filepath.Base(mainRoot)+"-"+name)
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Create", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		b := strings.TrimSpace(branch.Value())
		if b == "" {
			d.Err = "Branch can't be empty"
			return nil, false
		}
		path := strings.TrimSpace(loc.Value())
		if path == "" || !locTouched {
			path = defaultLoc()
		}
		if strings.HasPrefix(path, "~/") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[2:])
		}
		if _, err := os.Stat(path); err == nil {
			d.Err = path + " already exists"
			return nil, false
		}
		exists := a.Ref(b, git.LocalBranch) != nil
		from := strings.TrimSpace(base.Value())
		return a.Op("Create worktree", func() error { return a.Repo.AddWorktree(path, b, from, !exists) }, func(a *App) tea.Cmd {
			a.Notify("Worktree created", relHome(path))
			if switchAfter {
				return a.SwitchTo(path)
			}
			return nil
		}), true
	}}}
	// The location follows the branch name until the user edits it.
	d.BeforeKey = func(a *App, d *Dialog, k tea.KeyMsg) {
		if d.focusedField() == locField && (k.Type == tea.KeyRunes || keyName(k) == "backspace") {
			locTouched = true
		}
	}
	d.AfterKey = func(a *App, d *Dialog) {
		if !locTouched {
			loc.SetValue(relHome(defaultLoc()))
		}
	}
	a.Push(d)
}

func OpenRemoveWorktree(a *App, wt *git.Worktree) {
	if wt.Origin == git.OriginMain {
		a.Error("Cannot remove the main worktree", "")
		return
	}
	force := wt.Dirty > 0
	delBranch := wt.Merged && wt.Branch != ""
	act := a.Claude[wt.Path]
	msg := fmt.Sprintf("Remove worktree %s?", relHome(wt.Path))
	if wt.Dirty > 0 {
		msg += fmt.Sprintf("\n\nIt has %s that will be lost.", plural(wt.Dirty, "uncommitted change"))
	}
	if act.Open > 0 {
		msg += fmt.Sprintf("\n\n⚠ %s of Claude Code are running in it.", plural(act.Open, "session"))
	}
	d := &Dialog{Title: "Remove Worktree", Width: 72}
	d.Fields = []Field{&LabelField{Text: msg}, &CheckField{Label: "Force (discard local changes)", Val: &force}}
	if wt.Branch != "" {
		d.Fields = append(d.Fields, &CheckField{Label: "Also delete branch " + wt.Branch, Val: &delBranch})
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Remove", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		current := wt.Current
		mainRoot := a.Repo.MainRootCached()
		return a.Op("Remove worktree", func() error {
			if err := a.Repo.RemoveWorktree(wt.Path, force); err != nil {
				return err
			}
			if delBranch {
				return a.Repo.DeleteBranch(wt.Branch, !wt.Merged)
			}
			return nil
		}, func(a *App) tea.Cmd {
			a.Notify("Worktree removed", relHome(wt.Path))
			if current {
				return a.SwitchTo(mainRoot)
			}
			return a.loadWorktreeStatus()
		}), true
	}}}
	a.Push(d)
}

// OpenCleanup offers to remove worktrees that are missing, or merged with no local
// changes and no running Claude session.
func OpenCleanup(a *App) tea.Cmd {
	if a.wtStatusLoading || a.wtStatusAt.IsZero() {
		a.Notify("Loading worktree status…", "Run cleanup again in a moment")
		return a.loadWorktreeStatus()
	}
	var cands []*git.Worktree
	var lines []string
	var fgs []Color
	for _, wt := range a.Worktrees {
		if wt.Origin == git.OriginMain || wt.Current || wt.Locked {
			continue
		}
		act := a.Claude[wt.Path]
		reason := ""
		switch {
		case wt.Prunable:
			reason = "missing"
		case wt.Merged && wt.Dirty == 0 && act.Open == 0:
			reason = "merged, clean"
		}
		if reason == "" {
			continue
		}
		cands = append(cands, wt)
		lines = append(lines, fmt.Sprintf("%-40s %s · %s", Truncate(relHome(wt.Path), 40), reason, ago(latest(wt.Activity, act.LastWrite))))
		fgs = append(fgs, a.Th.Fg)
	}
	if len(cands) == 0 {
		a.Notify("Nothing to clean up", "No merged, clean worktrees without Claude sessions")
		return nil
	}
	delBranches := true
	d := &Dialog{Title: "Clean Up Worktrees", Width: 96}
	d.Fields = []Field{
		&LabelField{Text: fmt.Sprintf("%s can be removed. Worktrees with uncommitted changes, locked ones and those with running Claude sessions are kept.", plural(len(cands), "worktree"))},
		&ListField{Lines: lines, Fgs: fgs, H: 14},
		&CheckField{Label: "Also delete their merged branches", Val: &delBranches},
	}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: fmt.Sprintf("Remove %d", len(cands)), Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		repo := a.Repo
		return a.Op("Clean up worktrees", func() error {
			var errs []string
			for _, wt := range cands {
				if wt.Prunable {
					continue
				}
				if err := repo.RemoveWorktree(wt.Path, false); err != nil {
					errs = append(errs, relHome(wt.Path)+": "+err.Error())
					continue
				}
				if delBranches && wt.Branch != "" {
					repo.DeleteBranch(wt.Branch, false)
				}
			}
			repo.PruneWorktrees()
			if len(errs) > 0 {
				return fmt.Errorf("%s", strings.Join(errs, "\n"))
			}
			return nil
		}, func(a *App) tea.Cmd {
			a.Notify("Worktrees cleaned up", plural(len(cands), "worktree")+" removed")
			return a.loadWorktreeStatus()
		}), true
	}}}
	a.Push(d)
	return nil
}

// --- Projects ---

func (a *App) OpenProjects() {
	th := a.Th
	cur := a.Repo.MainRootCached()
	var items []MenuItem
	seen := map[string]bool{cur: true}
	if len(a.Cfg.RecentProjects) > 0 {
		items = append(items, MenuHeader("Recent Projects"))
		for _, p := range a.Cfg.RecentProjects {
			if seen[p] {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				continue
			}
			seen[p] = true
			p := p
			items = append(items, MenuItem{Label: filepath.Base(p), Extra: relHome(filepath.Dir(p)), Icon: "▣", Do: func(a *App) tea.Cmd { return a.SwitchTo(p) }})
		}
	}
	parent := filepath.Dir(cur)
	var found []MenuItem
	for _, p := range git.FindRepos(parent) {
		if seen[p] {
			continue
		}
		seen[p] = true
		p := p
		found = append(found, MenuItem{Label: filepath.Base(p), Extra: relHome(parent), Icon: "▣", IconFg: th.Dim, Do: func(a *App) tea.Cmd { return a.SwitchTo(p) }})
	}
	if len(found) > 0 {
		items = append(items, MenuHeader("Repositories in "+relHome(parent)))
		items = append(items, found...)
	}
	items = append(items, MenuSep(),
		MenuItem{Label: "Open Directory…", Icon: "⌂", Do: func(a *App) tea.Cmd { OpenDirectoryPrompt(a); return nil }},
		MenuItem{Label: "Worktrees…", Icon: "⌥", Hint: "W", Do: func(a *App) tea.Cmd { a.OpenWorktrees(); return nil }},
		MenuItem{Label: "Open Shell in " + filepath.Base(a.Repo.Root), Icon: "$", Hint: "ctrl+o", Do: func(a *App) tea.Cmd { return a.OpenShell(a.Repo.Root) }},
		MenuItem{Label: "Open in IntelliJ", Icon: "◧", Do: func(a *App) tea.Cmd { return a.OpenInIDE(a.Repo.Root) }},
	)
	m := NewMenu("Projects · "+filepath.Base(cur), items)
	m.MinWidth = 56
	a.Push(m)
}

func OpenDirectoryPrompt(a *App) {
	in := NewInput(relHome(filepath.Dir(a.Repo.MainRootCached())) + "/")
	d := &Dialog{Title: "Open Directory", Width: 80}
	hint := &LabelField{Text: "tab completes · any directory inside a git repository works", Fg: a.Th.Dim}
	d.Fields = []Field{&pathField{InputField: InputField{Label: "Path:", In: in}, hint: hint}, hint}
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Open", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		p := expandHome(strings.TrimSpace(in.Value()))
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			d.Err = "Not a directory: " + p
			return nil, false
		}
		if _, err := git.Open(p); err != nil {
			d.Err = err.Error()
			return nil, false
		}
		return a.SwitchTo(p), true
	}}}
	a.Push(d)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// pathField is an input with shell-like tab completion of directories.
type pathField struct {
	InputField
	hint *LabelField
}

func (f *pathField) WantsTab() bool { return true }

func (f *pathField) Key(a *App, k tea.KeyMsg) bool {
	if keyName(k) != "ctrl+i" && keyName(k) != "tab" {
		return f.In.Key(k)
	}
	cur := f.In.Value()
	full := expandHome(cur)
	dir, prefix := filepath.Dir(full), filepath.Base(full)
	if strings.HasSuffix(cur, "/") {
		dir, prefix = full, ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) && (prefix != "" || !strings.HasPrefix(e.Name(), ".")) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return true
	}
	common := matches[0]
	for _, m := range matches[1:] {
		for !strings.HasPrefix(m, common) {
			common = common[:len(common)-1]
		}
	}
	base := cur
	if !strings.HasSuffix(cur, "/") {
		base = cur[:len(cur)-len(prefix)]
	}
	next := base + common
	if len(matches) == 1 {
		next += "/"
	}
	f.In.SetValue(next)
	if len(matches) > 1 {
		shown := matches
		if len(shown) > 8 {
			shown = append(shown[:8], "…")
		}
		f.hint.Text = strings.Join(shown, "  ")
	}
	return true
}
