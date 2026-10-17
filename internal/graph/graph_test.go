package graph

import (
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
