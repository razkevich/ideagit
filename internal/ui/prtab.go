package ui

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/github"
)

// PRFilter composes a GitHub search query, like the Log's filter bar composes git log.
type PRFilter struct {
	State  string `json:"state"`  // "", open, draft, merged, closed
	Author string `json:"author"` // "", @me, login
	Review string `json:"review"` // "", requested, reviewed, involves
	Label  string `json:"label"`
	Base   string `json:"base"`
	Text   string `json:"text"`
}

// Options turns the filter into gh flags; only review filters and free text need
// GitHub's (slower) search syntax.
func (f PRFilter) Options(limit int) github.ListOptions {
	o := github.ListOptions{Author: f.Author, Label: f.Label, Base: f.Base, Limit: limit}
	var q []string
	switch f.State {
	case "open":
		o.State = "open"
	case "draft":
		o.State, o.Draft = "open", true
	case "merged":
		o.State = "merged"
	case "closed":
		q = append(q, "is:closed", "is:unmerged")
	}
	switch f.Review {
	case "requested":
		q = append(q, "review-requested:@me")
	case "reviewed":
		q = append(q, "reviewed-by:@me")
	case "involves":
		q = append(q, "involves:@me")
	}
	if f.Text != "" {
		q = append(q, f.Text)
	}
	o.Search = strings.Join(q, " ")
	return o
}

// Describe is a readable form of the filter for empty-list messages.
func (f PRFilter) Describe() string {
	var parts []string
	for _, p := range []string{f.State, f.Author, f.Review, f.Label, f.Base, f.Text} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

type prFocus int

const (
	pfTable prFocus = iota
	pfFilter
	pfFiles
	pfDetails
)

type PRTab struct {
	filter  PRFilter
	text    *TextInput
	prs     []github.PR
	sel     int
	top     int
	height  int
	shownN  int
	loading bool
	err     error
	gen     int
	textGen int
	focus   prFocus
	loaded  time.Time
	limit   int

	files     *Tree
	detail    *github.PR
	detFiles  []git.ChangedFile
	detBase   string
	detErr    string
	fetching  bool
	detGen    int64
	detLatest atomic.Int64
	detTop    int
	pending   int
	// openPending is a PR the user asked to open before its files had loaded.
	openPending int
	users       []string
	me          string
	// labels are the repository's labels, nil until loaded.
	labels        []string
	labelsLoading bool
	// cache keeps every PR viewed this session, shown at once on reselect while a
	// fresh copy loads.
	cache      map[int]*prCached
	refreshing bool
	// prefetchTried holds the head each PR was last prefetched at, so failures
	// aren't retried in a loop.
	prefetchTried map[int]string
	prefetching   bool
	hoverRow      int
	hoverSeq      int
}

type prCached struct {
	detail *github.PR
	files  []git.ChangedFile
	base   string
}

func NewPRTab(f PRFilter) *PRTab {
	if f == (PRFilter{}) {
		f.State = "open"
	}
	t := &PRTab{filter: f, text: NewInput(f.Text), files: NewTree(), limit: 100, cache: map[int]*prCached{}, prefetchTried: map[int]string{}}
	t.text.Placeholder = "Search pull requests"
	t.files.Empty = "Select a pull request"
	t.files.OnActivate = func(a *App, n *Node) tea.Cmd { return t.openDiff(a, n) }
	t.files.OnContext = func(a *App, n *Node, x, y int) tea.Cmd { t.fileMenu(a, x, y); return nil }
	return t
}

func (t *PRTab) client(a *App) *github.Client {
	repo := a.Repo
	return &github.Client{Dir: repo.Root, Log: func(cmd, out string, failed bool) { repo.LogExternal(cmd, out, failed) }}
}

func (t *PRTab) Title() string {
	switch {
	case t.filter.Author == "@me":
		return "PRs: mine"
	case t.filter.Review == "requested":
		return "PRs: to review"
	}
	return "PRs"
}

func (t *PRTab) Closable() bool { return false }
func (t *PRTab) Typing() bool   { return t.focus == pfFilter || t.files.Typing() }

func (t *PRTab) Hint() string {
	switch t.focus {
	case pfFilter:
		return "type to search · ⏎/esc back to list"
	case pfFiles:
		return filesHint
	}
	return "⏎ diff · o browser · C checkout · w worktree · a approve · r request changes · i comment · l labels · R re-run failed · M merge · n new PR · x menu"
}

func (t *PRTab) SelectedPath() string {
	if t.focus == pfFiles {
		if n := t.files.Selected(); n != nil {
			if f, ok := n.Data.(git.ChangedFile); ok {
				return f.Path
			}
		}
	}
	return ""
}

func (t *PRTab) OnData(a *App) {
	if t.loaded.IsZero() && !t.loading {
		a.pendingCmds = append(a.pendingCmds, t.Reload(a), t.loadLabels(a))
	}
}

// Select shows PR n even if the current filter hides it (ideagit --pr N).
func (t *PRTab) Select(a *App, n int) tea.Cmd {
	t.pending = n
	for i, p := range t.prs {
		if p.Number == n {
			t.sel = i
			t.pending = 0
			return t.loadDetails(a)
		}
	}
	if t.loading {
		return nil
	}
	c := t.client(a)
	return Async(func() func(a *App) tea.Cmd {
		p, err := c.View(n)
		return func(a *App) tea.Cmd {
			if err != nil {
				a.Error(fmt.Sprintf("Cannot open PR #%d", n), err.Error())
				return nil
			}
			t.prs = append([]github.PR{*p}, t.prs...)
			t.sel = 0
			t.pending = 0
			return t.loadDetails(a)
		}
	})
}

// Reload shows the first page fast, then fills in more PRs and finally CI checks, which
// are by far the slowest part of the GitHub query.
func (t *PRTab) Reload(a *App) tea.Cmd {
	t.gen++
	gen := t.gen
	t.loading = true
	c := t.client(a)
	first := t.filter.Options(30)
	full := t.filter.Options(t.limit)
	return Async(func() func(a *App) tea.Cmd {
		prs, err := c.List(first)
		return func(a *App) tea.Cmd {
			if gen != t.gen {
				return nil
			}
			cmd := tea.Batch(t.applyList(a, prs, err), t.prefetch(a, gen))
			var more tea.Cmd
			if err == nil && len(prs) == first.Limit && full.Limit > first.Limit {
				more = t.loadMore(a, gen, full)
			} else if err == nil {
				more = t.loadChecks(a, gen, first)
			}
			return tea.Batch(cmd, more)
		}
	})
}

func (t *PRTab) loadMore(a *App, gen int, o github.ListOptions) tea.Cmd {
	c := t.client(a)
	return Async(func() func(a *App) tea.Cmd {
		prs, err := c.List(o)
		return func(a *App) tea.Cmd {
			if gen != t.gen || err != nil {
				return nil
			}
			checks := map[int][]github.Check{}
			for _, p := range t.prs {
				if p.Checks != nil {
					checks[p.Number] = p.Checks
				}
			}
			for i := range prs {
				if ch, ok := checks[prs[i].Number]; ok {
					prs[i].Checks = ch
				}
			}
			cmd := tea.Batch(t.applyList(a, prs, nil), t.prefetch(a, gen))
			o.Limit = 30
			return tea.Batch(cmd, t.loadChecks(a, gen, o))
		}
	})
}

// loadChecks fills the CI column for the first rows; on a GitHub timeout the column
// just stays empty (details still show checks).
func (t *PRTab) loadChecks(a *App, gen int, o github.ListOptions) tea.Cmd {
	c := t.client(a)
	o.Limit = min(o.Limit, 30)
	return Async(func() func(a *App) tea.Cmd {
		checks, err := c.Checks(o)
		return func(a *App) tea.Cmd {
			if gen != t.gen || err != nil {
				return nil
			}
			for i := range t.prs {
				if ch, ok := checks[t.prs[i].Number]; ok {
					t.prs[i].Checks = ch
				}
			}
			return nil
		}
	})
}

func (t *PRTab) applyList(a *App, prs []github.PR, err error) tea.Cmd {
	t.loading = false
	t.loaded = time.Now()
	t.err = err
	if err != nil && t.pending != 0 {
		// The list failed, but one PR asked for by number can still be shown.
		t.err = nil
		return t.Select(a, t.pending)
	}
	if err != nil {
		return nil
	}
	selN := 0
	if p := t.selected(); p != nil {
		selN = p.Number
	}
	t.prs = prs
	t.sel = 0
	for i, p := range prs {
		if p.Number == selN {
			t.sel = i
		}
	}
	if t.pending != 0 {
		return t.Select(a, t.pending)
	}
	return t.loadDetails(a)
}

func (t *PRTab) selected() *github.PR {
	if t.sel >= 0 && t.sel < len(t.prs) {
		return &t.prs[t.sel]
	}
	return nil
}

// loadDetails fetches the full PR, then its head commit, then the changed files, so the
// description shows up before the (network) fetch finishes.
func (t *PRTab) loadDetails(a *App) tea.Cmd {
	p := t.selected()
	if p == nil {
		t.detail, t.detFiles = nil, nil
		t.files.SetRoots(nil)
		return nil
	}
	if t.detail != nil && t.detail.Number == p.Number && t.detail.HeadOid == p.HeadOid && t.detFiles != nil {
		return nil
	}
	t.detGen++
	gen := t.detGen
	t.detLatest.Store(gen)
	if t.detail == nil || t.detail.Number != p.Number {
		t.detail = nil
		t.detFiles = nil
		t.detErr = ""
		t.detTop = 0
		t.files.SetRoots(nil)
		if c := t.cache[p.Number]; c != nil {
			t.detail, t.detBase = c.detail, c.base
			if c.files != nil {
				t.showFiles(a, c.files)
			}
		}
	}
	t.refreshing = true
	n := p.Number
	c := t.client(a)
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		// Debounce: scrolling through the list shouldn't fetch every PR passed over.
		time.Sleep(200 * time.Millisecond)
		if t.detLatest.Load() != gen {
			return func(a *App) tea.Cmd { return nil }
		}
		d, err := c.View(n)
		return func(a *App) tea.Cmd {
			if gen != t.detGen {
				return nil
			}
			if err != nil {
				t.refreshing = false
				if t.detFiles == nil {
					t.detErr = err.Error()
				}
				t.failOpen(a, err.Error())
				return nil
			}
			c := t.cached(n)
			old := c.detail
			t.detail, c.detail = d, d
			if c.files != nil && old != nil && old.HeadOid == d.HeadOid && old.BaseOid == d.BaseOid {
				t.refreshing = false
				return t.openQueued(a, n)
			}
			t.fetching = true
			return t.loadFiles(a, repo, d, gen)
		}
	})
}

