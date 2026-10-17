package ui

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type tokSpan struct {
	start, end int
	fg         Color
	italic     bool
}

func lexerFor(path string, sample string) chroma.Lexer {
	base := filepath.Base(path)
	l := lexers.Match(base)
	if l == nil {
		switch strings.ToLower(filepath.Ext(base)) {
		case ".tf", ".tfvars", ".hcl":
			l = lexers.Get("terraform")
		case ".mdc":
			l = lexers.Get("markdown")
		}
	}
	if l == nil && sample != "" {
		l = lexers.Analyse(sample)
	}
	if l == nil {
		return nil
	}
	return chroma.Coalesce(l)
}

// highlightLines tokenizes the file once and splits colours per line, in rune offsets.
func highlightLines(th *Theme, path string, lines []string) [][]tokSpan {
	if len(lines) == 0 || len(lines) > 30000 {
		return nil
	}
	text := strings.Join(lines, "\n")
	lx := lexerFor(path, text[:min(len(text), 2000)])
	if lx == nil {
		return nil
	}
	it, err := lx.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	res := make([][]tokSpan, len(lines))
	line, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		fg, italic := th.TokenColor(tok.Type)
		parts := strings.Split(tok.Value, "\n")
		for pi, p := range parts {
			if pi > 0 {
				line++
				col = 0
			}
			if line >= len(lines) {
				break
			}
			n := len([]rune(p))
			if n > 0 && fg != th.Fg {
				res[line] = append(res[line], tokSpan{col, col + n, fg, italic})
			}
			col += n
		}
	}
	return res
}

func blend(a, b Color, t float64) Color {
	ar, ag, ab := uint8(a>>16), uint8(a>>8), uint8(a)
	br, bg, bb := uint8(b>>16), uint8(b>>8), uint8(b)
	mix := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return RGB(mix(ar, br), mix(ag, bg), mix(ab, bb))
}
