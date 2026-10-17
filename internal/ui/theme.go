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
	// Frame is the title bar, status bar and the gaps between panels ("islands").
	Frame, PopupBorder, ToolbarBg              Color
	Sel, SelInactive, Hover, Accent, Link      Color
	TabSelBg, TabSelBorder                     Color
	ButtonBg, ButtonFg, ButtonPrimary, InputBg Color
	InputBorder, InputFocus                    Color
	Error, Warning, Success                    Color
	UnversionedRowBg                           Color

	Modified, Added, Deleted, Unversioned, Conflict, Ignored Color

	RefHead, RefLocal, RefRemote, RefTag Color
	Graph                                []Color

	DiffMod, DiffModWord, DiffIns, DiffInsWord, DiffDel, DiffDelWord Color
	DiffConflict, DiffGutterNum, DiffFold, ScrollThumb, ScrollTrack  Color

	SynKeyword, SynString, SynNumber, SynComment, SynFunc, SynType, SynAttr, SynTag, SynConst, SynPunct Color
}

// DarkTheme follows IntelliJ's New UI "Islands Dark": panels are #191A1C islands on a
// #26282C frame (title bar, status bar and the gaps between panels). Values were sampled
// from IDE screenshots and converted from Display P3 to sRGB.
func DarkTheme() *Theme {
	return &Theme{
		Dark:             true,
		Bg:               Hex("#191A1C"),
		PanelBg:          Hex("#191A1C"),
		Frame:            Hex("#26282C"),
		HeaderBg:         Hex("#2B2D30"),
		Fg:               Hex("#DFE1E5"),
		Dim:              Hex("#7B7F86"),
		Faint:            Hex("#50555F"),
		Border:           Hex("#26282C"),
		PopupBorder:      Hex("#393B40"),
		Sel:              Hex("#2B4371"),
		SelInactive:      Hex("#2E3034"),
		Hover:            Hex("#212226"),
		Accent:           Hex("#3574F0"),
		Link:             Hex("#70A1FE"),
		TabSelBg:         Hex("#233658"),
		TabSelBorder:     Hex("#2F4F8D"),
		ButtonBg:         Hex("#191A1C"),
		ButtonFg:         Hex("#DFE1E5"),
		ButtonPrimary:    Hex("#3771E1"),
		InputBg:          Hex("#191A1C"),
		InputBorder:      Hex("#41434A"),
		InputFocus:       Hex("#3574F0"),
		Error:            Hex("#F75464"),
		Warning:          Hex("#F2C55C"),
		Success:          Hex("#5FB865"),
		UnversionedRowBg: Hex("#291F1B"),
		Modified:         Hex("#72B1FF"),
		Added:            Hex("#73BD79"),
		Deleted:          Hex("#7B7F86"),
		Unversioned:      Hex("#FE9B95"),
		Conflict:         Hex("#E55765"),
		Ignored:          Hex("#A8A35A"),
		RefHead:          Hex("#F2C55C"),
		RefLocal:         Hex("#5FB865"),
		RefRemote:        Hex("#BC8EF5"),
		RefTag:           Hex("#7B7F86"),
		Graph: []Color{
			Hex("#B08841"), Hex("#A64169"), Hex("#4BA53F"), Hex("#AFA343"),
			Hex("#5C9BD1"), Hex("#BC8EF5"), Hex("#D97D4C"), Hex("#4CB5A8"),
			Hex("#C9649C"), Hex("#7A8CD6"),
		},
		DiffMod:       Hex("#263451"),
		DiffModWord:   Hex("#36507D"),
		DiffIns:       Hex("#294436"),
		DiffInsWord:   Hex("#3A6249"),
		DiffDel:       Hex("#2C2D2E"),
		DiffDelWord:   Hex("#46484B"),
		DiffConflict:  Hex("#5B3A35"),
		DiffGutterNum: Hex("#50555F"),
		DiffFold:      Hex("#212326"),
		ScrollThumb:   Hex("#46484C"),
		ScrollTrack:   Hex("#191A1C"),
		ToolbarBg:     Hex("#212326"),
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
		Frame: Hex("#F2F3F5"), PopupBorder: Hex("#DFE1E5"), ToolbarBg: Hex("#F7F8FA"),
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
	PRFilter          PRFilter      `json:"prFilter"`
	// Favorites maps a repository root to its starred branches.
	Favorites         map[string][]string `json:"favoriteBranches,omitempty"`
	ShowProject       bool                `json:"showProject"`
	ProjectAutoscroll bool                `json:"projectAlwaysSelectOpenedFile"`
	ProjectHideDots   bool                `json:"projectHideDotFiles"`
	ProjectOpenFiles  bool                `json:"projectViewOpenFiles"`
	Icons             string              `json:"icons"`
	ProjectWidth      int                 `json:"projectWidth"`
	EditorSplit       float64             `json:"editorSplit"`
	HideGitWindow     bool                `json:"hideGitWindow"`
	path              string
}

type LogTabState struct {
	Filter   git.LogFilter `json:"filter"`
	Selected string        `json:"selected,omitempty"`
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
