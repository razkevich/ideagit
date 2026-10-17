package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (r *Repo) Checkout(ref string) error {
	_, err := r.Exec("checkout", ref)
	return err
}

func (r *Repo) CheckoutRemote(remoteBranch, local string) error {
	_, err := r.Exec("checkout", "-b", local, "--track", remoteBranch)
	return err
}

func (r *Repo) CreateBranch(name, from string, checkout bool) error {
	var err error
	if checkout {
		args := []string{"checkout", "-b", name}
		if from != "" {
			args = append(args, from)
		}
		_, err = r.Exec(args...)
	} else {
		args := []string{"branch", name}
		if from != "" {
			args = append(args, from)
		}
		_, err = r.Exec(args...)
	}
	return err
}

func (r *Repo) RenameBranch(old, name string) error {
	_, err := r.Exec("branch", "-m", old, name)
	return err
}

func (r *Repo) DeleteBranch(name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.Exec("branch", flag, name)
	return err
}

func (r *Repo) DeleteRemoteBranch(remoteBranch string) error {
	parts := strings.SplitN(remoteBranch, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("bad remote branch %q", remoteBranch)
	}
	_, err := r.Exec("push", parts[0], "--delete", parts[1])
	return err
}

func (r *Repo) Merge(ref string) error {
	_, err := r.Exec("merge", "--no-edit", ref)
	return err
}

func (r *Repo) Rebase(onto string) error {
	_, err := r.Exec("rebase", onto)
	return err
}

func (r *Repo) RebaseContinue() error { _, err := r.Exec("rebase", "--continue"); return err }
func (r *Repo) RebaseAbort() error    { _, err := r.Exec("rebase", "--abort"); return err }
func (r *Repo) RebaseSkip() error     { _, err := r.Exec("rebase", "--skip"); return err }
func (r *Repo) MergeAbort() error     { _, err := r.Exec("merge", "--abort"); return err }
func (r *Repo) CherryPickAbort() error {
	_, err := r.Exec("cherry-pick", "--abort")
	return err
}
func (r *Repo) RevertAbort() error { _, err := r.Exec("revert", "--abort"); return err }

func (r *Repo) CheckoutAndRebase(branch, onto string) error {
	_, err := r.Exec("rebase", onto, branch)
	return err
}

func (r *Repo) Fetch() error {
	_, err := r.Exec("fetch", "--all", "--prune")
	return err
}

func (r *Repo) Pull(rebase bool) error {
	mode := "--no-rebase"
	if rebase {
		mode = "--rebase"
	}
	_, err := r.Exec("pull", mode, "--autostash")
	return err
}

// UpdateBranch fast-forwards a non-current local branch from its upstream.
func (r *Repo) UpdateBranch(local, upstream string) error {
	parts := strings.SplitN(upstream, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("branch %s has no upstream", local)
	}
	_, err := r.Exec("fetch", parts[0], parts[1]+":"+local)
	return err
}

type PushOptions struct {
	Remote      string
	Local       string
	Target      string
	Force       bool
	Tags        bool
	SetUpstream bool
}

func (r *Repo) Push(o PushOptions) error {
	args := []string{"push", "--porcelain"}
	if o.Force {
		args = append(args, "--force-with-lease")
	}
	if o.Tags {
		args = append(args, "--follow-tags")
	}
	if o.SetUpstream {
		args = append(args, "--set-upstream")
	}
	args = append(args, o.Remote, "refs/heads/"+o.Local+":refs/heads/"+o.Target)
	_, err := r.Exec(args...)
	return err
}

func (r *Repo) Reset(rev, mode string) error {
	_, err := r.Exec("reset", "--"+mode, rev)
	return err
}

func (r *Repo) Revert(hash string) error {
	_, err := r.Exec("revert", "--no-edit", hash)
	return err
}

func (r *Repo) CherryPick(hash string) error {
	_, err := r.Exec("cherry-pick", hash)
	return err
}

func (r *Repo) CreateTag(name, rev, message string) error {
	args := []string{"tag"}
	if message != "" {
		args = append(args, "-a", "-m", message)
	}
	_, err := r.Exec(append(args, name, rev)...)
	return err
}

func (r *Repo) DeleteTag(name string) error {
	_, err := r.Exec("tag", "-d", name)
	return err
}

func (r *Repo) Add(paths []string) error {
	_, err := r.Exec(append([]string{"add", "--"}, paths...)...)
	return err
}

