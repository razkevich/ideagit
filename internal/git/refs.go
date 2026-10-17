package git

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type RefKind int

const (
	LocalBranch RefKind = iota
	RemoteBranch
	Tag
)

type Ref struct {
	Full     string
	Name     string
	Kind     RefKind
	Hash     string
	Upstream string
	Ahead    int
	Behind   int
	Gone     bool
	IsHead   bool
	Date     time.Time
	Remote   string
}

func (r *Repo) Refs() ([]Ref, error) {
	out, err := r.Read("for-each-ref", "--format=%(refname)%1f%(refname:short)%1f%(objectname)%1f%(upstream:short)%1f%(upstream:track)%1f%(HEAD)%1f%(committerdate:unix)%1f%(*objectname)",
		"refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	remotes := r.Remotes()
	var refs []Ref
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		p := strings.Split(line, "\x1f")
		if len(p) < 8 {
			continue
		}
		ref := Ref{Full: p[0], Name: p[1], Hash: p[2], Upstream: p[3], IsHead: p[5] == "*"}
		if p[7] != "" {
			ref.Hash = p[7]
		}
		if ts, err := strconv.ParseInt(p[6], 10, 64); err == nil {
			ref.Date = time.Unix(ts, 0)
		}
		track := strings.Trim(p[4], "[]")
		for _, part := range strings.Split(track, ", ") {
			switch {
			case strings.HasPrefix(part, "ahead "):
				ref.Ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "ahead "))
			case strings.HasPrefix(part, "behind "):
				ref.Behind, _ = strconv.Atoi(strings.TrimPrefix(part, "behind "))
			case part == "gone":
				ref.Gone = true
			}
		}
		switch {
		case strings.HasPrefix(ref.Full, "refs/heads/"):
			ref.Kind = LocalBranch
			ref.Name = strings.TrimPrefix(ref.Full, "refs/heads/")
		case strings.HasPrefix(ref.Full, "refs/remotes/"):
			if strings.HasSuffix(ref.Full, "/HEAD") {
				continue
			}
			ref.Kind = RemoteBranch
			ref.Name = strings.TrimPrefix(ref.Full, "refs/remotes/")
			for _, rm := range remotes {
				if strings.HasPrefix(ref.Name, rm+"/") {
					ref.Remote = rm
				}
			}
		default:
			ref.Kind = Tag
			ref.Name = strings.TrimPrefix(ref.Full, "refs/tags/")
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (r *Repo) Remotes() []string {
	out, err := r.Read("remote")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

// RecentBranches returns branch names in the order they were last checked out.
func (r *Repo) RecentBranches(limit int) []string {
	out, err := r.Read("reflog", "show", "--format=%gs", "-n", "300", "HEAD")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "checkout: moving from ") {
			continue
		}
		rest := strings.TrimPrefix(l, "checkout: moving from ")
		parts := strings.SplitN(rest, " to ", 2)
		for _, b := range parts {
			if b != "" && !seen[b] && len(b) != 40 {
				seen[b] = true
				res = append(res, b)
			}
		}
		if len(res) >= limit {
			break
		}
	}
	return res
}

func SortRefs(refs []Ref) {
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].Name < refs[j].Name
	})
}

func (r *Repo) BranchesContaining(hash string) []string {
	out, err := r.Read("branch", "-a", "--format=%(refname:short)", "--contains", hash)
	if err != nil {
		return nil
	}
	var res []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" && !strings.HasSuffix(l, "/HEAD") && l != "origin" {
			res = append(res, l)
		}
	}
	return res
}