func (t *PRTab) loadFiles(a *App, repo *git.Repo, d *github.PR, gen int64) tea.Cmd {
	pr := *d
	return Async(func() func(a *App) tea.Cmd {
		remote := repo.GitHubRemote()
		var err error
		prFetchMu.Lock()
		defer prFetchMu.Unlock()
		if !repo.HasCommit(pr.HeadOid) {
			err = repo.FetchPR(remote, pr.Number)
		}
		if err == nil && !repo.HasCommit(pr.BaseOid) {
			err = repo.FetchBranch(remote, pr.BaseRef)
		}
		var base string
		var files []git.ChangedFile
		if err == nil {
			base, err = repo.MergeBase(pr.BaseOid, pr.HeadOid)
		}
		if err == nil {
			files, err = repo.DiffNameStatus(base, pr.HeadOid)
		}
		return func(a *App) tea.Cmd {
			if gen != t.detGen {
				return nil
			}
			t.fetching = false
			t.refreshing = false
			if err != nil {
				if t.detFiles == nil {
					t.detErr = "Cannot load changes: " + err.Error()
				}
				t.failOpen(a, err.Error())
				return nil
			}
			if files == nil {
				files = []git.ChangedFile{}
			}
			c := t.cached(pr.Number)
			c.files, c.base = files, base
			t.detBase = base
			t.showFiles(a, files)
			return t.openQueued(a, pr.Number)
		}
	})
}

// prFetchMu keeps background prefetches and on-demand loads from running git fetch
// into the same repository at once.
var prFetchMu sync.Mutex

const prefetchBatch = 3

// prefetch fills the cache with every listed PR, newest first, a few at a time; a new
// list (filter change, reload) restarts it and skips PRs already cached at their head.
func (t *PRTab) prefetch(a *App, gen int) tea.Cmd {
	if t.prefetching || gen != t.gen {
		return nil
	}
	var todo []github.PR
	for _, p := range t.prs {
		if c := t.cache[p.Number]; c != nil && c.files != nil && c.detail != nil && c.detail.HeadOid == p.HeadOid {
			continue
		}
		if tried, ok := t.prefetchTried[p.Number]; ok && tried == p.HeadOid {
			continue
		}
		todo = append(todo, p)
	}
	if len(todo) == 0 {
		return nil
	}
	slices.SortStableFunc(todo, func(x, y github.PR) int { return y.Number - x.Number })
	todo = todo[:min(prefetchBatch, len(todo))]
	for _, p := range todo {
		t.prefetchTried[p.Number] = p.HeadOid
	}
	t.prefetching = true
	c := t.client(a)
	repo := a.Repo
	return Async(func() func(a *App) tea.Cmd {
		res := make([]prCached, len(todo))
		var wg sync.WaitGroup
		for i, p := range todo {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if d, err := c.View(p.Number); err == nil {
					res[i].detail = d
				}
			}()
		}
		wg.Wait()
		prefetchFiles(repo, res)
		return func(a *App) tea.Cmd {
			t.prefetching = false
			var cmds []tea.Cmd
			for _, r := range res {
				if r.detail == nil || r.files == nil {
					continue
				}
				n := r.detail.Number
				if old := t.cache[n]; old != nil && old.files != nil {
					continue
				}
				t.cache[n] = &prCached{detail: r.detail, files: r.files, base: r.base}
				if p := t.selected(); p != nil && p.Number == n && t.detFiles == nil {
					t.detail, t.detBase = r.detail, r.base
					t.showFiles(a, r.files)
					cmds = append(cmds, t.openQueued(a, n))
				}
			}
			return tea.Batch(append(cmds, t.prefetch(a, t.gen))...)
		}
	})
}

func prefetchFiles(repo *git.Repo, res []prCached) {
	prFetchMu.Lock()
	defer prFetchMu.Unlock()
	remote := repo.GitHubRemote()
	var heads []int
	bases := map[string]bool{}
	for _, r := range res {
		if d := r.detail; d != nil {
			if !repo.HasCommit(d.HeadOid) {
				heads = append(heads, d.Number)
			}
			if !repo.HasCommit(d.BaseOid) {
				bases[d.BaseRef] = true
			}
		}
	}
	if len(heads) > 0 && repo.FetchPRs(remote, heads) != nil {
		// One bad ref (e.g. a deleted fork) fails the whole fetch; retry one by one.
		for _, n := range heads {
			repo.FetchPR(remote, n)
		}
	}
	for b := range bases {
		repo.FetchBranch(remote, b)
	}
	for i := range res {
		d := res[i].detail
		if d == nil {
			continue
		}
		base, err := repo.MergeBase(d.BaseOid, d.HeadOid)
		if err != nil {
			continue
		}
		files, err := repo.DiffNameStatus(base, d.HeadOid)
		if err != nil {
			continue
		}
		if files == nil {
			files = []git.ChangedFile{}
		}
		res[i].files, res[i].base = files, base
	}
}

func (t *PRTab) cached(n int) *prCached {
	c := t.cache[n]
	if c == nil {
		c = &prCached{}
		t.cache[n] = c
	}
	return c
}

