package ui

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/changelist"
	"github.com/razkevich/ideagit/internal/claude"
	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/kitty"
	"github.com/razkevich/ideagit/internal/shelf"
)

type Tab interface {
	Title() string
	Draw(a *App, v View)
	Key(a *App, k tea.KeyMsg) (tea.Cmd, bool)
	Typing() bool
	Hint() string
	Closable() bool
	OnData(a *App)
}

type Overlay interface {
	Draw(a *App, screen View)
	Key(a *App, k tea.KeyMsg) tea.Cmd
}

// Mouse is a mouse event in absolute screen coordinates.
type Mouse struct {
	X, Y   int
	Button tea.MouseButton
	Action tea.MouseAction
	Double bool
	Shift  bool
	Ctrl   bool
	Alt    bool
}

func (m Mouse) LeftPress() bool {
	return m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress
}
func (m Mouse) RightPress() bool {
	return m.Button == tea.MouseButtonRight && m.Action == tea.MouseActionPress
}
func (m Mouse) Wheel() int {
	if m.Action != tea.MouseActionPress {
		return 0
	}
	switch m.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	}
	return 0
}
func (m Mouse) HWheel() int {
	switch m.Button {
	case tea.MouseButtonWheelLeft:
		return -1
	case tea.MouseButtonWheelRight:
		return 1
	}
	return 0
}

type hit struct {
	r    Rect
	fn   func(Mouse) tea.Cmd
	pass bool
}

// applyMsg carries a state update produced by background work. The epoch drops
// results that arrive after the app was switched to another repository.
type applyMsg struct {
	epoch int64
	fn    func(a *App) tea.Cmd
}
type tickMsg struct{}

var epoch atomic.Int64

func apply(fn func(a *App) tea.Cmd) tea.Msg { return applyMsg{epoch.Load(), fn} }

type note struct {
	title, body string
	err         bool
	until       time.Time
	actions     []noteAction
}

type noteAction struct {
	label string
	fn    func(a *App) tea.Cmd
}

type App struct {
	Repo    *git.Repo
	Th      *Theme
	Cfg     *Config
	W, H    int
	profile termenv.Profile

	Status  *git.Status
	statusK string
	Refs    []git.Ref
	Stashes []git.Stash
	Shelves []*shelf.Shelf
	CL      *changelist.Store
	CLMap   map[string]string
	User    string
	Recent  []string
	loadErr error
	loaded  bool

	tabs     []Tab
	active   int
	overlays []Overlay
	notes    []note
	busy     []string
	spin     int

	hits      []hit
	drag      func(Mouse) tea.Cmd
	regions   []textRegion
	textSel   *textSel
	frame     *Canvas
	lastClick time.Time
	lastX     int
	lastY     int

	consoleSeen  int
	refreshing   bool
	pendingFresh bool
	lastPoll     time.Time
	focused      bool
	snapshot     bool

	Worktrees       []*git.Worktree
	Claude          map[string]claude.Activity
	claudeScanning  bool
	lastClaudeScan  time.Time
	wtStatusLoading bool
	wtStatusAt      time.Time
	pendingCmds     []tea.Cmd

	project *ProjectPane
	editor  *Editor
	viewer  *Viewer
	focus   focusArea
	icons   bool

	restoreFiles  *ProjectState
	savedState    *ProjectState
	lastStateSave time.Time
	// tip is the name of the toolbar button under the pointer, shown in the status bar.
	tip          string
	lastRevealed string
	// prMode is an `ideagit --pr` session: PRs only, no editor, Project pane hidden.
	// It keeps its own state (prStatePath) so it never overwrites the normal session.
	prMode bool
	// projectHidden is the Project pane's visibility in PR mode.
	projectHidden bool
	// motionSeq counts pointer moves; a row is hovered only if it saw the latest one.
	motionSeq int
}

