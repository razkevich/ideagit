package ui

import (
	"regexp"
	"strings"

	. "github.com/razkevich/ideagit/internal/canvas"
)

// mdSeg is a run of text with one style; an mdLine is one rendered row.
type mdSeg struct {
	text string
	st   Style
}

type mdLine struct {
	segs []mdSeg
	bg   Color
}

var (
	mdHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	mdHTMLTag     = regexp.MustCompile(`</?(details|summary|p|div|span|sub|sup|kbd|b|i|em|strong|picture|source|img)[^>]*>`)
	mdBR          = regexp.MustCompile(`<br\s*/?>`)
	mdOrdered     = regexp.MustCompile(`^(\d+)[.)]\s+(.*)$`)
	mdTableSep    = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
)

// renderMarkdown renders GitHub-flavored markdown (PR descriptions) as styled, wrapped
// lines: headings, lists and task lists, emphasis, code, links, quotes, rules, tables.
func renderMarkdown(a *App, src string, width int) []mdLine {
	th := a.Th
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = mdHTMLComment.ReplaceAllString(src, "")
	src = mdBR.ReplaceAllString(src, "\n")
	src = mdHTMLTag.ReplaceAllString(src, "")
	base := Style{Fg: th.Fg, Bg: th.Bg}
	var out []mdLine
	blank := func() {
		if len(out) > 0 && len(out[len(out)-1].segs) > 0 {
			out = append(out, mdLine{})
		}
	}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~"):
			fence := trim[:3]
			lang := strings.TrimSpace(trim[3:])
			blank()
			codeBg := th.ToolbarBg
			if lang != "" {
				out = append(out, mdLine{segs: []mdSeg{{" " + lang, Style{Fg: th.Faint, Bg: codeBg}}}, bg: codeBg})
			}
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence); i++ {
				code := strings.ReplaceAll(lines[i], "\t", "    ")
				out = append(out, mdLine{segs: []mdSeg{{" " + Truncate(code, width-2), Style{Fg: th.SynType, Bg: codeBg}}}, bg: codeBg})
			}
			blank()
		case trim == "":
			blank()
		case strings.HasPrefix(trim, "#"):
			level := len(trim) - len(strings.TrimLeft(trim, "#"))
			text := strings.TrimSpace(strings.TrimLeft(trim, "#"))
			blank()
			st := base.WithBold()
			if level >= 3 {
				st.Fg = th.Dim
			}
			out = append(out, wrapSegs(inlineMD(a, text, st), width, "", "")...)
			if level <= 2 {
				out = append(out, mdLine{segs: []mdSeg{{strings.Repeat("─", min(width, Width(text)+2)), Style{Fg: th.Border, Bg: th.Bg}}}})
			}
		case isRule(trim):
			out = append(out, mdLine{segs: []mdSeg{{strings.Repeat("─", width), Style{Fg: th.Border, Bg: th.Bg}}}})
		case strings.HasPrefix(trim, ">"):
			text := strings.TrimSpace(strings.TrimLeft(trim, "> "))
			out = append(out, wrapSegs(inlineMD(a, text, Style{Fg: th.Dim, Bg: th.Bg, Italic: true}), width, "▎ ", "▎ ")...)
		case strings.HasPrefix(trim, "|") && i+1 < len(lines) && mdTableSep.MatchString(lines[i+1]):
			var rows [][]string
			rows = append(rows, tableCells(trim))
			i++
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
				rows = append(rows, tableCells(strings.TrimSpace(lines[i])))
			}
			out = append(out, renderTable(a, rows, width)...)
		default:
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			pad := strings.Repeat(" ", min(indent, 8))
			if marker, rest, ok := listItem(trim); ok {
				first := pad + marker
				cont := pad + strings.Repeat(" ", Width(marker))
				if strings.HasPrefix(rest, "[ ] ") || strings.HasPrefix(rest, "[x] ") || strings.HasPrefix(rest, "[X] ") {
					state := Unchecked
					if rest[1] != ' ' {
						state = Checked
					}
					first = pad + a.checkGlyph(state) + " "
					cont = pad + strings.Repeat(" ", Width(a.checkGlyph(state))+1)
					rest = rest[4:]
				}
				out = append(out, wrapSegs(inlineMD(a, rest, base), width, first, cont)...)
				continue
			}
			// Paragraph: join soft-wrapped source lines until a blank or block line.
			text := trim
			for i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				if next == "" || strings.HasPrefix(next, "#") || strings.HasPrefix(next, ">") || strings.HasPrefix(next, "```") || strings.HasPrefix(next, "|") || isRule(next) {
					break
				}
				if _, _, ok := listItem(next); ok {
					break
				}
				text += " " + next
				i++
			}
			out = append(out, wrapSegs(inlineMD(a, text, base), width, pad, pad)...)
		}
	}
	for len(out) > 0 && len(out[len(out)-1].segs) == 0 {
		out = out[:len(out)-1]
	}
	return out
}