// showFiles keeps the tree's selection when a refresh brings the same files back.
func (t *PRTab) showFiles(a *App, files []git.ChangedFile) {
	if t.detFiles != nil && sameFiles(t.detFiles, files) {
		return
	}
	first := t.detFiles == nil
	t.detFiles = files
	t.files.SetRoots(changedFileNodes(a, "pr", files))
	if first {
		t.files.Sel, t.files.Top = 0, 0
	}
}

func sameFiles(x, y []git.ChangedFile) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func (t *PRTab) openQueued(a *App, n int) tea.Cmd {
	if t.openPending != 0 && t.openPending == n {
		t.cancelOpen(a)
		return t.openDiff(a, nil)
	}
	return nil
}

func (t *PRTab) openingLabel() string { return fmt.Sprintf("Opening #%d", t.openPending) }

// cancelOpen drops a queued open, e.g. when the selection moves to another PR.
func (t *PRTab) cancelOpen(a *App) {
	if t.openPending == 0 {
		return
	}
	label := t.openingLabel()
	for i, b := range a.busy {
		if b == label {
			a.busy = append(a.busy[:i], a.busy[i+1:]...)
			break
		}
	}
	t.openPending = 0
}

func (t *PRTab) failOpen(a *App, msg string) {
	if t.openPending == 0 {
		return
	}
	n := t.openPending
	t.cancelOpen(a)
	a.Error(fmt.Sprintf("Cannot open #%d", n), msg)
}

func (t *PRTab) sources(a *App) []DiffSource {
	var srcs []DiffSource
	for _, f := range t.detFiles {
		s := commitSource(a.Repo, t.detail.HeadOid, t.detBase, f)
		s.LeftTitle = t.detail.BaseRef
		s.RightTitle = fmt.Sprintf("#%d %s", t.detail.Number, t.detail.HeadRef)
		srcs = append(srcs, s)
	}
	return srcs
}

func (t *PRTab) openDiff(a *App, n *Node) tea.Cmd {
	// Not loaded yet (or not even listed yet, with --pr N): open it once the files arrive.
	target := t.pending
	if p := t.selected(); p != nil && target == 0 {
		target = p.Number
	}
	if target != 0 && (t.detail == nil || t.detail.Number != target || t.detFiles == nil) {
		if t.openPending != target {
			t.cancelOpen(a)
			t.openPending = target
			a.busy = append(a.busy, t.openingLabel())
		}
		return nil
	}
	if t.detail == nil || t.detFiles == nil {
		return nil
	}
	idx := 0
	if n != nil {
		if f, ok := n.Data.(git.ChangedFile); ok {
			for i, x := range t.detFiles {
				if x.Path == f.Path {
					idx = i
				}
			}
		}
	}
	d := t.detail
	header := []headerPart{{"Changes in ", false}, {fmt.Sprintf("#%d", d.Number), true}, {" " + d.Title + "  ", false}, {d.BaseRef, true}, {" ← ", false}, {d.HeadRef, true}}
	return a.OpenChanges(fmt.Sprintf("#%d %s", d.Number, d.Title), header, t.sources(a), idx, nil)
}

func (t *PRTab) setFocus(f prFocus) {
	t.focus = f
	t.text.Focused = f == pfFilter
	t.files.Focused = f == pfFiles
}

func (t *PRTab) moveSel(a *App, d int) tea.Cmd {
	if len(t.prs) == 0 {
		return nil
	}
	t.cancelOpen(a)
	t.sel = max(0, min(len(t.prs)-1, t.sel+d))
	return t.loadDetails(a)
}

func (t *PRTab) Key(a *App, k tea.KeyMsg) (tea.Cmd, bool) {
	s := keyName(k)
	if t.focus == pfFilter {
		switch s {
		case "esc", "enter", "down", "tab":
			t.setFocus(pfTable)
			return t.applyText(a), true
		}
		if t.text.Key(k) {
			t.textGen++
			gen := t.textGen
			return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
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
		t.setFocus(map[prFocus]prFocus{pfTable: pfFiles, pfFiles: pfDetails, pfDetails: pfTable}[t.focus])
		return nil, true
	case "shift+tab":
		t.setFocus(map[prFocus]prFocus{pfTable: pfDetails, pfFiles: pfTable, pfDetails: pfFiles}[t.focus])
		return nil, true
	case "/", "ctrl+f":
		t.setFocus(pfFilter)
		return nil, true
	case "f5", "ctrl+r", "ctrl+alt+y":
		t.detail = nil
		return t.Reload(a), true
	}
	if t.focus == pfFiles {
		if cmd, ok := t.files.Key(a, k); ok {
			return cmd, true
		}
		if s == "esc" {
			t.setFocus(pfTable)
			return nil, true
		}
		// PR actions (approve, merge…) must not fire from the file list.
		return fileKey(a, s, t.SelectedPath(), t.fileDiff, func() { t.fileMenu(a, -1, -1) })
	}
	if t.focus == pfDetails {
		switch s {
		case "up", "k":
			t.detTop = max(0, t.detTop-1)
			return nil, true
		case "down", "j":
			t.detTop++
			return nil, true
		case "pgup":
			t.detTop = max(0, t.detTop-10)
			return nil, true
		case "pgdown", " ":
			t.detTop += 10
			return nil, true
		case "esc":
			t.setFocus(pfTable)
			return nil, true
		}
	}
	h := max(1, t.height)
	switch s {
	case "up", "k":
		return t.moveSel(a, -1), true
	case "down", "j":
		return t.moveSel(a, 1), true
	case "pgup":
		return t.moveSel(a, -h+1), true
	case "pgdown":
		return t.moveSel(a, h-1), true
	case "home":
		return t.moveSel(a, -len(t.prs)), true
	case "end":
		return t.moveSel(a, len(t.prs)), true
	case "enter", "ctrl+d":
		return t.openDiff(a, nil), true
	case ".", "shift+f10", "alt+enter", "x":
		t.menu(a, -1, -1)
		return nil, true
	case "esc":
		if t.filter != (PRFilter{State: "open"}) {
			t.filter = PRFilter{State: "open"}
			t.text.SetValue("")
			t.save(a)
			return t.Reload(a), true
		}
		return nil, false
	case "n":
		t.create(a)
		return nil, true
	}
	if p := t.selected(); p != nil {
		if cmd, ok := t.action(a, p, s); ok {
			return cmd, true
		}
	}
	return nil, false
}

func (t *PRTab) action(a *App, p *github.PR, key string) (tea.Cmd, bool) {
	c := t.client(a)
	n := p.Number
	refresh := func(a *App) tea.Cmd { t.detail = nil; return t.Reload(a) }
	switch key {
	case "o":
		return a.Op("Open in browser", func() error { return c.OpenWeb(n) }, nil), true
	case "y", "ctrl+shift+c":
		return copyToClipboard(a, p.URL), true
	case "C":
		return a.Op(fmt.Sprintf("Checkout #%d", n), func() error { return c.Checkout(n) }, func(a *App) tea.Cmd {
			a.Notify(fmt.Sprintf("Checked out #%d", n), p.HeadRef)
			return nil
		}), true
	case "w":
		t.checkoutWorktree(a, p)
		return nil, true
	case "a":
		t.review(a, p, "approve")
		return nil, true
	case "r":
		t.review(a, p, "request-changes")
		return nil, true
	case "i":
		t.review(a, p, "comment")
		return nil, true
	case "M":
		t.merge(a, p)
		return nil, true
	case "D":
		if p.State != "OPEN" {
			return nil, true
		}
		return a.Op("Change draft state", func() error { return c.SetDraft(n, !p.IsDraft) }, refresh), true
	case "l":
		m := NewMenuAt("Labels", t.labelItems(a, n), -1, -1)
		m.Reload = func(a *App) []MenuItem { return t.labelItems(a, n) }
		m.MinWidth = 28
		a.Push(m)
		return t.loadLabels(a), true
	case "L":
		return a.OpenLog(git.LogFilter{Branches: []string{"refs/ideagit/pr/" + fmt.Sprint(n)}}), true
	case "R":
		d := p
		if t.detail != nil && t.detail.Number == n {
			d = t.detail
		}
		return t.rerunFailed(a, d), true
	}
	return nil, false
}

func (t *PRTab) save(a *App) {
	if a.prMode {
		a.SaveState()
		return
	}
	a.Cfg.PRFilter = t.filter
	a.Cfg.Save()
}

func (t *PRTab) applyText(a *App) tea.Cmd {
	if t.text.Value() == t.filter.Text {
		return nil
	}
	t.filter.Text = t.text.Value()
	t.save(a)
	return t.Reload(a)
}

func (t *PRTab) menu(a *App, x, y int) {
	p := t.selected()
	none := p == nil
	open := p != nil && p.State == "OPEN"
	do := func(key string) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd {
			if p == nil {
				return nil
			}
			cmd, _ := t.action(a, p, key)
			return cmd
		}
	}
	draftLabel := "Mark as Draft"
	if p != nil && p.IsDraft {
		draftLabel = "Ready for Review"
	}
	closeLabel := "Close Pull Request"
	if p != nil && p.State == "CLOSED" {
		closeLabel = "Reopen Pull Request"
	}
	items := []MenuItem{
		{Label: "Show Diff", Hint: "⏎", Disabled: none, Do: func(a *App) tea.Cmd { return t.openDiff(a, nil) }},
		{Label: "Open in Browser", Hint: "o", Disabled: none, Do: do("o")},
		{Label: "Copy URL", Hint: "y", Disabled: none, Do: do("y")},
		MenuSep(),
		{Label: "Checkout", Hint: "C", Disabled: none, Do: do("C")},
		{Label: "Checkout in New Worktree…", Hint: "w", Disabled: none, Do: do("w")},
		{Label: "Show in Log", Hint: "L", Disabled: none || t.detFiles == nil, Do: do("L")},
		MenuSep(),
		{Label: "Approve…", Hint: "a", Disabled: !open, Do: do("a")},
		{Label: "Request Changes…", Hint: "r", Disabled: !open, Do: do("r")},
		{Label: "Comment…", Hint: "i", Disabled: none, Do: do("i")},
		{Label: "Labels", Hint: "l", Disabled: none, Sub: func(a *App) []MenuItem {
			a.pendingCmds = append(a.pendingCmds, t.loadLabels(a))
			return t.labelItems(a, p.Number)
		}},
		MenuSep(),
		{Label: "Re-run Failed Checks", Hint: "R", Disabled: !open || !t.hasFailedChecks(p), Do: do("R")},
		MenuSep(),
		{Label: "Merge…", Hint: "M", Disabled: !open || p.IsDraft, Do: do("M")},
		{Label: draftLabel, Hint: "D", Disabled: !open, Do: do("D")},
		{Label: closeLabel, Disabled: none || p.State == "MERGED", Do: func(a *App) tea.Cmd {
			c := t.client(a)
			reopen := p.State == "CLOSED"
			n := p.Number
			return a.Op("Close/reopen PR", func() error { return c.SetOpen(n, reopen) }, func(a *App) tea.Cmd { t.detail = nil; return t.Reload(a) })
		}},
		MenuSep(),
		{Label: "New Pull Request…", Hint: "n", Do: func(a *App) tea.Cmd { t.create(a); return nil }},
		{Label: "Refresh", Hint: "ctrl+r", Do: func(a *App) tea.Cmd { t.detail = nil; return t.Reload(a) }},
	}
	title := ""
	if p != nil {
		title = fmt.Sprintf("#%d %s", p.Number, Truncate(p.Title, 40))
	}
	m := NewMenuAt(title, items, x, y)
	m.Searchable = false
	a.Push(m)
}

