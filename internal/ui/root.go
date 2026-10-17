package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/razkevich/ideagit/internal/git"
)

// Root owns the App and rebuilds it when switching to another repository or worktree.
type Root struct {
	App      *App
	FinalDir string
}

func NewRoot(repo *git.Repo) *Root {
	a := NewApp(repo)
	a.Cfg.RememberProject(repo.MainRootCached())
	return &Root{App: a}
}

func (r *Root) Init() tea.Cmd { return r.App.Init() }

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s, ok := msg.(SwitchMsg); ok {
		repo, err := git.Open(s.Dir)
		if err != nil {
			r.App.Error("Cannot open", err.Error())
			return r, nil
		}
		epoch.Add(1)
		w, h := r.App.W, r.App.H
		active := r.App.active
		r.App = NewApp(repo)
		r.App.W, r.App.H = w, h
		r.App.active = min(active, len(r.App.tabs)-1)
		r.App.Cfg.RememberProject(repo.MainRootCached())
		r.FinalDir = repo.Root
		return r, r.App.Init()
	}
	_, cmd := r.App.Update(msg)
	return r, cmd
}

func (r *Root) View() string { return r.App.View() }