func isRule(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 3 {
		return false
	}
	for _, c := range []string{"-", "*", "_"} {
		if strings.Trim(s, c) == "" {
			return true
		}
	}
	return false
}

func listItem(s string) (marker, rest string, ok bool) {
	for _, b := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(s, b) {
			return "• ", s[2:], true
		}
	}
	if m := mdOrdered.FindStringSubmatch(s); m != nil {
		return m[1] + ". ", m[2], true
	}
	return "", "", false
}

func tableCells(row string) []string {
	row = strings.Trim(strings.TrimSpace(row), "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func renderTable(a *App, rows [][]string, width int) []mdLine {
	th := a.Th
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			widths[c] = max(widths[c], Width(stripInline(cell)))
		}
	}
	total := 0
	for _, w := range widths {
		total += w + 3
	}
	// Squeeze the widest columns until the table fits.
	for total > width && total > cols*6 {
		wi := 0
		for c := range widths {
			if widths[c] > widths[wi] {
				wi = c
			}
		}
		widths[wi]--
		total--
	}
	var out []mdLine
	for ri, r := range rows {
		var segs []mdSeg
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(r) {
				cell = stripInline(r[c])
			}
			st := Style{Fg: th.Fg, Bg: th.Bg, Bold: ri == 0}
			text := Truncate(cell, widths[c])
			segs = append(segs, mdSeg{text + strings.Repeat(" ", widths[c]-Width(text)), st})
			if c < cols-1 {
				segs = append(segs, mdSeg{" │ ", Style{Fg: th.Border, Bg: th.Bg}})
			}
		}
		out = append(out, mdLine{segs: segs})
		if ri == 0 {
			var sep strings.Builder
			for c, w := range widths {
				sep.WriteString(strings.Repeat("─", w))
				if c < cols-1 {
					sep.WriteString("─┼─")
				}
			}
			out = append(out, mdLine{segs: []mdSeg{{sep.String(), Style{Fg: th.Border, Bg: th.Bg}}}})
		}
	}
	return out
}

var mdLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)\s]*)[^)]*\)`)

func stripInline(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1")
	return strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
}

// inlineMD splits text into styled runs for **bold**, *italic*, `code` and links.
func inlineMD(a *App, text string, base Style) []mdSeg {
	th := a.Th
	var segs []mdSeg
	emit := func(s string, st Style) {
		if s != "" {
			segs = append(segs, mdSeg{s, st})
		}
	}
	rs := []rune(text)
	st := base
	var cur strings.Builder
	flush := func() { emit(cur.String(), st); cur.Reset() }
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		rest := string(rs[i:])
		switch {
		case r == '`':
			end := -1
			for j, c := range rs[i+1:] {
				if c == '`' {
					end = j
					break
				}
			}
			if end < 0 {
				cur.WriteRune(r)
				continue
			}
			flush()
			emit(string(rs[i+1:i+1+end]), Style{Fg: th.SynString, Bg: th.ToolbarBg})
			i += end + 1
		case r == '!' && strings.HasPrefix(rest, "!["), r == '[':
			m := mdLinkRe.FindStringSubmatchIndex(rest)
			if m == nil || m[0] != 0 {
				cur.WriteRune(r)
				continue
			}
			flush()
			label := stripInline(rest[m[2]:m[3]])
			if r == '!' {
				emit("[image: "+label+"]", Style{Fg: th.Dim, Bg: base.Bg, Italic: true})
			} else {
				if label == "" {
					label = rest[m[4]:m[5]]
				}
				ls := base
				ls.Fg = th.Link
				emit(label, ls)
			}
			i += len([]rune(rest[:m[1]])) - 1
		case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
			flush()
			st.Bold = !st.Bold
			i++
		case (r == '*' || r == '_') && (i+1 < len(rs) && rs[i+1] != ' ') && (i == 0 || !isWordRune(rs[i-1])):
			if strings.ContainsRune(string(rs[i+1:]), r) {
				flush()
				st.Italic = !st.Italic
				continue
			}
			cur.WriteRune(r)
		case (r == '*' || r == '_') && st.Italic && (i+1 >= len(rs) || !isWordRune(rs[i+1])):
			flush()
			st.Italic = false
		case strings.HasPrefix(rest, "https://") || strings.HasPrefix(rest, "http://"):
			flush()
			end := strings.IndexAny(rest, " )\t")
			if end < 0 {
				end = len(rest)
			}
			ls := base
			ls.Fg = th.Link
			emit(rest[:end], ls)
			i += len([]rune(rest[:end])) - 1
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return segs
}

// wrapSegs wraps styled runs to width, prefixing the first and continuation lines.
func wrapSegs(segs []mdSeg, width int, first, cont string) []mdLine {
	type word struct {
		text  string
		st    Style
		space bool // preceded by a space in the source
	}
	var words []word
	pendingSpace := false
	for _, s := range segs {
		for i, f := range strings.Split(s.text, " ") {
			if i > 0 {
				pendingSpace = true
			}
			if f == "" {
				continue
			}
			words = append(words, word{f, s.st, pendingSpace})
			pendingSpace = false
		}
	}
	prefix := Style{Fg: segs0Fg(segs), Bg: segs0Bg(segs)}
	var out []mdLine
	line := mdLine{segs: []mdSeg{{first, prefix}}}
	col, empty := Width(first), true
	for _, w := range words {
		sp := 0
		if w.space && !empty {
			sp = 1
		}
		if !empty && col+sp+Width(w.text) > width {
			out = append(out, line)
			line = mdLine{segs: []mdSeg{{cont, prefix}}}
			col, empty, sp = Width(cont), true, 0
		}
		if sp == 1 {
			line.segs = append(line.segs, mdSeg{" ", Style{Bg: w.st.Bg}})
			col++
		}
		text := w.text
		if Width(text) > width-col {
			text = Truncate(text, max(1, width-col))
		}
		line.segs = append(line.segs, mdSeg{text, w.st})
		col += Width(text)
		empty = false
	}
	return append(out, line)
}

func segs0Fg(s []mdSeg) Color {
	if len(s) > 0 {
		return s[0].st.Fg
	}
	return NoColor
}

func segs0Bg(s []mdSeg) Color {
	if len(s) > 0 {
		return s[0].st.Bg
	}
	return NoColor
}

func drawMDLine(v View, y int, l mdLine) {
	if l.bg != NoColor {
		v.FillRect(0, y, v.W(), 1, Style{Bg: l.bg})
	}
	x := 0
	for _, sg := range l.segs {
		x = v.Text(x, y, sg.text, sg.st)
	}
}

var gitTrailer = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*: \S`)

// splitTrailers separates git trailers (Signed-off-by:, Co-Authored-By: …) from a
// commit body, since as markdown they would run together into one paragraph.
func splitTrailers(body string) (string, []string) {
	lines := strings.Split(strings.TrimRight(body, "\n "), "\n")
	i := len(lines)
	for i > 0 && gitTrailer.MatchString(lines[i-1]) {
		i--
	}
	if i == len(lines) || (i > 0 && strings.TrimSpace(lines[i-1]) != "") {
		return body, nil
	}
	return strings.Join(lines[:i], "\n"), lines[i:]
}