func (t *PRTab) review(a *App, p *github.PR, kind string) {
	title := map[string]string{"approve": "Approve", "request-changes": "Request Changes", "comment": "Comment"}[kind]
	ta := NewTextArea("")
	ta.Placeholder = map[string]string{"approve": "Optional comment", "request-changes": "What needs to change?", "comment": "Comment"}[kind]
	d := &Dialog{Title: fmt.Sprintf("%s · #%d %s", title, p.Number, Truncate(p.Title, 40)), Width: 80}
	d.Fields = []Field{&areaField{ta: ta, h: 8}}
	n := p.Number
	c := t.client(a)
	d.Buttons = []Button{{Label: "Cancel"}, {Label: title, Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		body := strings.TrimSpace(ta.Value())
		if body == "" && kind != "approve" {
			d.Err = "Write something first"
			return nil, false
		}
		var op func() error
		if kind == "comment" {
			op = func() error { return c.Comment(n, body) }
		} else {
			op = func() error { return c.Review(n, kind, body) }
		}
		return a.Op(title+fmt.Sprintf(" #%d", n), op, func(a *App) tea.Cmd {
			a.Notify(fmt.Sprintf("%s: #%d", title, n), "")
			t.detail = nil
			return t.Reload(a)
		}), true
	}}}
	a.Push(d)
}

func (t *PRTab) merge(a *App, p *github.PR) {
	method := 0
	methods := []string{"merge", "squash", "rebase"}
	del := true
	d := &Dialog{Title: fmt.Sprintf("Merge #%d %s", p.Number, Truncate(p.Title, 40)), Width: 72}
	info := fmt.Sprintf("%s ← %s", p.BaseRef, p.HeadRef)
	if t.detail != nil && t.detail.Number == p.Number && t.detail.MergeState != "" {
		info += " · " + strings.ToLower(strings.ReplaceAll(t.detail.MergeState, "_", " "))
	}
	d.Fields = []Field{
		&LabelField{Text: info, Fg: a.Th.Dim},
		&RadioField{Options: []string{"Create a merge commit", "Squash and merge", "Rebase and merge"}, Sel: &method},
		&CheckField{Label: "Delete the branch after merging", Val: &del},
	}
	n := p.Number
	c := t.client(a)
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Merge", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		m := methods[method]
		return a.Op(fmt.Sprintf("Merge #%d", n), func() error { return c.Merge(n, m, del) }, func(a *App) tea.Cmd {
			a.Notify(fmt.Sprintf("Merged #%d", n), "")
			t.detail = nil
			return tea.Batch(t.Reload(a), a.reloadLogs())
		}), true
	}}}
	a.Push(d)
}

// checkoutWorktree puts the PR into its own worktree next to the repository, so it
// can be reviewed (or handed to a Claude session) without touching the current branch.
func (t *PRTab) checkoutWorktree(a *App, p *github.PR) {
	mainRoot := a.Repo.MainRootCached()
	defPath := fmt.Sprintf("%s-pr-%d", mainRoot, p.Number)
	loc := NewInput(relHome(defPath))
	branch := p.HeadRef
	if p.CrossRepo {
		branch = fmt.Sprintf("pr-%d", p.Number)
	}
	br := NewInput(branch)
	switchAfter := true
	d := &Dialog{Title: fmt.Sprintf("Checkout #%d in New Worktree", p.Number), Width: 80}
	d.Fields = []Field{
		&InputField{Label: "Location:", In: loc, LabelW: 10},
		&InputField{Label: "Branch:", In: br, LabelW: 10},
		&CheckField{Label: "Switch to the new worktree", Val: &switchAfter},
	}
	n := p.Number
	repo := a.Repo
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Create", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		path := expandHome(strings.TrimSpace(loc.Value()))
		b := strings.TrimSpace(br.Value())
		if path == "" || b == "" {
			d.Err = "Location and branch are required"
			return nil, false
		}
		return a.Op(fmt.Sprintf("Worktree for #%d", n), func() error {
			if err := repo.FetchPR(repo.GitHubRemote(), n); err != nil {
				return err
			}
			exists := a.Ref(b, git.LocalBranch) != nil
			return repo.AddWorktree(path, b, "refs/ideagit/pr/"+fmt.Sprint(n), !exists)
		}, func(a *App) tea.Cmd {
			a.Notify(fmt.Sprintf("Worktree for #%d created", n), relHome(path))
			if switchAfter {
				return a.SwitchTo(path)
			}
			return nil
		}), true
	}}}
	a.Push(d)
}

