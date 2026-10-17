package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"slices"
	"strings"
	"testing"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/github"
)

func TestPRDetailsOfferRerunForFailedChecks(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}}
	pr := github.PR{Number: 7, Title: "Fix things", State: "OPEN", Detailed: true, Checks: []github.Check{
		{Name: "test", State: "FAILURE", URL: "https://github.com/o/r/actions/runs/42/job/1"},
		{Name: "lint", State: "SUCCESS"},
	}}
	tab := NewPRTab(PRFilter{})
	tab.prs = []github.PR{pr}
	tab.detail = &pr
	c := New(70, 30, Style{})
	tab.drawDetails(a, c.View())
	text := strings.Join(c.PlainLines(), "\n")
	if !strings.Contains(text, "↻ Re-run failed") || !strings.Contains(text, "✗ test") {
		t.Fatalf("details:\n%s", text)
	}
	pr.State = "MERGED"
	tab.detail = &pr
	c = New(70, 30, Style{})
	tab.drawDetails(a, c.View())
	if strings.Contains(strings.Join(c.PlainLines(), "\n"), "Re-run failed") {
		t.Fatal("offered re-run on a merged PR")
	}
}

func TestPRSelectShowsCachedContentAtOnce(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}, Repo: &git.Repo{}}
	tab := NewPRTab(PRFilter{})
	tab.prs = []github.PR{{Number: 1, HeadOid: "a"}, {Number: 2, HeadOid: "b"}}
	d := &github.PR{Number: 2, Title: "Cached", HeadOid: "b"}
	tab.cache[2] = &prCached{detail: d, files: []git.ChangedFile{{Path: "x.go", Kind: git.Modified}}, base: "base"}
	tab.sel = 1
	tab.loadDetails(a)
	if tab.detail != d || len(tab.detFiles) != 1 || tab.detBase != "base" || !tab.refreshing {
		t.Fatalf("detail=%v files=%v base=%q refreshing=%v", tab.detail, tab.detFiles, tab.detBase, tab.refreshing)
	}
	if tab.openDiff(a, nil); tab.openPending != 0 {
		t.Fatal("a cached PR should open without waiting for the refresh")
	}
}

func TestPRLabelMenuTogglesAndMarksCurrent(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}, Repo: &git.Repo{}}
	tab := NewPRTab(PRFilter{})
	tab.prs = []github.PR{{Number: 3, Labels: []string{"bug"}}, {Number: 4, Labels: []string{"from-pr"}}}
	items := tab.labelItems(a, 3)
	if items[0].Label != "bug" || !items[0].Checked || items[len(items)-1].Label != "New Label…" {
		t.Fatalf("items before load: %+v", items)
	}
	if !slices.ContainsFunc(items, func(it MenuItem) bool { return it.Label == "Loading labels…" }) {
		t.Fatal("no loading placeholder before labels arrive")
	}
	tab.labels = []string{"bug", "enhancement"}
	items = tab.labelItems(a, 3)
	var names []string
	for _, it := range items {
		names = append(names, it.Label)
	}
	if strings.Join(names, ",") != "bug,enhancement,from-pr,,New Label…" {
		t.Fatalf("items: %v", names)
	}
	tab.setLabel(a, 3, "enhancement", true, false)
	tab.setLabel(a, 3, "bug", false, false)
	if got := strings.Join(tab.prs[0].Labels, ","); got != "enhancement" {
		t.Fatalf("labels after toggles: %q", got)
	}
}

func TestPRFilesPaneKeysDontActOnThePR(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}, Repo: &git.Repo{}}
	tab := NewPRTab(PRFilter{})
	tab.prs = []github.PR{{Number: 5, State: "OPEN"}}
	tab.detail = &tab.prs[0]
	tab.detFiles = []git.ChangedFile{{Path: "x.go"}}
	tab.files.SetRoots(changedFileNodes(a, "pr", tab.detFiles))
	tab.setFocus(pfFiles)
	for _, k := range []string{"a", "r", "M", "D", "C"} {
		tab.Key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		if len(a.overlays) > 0 {
			t.Fatalf("%q in the files pane opened %T", k, a.overlays[0])
		}
	}
	tab.Key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !a.Cfg.GroupByDir {
		t.Fatal("g didn't toggle tree view from the PR files pane")
	}
	tab.Key(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	if m, ok := a.Top().(*Menu); !ok || m.items[0].Label != "Show Diff" {
		t.Fatalf("files menu: %#v", a.Top())
	}
}
