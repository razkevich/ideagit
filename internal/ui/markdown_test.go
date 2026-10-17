package ui

import (
	"strings"
	"testing"

	. "github.com/razkevich/ideagit/internal/canvas"
)

func mdText(ls []mdLine) []string {
	var out []string
	for _, l := range ls {
		var b strings.Builder
		for _, s := range l.segs {
			b.WriteString(s.text)
		}
		out = append(out, b.String())
	}
	return out
}

func TestRenderMarkdown(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}}
	src := "## Summary\n\nFixes **the** bug in `tea.go`, see [issue](https://x/1) and [`CONTRIBUTING.md`](https://x/c).\n\n- one\n- [x] done\n1. first\n\n> quoted\n\n```go\nfunc f() {}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n<!-- hidden -->\n---\n![logo](x.png)"
	got := strings.Join(mdText(renderMarkdown(a, src, 60)), "\n")
	for _, want := range []string{"Summary", "Fixes the bug in tea.go, see issue and CONTRIBUTING.md.", "• one", "1. first", "▎ quoted", " go", " func f() {}", "a │ b", "1 │ 2", "[image: logo]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "hidden") || strings.Contains(got, "**") || strings.Contains(got, "https://x/1") {
		t.Errorf("markup leaked:\n%s", got)
	}
}

func TestMarkdownWraps(t *testing.T) {
	a := &App{Th: DarkTheme(), Cfg: &Config{}}
	for _, l := range mdText(renderMarkdown(a, strings.Repeat("word ", 40)+"\n- "+strings.Repeat("item ", 30), 30)) {
		if Width(l) > 30 {
			t.Fatalf("line wider than 30: %q", l)
		}
	}
}

func TestSplitTrailersKeepsThemOffTheBody(t *testing.T) {
	body, tr := splitTrailers("Explain why.\n\n- one\n- two\n\nSigned-off-by: A <a@x>\nCo-Authored-By: B <b@x>\n")
	if body != "Explain why.\n\n- one\n- two\n" || len(tr) != 2 || tr[1] != "Co-Authored-By: B <b@x>" {
		t.Fatalf("body=%q trailers=%q", body, tr)
	}
	if body, tr := splitTrailers("Note: this is prose\nthat continues"); tr != nil || body == "" {
		t.Fatalf("prose taken as trailers: %q", tr)
	}
}
