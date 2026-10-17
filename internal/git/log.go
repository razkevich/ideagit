package git

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Commit struct {
	Hash      string
	Parents   []string
	Author    string
	Email     string
	Date      time.Time
	Committer string
	CDate     time.Time
	Refs      []string
	Subject   string
}

type LogFilter struct {
	Text      string
	Regex     bool
	MatchCase bool
	Branches  []string
	Users     []string
	Since     string
	Until     string
	Paths     []string
	Limit     int
}

func (f LogFilter) limitsCommits() bool {
	return f.Text != "" || len(f.Users) > 0 || f.Since != "" || f.Until != ""
}

var hashRe = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

const logFormat = "%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%cn%x1f%ct%x1f%D%x1f%s"

func (r *Repo) Log(f LogFilter) ([]*Commit, error) {
	if f.Limit == 0 {
		f.Limit = 3000
	}
	args := []string{"log", "--date-order", "--format=" + logFormat, "-n", strconv.Itoa(f.Limit)}
	args = append(args, r.filterArgs(f)...)
	if len(f.Paths) > 0 {
		args = append(args, "--parents", "--")
		args = append(args, f.Paths...)
	}
	out, err := r.Read(args...)
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") || strings.Contains(err.Error(), "bad default revision") {
			return nil, nil
		}
		return nil, err
	}
	commits := parseLog(out)
	if f.Text != "" && hashRe.MatchString(f.Text) {
		if hc := r.commitByHash(f.Text); hc != nil {
			found := false
			for _, c := range commits {
				if c.Hash == hc.Hash {
					found = true
				}
			}
			if !found {
				commits = append([]*Commit{hc}, commits...)
			}
		}
	}
	if f.limitsCommits() {
		r.rewriteParents(commits, f)
	}
	return commits, nil
}

func (r *Repo) filterArgs(f LogFilter) []string {
	var args []string
	if len(f.Branches) > 0 {
		args = append(args, f.Branches...)
	} else {
		args = append(args, "--exclude=refs/stash", "--all")
	}
	for _, u := range f.Users {
		args = append(args, "--author="+regexp.QuoteMeta(u))
	}
	if f.Since != "" {
		args = append(args, "--since="+f.Since)
	}
	if f.Until != "" {
		args = append(args, "--until="+f.Until)
	}
	if f.Text != "" {
		args = append(args, "--grep="+f.Text)
		if !f.Regex {
			args = append(args, "--fixed-strings")
		} else {
			args = append(args, "--extended-regexp")
		}
		if !f.MatchCase {
			args = append(args, "--regexp-ignore-case")
		}
	}
	return args
}

func (r *Repo) commitByHash(h string) *Commit {
	out, err := r.Read("log", "-1", "--format="+logFormat, h, "--")
	if err != nil {
		return nil
	}
	cs := parseLog(out)
	if len(cs) == 0 {
		return nil
	}
	return cs[0]
}

func parseLog(out string) []*Commit {
	var res []*Commit
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(line, "\x1f")
		if len(p) < 9 {
			continue
		}
		c := &Commit{Hash: p[0], Author: p[2], Email: p[3], Committer: p[5], Subject: p[8]}
		if p[1] != "" {
			c.Parents = strings.Fields(p[1])
		}
		if ts, err := strconv.ParseInt(p[4], 10, 64); err == nil {
			c.Date = time.Unix(ts, 0)
		}
		if ts, err := strconv.ParseInt(p[6], 10, 64); err == nil {
			c.CDate = time.Unix(ts, 0)
		}
		if p[7] != "" {
			c.Refs = strings.Split(p[7], ", ")
		}
		res = append(res, c)
	}
	return res
}

// rewriteParents connects filtered commits to their nearest visible ancestors so the
// graph stays continuous, like IntelliJ does for user/text/date filters.
func (r *Repo) rewriteParents(commits []*Commit, f LogFilter) {
	visible := make(map[string]bool, len(commits))
	for _, c := range commits {
		visible[c.Hash] = true
	}
	args := []string{"rev-list", "--parents", "-n", "60000"}
	if len(f.Branches) > 0 {
		args = append(args, f.Branches...)
	} else {
		args = append(args, "--exclude=refs/stash", "--all")
	}
	out, err := r.Read(args...)
	if err != nil {
		return
	}
	parents := map[string][]string{}
	for _, l := range strings.Split(out, "\n") {
		fs := strings.Fields(l)
		if len(fs) > 0 {
			parents[fs[0]] = fs[1:]
		}
	}
	memo := map[string][]string{}
	var resolve func(h string, depth int) []string
	resolve = func(h string, depth int) []string {
		if v, ok := memo[h]; ok {
			return v
		}
		memo[h] = nil
		var res []string
		seen := map[string]bool{}
		for _, p := range parents[h] {
			if visible[p] {
				if !seen[p] {
					seen[p] = true
					res = append(res, p)
				}
				continue
			}
			if depth > 5000 {
				continue
			}
			for _, a := range resolve(p, depth+1) {
				if !seen[a] {
					seen[a] = true
					res = append(res, a)
				}
			}
		}
		if len(res) > 3 {
			res = res[:3]
		}
		memo[h] = res
		return res
	}
	for _, c := range commits {
		if _, ok := parents[c.Hash]; ok {
			c.Parents = resolve(c.Hash, 0)
		} else {
			c.Parents = nil
		}
	}
}

