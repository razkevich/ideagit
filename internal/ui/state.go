package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ProjectState is what ideagit restores per repository/worktree: tabs, filters,
// selections, expanded folders and the files open in the editor.
type ProjectState struct {
	ActiveTab   int             `json:"activeTab"`
	Focus       focusArea       `json:"focus"`
	LogTabs     []LogTabState   `json:"logTabs"`
	PRFilter    *PRFilter       `json:"prFilter,omitempty"`
	PRSelected  int             `json:"prSelected,omitempty"`
	ProjectExp  map[string]bool `json:"projectExpanded,omitempty"`
	ProjectSel  string          `json:"projectSelected,omitempty"`
	LocalExp    map[string]bool `json:"localChangesExpanded,omitempty"`
	LocalSel    string          `json:"localChangesSelected,omitempty"`
	OpenFiles   []string        `json:"openFiles,omitempty"`
	CurrentFile string          `json:"currentFile,omitempty"`
	Lines       map[string]int  `json:"lines,omitempty"`
}

var unsafePath = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func statePath(root string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ideagit", "projects", unsafePath.ReplaceAllString(root, "_")+".json")
}

// prStatePath keeps `ideagit --pr` sessions apart from the project's normal state.
func prStatePath(root string) string {
	p := statePath(root)
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(p, ".json") + ".pr.json"
}

func loadProjectState(root string) *ProjectState { return loadState(statePath(root)) }

func loadState(p string) *ProjectState {
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var st ProjectState
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	return &st
}

// SaveState writes the current session for this repository.
func (a *App) SaveState() {
	if a.snapshot || a.Cfg.path == "" {
		return
	}
	if a.prMode {
		a.savePRState()
		return
	}
	st := ProjectState{ActiveTab: a.active, Focus: a.focus}
	for _, t := range a.tabs {
		switch t := t.(type) {
		case *LogTab:
			f := t.filter
			f.Limit = 0
			st.LogTabs = append(st.LogTabs, LogTabState{Filter: f, Selected: t.selectedHash()})
		case *PRTab:
			f := t.filter
			st.PRFilter = &f
			if p := t.selected(); p != nil {
				st.PRSelected = p.Number
			}
		case *LocalChanges:
			st.LocalExp = t.tree.exp
			if n := t.tree.Selected(); n != nil {
				st.LocalSel = n.ID
			}
		}
	}
	st.ProjectExp = a.project.tree.exp
	if n := a.project.tree.Selected(); n != nil {
		st.ProjectSel = n.ID
	}
	if a.editor != nil && a.editor.Running() {
		bufs, cur := a.editor.Buffers()
		for _, b := range bufs {
			if rel := a.repoRel(b.path); rel != "" {
				st.OpenFiles = append(st.OpenFiles, rel)
			}
		}
		st.CurrentFile = a.repoRel(cur)
		st.Lines = a.editor.Lines(a)
	} else if a.viewer != nil {
		st.OpenFiles = []string{a.viewer.path}
		st.CurrentFile = a.viewer.path
	}
	a.writeState(statePath(a.Repo.Root), st)
}

// savePRState keeps only what a PR session restores: the filter and selected PR.
func (a *App) savePRState() {
	p, _ := a.PRTab()
	if p == nil {
		return
	}
	f := p.filter
	st := ProjectState{PRFilter: &f}
	if sel := p.selected(); sel != nil {
		st.PRSelected = sel.Number
	}
	a.writeState(prStatePath(a.Repo.Root), st)
}

func (a *App) writeState(p string, st ProjectState) {
	if p == "" {
		return
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.MarshalIndent(st, "", "  "); err == nil {
		os.WriteFile(p, b, 0o644)
	}
	a.lastStateSave = time.Now()
}

// restoreState applies saved per-project state to a freshly built App. Selections are
// applied once the data they refer to has loaded.
func (a *App) restoreState(st *ProjectState) {
	if st == nil {
		return
	}
	for _, t := range a.tabs {
		switch t := t.(type) {
		case *PRTab:
			if st.PRFilter != nil {
				t.filter = *st.PRFilter
				t.text.SetValue(t.filter.Text)
			}
			if st.PRSelected > 0 {
				t.pending = st.PRSelected
			}
		case *LocalChanges:
			if st.LocalExp != nil {
				t.tree.exp = st.LocalExp
			}
			t.tree.wantID = st.LocalSel
		}
	}
	logs := 0
	for _, t := range a.tabs {
		if l, ok := t.(*LogTab); ok {
			if logs < len(st.LogTabs) && st.LogTabs[logs].Selected != "" {
				l.pending = st.LogTabs[logs].Selected
			}
			logs++
		}
	}
	if st.ProjectExp != nil {
		a.project.tree.exp = st.ProjectExp
	}
	a.project.tree.wantID = st.ProjectSel
	if st.ActiveTab >= 0 && st.ActiveTab < len(a.tabs) {
		a.active = st.ActiveTab
	}
	a.focus = st.Focus
	if a.focus == focusProject && !a.showProject() {
		a.focus = focusGit
	}
	a.restoreFiles = st
}

// reopenFiles brings back the editor's files; the current file is opened last so it ends
// up in front, and focus goes back where it was.
func (a *App) reopenFiles() tea.Cmd {
	st := a.restoreFiles
	a.restoreFiles = nil
	if st == nil || len(st.OpenFiles) == 0 {
		if a.focus == focusEditor {
			a.focus = focusGit
		}
		return nil
	}
	focus := a.focus
	var cmds []tea.Cmd
	order := append([]string(nil), st.OpenFiles...)
	if st.CurrentFile != "" {
		order = append(order, st.CurrentFile)
	}
	for _, rel := range order {
		if _, err := os.Stat(a.Repo.Abs(rel)); err != nil {
			continue
		}
		cmds = append(cmds, a.OpenInEditor(rel, st.Lines[rel]))
	}
	a.focus = focus
	if a.focus == focusEditor && !a.editorOpen() {
		a.focus = focusGit
	}
	return tea.Batch(cmds...)
}
