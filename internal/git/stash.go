package git

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Stash struct {
	Index   int
	Ref     string
	Hash    string
	Message string
	Branch  string
	Date    time.Time
}

func (r *Repo) Stashes() ([]Stash, error) {
	out, err := r.Read("stash", "list", "--format=%gd%x1f%H%x1f%ct%x1f%gs")
	if err != nil {
		return nil, err
	}
	var res []Stash
	for i, l := range strings.Split(strings.TrimSpace(out), "\n") {
		p := strings.SplitN(l, "\x1f", 4)
		if len(p) < 4 {
			continue
		}
		s := Stash{Index: i, Ref: p[0], Hash: p[1]}
		if ts, err := strconv.ParseInt(p[2], 10, 64); err == nil {
			s.Date = time.Unix(ts, 0)
		}
		msg := p[3]
		// "WIP on branch: abc subject" or "On branch: message"
		for _, prefix := range []string{"WIP on ", "On "} {
			if strings.HasPrefix(msg, prefix) {
				rest := strings.TrimPrefix(msg, prefix)
				if i := strings.Index(rest, ": "); i >= 0 {
					s.Branch = rest[:i]
					msg = rest[i+2:]
					if prefix == "WIP on " {
						if sp := strings.IndexByte(msg, ' '); sp > 0 && hashRe.MatchString(msg[:sp]) {
							msg = msg[sp+1:]
						}
					}
				}
				break
			}
		}
		s.Message = msg
		res = append(res, s)
	}
	return res, nil
}

func (r *Repo) StashFiles(s Stash) ([]ChangedFile, bool, error) {
	out, err := r.Read("diff", "--name-status", "-z", "-M", s.Hash+"^1", s.Hash, "--")
	if err != nil {
		return nil, false, err
	}
	files := parseNameStatus(out)
	hasUntracked := false
	if u, err := r.Read("rev-parse", "-q", "--verify", s.Hash+"^3"); err == nil && strings.TrimSpace(u) != "" {
		hasUntracked = true
		uo, err := r.Read("ls-tree", "-r", "-z", "--name-only", s.Hash+"^3")
		if err == nil {
			for _, p := range strings.Split(uo, "\x00") {
				if p != "" {
					files = append(files, ChangedFile{Path: p, Kind: Untracked})
				}
			}
		}
	}
	return files, hasUntracked, nil
}

func (r *Repo) StashPush(message string, keepIndex, includeUntracked bool, paths []string) error {
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	if keepIndex {
		args = append(args, "--keep-index")
	}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	_, err := r.Exec(args...)
	return err
}

// checkStash makes sure stash@{N} still is the stash the user picked; refs/stash is
// shared by all worktrees, so another one may have pushed or dropped stashes meanwhile.
func (r *Repo) checkStash(s Stash) error {
	h, err := r.RevParse(s.Ref)
	if err != nil || h != s.Hash {
		return fmt.Errorf("the stash list changed (maybe from another worktree); refresh and try again")
	}
	return nil
}

func (r *Repo) StashApply(s Stash, pop, reinstateIndex bool) error {
	if err := r.checkStash(s); err != nil {
		return err
	}
	verb := "apply"
	if pop {
		verb = "pop"
	}
	args := []string{"stash", verb}
	if reinstateIndex {
		args = append(args, "--index")
	}
	_, err := r.Exec(append(args, s.Ref)...)
	return err
}

func (r *Repo) StashDrop(s Stash) error {
	if err := r.checkStash(s); err != nil {
		return err
	}
	_, err := r.Exec("stash", "drop", s.Ref)
	return err
}

func (r *Repo) StashClear() error {
	_, err := r.Exec("stash", "clear")
	return err
}

func (r *Repo) StashBranch(s Stash, branch string) error {
	if err := r.checkStash(s); err != nil {
		return err
	}
	_, err := r.Exec("stash", "branch", branch, s.Ref)
	return err
}
