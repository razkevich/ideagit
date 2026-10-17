package diff

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

type BlockType int

const (
	BlockModified BlockType = iota
	BlockInserted
	BlockDeleted
)

// Block is a changed region: old lines [O0,O1) were replaced by new lines [N0,N1).
type Block struct {
	O0, O1, N0, N1 int
	Type           BlockType
}

type IgnoreMode int

const (
	IgnoreNone IgnoreMode = iota
	IgnoreTrailing
	IgnoreAll
)

type Result struct {
	Old, New   []string
	OldNoEOL   bool
	NewNoEOL   bool
	Blocks     []Block
	Binary     bool
	TooLarge   bool
	Identical  bool
	OldMissing bool
	NewMissing bool
}

const maxDiffBytes = 8 << 20

func SplitLines(b []byte) ([]string, bool) {
	if len(b) == 0 {
		return nil, false
	}
	s := string(b)
	noEOL := !strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n"), noEOL
}

func JoinLines(lines []string, noEOL bool) []byte {
	if len(lines) == 0 {
		return []byte{}
	}
	s := strings.Join(lines, "\n")
	if !noEOL {
		s += "\n"
	}
	return []byte(s)
}

func IsBinary(b []byte) bool {
	n := min(len(b), 8000)
	return bytes.IndexByte(b[:n], 0) >= 0 || !utf8.Valid(b[:n-trailingPartialRune(b[:n])])
}

func trailingPartialRune(b []byte) int {
	for i := 1; i <= 3 && i <= len(b); i++ {
		if utf8.RuneStart(b[len(b)-i]) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return i
			}
			return 0
		}
	}
	return 0
}

func Compute(old, new []byte, ignore IgnoreMode) *Result {
	r := &Result{}
	if len(old)+len(new) > maxDiffBytes {
		r.TooLarge = true
		return r
	}
	if IsBinary(old) || IsBinary(new) {
		r.Binary = true
		r.Identical = bytes.Equal(old, new)
		return r
	}
	r.Old, r.OldNoEOL = SplitLines(old)
	r.New, r.NewNoEOL = SplitLines(new)
	r.Blocks = LineBlocks(r.Old, r.New, ignore)
	r.Identical = len(r.Blocks) == 0
	return r
}

func normalize(s string, ignore IgnoreMode) string {
	s = strings.TrimSuffix(s, "\r")
	switch ignore {
	case IgnoreTrailing:
		return strings.TrimRightFunc(s, unicode.IsSpace)
	case IgnoreAll:
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	return s
}

func LineBlocks(old, new []string, ignore IgnoreMode) []Block {
	ids := map[string]int{}
	intern := func(lines []string) []int {
		res := make([]int, len(lines))
		for i, l := range lines {
			k := normalize(l, ignore)
			id, ok := ids[k]
			if !ok {
				id = len(ids)
				ids[k] = id
			}
			res[i] = id
		}
		return res
	}
	a, b := intern(old), intern(new)
	m := Matches(a, b)
	var blocks []Block
	pi, pj := 0, 0
	flush := func(i, j int) {
		if i > pi || j > pj {
			t := BlockModified
			switch {
			case i == pi:
				t = BlockInserted
			case j == pj:
				t = BlockDeleted
			}
			blocks = append(blocks, Block{O0: pi, O1: i, N0: pj, N1: j, Type: t})
		}
	}
	for _, p := range m {
		flush(p[0], p[1])
		pi, pj = p[0]+1, p[1]+1
	}
	flush(len(old), len(new))
	return blocks
}

// Span is a highlighted rune range [Start,End) on a single line.
type Span struct{ Start, End int }

// WordDiff returns per-line inline highlights for a modified block, keyed by line index
// in Old and New respectively. Highlighting is computed over the whole block so that
// lines that moved within the block still align, as IntelliJ does.
func (r *Result) WordDiff(bi int) (map[int][]Span, map[int][]Span) {
	b := r.Blocks[bi]
	if b.Type != BlockModified {
		return nil, nil
	}
	ot := tokenize(r.Old[b.O0:b.O1], b.O0)
	nt := tokenize(r.New[b.N0:b.N1], b.N0)
	if len(ot)*len(nt) > 4_000_000 || len(ot) > 20000 || len(nt) > 20000 {
		return nil, nil
	}
	ids := map[string]int{}
	conv := func(ts []token) []int {
		res := make([]int, len(ts))
		for i, t := range ts {
			id, ok := ids[t.text]
			if !ok {
				id = len(ids)
				ids[t.text] = id
			}
			res[i] = id
		}
		return res
	}
	m := Matches(conv(ot), conv(nt))
	oldMatched := make([]bool, len(ot))
	newMatched := make([]bool, len(nt))
	for _, p := range m {
		oldMatched[p[0]] = true
		newMatched[p[1]] = true
	}
	return spans(ot, oldMatched), spans(nt, newMatched)
}

type token struct {
	text       string
	line       int
	start, end int
	space      bool
}

func tokenize(lines []string, base int) []token {
	var ts []token
	for li, l := range lines {
		rs := []rune(strings.TrimSuffix(l, "\r"))
		i := 0
		for i < len(rs) {
			j := i + 1
			cls := runeClass(rs[i])
			if cls != 2 {
				for j < len(rs) && runeClass(rs[j]) == cls {
					j++
				}
			}
			ts = append(ts, token{text: string(rs[i:j]), line: base + li, start: i, end: j, space: cls == 0})
			i = j
		}
		ts = append(ts, token{text: "\n", line: base + li, start: len(rs), end: len(rs), space: true})
	}
	return ts
}

func runeClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	}
	return 2
}

