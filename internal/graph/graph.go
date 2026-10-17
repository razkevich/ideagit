package graph

import "github.com/razkevich/ideagit/internal/git"

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
}

// Layout assigns each commit a lane and draws one row of box-drawing glyphs per commit.
// Merges and forks are drawn horizontally on the commit's own row.
func Layout(commits []*git.Commit) []Row {
	var lanes []lane
	nextColor := 0
	newColor := func() int { c := nextColor; nextColor++; return c }
	rows := make([]Row, len(commits))

	for ci, c := range commits {
		before := append([]lane(nil), lanes...)
		var expecting []int
		for i, l := range lanes {
			if l.hash == c.Hash {
				expecting = append(expecting, i)
			}
		}
		col := -1
		if len(expecting) > 0 {
			col = expecting[0]
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

		newParents := map[int]bool{}
		joinParents := map[int]bool{}
		if len(c.Parents) == 0 {
			lanes[col] = lane{}
		} else {
			lanes[col] = lane{hash: c.Parents[0], color: color}
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
				lanes[slot] = lane{hash: p, color: newColor()}
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
				cells[x] = Cell{r, before[i].color}
			case newParents[i]:
				r := '╮'
				if i < col {
					r = '╭'
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
