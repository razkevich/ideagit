package ui

import (
	"os"
	"path"
	"strings"

	. "github.com/razkevich/ideagit/internal/canvas"
)

type icon struct {
	glyph string
	color string
}

// Nerd Font glyphs and colors follow nvim-web-devicons so the tree matches the editor.
var iconsByExt = map[string]icon{
	"go": {"", "#519aba"}, "mod": {"", "#519aba"}, "sum": {"", "#519aba"},
	"md": {"", "#dddddd"}, "mdx": {"", "#519aba"}, "txt": {"\U000f0219", "#89e051"},
	"json": {"", "#cbcb41"}, "jsonc": {"", "#cbcb41"},
	"yaml": {"", "#6d8086"}, "yml": {"", "#6d8086"}, "toml": {"", "#9c4221"},
	"js": {"", "#cbcb41"}, "mjs": {"", "#f1e05a"}, "cjs": {"", "#cbcb41"},
	"ts": {"", "#519aba"}, "tsx": {"", "#1354bf"}, "jsx": {"", "#20c2e3"},
	"py": {"", "#ffbc03"}, "rs": {"", "#dea584"}, "rb": {"", "#701516"},
	"java": {"", "#cc3e44"}, "kt": {"", "#7f52ff"}, "swift": {"", "#e37933"},
	"c": {"", "#599eff"}, "h": {"", "#a074c4"}, "cpp": {"", "#519aba"}, "hpp": {"", "#a074c4"},
	"cs": {"\U000f031b", "#596706"}, "php": {"", "#a074c4"}, "lua": {"", "#51a0cf"},
	"sh": {"", "#4d5a5e"}, "bash": {"", "#89e051"}, "zsh": {"", "#89e051"}, "fish": {"", "#4d5a5e"},
	"html": {"", "#e44d26"}, "css": {"", "#42a5f5"}, "scss": {"", "#f55385"},
	"svg": {"\U000f0721", "#ffb13b"}, "png": {"", "#a074c4"}, "jpg": {"", "#a074c4"}, "jpeg": {"", "#a074c4"},
	"gif": {"", "#a074c4"}, "ico": {"", "#cbcb41"}, "webp": {"", "#a074c4"},
	"tf": {"", "#5f43e9"}, "tfvars": {"", "#5f43e9"}, "hcl": {"", "#5f43e9"},
	"sql": {"", "#dad8d8"}, "proto": {"", "#999999"}, "graphql": {"", "#e535ab"},
	"xml": {"\U000f05c0", "#e37933"}, "csv": {"", "#89e051"}, "lock": {"", "#bbbbbb"},
	"log": {"\U000f0331", "#dddddd"}, "env": {"", "#faf743"}, "conf": {"", "#6d8086"},
	"ini": {"", "#6d8086"}, "cfg": {"", "#6d8086"}, "vim": {"", "#019833"},
	"dockerfile": {"\U000f0868", "#458ee6"}, "zip": {"", "#eca517"}, "gz": {"", "#eca517"},
	"tar": {"", "#eca517"}, "pdf": {"", "#b30b00"}, "patch": {"", "#41535b"}, "diff": {"", "#41535b"},
	"vue": {"", "#8dc149"}, "svelte": {"", "#ff3e00"}, "dart": {"", "#03589c"},
	"ex": {"", "#a074c4"}, "exs": {"", "#a074c4"}, "erl": {"", "#b83998"},
	"hs": {"", "#a074c4"}, "scala": {"", "#cc3e44"}, "zig": {"", "#f69a1b"},
}

var iconsByName = map[string]icon{
	"makefile": {"", "#6d8086"}, "dockerfile": {"\U000f0868", "#458ee6"}, "license": {"", "#d0bf41"},
	"readme": {"", "#dddddd"}, ".gitignore": {"", "#f54d27"}, ".gitattributes": {"", "#f54d27"},
	".gitmodules": {"", "#f54d27"}, "codeowners": {"", "#f54d27"}, "package.json": {"", "#e8274b"},
	"go.mod": {"", "#519aba"}, "go.sum": {"", "#519aba"}, ".env": {"", "#faf743"},
	".editorconfig": {"", "#fff2f2"}, "docker-compose.yml": {"\U000f0868", "#458ee6"},
	"compose.yaml": {"\U000f0868", "#458ee6"}, ".dockerignore": {"\U000f0868", "#458ee6"},
}

const (
	glyphFolder     = ""
	glyphFolderOpen = ""
	glyphFile       = ""
	glyphRepo       = ""
)

// IconsEnabled decides whether Nerd Font glyphs can be used. Ghostty, WezTerm and kitty
// ship the Nerd Font symbols, so they work there without a patched font.
func IconsEnabled(cfg *Config) bool {
	switch cfg.Icons {
	case "nerd", "on", "true":
		return true
	case "none", "off", "false":
		return false
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	t := os.Getenv("TERM")
	return strings.Contains(t, "ghostty") || strings.Contains(t, "kitty") || strings.Contains(t, "wezterm")
}

// fileIcon returns the glyph and color for a file path, or "" when icons are off.
func (a *App) fileIcon(p string) (string, Color) {
	if !a.icons {
		return "", NoColor
	}
	base := strings.ToLower(path.Base(p))
	if ic, ok := iconsByName[base]; ok {
		return ic.glyph, Hex(ic.color)
	}
	if strings.HasPrefix(base, "readme") {
		return iconsByName["readme"].glyph, Hex(iconsByName["readme"].color)
	}
	if strings.HasPrefix(base, "dockerfile") {
		return iconsByName["dockerfile"].glyph, Hex(iconsByName["dockerfile"].color)
	}
	if strings.HasPrefix(base, "makefile") || strings.HasSuffix(base, ".mk") {
		return iconsByName["makefile"].glyph, Hex(iconsByName["makefile"].color)
	}
	if strings.HasPrefix(base, ".env") {
		return iconsByName[".env"].glyph, Hex(iconsByName[".env"].color)
	}
	ext := strings.TrimPrefix(path.Ext(base), ".")
	if ic, ok := iconsByExt[ext]; ok {
		return ic.glyph, Hex(ic.color)
	}
	return glyphFile, Hex("#6d8086")
}

// dirIcon returns the folder glyph; without Nerd Fonts it falls back to a plain box.
func (a *App) dirIcon(open bool) (string, Color) {
	if !a.icons {
		return "▤", NoColor
	}
	if open {
		return glyphFolderOpen, Hex("#8fa1b3")
	}
	return glyphFolder, Hex("#8fa1b3")
}

func (a *App) setFileIcon(n *Node, p string) {
	n.Icon, n.IconFg = a.fileIcon(p)
}
