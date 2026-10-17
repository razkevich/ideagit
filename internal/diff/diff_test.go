package diff

import (
	"math/rand"
	"strings"
	"testing"
)

func lcsLen(a, b []int) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp[0][0]
}

func TestMatchesOptimal(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 3000; iter++ {
		a := make([]int, rng.Intn(25))
		b := make([]int, rng.Intn(25))
		for i := range a {
			a[i] = rng.Intn(4)
		}
		for i := range b {
			b[i] = rng.Intn(4)
		}
		m := Matches(a, b)
		pi, pj := -1, -1
		for _, p := range m {
			if p[0] <= pi || p[1] <= pj || a[p[0]] != b[p[1]] {
				t.Fatalf("invalid match %v for %v %v: %v", p, a, b, m)
			}
			pi, pj = p[0], p[1]
		}
		if len(m) != lcsLen(a, b) {
			t.Fatalf("not optimal: got %d want %d for %v %v", len(m), lcsLen(a, b), a, b)
		}
	}
}

func TestMergeAndPatch(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	new := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk"
	r := Compute([]byte(old), []byte(new), IgnoreNone)
	if len(r.Blocks) != 2 {
		t.Fatalf("blocks %+v", r.Blocks)
	}
	if got := string(r.Merge(func(i int) bool { return true })); got != new {
		t.Fatalf("merge all %q", got)
	}
	if got := string(r.Merge(func(i int) bool { return i == 0 })); got != "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\n" {
		t.Fatalf("merge first %q", got)
	}
	hunks := r.UnifiedHunks(3)
	lines, noEOL, ok := ApplyHunks(r.Old, r.OldNoEOL, hunks)
	if !ok || string(JoinLines(lines, noEOL)) != new {
		t.Fatalf("apply %v %q\n%s", ok, JoinLines(lines, noEOL), FormatHunks(hunks))
	}
	_ = strings.TrimSpace
}

func TestWordDiff(t *testing.T) {
	r := Compute([]byte("if x.Code == 'M' {\n"), []byte("if x.Code == 'M' || x.Code == 'T' {\n"), IgnoreNone)
	o, n := r.WordDiff(0)
	if len(o) != 0 || len(n[0]) != 1 {
		t.Fatalf("spans %v %v", o, n)
	}
}
