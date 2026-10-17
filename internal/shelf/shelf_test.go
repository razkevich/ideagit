package shelf

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/razkevich/ideagit/internal/git"
)

func TestShelveUnshelveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "T")
	run("config", "user.email", "t@e")
	os.Mkdir(dir+"/.idea", 0o755)
	os.WriteFile(dir+"/f.txt", []byte("line \"one\"\n\ttwo\\x\n"), 0o644)
	run("add", "f.txt")
	run("commit", "-qm", "init")
	repo, _ := git.Open(dir)
	newContent := "line \"one\"\n\ttwo\\x\nthree\n"
	s, err := Create(repo, "my shelf", []Source{
		{Path: "f.txt", Kind: git.Modified, Old: []byte("line \"one\"\n\ttwo\\x\n"), New: []byte(newContent)},
		{Path: "n.txt", Kind: git.Untracked, New: []byte("new\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.XMLPath, ".idea/shelf/my_shelf.xml") {
		t.Fatalf("xml path %s", s.XMLPath)
	}
	list, _ := List(repo)
	if len(list) != 1 || len(list[0].Files) != 2 || list[0].Description != "my shelf" {
		t.Fatalf("list %+v", list)
	}
	if b := list[0].Files[0].Base; b == nil || *b != "line \"one\"\n\ttwo\\x\n" {
		t.Fatalf("base text not round-tripped: %q", *b)
	}
	if _, err := list[0].Unshelve(repo, list[0].Files); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dir + "/f.txt")
	if string(got) != newContent {
		t.Fatalf("unshelved %q", got)
	}
	got, _ = os.ReadFile(dir + "/n.txt")
	if string(got) != "new\n" {
		t.Fatalf("new file %q", got)
	}
}