// Rollback restores files to HEAD. Added files are unstaged and, if deleteAdded, removed.
func (r *Repo) Rollback(files []FileStatus, deleteAdded bool, unborn bool) error {
	var restore, unstage, remove []string
	for _, f := range files {
		switch f.Kind {
		case Untracked:
			if deleteAdded {
				remove = append(remove, f.Path)
			}
		case Added:
			unstage = append(unstage, f.Path)
			if deleteAdded {
				remove = append(remove, f.Path)
			}
		case Renamed:
			unstage = append(unstage, f.Path)
			restore = append(restore, f.OrigPath)
			remove = append(remove, f.Path)
		default:
			restore = append(restore, f.Path)
		}
	}
	if len(unstage) > 0 {
		var err error
		if unborn {
			_, err = r.Exec(append([]string{"rm", "--cached", "-r", "-q", "--"}, unstage...)...)
		} else {
			_, err = r.Exec(append([]string{"reset", "-q", "HEAD", "--"}, unstage...)...)
		}
		if err != nil {
			return err
		}
	}
	if len(restore) > 0 && !unborn {
		if _, err := r.Exec(append([]string{"checkout", "HEAD", "--"}, restore...)...); err != nil {
			return err
		}
	}
	for _, p := range remove {
		if err := os.RemoveAll(r.Abs(p)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) ResolveConflict(path string, theirs bool) error {
	side := "--ours"
	if theirs {
		side = "--theirs"
	}
	if _, err := r.Exec("checkout", side, "--", path); err != nil {
		// File deleted on that side.
		if _, err2 := r.Exec("rm", "-q", "--", path); err2 != nil {
			return err
		}
		return nil
	}
	_, err := r.Exec("add", "--", path)
	return err
}

func (r *Repo) MarkResolved(path string) error {
	_, err := r.Exec("add", "--", path)
	return err
}

type CommitFile struct {
	Path     string
	OrigPath string
	Deleted  bool
	// Content overrides the working-tree file for partial (chunk-level) commits.
	Content []byte
}

type CommitRequest struct {
	Message  string
	Amend    bool
	Author   string
	SignOff  bool
	NoVerify bool
	Files    []CommitFile
}

// Commit commits exactly the requested files through a temporary index, so whatever
// else is staged stays staged, matching IntelliJ's changelist-based commit semantics.
func (r *Repo) Commit(req CommitRequest) (string, error) {
	tmp := filepath.Join(r.GitDir, fmt.Sprintf("ideagit-index-%d", os.Getpid()))
	defer os.Remove(tmp)
	env := []string{"GIT_INDEX_FILE=" + tmp}
	exec := func(stdin string, args ...string) (string, error) {
		return r.run(runOpts{logged: true, env: env, stdin: stdin}, args...)
	}
	if r.HeadHash() == "" {
		if _, err := exec("", "read-tree", "--empty"); err != nil {
			return "", err
		}
	} else if _, err := exec("", "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(r.GitDir, "MERGE_HEAD")); err == nil {
		// Merge commits must include everything git already resolved in the real index.
		if err := copyFile(filepath.Join(r.GitDir, "index"), tmp); err != nil {
			return "", err
		}
	}
	var touched []string
	for _, f := range req.Files {
		if f.OrigPath != "" && f.OrigPath != f.Path {
			if _, err := exec("", "update-index", "--force-remove", "--", f.OrigPath); err != nil {
				return "", err
			}
			touched = append(touched, f.OrigPath)
		}
		touched = append(touched, f.Path)
		if f.Deleted {
			if _, err := exec("", "update-index", "--force-remove", "--", f.Path); err != nil {
				return "", err
			}
			continue
		}
		mode := "100644"
		var content []byte
		fi, err := os.Lstat(r.Abs(f.Path))
		if err != nil {
			return "", err
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			mode = "120000"
			t, _ := os.Readlink(r.Abs(f.Path))
			content = []byte(t)
		case fi.Mode()&0o111 != 0:
			mode = "100755"
		}
		if f.Content != nil {
			content = f.Content
		}
		var sha string
		if content != nil {
			sha, err = r.run(runOpts{stdin: string(content)}, "hash-object", "-w", "--stdin")
		} else {
			sha, err = r.run(runOpts{}, "hash-object", "-w", "--", f.Path)
		}
		if err != nil {
			return "", err
		}
		if _, err := exec("", "update-index", "--add", "--cacheinfo", mode+","+strings.TrimSpace(sha)+","+f.Path); err != nil {
			return "", err
		}
	}
	args := []string{"commit", "-F", "-", "--cleanup=strip"}
	if req.Amend {
		args = append(args, "--amend")
	}
	if req.Author != "" {
		args = append(args, "--author="+req.Author)
	}
	if req.SignOff {
		args = append(args, "--signoff")
	}
	if req.NoVerify {
		args = append(args, "--no-verify")
	}
	out, err := exec(req.Message, args...)
	if err != nil {
		return out, err
	}
	if len(touched) > 0 {
		if _, err := r.Exec(append([]string{"reset", "-q", "HEAD", "--"}, touched...)...); err != nil {
			return out, err
		}
	}
	if _, err := os.Stat(filepath.Join(r.GitDir, "MERGE_HEAD")); err == nil {
		r.Exec("reset", "-q", "HEAD")
	}
	return out, nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func (r *Repo) LastCommitMessage() string {
	out, err := r.Read("log", "-1", "--format=%B")
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n")
}

func (r *Repo) AmendMessage(message string) error {
	_, err := r.ExecInput(message, "commit", "--amend", "--only", "-F", "-")
	return err
}

func (r *Repo) MergeFile(current, base, other []byte) ([]byte, bool, error) {
	dir, err := os.MkdirTemp("", "ideagit-merge")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	paths := []string{filepath.Join(dir, "current"), filepath.Join(dir, "base"), filepath.Join(dir, "other")}
	for i, b := range [][]byte{current, base, other} {
		if err := os.WriteFile(paths[i], b, 0o644); err != nil {
			return nil, false, err
		}
	}
	out, err := r.run(runOpts{allowRC: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}, "merge-file", "-p", "-L", "Current", "-L", "Base", "-L", "Shelved", paths[0], paths[1], paths[2])
	if err != nil {
		return nil, false, err
	}
	return []byte(out), strings.Contains(out, "<<<<<<< Current"), nil
}

func (r *Repo) IsAncestorOfRemote(hash string) bool {
	out, err := r.Read("branch", "-r", "--contains", hash)
	return err == nil && strings.TrimSpace(out) != ""
}