// create opens a PR for the current branch, pushing it first when needed.
func (t *PRTab) create(a *App) {
	st := a.Status
	branch := a.CurrentBranch()
	if st == nil || branch == "" {
		a.Error("Cannot create a pull request", "Check out a branch first")
		return
	}
	base := strings.TrimPrefix(a.Repo.DefaultBranch(), a.Repo.GitHubRemote()+"/")
	if base == branch {
		a.Error("Cannot create a pull request", "You're on "+base+"; create a branch for your changes first")
		return
	}
	subject, body := a.Repo.PRTemplate(base)
	title := NewInput(subject)
	baseIn := NewInput(base)
	ta := NewTextArea(body)
	ta.MoveToStart()
	draft := false
	needPush := st.Upstream == "" || st.Ahead > 0
	push := needPush
	d := &Dialog{Title: "New Pull Request · " + branch, Width: 90}
	d.Fields = []Field{
		&InputField{Label: "Title:", In: title, LabelW: 7},
		&InputField{Label: "Base:", In: baseIn, LabelW: 7},
		&areaField{ta: ta, h: 10},
		&CheckField{Label: "Create as draft", Val: &draft},
		&CheckField{Label: "Push " + branch + " first", Val: &push, Disabled: !needPush},
	}
	c := t.client(a)
	repo := a.Repo
	d.Buttons = []Button{{Label: "Cancel"}, {Label: "Create Pull Request", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) {
		ti := strings.TrimSpace(title.Value())
		if ti == "" {
			d.Err = "Title can't be empty"
			return nil, false
		}
		b := strings.TrimSpace(baseIn.Value())
		bodyText := ta.Value()
		var url string
		return a.Op("Create pull request", func() error {
			if push {
				remote := repo.GitHubRemote()
				target := branch
				if st.Upstream != "" {
					_, target, _ = strings.Cut(st.Upstream, "/")
				}
				if err := repo.Push(git.PushOptions{Remote: remote, Local: branch, Target: target, SetUpstream: st.Upstream == ""}); err != nil {
					return err
				}
			}
			var err error
			url, err = c.Create(ti, bodyText, b, draft)
			return err
		}, func(a *App) tea.Cmd {
			a.Notify("Pull request created", url)
			if n, ok := github.ParseRef(url); ok {
				t.pending = n
			}
			return t.Reload(a)
		}), true
	}}}
	a.Push(d)
}

// --- drawing ---

func (t *PRTab) Draw(a *App, v View) {
	th := a.Th
	if err := github.Available(); err != nil {
		v.Fill(Style{Bg: th.Bg})
		v.Text(2, 1, err.Error(), Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	dw := max(34, min(a.Cfg.LogDetailsWidth+8, v.W()/2))
	mainW := v.W() - dw - 1
	main := v.Sub(0, 0, mainW, v.H())
	t.drawFilterBar(a, main.Sub(0, 0, mainW, 1))
	t.drawTable(a, main.Sub(0, 1, mainW, v.H()-1))
	v.VLine(mainW, 0, v.H(), '│', Style{Fg: th.Border, Bg: th.Bg})
	t.drawDetails(a, v.Sub(mainW+1, 0, dw, v.H()))
}

func (t *PRTab) drawFilterBar(a *App, v View) {
	th := a.Th
	st := Style{Fg: th.Fg, Bg: th.Bg}
	v.Fill(st)
	fw := max(12, min(30, v.W()/3))
	box := v.Sub(0, 0, fw, 1)
	box.Fill(Style{Bg: th.InputBg})
	box.Text(1, 0, "⌕", Style{Fg: th.Dim, Bg: th.InputBg})
	t.text.Focused = t.focus == pfFilter
	t.text.Draw(a, box.Sub(3, 0, max(1, fw-4), 1), Style{Fg: th.Fg, Bg: th.InputBg})
	a.HitPass(box.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			t.setFocus(pfFilter)
		}
		return nil
	})
	x := fw + 2
	y := v.Abs().Y + 1
	set := func(f func()) func(a *App) tea.Cmd {
		return func(a *App) tea.Cmd { f(); t.save(a); return t.Reload(a) }
	}
	chip := func(label, value string, items func() []MenuItem, clear func()) {
		text := label
		s := st.WithFg(th.Dim)
		if value != "" {
			text = label + ": " + value
			s = st
		}
		text = Truncate(text, 26)
		cx := x
		end := v.Text(x, 0, text, s)
		if value != "" && clear != nil {
			end = v.Text(end+1, 0, "×", st.WithFg(th.Dim))
			a.Hit(Rect{v.Abs().X + end - 1, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					clear()
					t.save(a)
					return t.Reload(a)
				}
				return nil
			})
		} else {
			end = v.Text(end, 0, " ⌄", st.WithFg(th.Dim))
		}
		a.Hit(Rect{v.Abs().X + cx, v.Abs().Y, Width(text), 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				list := items()
				menu := NewMenuAt(label, list, v.Abs().X+cx, y)
				menu.Searchable = len(list) > 8 || label == "Author" || label == "Label"
				menu.MinWidth = 28
				menu.Reload = func(a *App) []MenuItem { return items() }
				a.Push(menu)
				if label == "Author" && t.users == nil {
					return t.loadUsers(a, menu, set)
				}
				if label == "Label" && t.labels == nil {
					return t.loadLabels(a)
				}
			}
			return nil
		})
		x = end + 2
	}
	stateName := map[string]string{"": "All", "open": "Open", "draft": "Draft", "merged": "Merged", "closed": "Closed"}
	chip("State", stateName[t.filter.State], func() []MenuItem {
		var items []MenuItem
		for _, s := range []string{"open", "draft", "merged", "closed", ""} {
			s := s
			items = append(items, MenuItem{Label: stateName[s], Checked: t.filter.State == s, Do: set(func() { t.filter.State = s })})
		}
		return items
	}, nil)
	author := t.filter.Author
	if author == "@me" {
		author = "me"
	}
	chip("Author", author, func() []MenuItem { return t.authorItems(a, set) }, func() { t.filter.Author = "" })
	reviewName := map[string]string{"": "", "requested": "needs my review", "reviewed": "reviewed by me", "involves": "involves me"}
	chip("Review", reviewName[t.filter.Review], func() []MenuItem {
		var items []MenuItem
		for _, r := range []string{"", "requested", "reviewed", "involves"} {
			r := r
			label := reviewName[r]
			if label == "" {
				label = "Any"
			}
			items = append(items, MenuItem{Label: label, Checked: t.filter.Review == r, Do: set(func() { t.filter.Review = r })})
		}
		return items
	}, func() { t.filter.Review = "" })
	chip("Label", t.filter.Label, func() []MenuItem {
		items := []MenuItem{{Label: "Any", Checked: t.filter.Label == "", Do: set(func() { t.filter.Label = "" })}}
		for _, l := range t.allLabels() {
			l := l
			items = append(items, MenuItem{Label: l, Checked: t.filter.Label == l, Do: set(func() { t.filter.Label = l })})
		}
		if t.labels == nil {
			items = append(items, MenuItem{Label: "Loading labels…", Disabled: true})
		}
		return items
	}, func() { t.filter.Label = "" })
	chip("Base", t.filter.Base, func() []MenuItem {
		items := []MenuItem{{Label: "Any", Checked: t.filter.Base == "", Do: set(func() { t.filter.Base = "" })}}
		seen := map[string]bool{}
		for _, p := range t.prs {
			if !seen[p.BaseRef] {
				seen[p.BaseRef] = true
				b := p.BaseRef
				items = append(items, MenuItem{Label: b, Checked: t.filter.Base == b, Do: set(func() { t.filter.Base = b })})
			}
		}
		return items
	}, func() { t.filter.Base = "" })
	right := v.W() - 2
	icon := "⟳"
	if t.loading {
		icon = string([]rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")[a.spin%10])
	}
	if right > x {
		v.Text(right, 0, icon, st)
		a.Hit(Rect{v.Abs().X + right, v.Abs().Y, 1, 1}, func(m Mouse) tea.Cmd {
			if m.Action == tea.MouseActionMotion {
				a.tip = "Refresh (ctrl+r)"
			}
			if m.LeftPress() {
				t.detail = nil
				return t.Reload(a)
			}
			return nil
		})
	}
}

