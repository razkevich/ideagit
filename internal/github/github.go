// Package github reads and acts on pull requests through the GitHub CLI (gh), so it
// reuses the user's existing gh authentication.
package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Dir string
	// Log receives every mutating gh command, for the Console tab.
	Log func(cmd, output string, failed bool)
}

var ErrNoGH = errors.New("the GitHub CLI (gh) isn't installed — brew install gh, then gh auth login")

func Available() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return ErrNoGH
	}
	return nil
}

func (c *Client) run(logged bool, stdin string, args ...string) (string, error) {
	if err := Available(); err != nil {
		return "", err
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1", "GH_PAGER=")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	var stderr string
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		stderr = strings.TrimSpace(string(ee.Stderr))
	}
	if logged && c.Log != nil {
		c.Log("gh "+strings.Join(args, " "), strings.TrimSpace(string(out)+"\n"+stderr), err != nil)
	}
	if err != nil {
		if stderr != "" {
			return string(out), errors.New(stderr)
		}
		return string(out), err
	}
	return string(out), nil
}

type Check struct {
	Name  string
	State string // SUCCESS, FAILURE, PENDING, SKIPPED, NEUTRAL, CANCELLED…
	URL   string
}

func (c Check) Passed() bool {
	return c.State == "SUCCESS" || c.State == "SKIPPED" || c.State == "NEUTRAL"
}
func (c Check) Failed() bool {
	return c.State == "FAILURE" || c.State == "ERROR" || c.State == "CANCELLED" || c.State == "TIMED_OUT" || c.State == "ACTION_REQUIRED" || c.State == "STARTUP_FAILURE"
}
func (c Check) Pending() bool { return !c.Passed() && !c.Failed() }

type Review struct {
	Author string
	State  string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, PENDING
	At     time.Time
	Body   string
}

type Comment struct {
	Author string
	Body   string
	At     time.Time
}

type PR struct {
	Number         int
	Title          string
	State          string // OPEN, CLOSED, MERGED
	IsDraft        bool
	Author         string
	HeadRef        string
	BaseRef        string
	HeadOid        string
	BaseOid        string
	URL            string
	Labels         []string
	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	Checks         []Check
	Additions      int
	Deletions      int
	ChangedFiles   int
	CrossRepo      bool
	HeadOwner      string
	Created        time.Time
	Updated        time.Time

	// Only filled by View.
	Body          string
	Reviews       []Review
	RequestedFrom []string
	Comments      []Comment
	Mergeable     string
	MergeState    string
	Detailed      bool
}

// CheckSummary counts passed, failed and pending checks, collapsing re-runs by name.
func (p *PR) CheckSummary() (pass, fail, pending int) {
	latest := map[string]Check{}
	var order []string
	for _, c := range p.Checks {
		if _, ok := latest[c.Name]; !ok {
			order = append(order, c.Name)
		}
		latest[c.Name] = c
	}
	for _, n := range order {
		c := latest[n]
		switch {
		case c.Failed():
			fail++
		case c.Passed():
			pass++
		default:
			pending++
		}
	}
	return
}