func NewApp(repo *git.Repo) *App {
	cfg := LoadConfig()
	th := DarkTheme()
	theme := cfg.Theme
	if env := os.Getenv("IDEAGIT_THEME"); env != "" {
		theme = env
	}
	switch theme {
	case "light":
		th = LightTheme()
	case "dark":
	default:
		if !termenv.HasDarkBackground() {
			th = LightTheme()
		}
	}
	a := &App{Repo: repo, Th: th, Cfg: cfg, profile: termenv.EnvColorProfile(), focused: true}
	a.CL = changelist.Load(repo.Root, repo.GitDir)
	a.project = NewProjectPane()
	a.icons = IconsEnabled(cfg)
	a.User = repo.UserName()
	a.tabs = []Tab{NewLocalChanges(), NewShelfTab(), NewStashTab()}
	saved := loadProjectState(repo.Root)
	states := cfg.LogTabs
	prFilter := cfg.PRFilter
	if saved != nil {
		// Filters are per repository; the global ones only seed new projects.
		states = saved.LogTabs
		if saved.PRFilter != nil {
			prFilter = *saved.PRFilter
		}
	}
	if len(states) == 0 {
		states = []LogTabState{{}}
	}
	for i, s := range states {
		a.tabs = append(a.tabs, NewLogTab(s.Filter, i > 0))
	}
	a.tabs = append(a.tabs, NewPRTab(prFilter))
	if !cfg.ConsoleHidden {
		a.tabs = append(a.tabs, NewConsoleTab())
	}
	a.savedState = saved
	a.restoreState(saved)
	return a
}

// SnapshotMode renders once without a terminal, for testing layouts.
func (a *App) SnapshotMode(w, h int) {
	a.snapshot = true
	a.Cfg.path = ""
	a.W, a.H = w, h
	a.profile = termenv.Ascii
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.Refresh(), a.tick(), a.reloadLogs(), enableKittyKeys(), a.reopenFiles())
}

// KittyKeys is set when stdin goes through the kitty keyboard translator.
var KittyKeys bool

// DoubleShiftMsg is sent when Shift is tapped twice (needs the kitty keyboard protocol).
type DoubleShiftMsg struct{}

// enableKittyKeys turns the protocol on for the alternate screen, which keeps its own
// setting; it is re-sent whenever the terminal comes back from a shell or $EDITOR.
func enableKittyKeys() tea.Cmd {
	if !KittyKeys {
		return nil
	}
	return func() tea.Msg {
		time.Sleep(50 * time.Millisecond)
		os.Stdout.WriteString(kitty.Enable)
		return nil
	}
}

// mergeWorktrees keeps lazily loaded status for worktrees that still exist.
func (a *App) mergeWorktrees(wts []*git.Worktree) {
	old := map[string]*git.Worktree{}
	for _, w := range a.Worktrees {
		old[w.Path] = w
	}
	for _, w := range wts {
		if o, ok := old[w.Path]; ok && o.StatusOK && o.Head == w.Head {
			w.Dirty, w.Ahead, w.Behind, w.Merged, w.StatusOK = o.Dirty, o.Ahead, o.Behind, o.Merged, true
		}
	}
	first := a.Worktrees == nil
	a.Worktrees = wts
	if first {
		a.pendingCmds = append(a.pendingCmds, a.scanClaude())
	}
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func Async(fn func() func(a *App) tea.Cmd) tea.Cmd {
	ep := epoch.Load()
	return func() tea.Msg { return applyMsg{ep, fn()} }
}

// Refresh reloads status, refs, stashes and shelves in the background.
func (a *App) Refresh() tea.Cmd {
	if a.refreshing {
		a.pendingFresh = true
		return nil
	}
	a.refreshing = true
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		st, err := repo.Status()
		refs, _ := repo.Refs()
		stashes, _ := repo.Stashes()
		shelves, _ := shelf.List(repo)
		recent := repo.RecentBranches(8)
		wts, _ := repo.Worktrees()
		return func(a *App) tea.Cmd {
			a.refreshing = false
			a.loaded = true
			a.loadErr = err
			if st != nil {
				a.Status = st
				a.statusK = st.Key()
				a.assignChangelists()
			}
			a.Refs = refs
			a.Stashes = stashes
			a.Shelves = shelves
			a.Recent = recent
			a.mergeWorktrees(wts)
			for _, t := range a.tabs {
				t.OnData(a)
			}
			if a.showProject() {
				a.project.OnData(a)
			}
			for _, o := range a.overlays {
				if d, ok := o.(interface{ OnData(*App) }); ok {
					d.OnData(a)
				}
			}
			if a.pendingFresh {
				a.pendingFresh = false
				return a.Refresh()
			}
			return nil
		}
	})
}

func (a *App) assignChangelists() {
	var paths []string
	for _, f := range a.Status.Files {
		if f.Kind != git.Untracked {
			paths = append(paths, f.Path)
		}
	}
	m, dirty := a.CL.Assign(paths)
	a.CLMap = m
	if dirty {
		a.CL.Save()
	}
}

