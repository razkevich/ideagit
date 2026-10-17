package canvas

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

type Color uint32

const NoColor Color = 0

func RGB(r, g, b uint8) Color { return Color(1<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)) }

func Hex(s string) Color {
	s = strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return NoColor
	}
	return Color(1<<24 | uint32(v))
}

func (c Color) rgb() (uint8, uint8, uint8) { return uint8(c >> 16), uint8(c >> 8), uint8(c) }

func (c Color) hex() string {
	r, g, b := c.rgb()
	const digits = "0123456789abcdef"
	return string([]byte{'#', digits[r>>4], digits[r&15], digits[g>>4], digits[g&15], digits[b>>4], digits[b&15]})
}

type Style struct {
	Fg, Bg                               Color
	Bold, Italic, Underline, Strike, Dim bool
}

func (s Style) WithFg(c Color) Style { s.Fg = c; return s }
func (s Style) WithBg(c Color) Style { s.Bg = c; return s }
func (s Style) WithBold() Style      { s.Bold = true; return s }
func (s Style) WithItalic() Style    { s.Italic = true; return s }
func (s Style) WithUnderline() Style { s.Underline = true; return s }

type Cell struct {
	R    rune
	S    Style
	cont bool
}

type Rect struct{ X, Y, W, H int }

func (r Rect) Contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{x0, y0, 0, 0}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

type Canvas struct {
	W, H  int
	cells []Cell
}

func New(w, h int, base Style) *Canvas {
	c := &Canvas{W: w, H: h, cells: make([]Cell, w*h)}
	for i := range c.cells {
		c.cells[i] = Cell{R: ' ', S: base}
	}
	return c
}

func (c *Canvas) View() View { return View{c: c, r: Rect{0, 0, c.W, c.H}, clip: Rect{0, 0, c.W, c.H}} }

func (c *Canvas) At(x, y int) Cell { return c.cells[y*c.W+x] }

