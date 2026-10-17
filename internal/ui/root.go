package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/razkevich/ideagit/internal/git"
)

// Root owns the App and rebuilds it when switching to another repository or worktree.
type Root struct {
	App       *App
	FinalDir  string
	InitialPR int
}

func NewRoot(repo *git.Repo) *Root {
	a := NewApp(repo)
	a.Cfg.RememberProject(repo.MainRootCached())
	return &Root{App: a}
}

// InitialPR is set by --pr to open the PRs tab on that pull request at start.
func (r *Root) Init() tea.Cmd {
	if r.InitialPR != 0 {
		pr := r.App.EnterPRMode(r.InitialPR)
		return tea.Batch(r.App.Init(), pr)
	}
	return r.App.Init()
}

// recovered keeps a bug in one frame or handler from killing the app (and the embedded
// nvim with unsaved buffers): the stack goes to the crash log and a balloon.
func (r *Root) recovered(where string) {
	if p := recover(); p != nil {
		path := writeCrashLog(where, p)
		r.App.Error("Internal error (ideagit kept running)", fmt.Sprintf("%v — details in %s. Please report it.", p, relHome(path)))
	}
}

func writeCrashLog(where string, p any) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "ideagit", "crash.log")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "%s panic in %s: %v\n%s\n", time.Now().Format(time.RFC3339), where, p, debug.Stack())
		f.Close()
	}
	return path
}

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer r.recovered("update")
	if s, ok := msg.(SwitchMsg); ok {
		repo, err := git.Open(s.Dir)
		if err != nil {
			r.App.Error("Cannot open", err.Error())
			return r, nil
		}
		r.App.SaveState()
		if e := r.App.editor; e != nil && e.Running() {
			// Its files are part of the saved state and reopen when switching back.
			if e.ModifiedCount() > 0 {
				r.App.Error("Unsaved files", "Save or discard the editor's changes before switching projects")
				return r, nil
			}
			e.Quit(false)
		}
		epoch.Add(1)
		w, h := r.App.W, r.App.H
		r.App = NewApp(repo)
		r.App.W, r.App.H = w, h
		r.App.Cfg.RememberProject(repo.MainRootCached())
		r.FinalDir = repo.Root
		return r, r.App.Init()
	}
	_, cmd := r.App.Update(msg)
	return r, cmd
}

func (r *Root) View() (out string) {
	defer func() {
		if p := recover(); p != nil {
			path := writeCrashLog("render", p)
			r.App.Error("Internal error while drawing", fmt.Sprintf("%v — details in %s", p, relHome(path)))
			// Close whatever overlay was being drawn so the next frame has a chance.
			if r.App.Top() != nil {
				r.App.Pop(r.App.Top())
			}
			out = "ideagit hit an internal error while drawing; press any key to continue."
		}
	}()
	return r.App.View()
}

// SettleEditor lets snapshot tests wait for the embedded nvim to catch up.
func (r *Root) SettleEditor() {
	if e := r.App.editor; e != nil && e.Running() {
		e.Settle(3 * time.Second)
	}
}

// Shutdown stops the embedded nvim so it doesn't leave swap files behind.
func (r *Root) Shutdown() {
	if e := r.App.editor; e != nil && e.Running() {
		e.Quit(false)
	}
}