func (a *App) poll() tea.Cmd {
	if a.refreshing || !a.focused || len(a.busy) > 0 || time.Since(a.lastPoll) < 2500*time.Millisecond {
		return nil
	}
	a.lastPoll = time.Now()
	repo := a.Repo
	prev := a.statusK
	return Async(func() func(a *App) tea.Cmd {
		st, err := repo.Status()
		if err != nil || st.Key() == prev {
			return func(a *App) tea.Cmd { return nil }
		}
		return func(a *App) tea.Cmd { return a.Refresh() }
	})
}

// Op runs a mutating git operation in the background with a status-bar spinner,
// reports failures as an error balloon and refreshes afterwards.
func (a *App) Op(title string, fn func() error, then func(a *App) tea.Cmd) tea.Cmd {
	a.busy = append(a.busy, title)
	return Async(func() func(a *App) tea.Cmd {
		err := fn()
		return func(a *App) tea.Cmd {
			if i := slices.Index(a.busy, title); i >= 0 {
				a.busy = slices.Delete(a.busy, i, i+1)
			}
			var cmds []tea.Cmd
			if err != nil {
				a.Error(title+" failed", err.Error())
			} else if then != nil {
				cmds = append(cmds, then(a))
			}
			cmds = append(cmds, a.Refresh(), a.reloadLogs())
			return tea.Batch(cmds...)
		}
	})
}

