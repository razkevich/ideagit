package ui

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/razkevich/ideagit/internal/git"
)

func TestProjectStateRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "T"}, {"config", "user.email", "t@e"}, {"commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	repo, _ := git.Open(dir)
	a := NewApp(repo)
	if a.Cfg.path == "" || filepath.Dir(statePath(repo.Root)) == "" {
		t.Skip("no config dir")
	}
	a.active = 3
	a.focus = focusProject
	a.Cfg.ShowProject = true
	for _, tb := range a.tabs {
		switch tb := tb.(type) {
		case *LogTab:
			tb.filter.Text = "fix"
		case *PRTab:
			tb.filter.Author = "@me"
		}
	}
	a.project.tree.exp["p:src"] = true
	a.project.tree.wantID = ""
	a.SaveState()

	b := NewApp(repo)
	b.Cfg.ShowProject = true
	if b.active != 3 {
		t.Errorf("active tab %d", b.active)
	}
	var logText, prAuthor string
	for _, tb := range b.tabs {
		switch tb := tb.(type) {
		case *LogTab:
			logText = tb.filter.Text
		case *PRTab:
			prAuthor = tb.filter.Author
		}
	}
	if logText != "fix" || prAuthor != "@me" {
		t.Errorf("filters not restored: log=%q pr=%q", logText, prAuthor)
	}
	if !b.project.tree.exp["p:src"] {
		t.Error("project expansion not restored")
	}
}

func TestPRModeKeepsItsOwnState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "T"}, {"config", "user.email", "t@e"}, {"commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	repo, _ := git.Open(dir)
	a := NewApp(repo)
	if a.Cfg.path == "" {
		t.Skip("no config dir")
	}
	a.Cfg.ShowProject = true
	a.Cfg.Save()
	a.active = 0
	a.SaveState()

	pr := NewApp(repo)
	pr.EnterPRMode(-1)
	p, i := pr.PRTab()
	if pr.active != i || pr.showProject() || pr.restoreFiles != nil || p.filter.Author != "@me" {
		t.Fatalf("PR mode start: tab=%d/%d project=%v author=%q", pr.active, i, pr.showProject(), p.filter.Author)
	}
	p.filter.Review = "requested"
	p.save(pr)
	pr.toggleProject()
	pr.toggleProject()
	pr.SaveState()

	b := NewApp(repo)
	bp, _ := b.PRTab()
	if b.active != 0 || !b.showProject() || bp.filter.Review != "" || b.Cfg.PRFilter.Review != "" {
		t.Fatalf("normal session changed: tab=%d project=%v review=%q global=%q", b.active, b.showProject(), bp.filter.Review, b.Cfg.PRFilter.Review)
	}
	again := NewApp(repo)
	again.EnterPRMode(-1)
	if ap, _ := again.PRTab(); ap.filter.Review != "requested" {
		t.Fatalf("PR session filter not restored: %+v", ap.filter)
	}
}
