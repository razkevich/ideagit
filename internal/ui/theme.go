package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/alecthomas/chroma/v2"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

type Theme struct {
	Dark bool

	Bg, PanelBg, HeaderBg, Fg, Dim, Faint, Border Color
	Sel, SelInactive, Hover, Accent, Link         Color
	TabSelBg, TabSelBorder                        Color
	ButtonBg, ButtonFg, ButtonPrimary, InputBg    Color
	InputBorder, InputFocus                       Color
	Error, Warning, Success                       Color
	UnversionedRowBg                              Color

	Modified, Added, Deleted, Unversioned, Conflict, Ignored Color

	RefHead, RefLocal, RefRemote, RefTag Color
	Graph                                []Color

	DiffMod, DiffModWord, DiffIns, DiffInsWord, DiffDel, DiffDelWord Color
	DiffConflict, DiffGutterNum, DiffFold, ScrollThumb, ScrollTrack  Color

	SynKeyword, SynString, SynNumber, SynComment, SynFunc, SynType, SynAttr, SynTag, SynConst, SynPunct Color
}

func DarkTheme() *Theme {
	return &Theme{
		Dark:             true,
		Bg:               Hex("#1E1F22"),
		PanelBg:          Hex("#1E1F22"),
		HeaderBg:         Hex("#2B2D30"),
		Fg:               Hex("#DFE1E5"),
		Dim:              Hex("#868A91"),
		Faint:            Hex("#5A5D63"),
		Border:           Hex("#393B40"),
		Sel:              Hex("#2E436E"),
		SelInactive:      Hex("#393B40"),
		Hover:            Hex("#2B2D30"),
		Accent:           Hex("#3574F0"),
		Link:             Hex("#548AF7"),
		TabSelBg:         Hex("#25324D"),
		TabSelBorder:     Hex("#3574F0"),
		ButtonBg:         Hex("#2B2D30"),
		ButtonFg:         Hex("#DFE1E5"),
		ButtonPrimary:    Hex("#3574F0"),
		InputBg:          Hex("#1E1F22"),
		InputBorder:      Hex("#4E5157"),
		InputFocus:       Hex("#3574F0"),
		Error:            Hex("#F75464"),
		Warning:          Hex("#F2C55C"),
		Success:          Hex("#5FB865"),
		UnversionedRowBg: Hex("#2A2223"),
		Modified:         Hex("#6CA0F5"),
		Added:            Hex("#73BD79"),
		Deleted:          Hex("#868A91"),
		Unversioned:      Hex("#E07A6E"),
		Conflict:         Hex("#E55765"),
		Ignored:          Hex("#A8A35A"),
		RefHead:          Hex("#E2C08D"),
		RefLocal:         Hex("#8DB87A"),
		RefRemote:        Hex("#B48EAD"),
		RefTag:           Hex("#9DA0A8"),
		Graph: []Color{
			Hex("#C9A04B"), Hex("#C25272"), Hex("#5FA35F"), Hex("#4F94CD"),
			Hex("#A777C9"), Hex("#CF8350"), Hex("#4DB3B3"), Hex("#B8B84A"),
			Hex("#D06B9F"), Hex("#7A8CD6"),
		},
		DiffMod:       Hex("#303C4F"),
		DiffModWord:   Hex("#43577A"),
		DiffIns:       Hex("#294436"),
		DiffInsWord:   Hex("#3B6B4C"),
		DiffDel:       Hex("#3E4042"),
		DiffDelWord:   Hex("#5A5D60"),
		DiffConflict:  Hex("#5B3A35"),
		DiffGutterNum: Hex("#5A5D63"),
		DiffFold:      Hex("#2B2D30"),
		ScrollThumb:   Hex("#4E5157"),
		ScrollTrack:   Hex("#1E1F22"),
		SynKeyword:    Hex("#CF8E6D"),
		SynString:     Hex("#6AAB73"),
		SynNumber:     Hex("#2AACB8"),
		SynComment:    Hex("#7A7E85"),
		SynFunc:       Hex("#56A8F5"),
		SynType:       Hex("#BCBEC4"),
		SynAttr:       Hex("#B3AE60"),
		SynTag:        Hex("#D5B778"),
		SynConst:      Hex("#C77DBB"),
		SynPunct:      Hex("#BCBEC4"),
	}
}