func (a *App) prStateIcon(p *github.PR) (string, Color) {
	th := a.Th
	switch {
	case p.State == "MERGED":
		return "●", Hex("#A371F7")
	case p.State == "CLOSED":
		return "●", th.Error
	case p.IsDraft:
		return "◌", th.Dim
	}
	return "●", th.Success
}

func (a *App) prChecksIcon(p *github.PR) (string, Color) {
	pass, fail, pending := p.CheckSummary()
	switch {
	case fail > 0:
		return "✗", a.Th.Error
	case pending > 0:
		return "●", a.Th.Warning
	case pass > 0:
		return "✓", a.Th.Success
	}
	return " ", NoColor
}

func (a *App) prReviewIcon(p *github.PR) (string, Color) {
	switch p.ReviewDecision {
	case "APPROVED":
		return "✓", a.Th.Success
	case "CHANGES_REQUESTED":
		return "±", a.Th.Error
	case "REVIEW_REQUIRED":
		return "○", a.Th.Dim
	}
	return " ", NoColor
}

// prWorktree finds a local worktree that has the PR's branch checked out.
func (a *App) prWorktree(p *github.PR) *git.Worktree {
	if p.CrossRepo {
		return nil
	}
	for _, w := range a.Worktrees {
		if w.Branch == p.HeadRef {
			return w
		}
	}
	return nil
}