func spans(ts []token, matched []bool) map[int][]Span {
	res := map[int][]Span{}
	for i, t := range ts {
		if matched[i] || t.end == t.start {
			continue
		}
		if t.space {
			prevChanged := i > 0 && !matched[i-1] && ts[i-1].line == t.line
			nextChanged := i+1 < len(ts) && !matched[i+1] && ts[i+1].line == t.line && !ts[i+1].space
			if !(prevChanged && nextChanged) {
				continue
			}
		}
		ss := res[t.line]
		if n := len(ss); n > 0 && ss[n-1].End == t.start {
			ss[n-1].End = t.end
		} else {
			ss = append(ss, Span{t.start, t.end})
		}
		res[t.line] = ss
	}
	return res
}

// Merge builds content from Old with the selected blocks replaced by their New lines.
func (r *Result) Merge(include func(bi int) bool) []byte {
	var out []string
	pos := 0
	for bi, b := range r.Blocks {
		out = append(out, r.Old[pos:b.O0]...)
		if include(bi) {
			out = append(out, r.New[b.N0:b.N1]...)
		} else {
			out = append(out, r.Old[b.O0:b.O1]...)
		}
		pos = b.O1
	}
	out = append(out, r.Old[pos:]...)
	noEOL := r.OldNoEOL
	if len(r.Blocks) > 0 {
		last := r.Blocks[len(r.Blocks)-1]
		if last.O1 == len(r.Old) && include(len(r.Blocks)-1) {
			noEOL = r.NewNoEOL
		}
	}
	return JoinLines(out, noEOL)
}

// ApplyToNew returns New with block bi reverted to its Old lines.
func (r *Result) RevertInNew(bi int) []byte {
	b := r.Blocks[bi]
	out := append([]string{}, r.New[:b.N0]...)
	out = append(out, r.Old[b.O0:b.O1]...)
	out = append(out, r.New[b.N1:]...)
	noEOL := r.NewNoEOL
	if b.N1 == len(r.New) {
		noEOL = r.OldNoEOL
	}
	return JoinLines(out, noEOL)
}

// ApplyToOld returns Old with block bi taken from New.
func (r *Result) ApplyToOld(bi int) []byte {
	return r.Merge(func(i int) bool { return i == bi })
}