func LightTheme() *Theme {
	t := DarkTheme()
	*t = Theme{
		Dark: false, Bg: Hex("#FFFFFF"), PanelBg: Hex("#FFFFFF"), HeaderBg: Hex("#F7F8FA"),
		Fg: Hex("#000000"), Dim: Hex("#818594"), Faint: Hex("#A8ADBD"), Border: Hex("#EBECF0"),
		Sel: Hex("#D5E1FF"), SelInactive: Hex("#DFE1E5"), Hover: Hex("#F0F1F3"), Accent: Hex("#3574F0"), Link: Hex("#2F5EB9"),
		TabSelBg: Hex("#EDF3FF"), TabSelBorder: Hex("#3574F0"),
		ButtonBg: Hex("#FFFFFF"), ButtonFg: Hex("#000000"), ButtonPrimary: Hex("#3574F0"), InputBg: Hex("#FFFFFF"),
		InputBorder: Hex("#C9CCD6"), InputFocus: Hex("#3574F0"),
		Error: Hex("#DB3B4B"), Warning: Hex("#C27D04"), Success: Hex("#208A3C"), UnversionedRowBg: Hex("#FBEDEC"),
		Modified: Hex("#0A50C8"), Added: Hex("#067D17"), Deleted: Hex("#6C707E"), Unversioned: Hex("#B74A3E"),
		Conflict: Hex("#D1253A"), Ignored: Hex("#8C8C00"),
		RefHead: Hex("#A87400"), RefLocal: Hex("#2E7D32"), RefRemote: Hex("#7B4FA0"), RefTag: Hex("#6C707E"),
		Graph: []Color{Hex("#B5891E"), Hex("#C23B67"), Hex("#3B8F3B"), Hex("#2F78C4"), Hex("#8F52B5"),
			Hex("#C26A2E"), Hex("#279999"), Hex("#93931C"), Hex("#B8478A"), Hex("#5266C2")},
		DiffMod: Hex("#E8EFFC"), DiffModWord: Hex("#C4D5F7"), DiffIns: Hex("#E2F3E2"), DiffInsWord: Hex("#B8E2B8"),
		DiffDel: Hex("#EEEEEE"), DiffDelWord: Hex("#D2D2D2"), DiffConflict: Hex("#FBE3DA"), DiffGutterNum: Hex("#AEB3C2"),
		DiffFold: Hex("#F2F3F5"), ScrollThumb: Hex("#C9CCD6"), ScrollTrack: Hex("#FFFFFF"),
		SynKeyword: Hex("#0033B3"), SynString: Hex("#067D17"), SynNumber: Hex("#1750EB"), SynComment: Hex("#8C8C8C"),
		SynFunc: Hex("#00627A"), SynType: Hex("#000000"), SynAttr: Hex("#9E880D"), SynTag: Hex("#0033B3"),
		SynConst: Hex("#871094"), SynPunct: Hex("#080808"),
	}
	return t
}

func (t *Theme) S() Style { return Style{Fg: t.Fg, Bg: t.Bg} }

func (t *Theme) KindColor(k git.ChangeKind) Color {
	switch k {
	case git.Added:
		return t.Added
	case git.Deleted:
		return t.Deleted
	case git.Untracked:
		return t.Unversioned
	case git.Conflict:
		return t.Conflict
	}
	return t.Modified
}

func (t *Theme) GraphColor(i int) Color {
	if i < 0 {
		return t.Faint
	}
	return t.Graph[i%len(t.Graph)]
}

func (t *Theme) TokenColor(tt chroma.TokenType) (Color, bool) {
	switch {
	case tt == chroma.Comment || tt.InSubCategory(chroma.Comment) || tt.InCategory(chroma.Comment):
		return t.SynComment, true
	case tt == chroma.KeywordType:
		return t.SynKeyword, false
	case tt.InCategory(chroma.Keyword):
		return t.SynKeyword, false
	case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
		return t.SynFunc, false
	case tt == chroma.NameClass || tt == chroma.NameNamespace:
		return t.SynType, false
	case tt == chroma.NameTag:
		return t.SynTag, false
	case tt == chroma.NameAttribute || tt == chroma.NameDecorator:
		return t.SynAttr, false
	case tt == chroma.NameConstant || tt == chroma.NameBuiltin || tt == chroma.KeywordConstant:
		return t.SynConst, false
	case tt.InCategory(chroma.LiteralString):
		return t.SynString, false
	case tt.InCategory(chroma.LiteralNumber):
		return t.SynNumber, false
	case tt == chroma.GenericHeading || tt == chroma.GenericSubheading:
		return t.SynKeyword, false
	case tt == chroma.GenericInserted:
		return t.Added, false
	case tt == chroma.GenericDeleted:
		return t.Deleted, false
	}
	return t.Fg, false
}

type Config struct {
	Theme             string        `json:"theme"`
	DiffUnified       bool          `json:"diffUnified"`
	IgnoreWS          int           `json:"ignoreWhitespace"`
	NoWordHighlight   bool          `json:"noWordHighlight"`
	CollapseUnchanged bool          `json:"collapseUnchanged"`
	GroupByDir        bool          `json:"groupByDirectory"`
	HidePreview       bool          `json:"hideDiffPreview"`
	HideStagedMarks   bool          `json:"hideStagedMarks"`
	BranchesPane      bool          `json:"branchesPane"`
	UpdateRebase      bool          `json:"updateRebase"`
	CommitHistory     []string      `json:"commitHistory"`
	LogTabs           []LogTabState `json:"logTabs"`
	LocalSplit        float64       `json:"localSplit"`
	StashSplit        float64       `json:"stashSplit"`
	LogDetailsWidth   int           `json:"logDetailsWidth"`
	BranchesWidth     int           `json:"branchesWidth"`
	ConsoleHidden     bool          `json:"consoleHidden"`
	RecentProjects    []string      `json:"recentProjects"`
	path              string
}

type LogTabState struct {
	Filter git.LogFilter `json:"filter"`
}

func LoadConfig() *Config {
	c := &Config{LocalSplit: 0.5, StashSplit: 0.5, LogDetailsWidth: 46, BranchesWidth: 30}
	dir, err := os.UserConfigDir()
	if err == nil {
		c.path = filepath.Join(dir, "ideagit", "settings.json")
		if b, err := os.ReadFile(c.path); err == nil {
			json.Unmarshal(b, c)
		}
	}
	return c
}

func (c *Config) Save() {
	if c.path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(c.path), 0o755)
	b, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(c.path, b, 0o644)
}

func (c *Config) RememberMessage(msg string) {
	if msg == "" {
		return
	}
	hist := []string{msg}
	for _, m := range c.CommitHistory {
		if m != msg && len(hist) < 40 {
			hist = append(hist, m)
		}
	}
	c.CommitHistory = hist
	c.Save()
}

func (c *Config) RememberProject(root string) {
	list := []string{root}
	for _, p := range c.RecentProjects {
		if p != root && len(list) < 20 {
			list = append(list, p)
		}
	}
	c.RecentProjects = list
	c.Save()
}
