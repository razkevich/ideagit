package diff

// Matches returns index pairs (i, j) with a[i] == b[j] forming a longest common
// subsequence, computed with the linear-space variant of Myers' O(ND) algorithm.
func Matches(a, b []int) [][2]int {
	var out [][2]int
	d := &myers{a: a, b: b, out: &out}
	n, m := len(a), len(b)
	size := 2*(n+m+1) + 4
	d.vf = make([]int, size)
	d.vb = make([]int, size)
	d.compare(0, n, 0, m)
	return out
}

type myers struct {
	a, b   []int
	vf, vb []int
	out    *[][2]int
}

func (d *myers) emit(i, j int) { *d.out = append(*d.out, [2]int{i, j}) }

func (d *myers) compare(aLo, aHi, bLo, bHi int) {
	for aLo < aHi && bLo < bHi && d.a[aLo] == d.b[bLo] {
		d.emit(aLo, bLo)
		aLo++
		bLo++
	}
	sufA, sufB := aHi, bHi
	for sufA > aLo && sufB > bLo && d.a[sufA-1] == d.b[sufB-1] {
		sufA--
		sufB--
	}
	if aLo < sufA && bLo < sufB {
		x0, y0, x1, y1 := d.middleSnake(aLo, sufA, bLo, sufB)
		// A split that doesn't shrink the problem would recurse forever; treat the range as wholly different.
		if !(x0 == sufA && y0 == sufB) && !(x1 == aLo && y1 == bLo) {
			d.compare(aLo, x0, bLo, y0)
			for i := 0; i < x1-x0; i++ {
				d.emit(x0+i, y0+i)
			}
			d.compare(x1, sufA, y1, sufB)
		}
	}
	for i := 0; i < aHi-sufA; i++ {
		d.emit(sufA+i, sufB+i)
	}
}

func (d *myers) middleSnake(aLo, aHi, bLo, bHi int) (int, int, int, int) {
	n, m := aHi-aLo, bHi-bLo
	delta := n - m
	odd := delta&1 != 0
	maxD := (n + m + 1) / 2
	off := maxD + 1
	vf := d.vf[:2*off+2]
	vb := d.vb[:2*off+2]
	for i := range vf {
		vf[i] = 0
		vb[i] = 0
	}
	for dd := 0; dd <= maxD; dd++ {
		for k := -dd; k <= dd; k += 2 {
			var x int
			if k == -dd || (k != dd && vf[off+k-1] < vf[off+k+1]) {
				x = vf[off+k+1]
			} else {
				x = vf[off+k-1] + 1
			}
			y := x - k
			x0, y0 := x, y
			for x < n && y < m && d.a[aLo+x] == d.b[bLo+y] {
				x++
				y++
			}
			vf[off+k] = x
			kb := delta - k
			if odd && kb >= -(dd-1) && kb <= dd-1 && x+vb[off+kb] >= n {
				return aLo + x0, bLo + y0, aLo + x, bLo + y
			}
		}
		for k := -dd; k <= dd; k += 2 {
			var x int
			if k == -dd || (k != dd && vb[off+k-1] < vb[off+k+1]) {
				x = vb[off+k+1]
			} else {
				x = vb[off+k-1] + 1
			}
			y := x - k
			x0, y0 := x, y
			for x < n && y < m && d.a[aHi-1-x] == d.b[bHi-1-y] {
				x++
				y++
			}
			vb[off+k] = x
			kf := delta - k
			if !odd && kf >= -dd && kf <= dd && x+vf[off+kf] >= n {
				return aLo + n - x, bLo + m - y, aLo + n - x0, bLo + m - y0
			}
		}
	}
	return aLo, bLo, aLo, bLo
}