type rawPR struct {
	Number              int                     `json:"number"`
	Title               string                  `json:"title"`
	State               string                  `json:"state"`
	IsDraft             bool                    `json:"isDraft"`
	Author              struct{ Login string }  `json:"author"`
	HeadRefName         string                  `json:"headRefName"`
	BaseRefName         string                  `json:"baseRefName"`
	HeadRefOid          string                  `json:"headRefOid"`
	BaseRefOid          string                  `json:"baseRefOid"`
	URL                 string                  `json:"url"`
	Labels              []struct{ Name string } `json:"labels"`
	ReviewDecision      string                  `json:"reviewDecision"`
	StatusCheckRollup   []json.RawMessage       `json:"statusCheckRollup"`
	Additions           int                     `json:"additions"`
	Deletions           int                     `json:"deletions"`
	ChangedFiles        int                     `json:"changedFiles"`
	IsCrossRepository   bool                    `json:"isCrossRepository"`
	HeadRepositoryOwner struct{ Login string }  `json:"headRepositoryOwner"`
	CreatedAt           time.Time               `json:"createdAt"`
	UpdatedAt           time.Time               `json:"updatedAt"`
	Body                string                  `json:"body"`
	Reviews             []struct {
		Author      struct{ Login string } `json:"author"`
		State       string                 `json:"state"`
		SubmittedAt time.Time              `json:"submittedAt"`
		Body        string                 `json:"body"`
	} `json:"reviews"`
	ReviewRequests []struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Slug  string `json:"slug"`
	} `json:"reviewRequests"`
	Comments []struct {
		Author    struct{ Login string } `json:"author"`
		Body      string                 `json:"body"`
		CreatedAt time.Time              `json:"createdAt"`
	} `json:"comments"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
}

func (r rawPR) toPR(detailed bool) PR {
	p := PR{
		Number: r.Number, Title: r.Title, State: r.State, IsDraft: r.IsDraft, Author: r.Author.Login,
		HeadRef: r.HeadRefName, BaseRef: r.BaseRefName, HeadOid: r.HeadRefOid, BaseOid: r.BaseRefOid, URL: r.URL,
		ReviewDecision: r.ReviewDecision, Additions: r.Additions, Deletions: r.Deletions, ChangedFiles: r.ChangedFiles,
		CrossRepo: r.IsCrossRepository, HeadOwner: r.HeadRepositoryOwner.Login, Created: r.CreatedAt, Updated: r.UpdatedAt,
		Body: r.Body, Mergeable: r.Mergeable, MergeState: r.MergeStateStatus, Detailed: detailed,
	}
	for _, l := range r.Labels {
		p.Labels = append(p.Labels, l.Name)
	}
	for _, raw := range r.StatusCheckRollup {
		var c struct {
			Typename   string `json:"__typename"`
			Name       string `json:"name"`
			Context    string `json:"context"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
			DetailsURL string `json:"detailsUrl"`
			TargetURL  string `json:"targetUrl"`
		}
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		ch := Check{Name: c.Name, URL: c.DetailsURL}
		if c.Typename == "StatusContext" {
			ch = Check{Name: c.Context, State: c.State, URL: c.TargetURL}
		} else if c.Status != "COMPLETED" {
			ch.State = "PENDING"
		} else {
			ch.State = c.Conclusion
		}
		p.Checks = append(p.Checks, ch)
	}
	for _, rv := range r.Reviews {
		p.Reviews = append(p.Reviews, Review{Author: rv.Author.Login, State: rv.State, At: rv.SubmittedAt, Body: rv.Body})
	}
	for _, rq := range r.ReviewRequests {
		p.RequestedFrom = append(p.RequestedFrom, firstNonEmpty(rq.Login, rq.Slug, rq.Name))
	}
	for _, cm := range r.Comments {
		p.Comments = append(p.Comments, Comment{Author: cm.Author.Login, Body: cm.Body, At: cm.CreatedAt})
	}
	return p
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// listFields are the cheap ones: diff stats and especially check rollups make GitHub's
// GraphQL slow (checks for 100 PRs can time out), so those load separately.
const listFields = "number,title,state,isDraft,author,headRefName,baseRefName,headRefOid,url,labels,reviewDecision,isCrossRepository,updatedAt"
const viewFields = listFields + ",additions,deletions,changedFiles,headRepositoryOwner,createdAt,statusCheckRollup,baseRefOid,body,reviews,reviewRequests,comments,mergeable,mergeStateStatus"

// ListOptions map to gh's own filter flags where possible; Search (GitHub search
// syntax) is only needed for review filters and free text, and is slower.
type ListOptions struct {
	State  string // open, closed, merged, all
	Draft  bool
	Author string
	Label  string
	Base   string
	Search string
	Limit  int
}

func (o ListOptions) args(fields string) []string {
	state := o.State
	if state == "" {
		state = "all"
	}
	args := []string{"pr", "list", "--state", state, "--limit", strconv.Itoa(o.Limit), "--json", fields}
	if o.Draft {
		args = append(args, "--draft")
	}
	if o.Author != "" {
		args = append(args, "--author", o.Author)
	}
	if o.Label != "" {
		args = append(args, "--label", o.Label)
	}
	if o.Base != "" {
		args = append(args, "--base", o.Base)
	}
	if strings.TrimSpace(o.Search) != "" {
		args = append(args, "--search", o.Search)
	}
	return args
}

func (c *Client) List(o ListOptions) ([]PR, error) {
	out, err := c.run(false, "", o.args(listFields)...)
	if err != nil {
		return nil, err
	}
	var raws []rawPR
	if err := json.Unmarshal([]byte(out), &raws); err != nil {
		return nil, fmt.Errorf("unexpected gh output: %w", err)
	}
	prs := make([]PR, len(raws))
	for i, r := range raws {
		prs[i] = r.toPR(false)
	}
	return prs, nil
}

// Checks loads CI check rollups for the same filter, keyed by PR number.
func (c *Client) Checks(o ListOptions) (map[int][]Check, error) {
	out, err := c.run(false, "", o.args("number,statusCheckRollup")...)
	if err != nil {
		return nil, err
	}
	var raws []rawPR
	if err := json.Unmarshal([]byte(out), &raws); err != nil {
		return nil, err
	}
	res := map[int][]Check{}
	for _, r := range raws {
		res[r.Number] = r.toPR(false).Checks
		if res[r.Number] == nil {
			res[r.Number] = []Check{}
		}
	}
	return res, nil
}