func (a *App) reloadLogs() tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range a.tabs {
		if l, ok := t.(*LogTab); ok {
			cmds = append(cmds, l.Reload(a))
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) Notify(title, body string) {
	a.notes = append(a.notes, note{title: title, body: body, until: time.Now().Add(6 * time.Second)})
}

func (a *App) Error(title, body string) {
	a.notes = append(a.notes, note{title: title, body: body, err: true, until: time.Now().Add(15 * time.Second)})
}

func (a *App) NotifyActions(title, body string, actions ...noteAction) {
	a.notes = append(a.notes, note{title: title, body: body, until: time.Now().Add(12 * time.Second), actions: actions})
}

func (a *App) Push(o Overlay) { a.overlays = append(a.overlays, o) }

func (a *App) Pop(o Overlay) {
	if i := slices.Index(a.overlays, o); i >= 0 {
		a.overlays = slices.Delete(a.overlays, i, i+1)
	}
}

func (a *App) PopAll() { a.overlays = nil }

func (a *App) Top() Overlay {
	if len(a.overlays) == 0 {
		return nil
	}
	return a.overlays[len(a.overlays)-1]
}

func (a *App) Hit(r Rect, fn func(Mouse) tea.Cmd) { a.hits = append(a.hits, hit{r: r, fn: fn}) }

// HitPass registers a handler that observes clicks without stopping lower handlers.
func (a *App) HitPass(r Rect, fn func(Mouse) tea.Cmd) {
	a.hits = append(a.hits, hit{r: r, fn: fn, pass: true})
}

func (a *App) Drag(fn func(Mouse) tea.Cmd) { a.drag = fn }

func (a *App) ActiveTab() Tab { return a.tabs[a.active] }

func (a *App) SelectTab(i int) {
	if i >= 0 && i < len(a.tabs) {
		a.active = i
	}
}

func (a *App) LocalChanges() *LocalChanges {
	for _, t := range a.tabs {
		if l, ok := t.(*LocalChanges); ok {
			return l
		}
	}
	return nil
}

func (a *App) OpenLog(f git.LogFilter) tea.Cmd {
	t := NewLogTab(f, true)
	// New Log tabs go right after the existing ones, before PRs and Console.
	idx := len(a.tabs)
	for i, tt := range a.tabs {
		if _, ok := tt.(*LogTab); ok {
			idx = i + 1
		}
	}
	a.tabs = slices.Insert(a.tabs, idx, Tab(t))
	a.active = idx
	a.saveLogTabs()
	return t.Reload(a)
}

func (a *App) CloseTab(i int) {
	if i < 0 || i >= len(a.tabs) || !a.tabs[i].Closable() {
		return
	}
	if _, ok := a.tabs[i].(*ConsoleTab); ok {
		a.Cfg.ConsoleHidden = true
		a.Cfg.Save()
	}
	a.tabs = slices.Delete(a.tabs, i, i+1)
	if a.active >= len(a.tabs) {
		a.active = len(a.tabs) - 1
	}
	a.saveLogTabs()
}

func (a *App) ShowConsole() {
	for i, t := range a.tabs {
		if _, ok := t.(*ConsoleTab); ok {
			a.active = i
			return
		}
	}
	a.tabs = append(a.tabs, NewConsoleTab())
	a.active = len(a.tabs) - 1
	a.Cfg.ConsoleHidden = false
	a.Cfg.Save()
}

func (a *App) saveLogTabs() {
	if a.prMode {
		return
	}
	var states []LogTabState
	for _, t := range a.tabs {
		if l, ok := t.(*LogTab); ok {
			f := l.filter
			f.Limit = 0
			states = append(states, LogTabState{Filter: f})
		}
	}
	a.Cfg.LogTabs = states
	a.Cfg.Save()
}

func (a *App) CurrentBranch() string {
	if a.Status == nil {
		return ""
	}
	if a.Status.Detached {
		return ""
	}
	return a.Status.Branch
}

func (a *App) HeadLabel() string {
	if a.Status == nil {
		return ""
	}
	if a.Status.Detached {
		if len(a.Status.OID) >= 8 {
			return a.Status.OID[:8]
		}
		return "HEAD"
	}
	return a.Status.Branch
}

func (a *App) Ref(name string, kind git.RefKind) *git.Ref {
	for i := range a.Refs {
		if a.Refs[i].Name == name && a.Refs[i].Kind == kind {
			return &a.Refs[i]
		}
	}
	return nil
}

// EditFile is "Jump to Source": the embedded editor when available, else $EDITOR.
func (a *App) EditFile(path string, line int) tea.Cmd {
	return a.OpenInEditor(path, line)
}

func (a *App) ExternalEdit(path string, line int) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	args := parts[1:]
	if line > 0 {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, a.Repo.Abs(path))
	c := exec.Command(parts[0], args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return apply(func(a *App) tea.Cmd {
			if err != nil {
				a.Error("Cannot open editor", err.Error())
			}
			return tea.Batch(a.Refresh(), enableKittyKeys())
		})
	})
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.W, a.H = m.Width, m.Height
		return a, nil
	case tea.FocusMsg:
		a.focused = true
		return a, a.Refresh()
	case tea.BlurMsg:
		a.focused = false
		return a, nil
	case DoubleShiftMsg:
		if s, ok := a.Top().(*SearchEverywhere); ok {
			a.Pop(s)
			return a, nil
		}
		if a.Top() != nil {
			return a, nil
		}
		return a, a.OpenSearch(0)
	case applyMsg:
		if m.epoch != epoch.Load() {
			return a, nil
		}
		return a, a.flush(m.fn(a))
	case tickMsg:
		a.spin++
		now := time.Now()
		a.notes = slices.DeleteFunc(a.notes, func(n note) bool { return now.After(n.until) })
		var scan tea.Cmd
		if a.focused && time.Since(a.lastClaudeScan) > 4*time.Second {
			scan = a.scanClaude()
		}
		if time.Since(a.lastStateSave) > 30*time.Second {
			a.SaveState()
		}
		if p, ok := a.ActiveTab().(*PRTab); ok && !p.loading && !p.loaded.IsZero() && time.Since(p.loaded) > 2*time.Minute {
			scan = tea.Batch(scan, p.Reload(a))
		}
		return a, a.flush(tea.Batch(a.tick(), a.poll(), scan))
	case tea.KeyMsg:
		if m.Type == tea.KeyRunes && len(m.Runes) > 1 && !m.Paste {
			if parts := splitChords(m); len(parts) > 1 {
				var cmds []tea.Cmd
				for _, p := range parts {
					cmds = append(cmds, a.handleKey(p))
				}
				return a, a.flush(tea.Batch(cmds...))
			}
		}
		return a, a.flush(a.handleKey(m))
	case tea.MouseMsg:
		return a, a.flush(a.handleMouse(m))
	}
	return a, nil
}

// flush adds commands queued by code paths that can't return one (e.g. menu helpers).
func (a *App) flush(cmd tea.Cmd) tea.Cmd {
	if len(a.pendingCmds) == 0 {
		return cmd
	}
	cmds := append(a.pendingCmds, cmd)
	a.pendingCmds = nil
	return tea.Batch(cmds...)
}