// PlainLines renders without escape codes; used by snapshot mode and tests.
func (c *Canvas) PlainLines() []string {
	out := make([]string, c.H)
	for y := 0; y < c.H; y++ {
		var b strings.Builder
		for x := 0; x < c.W; x++ {
			cell := c.cells[y*c.W+x]
			if !cell.cont {
				b.WriteRune(cell.R)
			}
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

func (c *Canvas) Render(p termenv.Profile) string {
	var b strings.Builder
	b.Grow(c.W * c.H * 4)
	cache := map[Style]string{}
	for y := 0; y < c.H; y++ {
		var cur Style
		first := true
		for x := 0; x < c.W; x++ {
			cell := c.cells[y*c.W+x]
			if cell.cont {
				continue
			}
			if first || cell.S != cur {
				seq, ok := cache[cell.S]
				if !ok {
					seq = sgr(cell.S, p)
					cache[cell.S] = seq
				}
				b.WriteString(seq)
				cur = cell.S
				first = false
			}
			b.WriteRune(cell.R)
		}
		b.WriteString("\x1b[0m")
		if y < c.H-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func sgr(s Style, p termenv.Profile) string {
	parts := []string{"0"}
	if s.Bold {
		parts = append(parts, "1")
	}
	if s.Dim {
		parts = append(parts, "2")
	}
	if s.Italic {
		parts = append(parts, "3")
	}
	if s.Underline {
		parts = append(parts, "4")
	}
	if s.Strike {
		parts = append(parts, "9")
	}
	if s.Fg != NoColor && p != termenv.Ascii {
		parts = append(parts, colorSeq(s.Fg, p, false))
	}
	if s.Bg != NoColor && p != termenv.Ascii {
		parts = append(parts, colorSeq(s.Bg, p, true))
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func colorSeq(c Color, p termenv.Profile, bg bool) string {
	if p == termenv.TrueColor {
		r, g, b := c.rgb()
		lead := "38;2;"
		if bg {
			lead = "48;2;"
		}
		return lead + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b))
	}
	return p.Color(c.hex()).Sequence(bg)
}

// View is a clipped, translated window onto a Canvas.
type View struct {
	c    *Canvas
	r    Rect
	clip Rect
}

func (v View) W() int          { return v.r.W }
func (v View) H() int          { return v.r.H }
func (v View) Abs() Rect       { return v.r }
func (v View) Canvas() *Canvas { return v.c }

func (v View) Sub(x, y, w, h int) View {
	r := Rect{v.r.X + x, v.r.Y + y, w, h}
	return View{c: v.c, r: r, clip: v.clip.Intersect(r)}
}

func (v View) set(x, y int, cell Cell) {
	ax, ay := v.r.X+x, v.r.Y+y
	if !v.clip.Contains(ax, ay) {
		return
	}
	i := ay*v.c.W + ax
	if old := v.c.cells[i]; old.cont && ax > 0 {
		v.c.cells[i-1] = Cell{R: ' ', S: v.c.cells[i-1].S}
	}
	v.c.cells[i] = cell
}

func (v View) Set(x, y int, r rune, s Style) {
	w := runewidth.RuneWidth(r)
	if w == 2 {
		if !v.clip.Contains(v.r.X+x+1, v.r.Y+y) {
			v.set(x, y, Cell{R: ' ', S: s})
			return
		}
		v.set(x, y, Cell{R: r, S: s})
		v.set(x+1, y, Cell{R: ' ', S: s, cont: true})
		return
	}
	if w == 0 {
		r = ' '
	}
	v.set(x, y, Cell{R: r, S: s})
}

// Text draws s at (x,y) clipped to the view width and returns the column after the last rune.
func (v View) Text(x, y int, s string, st Style) int {
	for _, r := range s {
		if x >= v.r.W {
			break
		}
		if r == '\t' {
			r = ' '
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		if x+w > v.r.W {
			break
		}
		v.Set(x, y, r, st)
		x += w
	}
	return x
}

// TextFit draws s truncated with an ellipsis so that it fits within maxW columns.
func (v View) TextFit(x, y int, s string, maxW int, st Style) int {
	return v.Text(x, y, Truncate(s, maxW), st)
}

func (v View) TextRight(right, y int, s string, st Style) int {
	x := right - runewidth.StringWidth(s)
	v.Text(x, y, s, st)
	return x
}

func (v View) Fill(st Style) { v.FillRect(0, 0, v.r.W, v.r.H, st) }

func (v View) FillRect(x, y, w, h int, st Style) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			v.set(xx, yy, Cell{R: ' ', S: st})
		}
	}
}

// Tint changes the background of existing cells without touching their glyphs.
func (v View) Tint(x, y, w int, bg Color) {
	for xx := x; xx < x+w; xx++ {
		ax, ay := v.r.X+xx, v.r.Y+y
		if !v.clip.Contains(ax, ay) {
			continue
		}
		v.c.cells[ay*v.c.W+ax].S.Bg = bg
	}
}

func (v View) HLine(x, y, w int, r rune, st Style) {
	for xx := x; xx < x+w; xx++ {
		v.Set(xx, y, r, st)
	}
}

func (v View) VLine(x, y, h int, r rune, st Style) {
	for yy := y; yy < y+h; yy++ {
		v.Set(x, yy, r, st)
	}
}

func (v View) Box(st Style, title string, titleSt Style) {
	w, h := v.r.W, v.r.H
	if w < 2 || h < 2 {
		return
	}
	v.Set(0, 0, '╭', st)
	v.Set(w-1, 0, '╮', st)
	v.Set(0, h-1, '╰', st)
	v.Set(w-1, h-1, '╯', st)
	v.HLine(1, 0, w-2, '─', st)
	v.HLine(1, h-1, w-2, '─', st)
	v.VLine(0, 1, h-2, '│', st)
	v.VLine(w-1, 1, h-2, '│', st)
	if title != "" {
		v.Text(2, 0, " "+Truncate(title, w-6)+" ", titleSt)
	}
}

func Width(s string) int { return runewidth.StringWidth(s) }

func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

// TruncateLeft keeps the tail of s, prefixing an ellipsis, IntelliJ-style for long ref names.
func TruncateLeft(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	rs := []rune(s)
	acc := 0
	i := len(rs)
	for i > 0 {
		cw := runewidth.RuneWidth(rs[i-1])
		if acc+cw > w-1 {
			break
		}
		acc += cw
		i--
	}
	return "…" + string(rs[i:])
}