func (t *PRTab) drawTable(a *App, v View) {
	th := a.Th
	h := v.H()
	t.height = h
	a.Selectable(v.Abs(), false)
	v.Fill(Style{Bg: th.Bg})
	if t.err != nil {
		for i, l := range wrapText(t.err.Error(), v.W()-4) {
			v.Text(2, 1+i, l, Style{Fg: th.Error, Bg: th.Bg})
		}
		return
	}
	if len(t.prs) == 0 {
		msg := "No pull requests"
		if t.loading {
			msg = "Loading pull requests…"
		} else if t.filter.Describe() != "" {
			msg = "No pull requests match: " + t.filter.Describe()
		}
		v.TextFit(max(0, (v.W()-Width(msg))/2), h/3, msg, v.W(), Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	if t.sel < t.top {
		t.top = t.sel
	}
	if t.sel >= t.top+h {
		t.top = t.sel - h + 1
	}
	t.top = max(0, min(t.top, len(t.prs)-h))
	w := v.W() - 1
	dateW, authorW := 17, min(14, max(6, w/9))
	numW := 7
	statusW := 4
	titleX := 3 + numW
	titleW := w - titleX - statusW - authorW - dateW - 3
	for i := 0; i < h && t.top+i < len(t.prs); i++ {
		idx := t.top + i
		p := &t.prs[idx]
		bg := th.Bg
		if idx == t.sel {
			bg = th.SelInactive
			if t.focus == pfTable {
				bg = th.Sel
			}
		} else if idx == t.hoverRow && a.hovered(t.hoverSeq) {
			bg = th.Hover
		}
		rv := v.Sub(0, i, w, 1)
		rv.Fill(Style{Bg: bg})
		icon, fg := a.prStateIcon(p)
		rv.Text(1, 0, icon, Style{Fg: fg, Bg: bg})
		rv.TextFit(3, 0, fmt.Sprintf("#%d", p.Number), numW, Style{Fg: th.Dim, Bg: bg})
		// Right side of the title column: worktree/Claude badge, then labels.
		var tags []refLabel
		if wt := a.prWorktree(p); wt != nil {
			label := "⌥ " + wt.Name()
			if act := a.Claude[wt.Path]; act.Open > 0 {
				if act.Working() {
					label += " ●"
				} else {
					label += " ○"
				}
			}
			tags = append(tags, refLabel{label, th.RefLocal})
		}
		for _, l := range p.Labels {
			tags = append(tags, refLabel{l, th.RefTag})
		}
		tagW := 0
		var tagText []string
		for _, tg := range tags {
			tagText = append(tagText, tg.text)
		}
		if len(tags) > 0 {
			tagW = min(Width(strings.Join(tagText, "  "))+2, titleW/3)
		}
		titleSt := Style{Fg: th.Fg, Bg: bg}
		if p.State != "OPEN" {
			titleSt.Fg = th.Dim
		}
		rv.TextFit(titleX, 0, p.Title, max(1, titleW-tagW), titleSt)
		if tagW > 0 {
			tx := titleX + titleW - tagW + 1
			for _, tg := range tags {
				if tx >= titleX+titleW {
					break
				}
				tx = rv.TextFit(tx, 0, tg.text, titleX+titleW-tx, Style{Fg: tg.fg, Bg: bg}) + 2
			}
		}
		sx := titleX + titleW + 1
		ci, cfg := a.prChecksIcon(p)
		rv.Text(sx, 0, ci, Style{Fg: cfg, Bg: bg})
		ri, rfg := a.prReviewIcon(p)
		rv.Text(sx+2, 0, ri, Style{Fg: rfg, Bg: bg})
		rv.TextFit(sx+statusW, 0, p.Author, authorW, Style{Fg: th.Fg, Bg: bg})
		rv.TextFit(sx+statusW+authorW+1, 0, fmtDate(p.Updated.Local()), dateW, Style{Fg: th.Fg, Bg: bg})
	}
	if len(t.prs) > h {
		DrawScrollbar(a, v.Sub(w, 0, 1, h), t.top, h, len(t.prs), func(top int) { t.top = top })
	}
	abs := v.Abs()
	a.Hit(Rect{abs.X, abs.Y, w, h}, func(m Mouse) tea.Cmd {
		if m.Action == tea.MouseActionMotion {
			t.hoverRow, t.hoverSeq = t.top+(m.Y-abs.Y), a.motionSeq
			return nil
		}
		if d := m.Wheel(); d != 0 {
			t.top = max(0, min(len(t.prs)-h, t.top+d*3))
			return nil
		}
		idx := t.top + m.Y - abs.Y
		if idx < 0 || idx >= len(t.prs) {
			return nil
		}
		if m.LeftPress() || m.RightPress() {
			t.setFocus(pfTable)
			if idx != t.sel {
				t.cancelOpen(a)
			}
			t.sel = idx
			cmd := t.loadDetails(a)
			if m.RightPress() {
				t.menu(a, m.X, m.Y)
			}
			if m.Double {
				return tea.Batch(cmd, func() tea.Msg { return apply(func(a *App) tea.Cmd { return t.openDiff(a, nil) }) })
			}
			return cmd
		}
		return nil
	})
}

func (t *PRTab) drawDetails(a *App, v View) {
	th := a.Th
	v.Fill(Style{Bg: th.Bg})
	p := t.selected()
	filesH := v.H() * 2 / 5
	if p == nil {
		v.Text(max(0, (v.W()-24)/2), v.H()/3, "Select a pull request", Style{Fg: th.Dim, Bg: th.Bg})
		return
	}
	fv := v.Sub(0, 0, v.W(), filesH)
	switch {
	case t.detFiles != nil:
		drawFilesToolbar(a, fv.Sub(0, 0, fv.W(), 1), plural(len(t.detFiles), "file"), t.files)
		t.files.Draw(a, fv.Sub(0, 1, fv.W(), fv.H()-1))
	case t.fetching:
		fv.Text(2, 1, "Fetching changes…", Style{Fg: th.Dim, Bg: th.Bg})
	case t.detErr != "":
		for i, l := range wrapText(t.detErr, fv.W()-4) {
			if i < fv.H()-1 {
				fv.Text(2, 1+i, l, Style{Fg: th.Error, Bg: th.Bg})
			}
		}
	default:
		fv.Text(2, 1, "Loading…", Style{Fg: th.Dim, Bg: th.Bg})
	}
	a.HitPass(fv.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() || m.RightPress() {
			t.setFocus(pfFiles)
		}
		return nil
	})
	v.HLine(0, filesH, v.W(), '─', Style{Fg: th.Border, Bg: th.Bg})
	if t.refreshing && (t.detFiles != nil || t.detail != nil) {
		spin := string([]rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")[a.spin%10])
		v.TextRight(v.W()-1, filesH, " "+spin+" refreshing ", Style{Fg: th.Dim, Bg: th.Bg})
	}
	dv := v.Sub(1, filesH+1, v.W()-2, v.H()-filesH-1)
	d := t.detail
	if d == nil || d.Number != p.Number {
		d = p
	}
	a.Selectable(dv.Abs(), true)
	lines := t.detailLines(a, d, dv.W())
	t.detTop = max(0, min(t.detTop, len(lines)-dv.H()))
	for i := 0; i < dv.H() && t.detTop+i < len(lines); i++ {
		l := lines[t.detTop+i]
		if l.segs != nil {
			drawMDLine(dv, i, mdLine{segs: l.segs, bg: l.bg})
			continue
		}
		end := dv.Text(0, i, l.text, Style{Fg: l.fg, Bg: th.Bg, Bold: l.bold})
		if l.link != "" {
			lx := end + 2
			dv.Text(lx, i, l.link, Style{Fg: th.Link, Bg: th.Bg})
			do := l.do
			a.Hit(Rect{dv.Abs().X + lx, dv.Abs().Y + i, Width(l.link), 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					return do(a)
				}
				return nil
			})
		}
	}
	a.Hit(dv.Abs(), func(m Mouse) tea.Cmd {
		if dd := m.Wheel(); dd != 0 {
			t.detTop = max(0, t.detTop+dd*2)
		}
		if m.LeftPress() {
			t.setFocus(pfDetails)
		}
		return nil
	})
}

type detLine struct {
	text string
	fg   Color
	bold bool
	// link, when set, is a clickable action drawn after the text.
	link string
	do   func(a *App) tea.Cmd
	// segs/bg, when set, are pre-styled markdown runs instead of text/fg.
	segs []mdSeg
	bg   Color
}

func (t *PRTab) detailLines(a *App, d *github.PR, w int) []detLine {
	th := a.Th
	var out []detLine
	add := func(s string, fg Color, bold bool) {
		for _, l := range wrapText(s, w) {
			out = append(out, detLine{text: l, fg: fg, bold: bold})
		}
	}
	blank := func() { out = append(out, detLine{fg: th.Fg}) }
	add(d.Title, th.Fg, true)
	state := strings.ToLower(d.State)
	if d.IsDraft && d.State == "OPEN" {
		state = "draft"
	}
	add(fmt.Sprintf("#%d · %s · %s · %s ← %s", d.Number, state, d.Author, d.BaseRef, d.HeadRef), th.Dim, false)
	add(fmt.Sprintf("+%d −%d · %s · updated %s", d.Additions, d.Deletions, plural(d.ChangedFiles, "file"), ago(d.Updated)), th.Dim, false)
	if wt := a.prWorktree(d); wt != nil {
		s := "⌥ checked out in " + relHome(wt.Path)
		if act := a.Claude[wt.Path]; act.Open > 0 {
			if act.Working() {
				s += " · claude working"
			} else {
				s += " · claude idle"
			}
		}
		add(s, th.RefLocal, false)
	}
	if !d.Detailed {
		blank()
		add("Loading details…", th.Dim, false)
		return out
	}
	if d.State == "OPEN" && d.MergeState != "" {
		ms := strings.ToLower(strings.ReplaceAll(d.MergeState, "_", " "))
		fg := th.Dim
		switch d.MergeState {
		case "CLEAN":
			fg, ms = th.Success, "ready to merge"
		case "DIRTY":
			fg, ms = th.Error, "has conflicts"
		case "BLOCKED":
			fg = th.Warning
		case "BEHIND":
			fg, ms = th.Warning, "behind "+d.BaseRef
		}
		add("Merge: "+ms, fg, false)
	}
	// Reviews: latest verdict per reviewer, then still-requested reviewers.
	latest := map[string]github.Review{}
	var order []string
	for _, r := range d.Reviews {
		if r.State == "COMMENTED" || r.State == "PENDING" {
			if _, ok := latest[r.Author]; ok {
				continue
			}
		}
		if _, ok := latest[r.Author]; !ok {
			order = append(order, r.Author)
		}
		latest[r.Author] = r
	}
	if len(order) > 0 || len(d.RequestedFrom) > 0 {
		blank()
		add("Reviews", th.Fg, true)
		for _, who := range order {
			r := latest[who]
			switch r.State {
			case "APPROVED":
				add("✓ "+who+" approved", th.Success, false)
			case "CHANGES_REQUESTED":
				add("± "+who+" requested changes", th.Error, false)
			case "DISMISSED":
				add("· "+who+" (dismissed)", th.Dim, false)
			default:
				add("· "+who+" commented", th.Dim, false)
			}
		}
		for _, who := range d.RequestedFrom {
			add("○ waiting for "+who, th.Dim, false)
		}
	}
	if len(d.Checks) > 0 {
		pass, fail, pending := d.CheckSummary()
		blank()
		var parts []string
		if pass > 0 {
			parts = append(parts, fmt.Sprintf("✓ %d passed", pass))
		}
		if fail > 0 {
			parts = append(parts, fmt.Sprintf("✗ %d failed", fail))
		}
		if pending > 0 {
			parts = append(parts, fmt.Sprintf("● %d pending", pending))
		}
		add("Checks  "+strings.Join(parts, " · "), th.Fg, true)
		if fail > 0 && d.State == "OPEN" {
			pr := d
			out[len(out)-1].link = "↻ Re-run failed"
			out[len(out)-1].do = func(a *App) tea.Cmd { return t.rerunFailed(a, pr) }
		}
		seen := map[string]bool{}
		for i := len(d.Checks) - 1; i >= 0; i-- {
			c := d.Checks[i]
			if seen[c.Name] || c.Passed() {
				continue
			}
			seen[c.Name] = true
			if c.Failed() {
				add("✗ "+c.Name, th.Error, false)
			} else {
				add("● "+c.Name, th.Warning, false)
			}
		}
	}
	if len(d.Labels) > 0 {
		blank()
		add("Labels  "+strings.Join(d.Labels, ", "), th.RefTag, false)
	}
	if body := strings.TrimSpace(d.Body); body != "" {
		blank()
		for _, ml := range renderMarkdown(a, body, w) {
			out = append(out, detLine{segs: ml.segs, bg: ml.bg})
		}
	}
	if len(d.Comments) > 0 {
		blank()
		add(plural(len(d.Comments), "comment"), th.Fg, true)
		for _, c := range d.Comments[max(0, len(d.Comments)-5):] {
			add(c.Author+" · "+ago(c.At), th.Dim, false)
			md := renderMarkdown(a, strings.TrimSpace(c.Body), w-2)
			for i, ml := range md {
				if i == 6 {
					add("  …", th.Dim, false)
					break
				}
				out = append(out, detLine{segs: append([]mdSeg{{"  ", Style{Bg: th.Bg}}}, ml.segs...), bg: ml.bg})
			}
		}
	}
	blank()
	add(d.URL, th.Link, false)
	return out
}

// authorItems: "me" pinned on top, then authors of the loaded PRs, then everyone else
// with access to the repository (loaded once, in the background).
func (t *PRTab) authorItems(a *App, set func(func()) func(a *App) tea.Cmd) []MenuItem {
	me := "me"
	extra := ""
	if t.me != "" {
		extra = t.me
	}
	items := []MenuItem{
		{Label: "Anyone", Checked: t.filter.Author == "", Do: set(func() { t.filter.Author = "" })},
		{Label: me, Extra: extra, Checked: t.filter.Author == "@me", Do: set(func() { t.filter.Author = "@me" })},
	}
	seen := map[string]bool{t.me: true}
	add := func(login string) {
		if login == "" || seen[login] {
			return
		}
		seen[login] = true
		l := login
		items = append(items, MenuItem{Label: l, Checked: t.filter.Author == l, Do: set(func() { t.filter.Author = l })})
	}
	for _, p := range t.prs {
		add(p.Author)
	}
	if len(t.users) > 0 {
		items = append(items, MenuSep())
		for _, u := range t.users {
			add(u)
		}
	}
	return items
}

func (t *PRTab) loadUsers(a *App, menu *Menu, set func(func()) func(a *App) tea.Cmd) tea.Cmd {
	c := t.client(a)
	return Async(func() func(a *App) tea.Cmd {
		users, _ := c.Assignees()
		me := c.Me()
		return func(a *App) tea.Cmd {
			t.users = users
			if t.users == nil {
				t.users = []string{}
			}
			t.me = me
			if a.Top() == menu {
				menu.SetItems(t.authorItems(a, set))
			}
			return nil
		}
	})
}

func (t *PRTab) hasFailedChecks(p *github.PR) bool {
	if t.detail != nil && t.detail.Number == p.Number {
		p = t.detail
	}
	_, fail, _ := p.CheckSummary()
	return fail > 0
}

// rerunFailed re-runs the failed GitHub Actions jobs of a PR, then refreshes it once
// GitHub has had a moment to mark them pending.
func (t *PRTab) rerunFailed(a *App, p *github.PR) tea.Cmd {
	if _, fail, _ := p.CheckSummary(); fail == 0 {
		a.Notify("No failed checks", fmt.Sprintf("#%d", p.Number))
		return nil
	}
	c := t.client(a)
	checks := p.Checks
	n := p.Number
	var runs int
	var other []string
	return a.Op(fmt.Sprintf("Re-run failed checks of #%d", n), func() error {
		var err error
		runs, other, err = c.RerunFailed(checks)
		return err
	}, func(a *App) tea.Cmd {
		body := fmt.Sprintf("Re-running failed jobs in %s", plural(runs, "workflow run"))
		if len(other) > 0 {
			body += "\nNot GitHub Actions, re-run them in their CI: " + strings.Join(other, ", ")
		}
		if runs == 0 {
			a.Error("Nothing to re-run here", "The failed checks aren't GitHub Actions: "+strings.Join(other, ", "))
			return nil
		}
		a.Notify(fmt.Sprintf("#%d: checks re-triggered", n), body)
		return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
			return apply(func(a *App) tea.Cmd { t.detail = nil; return t.Reload(a) })
		})
	})
}