func (c *Client) View(number int) (*PR, error) {
	out, err := c.run(false, "", "pr", "view", strconv.Itoa(number), "--json", viewFields)
	if err != nil {
		return nil, err
	}
	var r rawPR
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, fmt.Errorf("unexpected gh output: %w", err)
	}
	p := r.toPR(true)
	return &p, nil
}

func (c *Client) Me() string {
	out, err := c.run(false, "", "api", "user", "--jq", ".login")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (c *Client) Labels() []string {
	out, err := c.run(false, "", "label", "list", "--limit", "200", "--json", "name", "--jq", ".[].name")
	if err != nil {
		return nil
	}
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

func (c *Client) do(stdin string, args ...string) error {
	_, err := c.run(true, stdin, args...)
	return err
}

func (c *Client) Checkout(n int) error { return c.do("", "pr", "checkout", strconv.Itoa(n)) }
func (c *Client) OpenWeb(n int) error  { return c.do("", "pr", "view", strconv.Itoa(n), "--web") }

func (c *Client) Review(n int, kind, body string) error {
	args := []string{"pr", "review", strconv.Itoa(n), "--" + kind}
	if body != "" || kind != "approve" {
		args = append(args, "--body-file", "-")
	}
	return c.do(body, args...)
}

func (c *Client) Comment(n int, body string) error {
	return c.do(body, "pr", "comment", strconv.Itoa(n), "--body-file", "-")
}

func (c *Client) Merge(n int, method string, deleteBranch bool) error {
	args := []string{"pr", "merge", strconv.Itoa(n), "--" + method}
	if deleteBranch {
		args = append(args, "--delete-branch")
	}
	return c.do("", args...)
}

func (c *Client) SetDraft(n int, draft bool) error {
	args := []string{"pr", "ready", strconv.Itoa(n)}
	if draft {
		args = append(args, "--undo")
	}
	return c.do("", args...)
}

func (c *Client) EditLabels(n int, add, remove []string) error {
	args := []string{"pr", "edit", strconv.Itoa(n)}
	for _, l := range add {
		args = append(args, "--add-label", l)
	}
	for _, l := range remove {
		args = append(args, "--remove-label", l)
	}
	return c.do("", args...)
}

func (c *Client) CreateLabel(name string) error { return c.do("", "label", "create", name) }

func (c *Client) SetOpen(n int, open bool) error {
	if open {
		return c.do("", "pr", "reopen", strconv.Itoa(n))
	}
	return c.do("", "pr", "close", strconv.Itoa(n))
}

// Create opens a PR for the current branch and returns its URL.
func (c *Client) Create(title, body, base string, draft bool) (string, error) {
	args := []string{"pr", "create", "--title", title, "--body-file", "-"}
	if base != "" {
		args = append(args, "--base", base)
	}
	if draft {
		args = append(args, "--draft")
	}
	out, err := c.run(true, body, args...)
	return strings.TrimSpace(out), err
}

var prURL = regexp.MustCompile(`/pull/(\d+)`)

// ParseRef accepts "123", "#123" or a pull request URL.
func ParseRef(s string) (int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, true
	}
	if m := prURL.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, n > 0
	}
	return 0, false
}

// Assignees lists users who can be assigned in this repository (people with access),
// which is the closest thing to "all users" for an author filter.
func (c *Client) Assignees() ([]string, error) {
	out, err := c.run(false, "", "api", "--paginate", "repos/{owner}/{repo}/assignees?per_page=100", "--jq", ".[].login")
	if err != nil {
		return nil, err
	}
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res, nil
}

var actionsRun = regexp.MustCompile(`/actions/runs/(\d+)`)

// RerunFailed re-runs the failed jobs of every GitHub Actions run behind the failed
// checks. Checks from other CI systems can't be re-run through gh; their names are
// returned so the caller can say so.
func (c *Client) RerunFailed(checks []Check) (runs int, other []string, err error) {
	seen := map[string]bool{}
	latest := map[string]Check{}
	for _, ch := range checks {
		latest[ch.Name] = ch
	}
	for _, ch := range latest {
		if !ch.Failed() {
			continue
		}
		m := actionsRun.FindStringSubmatch(ch.URL)
		if m == nil {
			other = append(other, ch.Name)
			continue
		}
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		if err := c.do("", "run", "rerun", m[1], "--failed"); err != nil {
			return runs, other, err
		}
		runs++
	}
	return runs, other, nil
}