type ChangedFile struct {
	Path     string
	OrigPath string
	Kind     ChangeKind
}

func parseNameStatus(out string) []ChangedFile {
	var res []ChangedFile
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		code := fields[i]
		if code == "" {
			continue
		}
		cf := ChangedFile{}
		switch code[0] {
		case 'A':
			cf.Kind = Added
		case 'D':
			cf.Kind = Deleted
		case 'R', 'C':
			cf.Kind = Renamed
			if i+2 < len(fields) {
				cf.OrigPath = fields[i+1]
				cf.Path = fields[i+2]
				i += 2
				if code[0] == 'C' {
					cf.Kind = Added
					cf.OrigPath = ""
				}
				res = append(res, cf)
			}
			continue
		case 'T':
			cf.Kind = TypeChanged
		case 'U':
			cf.Kind = Conflict
		default:
			cf.Kind = Modified
		}
		if i+1 < len(fields) {
			cf.Path = fields[i+1]
			i++
		}
		res = append(res, cf)
	}
	return res
}

// CommitFiles lists changes of a commit against its first parent.
func (r *Repo) CommitFiles(c *Commit) ([]ChangedFile, error) {
	base := EmptyTree
	if p := r.realFirstParent(c.Hash); p != "" {
		base = p
	}
	out, err := r.Read("diff", "--name-status", "-z", "-M", base, c.Hash, "--")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func (r *Repo) realFirstParent(hash string) string {
	out, err := r.Read("rev-parse", "-q", "--verify", hash+"^1")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (r *Repo) ParentRev(hash string) string {
	if p := r.realFirstParent(hash); p != "" {
		return p
	}
	return EmptyTree
}

func (r *Repo) DiffNameStatus(from, to string) ([]ChangedFile, error) {
	args := []string{"diff", "--name-status", "-z", "-M", from}
	if to != "" {
		args = append(args, to)
	}
	out, err := r.Read(append(args, "--")...)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

type CommitDetails struct {
	Hash      string
	Message   string
	Author    string
	Email     string
	Date      time.Time
	Committer string
	CEmail    string
	CDate     time.Time
	Parents   []string
}

func (r *Repo) Details(hash string) (*CommitDetails, error) {
	out, err := r.Read("show", "-s", "--format=%H%x1f%an%x1f%ae%x1f%at%x1f%cn%x1f%ce%x1f%ct%x1f%P%x1f%B", hash)
	if err != nil {
		return nil, err
	}
	p := strings.SplitN(out, "\x1f", 9)
	if len(p) < 9 {
		return nil, nil
	}
	d := &CommitDetails{Hash: p[0], Author: p[1], Email: p[2], Committer: p[4], CEmail: p[5], Parents: strings.Fields(p[7]), Message: strings.TrimRight(p[8], "\n")}
	if ts, err := strconv.ParseInt(p[3], 10, 64); err == nil {
		d.Date = time.Unix(ts, 0)
	}
	if ts, err := strconv.ParseInt(p[6], 10, 64); err == nil {
		d.CDate = time.Unix(ts, 0)
	}
	return d, nil
}

func (r *Repo) Authors() []string {
	out, err := r.Read("log", "--all", "--format=%an", "-n", "3000")
	if err != nil {
		return nil
	}
	count := map[string]int{}
	var order []string
	for _, a := range strings.Split(out, "\n") {
		if a == "" {
			continue
		}
		if count[a] == 0 {
			order = append(order, a)
		}
		count[a]++
	}
	return order
}

// Outgoing lists commits on HEAD not yet on the push target.
func (r *Repo) Outgoing(target string) ([]*Commit, error) {
	args := []string{"log", "--format=" + logFormat, "-n", "500", "HEAD"}
	if target != "" {
		if _, err := r.RevParse(target); err == nil {
			args = append(args, "^"+target)
		} else {
			args = append(args, "--not", "--remotes")
		}
	} else {
		args = append(args, "--not", "--remotes")
	}
	out, err := r.Read(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}
