package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ChangeKind int

const (
	Modified ChangeKind = iota
	Added
	Deleted
	Renamed
	Untracked
	Conflict
	TypeChanged
)

type FileStatus struct {
	Path     string
	OrigPath string
	X, Y     byte
	Kind     ChangeKind
	IsDir    bool
}

// Staged reports how much of the change is in the index: 0 none, 1 partial, 2 fully.
func (f FileStatus) Staged() int {
	if f.Kind == Untracked || f.Kind == Conflict || f.X == '.' {
		return 0
	}
	if f.Y == '.' {
		return 2
	}
	return 1
}

type RepoState int

const (
	StateClean RepoState = iota
	StateMerging
	StateRebasing
	StateCherryPicking
	StateReverting
)

func (s RepoState) String() string {
	return [...]string{"", "Merging", "Rebasing", "Cherry-picking", "Reverting"}[s]
}

type Status struct {
	Branch     string
	OID        string
	Upstream   string
	Ahead      int
	Behind     int
	Detached   bool
	Unborn     bool
	Files      []FileStatus
	State      RepoState
	RebaseOnto string
}

func (s *Status) Key() string {
	var b strings.Builder
	b.WriteString(s.Branch + s.OID + s.Upstream + strconv.Itoa(s.Ahead) + strconv.Itoa(s.Behind) + strconv.Itoa(int(s.State)))
	for _, f := range s.Files {
		b.WriteString("\x00" + f.Path + f.OrigPath + string([]byte{f.X, f.Y}))
	}
	return b.String()
}

func (r *Repo) Status() (*Status, error) {
	out, err := r.Read("status", "--porcelain=v2", "--branch", "-z", "--untracked-files=normal", "--find-renames")
	if err != nil {
		return nil, err
	}
	st := &Status{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		switch f[0] {
		case '#':
			parseBranchHeader(st, f)
		case '1':
			p := strings.SplitN(f, " ", 9)
			if len(p) < 9 {
				continue
			}
			st.Files = append(st.Files, newChange(p[1], p[8], ""))
		case '2':
			p := strings.SplitN(f, " ", 10)
			if len(p) < 10 {
				continue
			}
			orig := ""
			if i+1 < len(fields) {
				orig = fields[i+1]
				i++
			}
			st.Files = append(st.Files, newChange(p[1], p[9], orig))
		case 'u':
			p := strings.SplitN(f, " ", 11)
			if len(p) < 11 {
				continue
			}
			st.Files = append(st.Files, FileStatus{Path: p[10], X: p[1][0], Y: p[1][1], Kind: Conflict})
		case '?':
			path := f[2:]
			isDir := strings.HasSuffix(path, "/")
			st.Files = append(st.Files, FileStatus{Path: strings.TrimSuffix(path, "/"), X: '?', Y: '?', Kind: Untracked, IsDir: isDir})
		}
	}
	st.State, st.RebaseOnto = r.state()
	return st, nil
}

func parseBranchHeader(st *Status, f string) {
	switch {
	case strings.HasPrefix(f, "# branch.oid "):
		st.OID = strings.TrimPrefix(f, "# branch.oid ")
		st.Unborn = st.OID == "(initial)"
	case strings.HasPrefix(f, "# branch.head "):
		st.Branch = strings.TrimPrefix(f, "# branch.head ")
		st.Detached = st.Branch == "(detached)"
	case strings.HasPrefix(f, "# branch.upstream "):
		st.Upstream = strings.TrimPrefix(f, "# branch.upstream ")
	case strings.HasPrefix(f, "# branch.ab "):
		parts := strings.Fields(strings.TrimPrefix(f, "# branch.ab "))
		if len(parts) == 2 {
			st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
			st.Behind, _ = strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
		}
	}
}

func newChange(xy, path, orig string) FileStatus {
	c := FileStatus{Path: path, OrigPath: orig, X: xy[0], Y: xy[1]}
	switch {
	case c.X == 'C':
		// A copy adds a file; the source stays, so it must not be treated as a rename.
		c.Kind = Added
		c.OrigPath = ""
	case c.X == 'R':
		c.Kind = Renamed
	case c.X == 'A':
		c.Kind = Added
	case c.X == 'D' || c.Y == 'D':
		c.Kind = Deleted
	case c.X == 'T' || c.Y == 'T':
		c.Kind = TypeChanged
	default:
		c.Kind = Modified
	}
	return c
}

func (r *Repo) state() (RepoState, string) {
	exists := func(p string) bool { _, err := os.Stat(filepath.Join(r.GitDir, p)); return err == nil }
	switch {
	case exists("rebase-merge") || exists("rebase-apply"):
		onto := ""
		for _, d := range []string{"rebase-merge", "rebase-apply"} {
			if b, err := os.ReadFile(filepath.Join(r.GitDir, d, "onto")); err == nil {
				onto = strings.TrimSpace(string(b))
			}
		}
		return StateRebasing, onto
	case exists("MERGE_HEAD"):
		return StateMerging, ""
	case exists("CHERRY_PICK_HEAD"):
		return StateCherryPicking, ""
	case exists("REVERT_HEAD"):
		return StateReverting, ""
	}
	return StateClean, ""
}

func (r *Repo) RebaseBranch() string {
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if b, err := os.ReadFile(filepath.Join(r.GitDir, d, "head-name")); err == nil {
			return strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/")
		}
	}
	return ""
}

func (r *Repo) UntrackedUnder(dir string) []string {
	out, err := r.Read("ls-files", "--others", "--exclude-standard", "-z", "--", dir+"/")
	if err != nil {
		return nil
	}
	var res []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

func (r *Repo) MergeMessage() string {
	b, err := os.ReadFile(filepath.Join(r.GitDir, "MERGE_MSG"))
	if err != nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Files lists tracked and untracked (not ignored) files for Search Everywhere.
func (r *Repo) Files() []string {
	out, err := r.Read("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var res []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" && !seen[p] {
			seen[p] = true
			res = append(res, p)
		}
	}
	return res
}