func (a *App) handleKey(k tea.KeyMsg) tea.Cmd {
	if cmd, ok := a.selectKey(k); ok {
		return cmd
	}
	if o := a.Top(); o != nil {
		return o.Key(a, k)
	}
	if a.focus == focusEditor && a.editorOpen() {
		return a.editorKey(k)
	}
	if keyName(k) == "ctrl+c" {
		return a.Quit()
	}
	if a.focus == focusProject && a.showProject() {
		if cmd, ok := a.project.Key(a, k); ok {
			return cmd
		}
		if cmd, ok := a.globalKey(k); ok {
			return cmd
		}
		if keyName(k) == "esc" {
			a.focus = focusGit
			if a.editorOpen() {
				a.focus = focusEditor
			}
		}
		return nil
	}
	// The focused pane sees a key before global shortcuts, so single-letter git keys can
	// mean something local (c = cherry-pick in the Log, commit elsewhere), as in lazygit.
	t := a.ActiveTab()
	if cmd, ok := t.Key(a, k); ok {
		return cmd
	}
	if cmd, ok := a.globalKey(k); ok {
		return cmd
	}
	if keyName(k) == "esc" && a.editorOpen() {
		a.focus = focusEditor
	}
	return nil
}

func (a *App) globalKey(k tea.KeyMsg) (tea.Cmd, bool) {
	s := keyName(k)
	// IntelliJ's Windows/Linux keymap; single letters are fallbacks for terminals that
	// can't send Ctrl+Shift chords (those need the kitty keyboard protocol).
	switch s {
	case "q", "ctrl+q":
		return a.Quit(), true
	case "alt+1", "ctrl+1":
		a.toggleProject()
	case "alt+9", "ctrl+9":
		a.toggleGit()
	case "alt+0", "ctrl+0":
		return a.OpenCommit(nil), true
	case "1", "2", "3", "4", "5", "6", "7":
		a.SelectTab(int(s[0] - '1'))
		a.focus = focusGit
	case "alt+right", "ctrl+right", "ctrl+shift+]", "}":
		a.SelectTab((a.active + 1) % len(a.tabs))
	case "alt+left", "ctrl+left", "ctrl+shift+[", "{":
		a.SelectTab((a.active + len(a.tabs) - 1) % len(a.tabs))
	case "ctrl+w", "ctrl+f4":
		a.CloseTab(a.active)
	case "ctrl+k", "ctrl+alt+k":
		return a.OpenCommit(nil), true
	case "ctrl+shift+k", "alt+k", "P":
		return a.OpenPush(), true
	case "ctrl+t", "U", "p":
		return a.OpenUpdate(), true
	case "F", "f":
		return a.Op("Fetch", a.Repo.Fetch, func(a *App) tea.Cmd { a.Notify("Fetch successful", ""); return nil }), true
	case "c":
		return a.OpenCommit(nil), true
	case "A":
		cmd := a.OpenCommit(nil)
		if d, ok := a.Top().(*CommitDialog); ok {
			d.setAmend(a, true)
		}
		return cmd, true
	case "M":
		a.pickRef("Merge into "+a.HeadLabel(), func(a *App, r git.Ref) tea.Cmd {
			return a.Op("Merge", func() error { return a.Repo.Merge(r.Name) }, nil)
		})
	case "R":
		a.pickRef("Rebase "+a.HeadLabel()+" onto", func(a *App, r git.Ref) tea.Cmd {
			return a.Op("Rebase", func() error { return a.Repo.Rebase(r.Name) }, nil)
		})
	case "S":
		OpenStashDialog(a, nil)
	case "x":
		// lazygit's "x" opens the actions menu of whatever is focused.
		dot := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")}
		if a.focus == focusProject && a.showProject() {
			cmd, _ := a.project.Key(a, dot)
			return cmd, true
		}
		cmd, _ := a.ActiveTab().Key(a, dot)
		return cmd, true
	case "ctrl+p", "shift+shift":
		return a.OpenSearch(0), true
	case "ctrl+shift+n", "ctrl+shift+o", "ctrl+e":
		return a.OpenSearch(1), true
	case "ctrl+shift+a":
		return a.OpenSearch(3), true
	case "ctrl+shift+`", "b":
		a.OpenBranches()
	case "alt+f1":
		a.selectOpenedFile(true)
	case "shift+esc":
		a.hideActiveToolWindow()
	case "ctrl+tab":
		a.cycleFocus()
	case "ctrl+shift+c":
		if p := a.selectedPath(); p != "" {
			return copyToClipboard(a, a.Repo.Abs(p)), true
		}
	case "W", "w":
		a.OpenWorktrees()
	case "O":
		a.OpenProjects()
	case "ctrl+o":
		return a.OpenShell(a.Repo.Root), true
	case "`", "alt+`":
		a.OpenVCSOperations()
	case "ctrl+alt+y", "ctrl+r", "f5":
		return tea.Batch(a.Refresh(), a.reloadLogs()), true
	case "?", "f1":
		a.Push(NewHelp())
	default:
		return nil, false
	}
	return nil, true
}