// loadLabels fetches the repository's labels once, in the background; an open label
// menu fills in when they arrive.
func (t *PRTab) loadLabels(a *App) tea.Cmd {
	if t.labels != nil || t.labelsLoading {
		return nil
	}
	t.labelsLoading = true
	c := t.client(a)
	return Async(func() func(a *App) tea.Cmd {
		labels := c.Labels()
		return func(a *App) tea.Cmd {
			t.labelsLoading = false
			if labels == nil {
				labels = []string{}
			}
			t.labels = labels
			if m, ok := a.Top().(*Menu); ok && (m.Title == "Labels" || m.Title == "Label") {
				m.refresh(a)
			}
			return nil
		}
	})
}

// allLabels is the repository's labels plus any seen on PRs but not listed yet.
func (t *PRTab) allLabels() []string {
	res := append([]string(nil), t.labels...)
	seen := map[string]bool{}
	for _, l := range res {
		seen[l] = true
	}
	for _, p := range t.prs {
		for _, l := range p.Labels {
			if !seen[l] {
				seen[l] = true
				res = append(res, l)
			}
		}
	}
	return res
}

func (t *PRTab) prLabels(n int) []string {
	if t.detail != nil && t.detail.Number == n {
		return t.detail.Labels
	}
	for _, p := range t.prs {
		if p.Number == n {
			return p.Labels
		}
	}
	return nil
}

// labelItems toggles labels on PR n; the menu stays open so several can be changed.
func (t *PRTab) labelItems(a *App, n int) []MenuItem {
	has := map[string]bool{}
	for _, l := range t.prLabels(n) {
		has[l] = true
	}
	var items []MenuItem
	for _, l := range t.allLabels() {
		l := l
		items = append(items, MenuItem{Label: l, Checked: has[l], Keep: true, Do: func(a *App) tea.Cmd {
			cmd := t.setLabel(a, n, l, !has[l], false)
			if m, ok := a.Top().(*Menu); ok {
				m.refresh(a)
			}
			return cmd
		}})
	}
	if t.labels == nil {
		items = append(items, MenuItem{Label: "Loading labels…", Disabled: true})
	}
	if len(items) > 0 {
		items = append(items, MenuSep())
	}
	items = append(items, MenuItem{Label: "New Label…", Do: func(a *App) tea.Cmd {
		a.Push(Prompt(fmt.Sprintf("Add New Label to #%d", n), "Name:", "", "Add", func(a *App, name string) (tea.Cmd, string) {
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, "Enter a label name"
			}
			for _, l := range t.allLabels() {
				if strings.EqualFold(l, name) {
					return t.setLabel(a, n, l, true, false), ""
				}
			}
			return t.setLabel(a, n, name, true, true), ""
		}))
		return nil
	}})
	return items
}

// setLabel updates the list at once and reverts it if GitHub refuses.
func (t *PRTab) setLabel(a *App, n int, label string, on, create bool) tea.Cmd {
	update := func(labels []string, on bool) []string {
		var res []string
		for _, l := range labels {
			if l != label {
				res = append(res, l)
			}
		}
		if on {
			res = append(res, label)
		}
		return res
	}
	apply := func(on bool) {
		for i := range t.prs {
			if t.prs[i].Number == n {
				t.prs[i].Labels = update(t.prs[i].Labels, on)
			}
		}
		if t.detail != nil && t.detail.Number == n {
			t.detail.Labels = update(t.detail.Labels, on)
		}
		if c := t.cache[n]; c != nil && c.detail != nil && c.detail != t.detail {
			c.detail.Labels = update(c.detail.Labels, on)
		}
	}
	apply(on)
	if create {
		t.labels = append(t.labels, label)
	}
	c := t.client(a)
	verb := "Remove"
	if on {
		verb = "Add"
	}
	title := fmt.Sprintf("%s label %s on #%d", verb, label, n)
	a.busy = append(a.busy, title)
	return Async(func() func(a *App) tea.Cmd {
		var err error
		if create {
			err = c.CreateLabel(label)
		}
		if err == nil && on {
			err = c.EditLabels(n, []string{label}, nil)
		} else if err == nil {
			err = c.EditLabels(n, nil, []string{label})
		}
		return func(a *App) tea.Cmd {
			if i := slices.Index(a.busy, title); i >= 0 {
				a.busy = slices.Delete(a.busy, i, i+1)
			}
			if err != nil {
				apply(!on)
				if m, ok := a.Top().(*Menu); ok {
					m.refresh(a)
				}
				a.Error(title+" failed", err.Error())
			}
			return nil
		}
	})
}

func (t *PRTab) regroup(a *App) {
	if t.detFiles != nil {
		t.files.SetRoots(changedFileNodes(a, "pr", t.detFiles))
	}
}

func (t *PRTab) fileDiff(a *App) tea.Cmd { return t.openDiff(a, t.files.Selected()) }

func (t *PRTab) fileMenu(a *App, x, y int) {
	if p := t.SelectedPath(); p != "" {
		showFileMenu(a, fileItems(a, p, t.fileDiff), x, y)
	}
}
