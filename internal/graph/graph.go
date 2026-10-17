package graph

import (
	"slices"

	"github.com/razkevich/ideagit/internal/git"
)

type Cell struct {
	R     rune
	Color int
}

type Row struct {
	Cells    []Cell
	NodeLane int
	Color    int
}

type lane struct {
	hash  string
	color int
	// A long edge stops being drawn (with ↓) at row cut.
	long bool
	cut  int
}

// LongEdge is the length (in rows) from which an edge is drawn as two short stubs
// with arrows instead of a line, like IntelliJ, so it doesn't hold a lane for
// hundreds of rows. Edges to commits that aren't loaded count as long.
const LongEdge = 30

// Layout assigns each commit a lane and draws one row of box-drawing glyphs per commit.
// Merges and forks are drawn horizontally on the commit's own row.
func Layout(commits []*git.Commit) []Row {
	var lanes []lane
	nextColor := 0
	newColor := func() int { c := nextColor; nextColor++; return c }
	rows := make([]Row, len(commits))
	index := make(map[string]int, len(commits))
	for i, c := range commits {
		index[c.Hash] = i
	}
	// arrivals are the ↑ stubs of long edges, opened the row before their parent.
	arrivals := map[int][]lane{}
	scheduled := map[string]bool{}
	edge := func(ci int, p string, color int) lane {
		l := lane{hash: p, color: color}
		pi, ok := index[p]
		if !ok || pi-ci >= LongEdge {
			l.long, l.cut = true, ci+1
			if ok && !scheduled[p] {
				scheduled[p] = true
				arrivals[pi-1] = append(arrivals[pi-1], lane{hash: p, color: color})
			}
		}
		return l
	}

	for ci, c := range commits {
		up := map[int]bool{}
		for _, ar := range arrivals[ci] {
			if slices.ContainsFunc(lanes, func(l lane) bool { return l.hash == ar.hash }) {
				continue
			}
			slot := firstFree(lanes, nil)
			if slot == len(lanes) {
				lanes = append(lanes, lane{})
			}
			lanes[slot] = ar
			up[slot] = true
		}
		before := append([]lane(nil), lanes...)
		var expecting []int
		for i, l := range lanes {
			if l.hash == c.Hash {
				expecting = append(expecting, i)
			}
		}
		col := -1
		slid := -1
		if len(expecting) > 0 {
			col = expecting[0]
			// Pack left: the line from above turns into the node, as IntelliJ's diagonal.
			j := col
			for j > 0 && lanes[j-1].hash == "" {
				j--
			}
			if j < col {
				slid = col
				lanes[j] = lanes[col]
				col = j
			}
		} else {
			col = firstFree(lanes, nil)
			if col == len(lanes) {
				lanes = append(lanes, lane{})
			}
			lanes[col] = lane{hash: c.Hash, color: newColor()}
		}
		color := lanes[col].color

		merging := map[int]bool{}
		for _, i := range expecting[min(1, len(expecting)):] {
			merging[i] = true
			lanes[i] = lane{}
		}
		if slid >= 0 {
			merging[slid] = true
			lanes[slid] = lane{}
		}

		newParents := map[int]bool{}
		joinParents := map[int]bool{}
		if len(c.Parents) == 0 {
			lanes[col] = lane{}
		} else {
			lanes[col] = edge(ci, c.Parents[0], color)
			for _, p := range c.Parents[1:] {
				existing := -1
				for i, l := range lanes {
					if i != col && l.hash == p {
						existing = i
						break
					}
				}
				if existing >= 0 {
					joinParents[existing] = true
					continue
				}
				slot := firstFree(lanes, merging)
				if slot == len(lanes) {
					lanes = append(lanes, lane{})
				}
				lanes[slot] = edge(ci, p, newColor())
				newParents[slot] = true
			}
		}

		width := max(len(before), len(lanes))
		cells := make([]Cell, width*2)
		for i := range cells {
			cells[i] = Cell{R: ' ', Color: -1}
		}
		active := func(ls []lane, i int) bool { return i < len(ls) && ls[i].hash != "" }

		lo, hi := col, col
		for i := range merging {
			lo, hi = min(lo, i), max(hi, i)
		}
		for i := range newParents {
			lo, hi = min(lo, i), max(hi, i)
		}
		for i := range joinParents {
			lo, hi = min(lo, i), max(hi, i)
		}
		edgeColor := func(i int) int {
			if i < len(lanes) && lanes[i].hash != "" && (newParents[i] || joinParents[i]) {
				return lanes[i].color
			}
			if i < len(before) && before[i].hash != "" {
				return before[i].color
			}
			return color
		}

		for i := 0; i < width; i++ {
			x := 2 * i
			switch {
			case i == col:
				cells[x] = Cell{'●', color}
			case merging[i]:
				r := '╯'
				if i < col {
					r = '╰'
				}
				if i > lo && i < hi {
					// The horizontal line continues past this lane.
					r = '┴'
				}
				cells[x] = Cell{r, before[i].color}
			case newParents[i]:
				r := '╮'
				if i < col {
					r = '╭'
				}
				if i > lo && i < hi {
					r = '┬'
				}
				cells[x] = Cell{r, lanes[i].color}
			case joinParents[i]:
				r := '┤'
				if i < col {
					r = '├'
				}
				cells[x] = Cell{r, lanes[i].color}
			case active(before, i) && active(lanes, i):
				r := '│'
				if i > lo && i < hi {
					r = '┼'
				}
				cells[x] = Cell{r, before[i].color}
			case i > lo && i < hi:
				cells[x] = Cell{'─', edgeColor(nearestEnd(i, col, lo, hi))}
			}
		}
		for i := lo; i < hi; i++ {
			end := hi
			if i < col {
				end = lo
			}
			cells[2*i+1] = Cell{'─', edgeColor(end)}
		}
		compact(lanes, cells, lo, hi)
		for i := range lanes {
			x := 2 * i
			if x >= len(cells) || cells[x].R != '│' {
				if lanes[i].long && lanes[i].cut == ci {
					// A connector landed on the stub; end it a row later.
					lanes[i].cut++
				}
				continue
			}
			switch {
			case lanes[i].long && lanes[i].cut == ci && lanes[i].hash != "":
				cells[x].R = '↓'
				lanes[i] = lane{}
			case up[i]:
				cells[x].R = '↑'
			}
		}
		for len(lanes) > 0 && lanes[len(lanes)-1].hash == "" {
			lanes = lanes[:len(lanes)-1]
		}
		n := len(cells)
		for n > 0 && cells[n-1].R == ' ' {
			n--
		}
		rows[ci] = Row{Cells: cells[:n], NodeLane: col, Color: color}
	}
	return rows
}

