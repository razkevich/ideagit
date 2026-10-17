package diff

import (
	"fmt"
	"strconv"
	"strings"
)

type Hunk struct {
	OldStart, OldLen int
	NewStart, NewLen int
	Lines            []string // prefixed with ' ', '-', '+', or '\'
}

// UnifiedHunks renders blocks as unified-diff hunks with the given context.
func (r *Result) UnifiedHunks(context int) []Hunk {
	var hunks []Hunk
	i := 0
	for i < len(r.Blocks) {
		j := i
		for j+1 < len(r.Blocks) && r.Blocks[j+1].O0-r.Blocks[j].O1 <= 2*context {
			j++
		}
		first, last := r.Blocks[i], r.Blocks[j]
		o0 := max(0, first.O0-context)
		n0 := first.N0 - (first.O0 - o0)
		o1 := min(len(r.Old), last.O1+context)
		n1 := last.N1 + (o1 - last.O1)
		h := Hunk{OldStart: o0 + 1, OldLen: o1 - o0, NewStart: n0 + 1, NewLen: n1 - n0}
		if h.OldLen == 0 {
			h.OldStart = o0
		}
		if h.NewLen == 0 {
			h.NewStart = n0
		}
		pos := o0
		for k := i; k <= j; k++ {
			b := r.Blocks[k]
			for ; pos < b.O0; pos++ {
				h.Lines = append(h.Lines, " "+r.Old[pos])
				r.noEOLMarker(&h, pos, -1)
			}
			for x := b.O0; x < b.O1; x++ {
				h.Lines = append(h.Lines, "-"+r.Old[x])
				r.noEOLMarker(&h, x, -1)
			}
			for x := b.N0; x < b.N1; x++ {
				h.Lines = append(h.Lines, "+"+r.New[x])
				r.noEOLMarker(&h, -1, x)
			}
			pos = b.O1
		}
		for ; pos < o1; pos++ {
			h.Lines = append(h.Lines, " "+r.Old[pos])
			r.noEOLMarker(&h, pos, -1)
		}
		hunks = append(hunks, h)
		i = j + 1
	}
	return hunks
}

func (r *Result) noEOLMarker(h *Hunk, oldIdx, newIdx int) {
	if (oldIdx >= 0 && oldIdx == len(r.Old)-1 && r.OldNoEOL) || (newIdx >= 0 && newIdx == len(r.New)-1 && r.NewNoEOL) {
		h.Lines = append(h.Lines, `\ No newline at end of file`)
	}
}

func FormatHunks(hunks []Hunk) string {
	var b strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLen, h.NewStart, h.NewLen)
		for _, l := range h.Lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func ParseHunkHeader(line string) (Hunk, bool) {
	var h Hunk
	if !strings.HasPrefix(line, "@@ -") {
		return h, false
	}
	rest := strings.TrimPrefix(line, "@@ -")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return h, false
	}
	parts := strings.Fields(rest[:end])
	if len(parts) != 2 {
		return h, false
	}
	parse := func(s string) (int, int) {
		s = strings.TrimLeft(s, "-+")
		a, b, found := strings.Cut(s, ",")
		x, _ := strconv.Atoi(a)
		y := 1
		if found {
			y, _ = strconv.Atoi(b)
		}
		return x, y
	}
	h.OldStart, h.OldLen = parse(parts[0])
	h.NewStart, h.NewLen = parse(parts[1])
	return h, true
}

// ApplyHunks applies hunks to base, searching near the expected position for each hunk's
// context like `patch` does. It reports false if any hunk can't be placed.
func ApplyHunks(base []string, baseNoEOL bool, hunks []Hunk) ([]string, bool, bool) {
	var out []string
	pos := 0
	offset := 0
	noEOL := baseNoEOL
	for _, h := range hunks {
		var oldLines, newLines []string
		newNoEOL := false
		prev := byte(0)
		for _, l := range h.Lines {
			if l == "" {
				l = " "
			}
			switch l[0] {
			case ' ':
				oldLines = append(oldLines, l[1:])
				newLines = append(newLines, l[1:])
			case '-':
				oldLines = append(oldLines, l[1:])
			case '+':
				newLines = append(newLines, l[1:])
			case '\\':
				if prev == '+' || prev == ' ' {
					newNoEOL = true
				}
			}
			prev = l[0]
		}
		want := h.OldStart - 1 + offset
		if h.OldLen == 0 {
			want = h.OldStart + offset
		}
		at := findLines(base, oldLines, want, pos)
		if at < 0 {
			return nil, false, false
		}
		out = append(out, base[pos:at]...)
		out = append(out, newLines...)
		pos = at + len(oldLines)
		offset = at - (h.OldStart - 1)
		if pos == len(base) {
			noEOL = newNoEOL
		}
	}
	out = append(out, base[pos:]...)
	return out, noEOL, true
}

func findLines(base, needle []string, want, minPos int) int {
	match := func(at int) bool {
		if at < minPos || at+len(needle) > len(base) {
			return false
		}
		for i, l := range needle {
			if strings.TrimSuffix(base[at+i], "\r") != strings.TrimSuffix(l, "\r") {
				return false
			}
		}
		return true
	}
	if len(needle) == 0 {
		return max(minPos, min(want, len(base)))
	}
	for d := 0; d <= len(base); d++ {
		if match(want - d) {
			return want - d
		}
		if d > 0 && match(want+d) {
			return want + d
		}
	}
	return -1
}
