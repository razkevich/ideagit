package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type WorktreeOrigin int

const (
	OriginMain WorktreeOrigin = iota
	OriginClaudeAgent
	OriginClaude
	OriginClaudeJob
	OriginNested
	OriginExternal
)

func (o WorktreeOrigin) String() string {
	return [...]string{"Main", "Claude agents", "Claude worktrees", "Claude jobs", "Nested", "Other checkouts"}[o]
}

func (o WorktreeOrigin) Tag() string {
	return [...]string{"main", "claude agent", "claude", "claude job", "nested", ""}[o]
}

type Worktree struct {
	Path     string
	Head     string
	Branch   string
	Detached bool
	Bare     bool
	Locked   bool
	Prunable bool
	Origin   WorktreeOrigin
	Current  bool
	Activity time.Time
	// Filled in lazily by WorktreeStatus.
	Dirty    int
	Ahead    int
	Behind   int
	Merged   bool
	StatusOK bool
}

func (w *Worktree) Name() string {
	if w.Origin == OriginMain {
		return filepath.Base(w.Path)
	}
	return filepath.Base(w.Path)
}

// MainRoot is the primary worktree of the repository (the one owning the .git dir).
func (r *Repo) MainRoot() string {
	out, err := r.Read("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return r.Root
	}
	common := strings.TrimSpace(out)
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return r.Root
}

func (r *Repo) Worktrees() ([]*Worktree, error) {
	out, err := r.Read("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	mainRoot := r.MainRoot()
	home, _ := os.UserHomeDir()
	var res []*Worktree
	var cur *Worktree
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "worktree "):
			cur = &Worktree{Path: strings.TrimPrefix(l, "worktree ")}
			res = append(res, cur)
		case cur == nil:
		case strings.HasPrefix(l, "HEAD "):
			cur.Head = strings.TrimPrefix(l, "HEAD ")
		case strings.HasPrefix(l, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(l, "branch "), "refs/heads/")
		case l == "detached":
			cur.Detached = true
		case l == "bare":
			cur.Bare = true
		case strings.HasPrefix(l, "locked"):
			cur.Locked = true
		case strings.HasPrefix(l, "prunable"):
			cur.Prunable = true
		}
	}
	for i, w := range res {
		w.Current = samePath(w.Path, r.Root)
		switch {
		case i == 0 || samePath(w.Path, mainRoot):
			w.Origin = OriginMain
		case strings.Contains(w.Path, "/.claude/worktrees/agent-"):
			w.Origin = OriginClaudeAgent
		case strings.Contains(w.Path, "/.claude/worktrees/"):
			w.Origin = OriginClaude
		case home != "" && strings.HasPrefix(w.Path, filepath.Join(home, ".claude")):
			w.Origin = OriginClaudeJob
		case strings.HasPrefix(w.Path, mainRoot+"/"):
			w.Origin = OriginNested
		default:
			w.Origin = OriginExternal
		}
		w.Activity = worktreeActivity(w.Path)
	}
	return res, nil
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		return ra == rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func worktreeActivity(path string) time.Time {
	var latest time.Time
	for _, p := range []string{filepath.Join(path, ".git"), path} {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
	}
	gitFile := filepath.Join(path, ".git")
	if b, err := os.ReadFile(gitFile); err == nil && strings.HasPrefix(string(b), "gitdir: ") {
		gd := strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir: "))
		for _, f := range []string{"HEAD", "index", "logs/HEAD"} {
			if fi, err := os.Stat(filepath.Join(gd, f)); err == nil && fi.ModTime().After(latest) {
				latest = fi.ModTime()
			}
		}
	}
	return latest
}

// WorktreeStatus fills dirty/ahead/behind/merged for one worktree.
func (r *Repo) WorktreeStatus(w *Worktree, base string) {
	if w.Prunable || w.Bare {
		w.StatusOK = true
		return
	}
	cmd := exec.Command("git", "-C", w.Path, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			switch {
			case l == "":
			case strings.HasPrefix(l, "# branch.ab "):
				var st Status
				parseBranchHeader(&st, l)
				w.Ahead, w.Behind = st.Ahead, st.Behind
			case !strings.HasPrefix(l, "#"):
				w.Dirty++
			}
		}
	}
	if base != "" && w.Head != "" && w.Origin != OriginMain {
		c := exec.Command("git", "-C", r.Root, "merge-base", "--is-ancestor", w.Head, base)
		w.Merged = c.Run() == nil
	}
	w.StatusOK = true
}

// DefaultBranch guesses the integration branch (origin/HEAD, then main/master).
func (r *Repo) DefaultBranch() string {
	if out, err := r.Read("symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if s := strings.TrimSpace(out); s != "" {
			return s
		}
	}
	for _, b := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := r.RevParse(b); err == nil {
			return b
		}
	}
	return ""
}

func (r *Repo) AddWorktree(path, branch, from string, newBranch bool) error {
	args := []string{"worktree", "add"}
	if newBranch {
		args = append(args, "-b", branch, path)
		if from != "" {
			args = append(args, from)
		}
	} else {
		args = append(args, path, branch)
	}
	_, err := r.Exec(args...)
	return err
}

func (r *Repo) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force", "--force")
	}
	_, err := r.Exec(append(args, path)...)
	return err
}

func (r *Repo) PruneWorktrees() error {
	_, err := r.Exec("worktree", "prune", "-v")
	return err
}

func (r *Repo) SetWorktreeLock(path string, lock bool) error {
	verb := "unlock"
	if lock {
		verb = "lock"
	}
	_, err := r.Exec("worktree", verb, path)
	return err
}

// FindRepos lists git repositories directly under dir (one level deep).
func FindRepos(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var res []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if fi, err := os.Stat(filepath.Join(p, ".git")); err == nil && fi.IsDir() {
			res = append(res, p)
		}
	}
	return res
}

// MainRootCached memoizes MainRoot, which is asked for on every frame by the title bar.
func (r *Repo) MainRootCached() string {
	r.mu.Lock()
	cached := r.mainRoot
	r.mu.Unlock()
	if cached != "" {
		return cached
	}
	m := r.MainRoot()
	r.mu.Lock()
	r.mainRoot = m
	r.mu.Unlock()
	return m
}