func (a *App) handleMouse(m tea.MouseMsg) tea.Cmd {
	ev := Mouse{X: m.X, Y: m.Y, Button: m.Button, Action: m.Action, Shift: m.Shift, Ctrl: m.Ctrl, Alt: m.Alt}
	if a.drag != nil {
		a.textSel = nil
	}
	if cmd, ok := a.selectMouse(ev); ok {
		return cmd
	}
	if a.drag != nil {
		if ev.Action == tea.MouseActionRelease {
			d := a.drag
			a.drag = nil
			return d(ev)
		}
		if ev.Action == tea.MouseActionMotion {
			return a.drag(ev)
		}
	}
	if ev.Action == tea.MouseActionRelease {
		return nil
	}
	if ev.Action == tea.MouseActionMotion {
		a.tip = ""
		a.motionSeq++
		// Hover only goes to the topmost handler (menus highlight the item under the pointer).
		for i := len(a.hits) - 1; i >= 0; i-- {
			if a.hits[i].r.Contains(ev.X, ev.Y) && !a.hits[i].pass {
				return a.hits[i].fn(ev)
			}
		}
		return nil
	}
	if ev.LeftPress() {
		if time.Since(a.lastClick) < 400*time.Millisecond && ev.X == a.lastX && ev.Y == a.lastY {
			ev.Double = true
			a.lastClick = time.Time{}
		} else {
			a.lastClick = time.Now()
			a.lastX, a.lastY = ev.X, ev.Y
		}
	}
	return a.dispatch(ev)
}

func (a *App) dispatch(ev Mouse) tea.Cmd {
	var cmds []tea.Cmd
	for i := len(a.hits) - 1; i >= 0; i-- {
		if a.hits[i].r.Contains(ev.X, ev.Y) {
			cmds = append(cmds, a.hits[i].fn(ev))
			if !a.hits[i].pass {
				break
			}
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) View() string {
	if a.W == 0 || a.H == 0 {
		return ""
	}
	c := a.Render()
	if a.snapshot {
		return strings.Join(c.PlainLines(), "\n")
	}
	return c.Render(a.profile)
}

func (a *App) Render() *Canvas {
	th := a.Th
	c := New(a.W, a.H, th.S())
	v := c.View()
	a.hits = a.hits[:0]
	a.regions = a.regions[:0]
	a.frame = c
	if a.H < 6 || a.W < 30 {
		v.Text(0, 0, "Window too small", th.S())
		return c
	}
	a.drawTitleBar(v.Sub(0, 0, a.W, 1))
	a.drawWorkspace(v.Sub(0, 1, a.W, a.H-2))
	a.drawStatusBar(v.Sub(0, a.H-1, a.W, 1))
	a.drawNotes(v)
	for _, o := range a.overlays {
		// Clicks outside a modal overlay must not reach what's underneath.
		a.hits = a.hits[:0]
		a.regions = a.regions[:0]
		top := o
		a.Hit(Rect{0, 0, a.W, a.H}, func(m Mouse) tea.Cmd {
			if m.LeftPress() || m.RightPress() {
				if d, ok := top.(interface{ Dismissable() bool }); ok && d.Dismissable() {
					a.Pop(top)
				}
			}
			return nil
		})
		o.Draw(a, v)
	}
	a.drawTextSel(v)
	return c
}

func (a *App) drawTabBar(v View) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	x := 1
	x = v.Text(x, 0, "Git", Style{Fg: th.Fg, Bg: th.Bg, Bold: true}) + 2
	for i, t := range a.tabs {
		title := t.Title()
		w := Width(title) + 2
		if t.Closable() {
			w += 2
		}
		st := Style{Fg: th.Fg, Bg: th.Bg}
		if i == a.active {
			st = Style{Fg: th.Fg, Bg: th.TabSelBg, Bold: true}
		}
		v.FillRect(x, 0, w, 1, st)
		v.Text(x+1, 0, title, st)
		idx := i
		a.Hit(Rect{v.Abs().X + x, v.Abs().Y, w, 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.SelectTab(idx)
			}
			if m.Button == tea.MouseButtonMiddle && m.Action == tea.MouseActionPress {
				a.CloseTab(idx)
			}
			return nil
		})
		// Registered after the tab's own hit so it takes precedence.
		if t.Closable() {
			cx := x + w - 2
			v.Text(cx, 0, "×", st.WithFg(th.Dim))
			a.Hit(Rect{v.Abs().X + cx, v.Abs().Y, 2, 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					a.CloseTab(idx)
				}
				return nil
			})
		}
		x += w + 1
	}
	st := Style{Fg: th.Fg, Bg: th.Bg}
	v.Text(x, 0, "+", st)
	a.Hit(Rect{v.Abs().X + x, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			return a.OpenLog(git.LogFilter{})
		}
		return nil
	})
	x += 2
	v.Text(x, 0, "⌄", st)
	a.Hit(Rect{v.Abs().X + x, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.openTabsMenu(x, 1)
		}
		return nil
	})
	right := v.W() - 1
	help := "? Help"
	hx := v.TextRight(right, 0, help, Style{Fg: th.Dim, Bg: th.Bg})
	a.Hit(Rect{v.Abs().X + hx, v.Abs().Y, Width(help), 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			a.Push(NewHelp())
		}
		return nil
	})
}

