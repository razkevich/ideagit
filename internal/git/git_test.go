package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "t@example.com"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, r *Repo, path, content string) {
	t.Helper()
	p := r.Abs(path)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func show(t *testing.T, r *Repo, rev, path string) string {
	t.Helper()
	b, ok := r.Show(rev, path)
	if !ok {
		t.Fatalf("%s:%s missing", rev, path)
	}
	return string(b)
}

func TestCommitSelectedFilesKeepsOtherStagedChanges(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a1\n")
	write(t, r, "b.txt", "b1\n")
	if _, err := r.Commit(CommitRequest{Message: "init", Files: []CommitFile{{Path: "a.txt"}, {Path: "b.txt"}}}); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.txt", "a2\n")
	write(t, r, "b.txt", "b2\n")
	r.Add([]string{"b.txt"})
	if _, err := r.Commit(CommitRequest{Message: "only a", Files: []CommitFile{{Path: "a.txt"}}}); err != nil {
		t.Fatal(err)
	}
	if got := show(t, r, "HEAD", "a.txt"); got != "a2\n" {
		t.Fatalf("a in HEAD = %q", got)
	}
	if got := show(t, r, "HEAD", "b.txt"); got != "b1\n" {
		t.Fatalf("b must not be committed, got %q", got)
	}
	st, _ := r.Status()
	for _, f := range st.Files {
		if f.Path == "b.txt" && f.Staged() != 2 {
			t.Fatalf("b.txt should stay staged: %+v", f)
		}
		if f.Path == "a.txt" {
			t.Fatalf("a.txt should be clean: %+v", f)
		}
	}
}

func TestPartialCommitContent(t *testing.T) {
	r := newRepo(t)
	write(t, r, "f.txt", "1\n2\n3\n")
	r.Commit(CommitRequest{Message: "init", Files: []CommitFile{{Path: "f.txt"}}})
	write(t, r, "f.txt", "ONE\n2\nTHREE\n")
	if _, err := r.Commit(CommitRequest{Message: "partial", Files: []CommitFile{{Path: "f.txt", Content: []byte("ONE\n2\n3\n")}}}); err != nil {
		t.Fatal(err)
	}
	if got := show(t, r, "HEAD", "f.txt"); got != "ONE\n2\n3\n" {
		t.Fatalf("HEAD = %q", got)
	}
	b, _ := os.ReadFile(r.Abs("f.txt"))
	if string(b) != "ONE\n2\nTHREE\n" {
		t.Fatalf("working tree changed: %q", b)
	}
	st, _ := r.Status()
	if len(st.Files) != 1 || st.Files[0].Y != 'M' || st.Files[0].X != '.' {
		t.Fatalf("expected remaining unstaged change, got %+v", st.Files)
	}
}

func TestCommitNewDeletedAndAmend(t *testing.T) {
	r := newRepo(t)
	write(t, r, "keep.txt", "k\n")
	write(t, r, "gone.txt", "g\n")
	r.Commit(CommitRequest{Message: "init", Files: []CommitFile{{Path: "keep.txt"}, {Path: "gone.txt"}}})
	os.Remove(r.Abs("gone.txt"))
	write(t, r, "new.txt", "n\n")
	if _, err := r.Commit(CommitRequest{Message: "second", Files: []CommitFile{{Path: "gone.txt", Deleted: true}, {Path: "new.txt"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Show("HEAD", "gone.txt"); ok {
		t.Fatal("gone.txt still in HEAD")
	}
	write(t, r, "keep.txt", "k2\n")
	if _, err := r.Commit(CommitRequest{Message: "second amended", Amend: true, Files: []CommitFile{{Path: "keep.txt"}}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.LastCommitMessage()); got != "second amended" {
		t.Fatalf("message %q", got)
	}
	out, _ := r.Read("rev-list", "--count", "HEAD")
	if strings.TrimSpace(out) != "2" {
		t.Fatalf("amend should not add a commit, count=%s", out)
	}
	if show(t, r, "HEAD", "new.txt") != "n\n" || show(t, r, "HEAD", "keep.txt") != "k2\n" {
		t.Fatal("amended tree wrong")
	}
}

func TestRollback(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	r.Commit(CommitRequest{Message: "init", Files: []CommitFile{{Path: "a.txt"}}})
	write(t, r, "a.txt", "changed\n")
	write(t, r, "added.txt", "x\n")
	r.Add([]string{"added.txt"})
	st, _ := r.Status()
	if err := r.Rollback(st.Files, true, false); err != nil {
		t.Fatal(err)
	}
	st, _ = r.Status()
	if len(st.Files) != 0 {
		t.Fatalf("expected clean, got %+v", st.Files)
	}
}

func TestWorktreesAndStatus(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	r.Commit(CommitRequest{Message: "init", Files: []CommitFile{{Path: "a.txt"}}})
	wt := filepath.Join(t.TempDir(), "wt")
	if err := r.AddWorktree(wt, "feature", "main", true); err != nil {
		t.Fatal(err)
	}
	wts, err := r.Worktrees()
	if err != nil || len(wts) != 2 {
		t.Fatalf("worktrees %v %v", wts, err)
	}
	if wts[0].Origin != OriginMain || !wts[0].Current || wts[1].Branch != "feature" {
		t.Fatalf("bad worktrees %+v %+v", wts[0], wts[1])
	}
	r.WorktreeStatus(wts[1], "main")
	if !wts[1].Merged || wts[1].Dirty != 0 {
		t.Fatalf("expected merged clean: %+v", wts[1])
	}
	if err := r.RemoveWorktree(wt, false); err != nil {
		t.Fatal(err)
	}
}
