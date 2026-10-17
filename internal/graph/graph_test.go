package graph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/razkevich/ideagit/internal/git"
)

func render(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		for _, c := range r.Cells {
			b.WriteRune(c.R)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestMergeLayout(t *testing.T) {
	cs := []*git.Commit{
		{Hash: "m", Parents: []string{"a", "b"}},
		{Hash: "a", Parents: []string{"r"}},
		{Hash: "b", Parents: []string{"r"}},
		{Hash: "r"},
	}
	got := render(Layout(cs))
	want := "●─╮\n● │\n│ ●\n●─╯\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMergeWithBranchOffMerge(t *testing.T) {
	// feature branched off the merge commit M; M's second parent is fix.
	cs := []*git.Commit{
		{Hash: "top", Parents: []string{"M"}},
		{Hash: "f2", Parents: []string{"f1"}},
		{Hash: "f1", Parents: []string{"M"}},
		{Hash: "M", Parents: []string{"d", "x"}},
		{Hash: "d", Parents: []string{"base"}},
		{Hash: "x", Parents: []string{"base"}},
		{Hash: "base"},
	}
	got := render(Layout(cs))
	want := "●\n│ ●\n│ ●\n●─┴─╮\n● ╭─╯\n│ ●\n●─╯\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestLongEdgesBecomeArrowStubs(t *testing.T) {
	// tip's parent is far down main; the edge must not hold a lane in between.
	cs := []*git.Commit{{Hash: "tip", Parents: []string{"base"}}}
	for i := 0; i < LongEdge+5; i++ {
		cs = append(cs, &git.Commit{Hash: fmt.Sprint("m", i), Parents: []string{fmt.Sprint("m", i+1)}})
	}
	cs = append(cs, &git.Commit{Hash: fmt.Sprint("m", LongEdge+5), Parents: []string{"base"}}, &git.Commit{Hash: "base"})
	lines := strings.Split(strings.TrimSuffix(render(Layout(cs)), "\n"), "\n")
	if lines[0] != "●" || lines[1] != "↓ ●" || lines[2] != "●─╯" || lines[3] != "●" {
		t.Fatalf("child side:\n%s", strings.Join(lines[:4], "\n"))
	}
	n := len(lines)
	if lines[n-2] != "● ↑" || lines[n-1] != "●─╯" {
		t.Fatalf("parent side:\n%s", strings.Join(lines[n-4:], "\n"))
	}
}

func TestEdgeToUnloadedParentIsCut(t *testing.T) {
	cs := []*git.Commit{{Hash: "a", Parents: []string{"gone"}}, {Hash: "b", Parents: []string{"c"}}, {Hash: "c"}}
	if got := render(Layout(cs)); got != "●\n↓ ●\n●─╯\n" {
		t.Fatalf("got\n%s", got)
	}
}