func (a *App) openTabsMenu(x, y int) {
	var items []MenuItem
	for i, t := range a.tabs {
		idx := i
		items = append(items, MenuItem{Label: t.Title(), Checked: i == a.active, Do: func(a *App) tea.Cmd { a.SelectTab(idx); return nil }})
	}
	items = append(items, MenuSep(), MenuItem{Label: "New Log Tab", Do: func(a *App) tea.Cmd { return a.OpenLog(git.LogFilter{}) }})
	items = append(items, MenuItem{Label: "Console", Do: func(a *App) tea.Cmd { a.ShowConsole(); return nil }})
	a.Push(NewMenuAt("", items, x, y))
}

func (a *App) drawStatusBar(v View) {
	th := a.Th
	st := Style{Fg: th.Dim, Bg: th.Frame}
	v.Fill(st)
	x := 1
	if len(a.busy) > 0 {
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		v.Set(x, 0, frames[a.spin%len(frames)], st.WithFg(th.Accent))
		x = v.Text(x+2, 0, a.busy[len(a.busy)-1]+"…", st.WithFg(th.Fg)) + 3
	}
	if crumbs := a.breadcrumbs(); crumbs != "" && len(a.busy) == 0 {
		x = v.Text(x, 0, crumbs, st.WithFg(th.Fg)) + 3
	}
	hint := ""
	if o := a.Top(); o != nil {
		if h, ok := o.(interface{ Hint() string }); ok {
			hint = h.Hint()
		}
	} else if a.tip != "" {
		hint = a.tip
	} else if h := a.workspaceHint(); h != "" {
		hint = h
	} else {
		hint = a.ActiveTab().Hint()
	}

	// The branch widget lives bottom-right, as in IntelliJ's status bar.
	right := v.W() - 1
	if branch := a.HeadLabel(); branch != "" {
		label := "⎇ " + branch
		if st := a.Status; st != nil {
			if ab := aheadBehind(st.Ahead, st.Behind); ab != "" {
				label += " " + ab
			}
			if st.State != git.StateClean {
				label = st.State.String() + " " + label
			}
		}
		bx := v.TextRight(right, 0, label, st.WithFg(th.Fg))
		a.Hit(Rect{v.Abs().X + bx, v.Abs().Y, Width(label), 1}, func(m Mouse) tea.Cmd {
			if m.Action == tea.MouseActionMotion {
				a.tip = "Git Branches (ctrl+shift+`, b)"
				return nil
			}
			if m.LeftPress() {
				a.OpenBranches()
			}
			return nil
		})
		right = bx - 3
	}
	if hint != "" && right-x > 10 {
		v.TextFit(x, 0, hint, right-x, st.WithFg(th.Faint))
	}
}

