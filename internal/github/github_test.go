package github

import "testing"

func TestParseRef(t *testing.T) {
	for in, want := range map[string]int{"123": 123, "#45": 45, "https://github.com/o/r/pull/678": 678, "https://github.com/o/r/pull/9/files": 9} {
		if n, ok := ParseRef(in); !ok || n != want {
			t.Errorf("%q → %d %v", in, n, ok)
		}
	}
	if _, ok := ParseRef("main"); ok {
		t.Error("branch name parsed as PR")
	}
}

func TestActionsRunFromCheckURL(t *testing.T) {
	m := actionsRun.FindStringSubmatch("https://github.com/o/r/actions/runs/37817532223/job/113449872372")
	if m == nil || m[1] != "37817532223" {
		t.Fatalf("got %v", m)
	}
}

func TestCheckSummaryUsesLatestRun(t *testing.T) {
	p := PR{Checks: []Check{{Name: "test", State: "FAILURE"}, {Name: "test", State: "SUCCESS"}, {Name: "lint", State: "PENDING"}}}
	pass, fail, pending := p.CheckSummary()
	if pass != 1 || fail != 0 || pending != 1 {
		t.Fatalf("pass=%d fail=%d pending=%d", pass, fail, pending)
	}
}