// compact moves passing lines left into free lanes where this row has room for a
// "╭─╯" jog.
func compact(lanes []lane, cells []Cell, lo, hi int) {
	for k := 1; k < len(lanes); k++ {
		if lanes[k].hash == "" || cells[2*k].R != '│' || (lanes[k].long && lanes[k].cut <= 0) {
			continue
		}
		j := k
		for j > 0 && lanes[j-1].hash == "" && cells[2*(j-1)].R == ' ' {
			j--
		}
		if j == k || (j <= hi && k >= lo) {
			continue
		}
		for x := 2*j + 1; x < 2*k; x++ {
			if cells[x].R != ' ' {
				j = -1
				break
			}
		}
		if j < 0 {
			continue
		}
		color := cells[2*k].Color
		cells[2*j] = Cell{'╭', color}
		for x := 2*j + 1; x < 2*k; x++ {
			cells[x] = Cell{'─', color}
		}
		cells[2*k] = Cell{'╯', color}
		lanes[j], lanes[k] = lanes[k], lane{}
	}
}

func nearestEnd(i, col, lo, hi int) int {
	if i < col {
		return lo
	}
	return hi
}

func firstFree(lanes []lane, reserved map[int]bool) int {
	for i, l := range lanes {
		if l.hash == "" && !reserved[i] {
			return i
		}
	}
	return len(lanes)
}