func (a *App) breadcrumbs() string {
	if path := a.editorPath(); path != "" && a.editorOpen() {
		return a.Repo.Name + " › " + strings.ReplaceAll(path, "/", " › ")
	}
	return ""
}

func (a *App) drawNotes(v View) {
	th := a.Th
	y := a.H - 2
	for i := len(a.notes) - 1; i >= 0 && y > 4; i-- {
		n := a.notes[i]
		w := min(60, a.W-4)
		lines := wrapText(n.body, w-4)
		if len(lines) > 6 {
			lines = append(lines[:5], "…")
		}
		h := 3 + len(lines)
		if n.body == "" {
			h = 3
		}
		if len(n.actions) > 0 {
			h++
		}
		top := y - h
		box := v.Sub(a.W-w-2, top, w, h)
		bg := th.HeaderBg
		box.Fill(Style{Bg: bg})
		border := th.Border
		if n.err {
			border = th.Error
		}
		box.Box(Style{Fg: border, Bg: bg}, "", Style{})
		icon, ic := "ⓘ ", th.Link
		if n.err {
			icon, ic = "⚠ ", th.Error
		}
		box.Text(2, 1, icon, Style{Fg: ic, Bg: bg})
		box.TextFit(4, 1, n.title, w-6, Style{Fg: th.Fg, Bg: bg, Bold: true})
		for li, l := range lines {
			if n.body != "" {
				box.Text(2, 2+li, l, Style{Fg: th.Fg, Bg: bg})
			}
		}
		if len(n.actions) > 0 {
			ax := 2
			for _, act := range n.actions {
				act := act
				box.Text(ax, h-2, act.label, Style{Fg: th.Link, Bg: bg})
				a.Hit(Rect{box.Abs().X + ax, box.Abs().Y + h - 2, Width(act.label), 1}, func(m Mouse) tea.Cmd {
					if m.LeftPress() {
						a.notes = slices.DeleteFunc(a.notes, func(x note) bool { return x.title == n.title })
						return act.fn(a)
					}
					return nil
				})
				ax += Width(act.label) + 3
			}
		}
		idx := i
		a.HitPass(box.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() && idx < len(a.notes) {
				a.notes = slices.Delete(a.notes, idx, idx+1)
			}
			return nil
		})
		y = top - 1
	}
}

func wrapText(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range words {
			for Width(word) > w {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, Truncate(word, w))
				rs := []rune(word)
				word = string(rs[min(len(rs), w-1):])
			}
			if line == "" {
				line = word
			} else if Width(line)+1+Width(word) <= w {
				line += " " + word
			} else {
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (a *App) PRTab() (*PRTab, int) {
	for i, t := range a.tabs {
		if p, ok := t.(*PRTab); ok {
			return p, i
		}
	}
	return nil, -1
}

// EnterPRMode turns a freshly built App into an `ideagit --pr` session on pull request
// n (n <= 0: the list). It must run before Init so no editor files are reopened.
func (a *App) EnterPRMode(n int) tea.Cmd {
	p, i := a.PRTab()
	if p == nil {
		return nil
	}
	a.prMode = true
	a.projectHidden = true
	a.restoreFiles = nil
	a.SelectTab(i)
	a.focus = focusGit
	p.filter = PRFilter{State: "open", Author: "@me"}
	p.pending = 0
	if st := loadState(prStatePath(a.Repo.Root)); st != nil && st.PRFilter != nil {
		p.filter = *st.PRFilter
		p.pending = st.PRSelected
	}
	p.text.SetValue(p.filter.Text)
	if n > 0 {
		p.pending = n
	}
	return tea.Batch(p.Reload(a), p.loadLabels(a))
}

// hovered reports whether a hover recorded at seq is still current.
func (a *App) hovered(seq int) bool { return seq != 0 && seq == a.motionSeq }

// checkGlyph draws IntelliJ-style checkboxes with Nerd Font glyphs when available.
func (a *App) checkGlyph(state int) string {
	if a.icons {
		switch state {
		case Checked:
			return "\U000f0132"
		case Partial:
			return "\U000f0856"
		}
		return "\U000f0131"
	}
	switch state {
	case Checked:
		return "[x]"
	case Partial:
		return "[-]"
	}
	return "[ ]"
}
