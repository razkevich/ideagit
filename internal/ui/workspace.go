package ui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/razkevich/ideagit/internal/canvas"
	"github.com/razkevich/ideagit/internal/git"
)

type focusArea int

const (
	focusGit focusArea = iota
	focusProject
	focusEditor
)

// Workspace layout, IntelliJ-style: Project on the left, the editor on top and the
// Git tool window below it. Without an open editor the Git tool window fills the right.

func (a *App) editorOpen() bool {
	if a.editor != nil && a.editor.Running() {
		return true
	}
	return a.viewer != nil
}

// OpenInEditor shows rel in the embedded nvim (or the read-only viewer without nvim).
func (a *App) OpenInEditor(rel string, line int) tea.Cmd {
	a.PopAll()
	a.focus = focusEditor
	if a.showProject() {
		a.project.Reveal(a, rel)
	}
	if !NvimAvailable() {
		if a.viewer == nil {
			a.viewer = &Viewer{}
		}
		a.viewer.Load(a, rel, line)
		return nil
	}
	if a.editor == nil {
		a.editor = NewEditor()
	}
	if a.editor.Running() {
		a.editor.Open(a.Repo.Abs(rel), line)
		return nil
	}
	w, h := a.editorSize()
	if err := a.editor.Start(a.Repo.Root, w, h); err != nil {
		a.Error("Cannot start nvim", err.Error())
		a.focus = focusGit
		return a.ExternalEdit(rel, line)
	}
	a.editor.Open(a.Repo.Abs(rel), line)
	return a.editor.Wait()
}

// JumpToSource is IntelliJ's F4: open the working-tree version of rel in the editor.
func (a *App) JumpToSource(rel string, line int) tea.Cmd {
	if rel == "" {
		return nil
	}
	if fi, err := os.Stat(a.Repo.Abs(rel)); err != nil || fi.IsDir() {
		a.Notify("Cannot jump to source", rel+" doesn't exist in the working tree")
		return nil
	}
	return a.OpenInEditor(rel, line)
}

func (a *App) onEditorEvent(ev editorEvent) tea.Cmd {
	switch ev {
	case evExit:
		a.editor = nil
		if a.focus == focusEditor {
			a.focus = focusGit
		}
		return a.Refresh()
	case evSaved:
		return tea.Batch(a.Refresh(), a.editor.Wait())
	case evEmpty:
		// Closing the last tab hides the editor: nvim exits and evExit gives the space back.
		if a.editor != nil {
			a.editor.QuitIfIdle()
			return a.editor.Wait()
		}
		return nil
	}
	if a.editor == nil {
		return nil
	}
	if a.showProject() {
		if a.Cfg.ProjectOpenFiles {
			a.project.OnData(a)
		}
		if a.Cfg.ProjectAutoscroll {
			if _, cur := a.editor.Buffers(); cur != "" {
				if r := a.repoRel(cur); r != "" && r != a.lastRevealed {
					a.selectOpenedFile(false)
				}
			}
		}
	}
	return a.editor.Wait()
}

func (a *App) closeViewer() {
	a.viewer = nil
	if a.focus == focusEditor {
		a.focus = focusGit
	}
}

func (a *App) toggleProject() {
	switch {
	case !a.showProject():
		a.setProjectShown(true)
		a.focus = focusProject
		a.project.OnData(a)
	case a.focus != focusProject:
		a.focus = focusProject
	default:
		a.setProjectShown(false)
		a.focus = focusGit
		if a.editorOpen() {
			a.focus = focusEditor
		}
	}
}

func (a *App) toggleGit() {
	switch {
	case a.Cfg.HideGitWindow:
		a.Cfg.HideGitWindow = false
		a.focus = focusGit
	case a.focus != focusGit:
		a.focus = focusGit
	case a.editorOpen():
		a.Cfg.HideGitWindow = true
		a.focus = focusEditor
	}
	a.Cfg.Save()
}

func (a *App) projectWidth() int {
	if !a.showProject() {
		return 0
	}
	w := a.Cfg.ProjectWidth
	if w == 0 {
		w = 34
	}
	return max(16, min(w, a.W/2))
}

// editorSize is the nvim grid size for the current layout (below the editor tab row).
func (a *App) editorSize() (int, int) {
	pw := a.projectWidth()
	if pw > 0 {
		pw++
	}
	w := a.W - pw
	h := a.H - 2
	if !a.Cfg.HideGitWindow {
		h = a.editorHeight(h)
	}
	return w, h - 1
}

func (a *App) editorHeight(total int) int {
	split := a.Cfg.EditorSplit
	if split == 0 {
		split = 0.6
	}
	return max(5, min(total-6, int(float64(total)*split)))
}

func (a *App) drawWorkspace(v View) {
	th := a.Th
	x := 0
	if pw := a.projectWidth(); pw > 0 {
		pv := v.Sub(0, 0, pw, v.H())
		a.project.Draw(a, pv, a.focus == focusProject)
		a.HitPass(pv.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() || m.RightPress() {
				a.focus = focusProject
			}
			return nil
		})
		v.FillRect(pw, 0, 1, v.H(), Style{Bg: th.Frame})
		abs := v.Abs()
		a.Hit(Rect{abs.X + pw, abs.Y, 1, v.H()}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.Drag(func(m Mouse) tea.Cmd {
					a.Cfg.ProjectWidth = max(16, m.X-abs.X)
					if m.Action == tea.MouseActionRelease {
						a.Cfg.Save()
					}
					return nil
				})
			}
			return nil
		})
		x = pw + 1
	}
	right := v.Sub(x, 0, v.W()-x, v.H())
	if !a.editorOpen() {
		a.drawGitWindow(right)
		return
	}
	edH := right.H()
	if !a.Cfg.HideGitWindow {
		edH = a.editorHeight(right.H())
	}
	if a.Cfg.HideGitWindow {
		a.drawEditorArea(right.Sub(0, 0, right.W(), edH))
		return
	}
	a.drawEditorArea(right.Sub(0, 0, right.W(), edH-1))
	right.FillRect(0, edH-1, right.W(), 1, Style{Bg: th.Frame})
	gv := right.Sub(0, edH, right.W(), right.H()-edH)
	a.drawGitWindow(gv)
	abs := gv.Abs()
	// The Git tool window's tab bar doubles as the splitter, as in IntelliJ.
	a.HitPass(Rect{abs.X, abs.Y, gv.W(), 1}, func(m Mouse) tea.Cmd {
		if m.LeftPress() {
			top := right.Abs().Y
			a.Drag(func(m Mouse) tea.Cmd {
				a.Cfg.EditorSplit = max(0.2, min(0.85, float64(m.Y-top)/float64(max(1, right.H()))))
				if m.Action == tea.MouseActionRelease {
					a.Cfg.Save()
				}
				return nil
			})
		}
		return nil
	})
}

func (a *App) drawGitWindow(v View) {
	th := a.Th
	a.drawTabBar(v.Sub(0, 0, v.W(), 1))
	body := v.Sub(0, 1, v.W(), v.H()-1)
	if !a.loaded {
		body.Text(2, 1, "Loading…", th.S().WithFg(th.Dim))
	} else if a.loadErr != nil {
		body.Text(2, 1, "Error: "+a.loadErr.Error(), th.S().WithFg(th.Error))
	} else {
		a.ActiveTab().Draw(a, body)
	}
	a.HitPass(v.Abs(), func(m Mouse) tea.Cmd {
		if m.LeftPress() || m.RightPress() {
			a.focus = focusGit
		}
		return nil
	})
}

func (a *App) drawEditorArea(v View) {
	th := a.Th
	focused := a.focus == focusEditor
	tabs := v.Sub(0, 0, v.W(), 1)
	tabs.Fill(Style{Bg: th.HeaderBg})
	body := v.Sub(0, 1, v.W(), v.H()-1)
	if a.editor != nil && a.editor.Running() && a.editor.NativeTabs() {
		a.editor.Draw(a, v, focused)
		a.forwardMouse(v)
		return
	}
	if a.editor != nil && a.editor.Running() {
		bufs, cur := a.editor.Buffers()
		x := 1
		for _, b := range bufs {
			b := b
			name := filepath.Base(b.path)
			label := " " + name
			if g, _ := a.fileIcon(b.path); g != "" {
				label = " " + g + " " + name
			}
			if b.modified {
				label += " ●"
			}
			label += "  ×"
			st := Style{Fg: th.Fg, Bg: th.HeaderBg}
			if b.path == cur {
				st = Style{Fg: th.Fg, Bg: th.TabSelBg, Bold: true}
			}
			if k := a.kindOfAbs(b.path); k != NoColor {
				st.Fg = k
			}
			if x+Width(label)+1 > tabs.W() {
				break
			}
			end := tabs.Text(x, 0, label, st)
			tabs.Text(end, 0, " ", st)
			tx := x
			a.Hit(Rect{tabs.Abs().X + tx, tabs.Abs().Y, end - tx - 2, 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					a.focus = focusEditor
					a.editor.SwitchTo(b.path)
				}
				if m.Button == tea.MouseButtonMiddle && m.Action == tea.MouseActionPress {
					a.editor.CloseBuffer(b.path)
				}
				return nil
			})
			a.Hit(Rect{tabs.Abs().X + end - 1, tabs.Abs().Y, 2, 1}, func(m Mouse) tea.Cmd {
				if m.LeftPress() {
					a.editor.CloseBuffer(b.path)
				}
				return nil
			})
			x = end + 2
		}
		a.editor.Draw(a, body, focused)
		a.forwardMouse(body)
		return
	}
	if a.viewer != nil {
		label := " " + filepath.Base(a.viewer.path) + "  (read-only: install nvim to edit)  ×"
		tabs.Text(1, 0, label, Style{Fg: th.Fg, Bg: th.TabSelBg, Bold: true})
		a.Hit(Rect{tabs.Abs().X + Width(label) - 1, tabs.Abs().Y, 2, 1}, func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.closeViewer()
			}
			return nil
		})
		a.viewer.Draw(a, body)
		a.HitPass(body.Abs(), func(m Mouse) tea.Cmd {
			if m.LeftPress() {
				a.focus = focusEditor
			}
			return nil
		})
	}
}

func (a *App) kindOfAbs(abs string) Color {
	if a.Status == nil {
		return NoColor
	}
	rel, err := filepath.Rel(a.Repo.Root, abs)
	if err != nil {
		return NoColor
	}
	rel = filepath.ToSlash(rel)
	for _, f := range a.Status.Files {
		if f.Path == rel {
			return a.Th.KindColor(f.Kind)
		}
	}
	return NoColor
}

// forwardMouse sends clicks, drags and wheel events in the editor body to nvim.
func (a *App) forwardMouse(body View) {
	abs := body.Abs()
	a.Hit(abs, func(m Mouse) tea.Cmd {
		e := a.editor
		if e == nil {
			return nil
		}
		row, col := m.Y-abs.Y, m.X-abs.X
		mods := ""
		if m.Shift {
			mods += "S"
		}
		if m.Ctrl {
			mods += "C"
		}
		if m.Alt {
			mods += "A"
		}
		if d := m.Wheel(); d != 0 {
			dir := "down"
			if d < 0 {
				dir = "up"
			}
			e.Mouse("wheel", dir, mods, row, col)
			return nil
		}
		if m.Action != tea.MouseActionPress {
			return nil
		}
		button := ""
		switch m.Button {
		case tea.MouseButtonLeft:
			button = "left"
		case tea.MouseButtonRight:
			button = "right"
		case tea.MouseButtonMiddle:
			button = "middle"
		default:
			return nil
		}
		a.focus = focusEditor
		e.Mouse(button, "press", mods, row, col)
		a.Drag(func(m Mouse) tea.Cmd {
			r, c := max(0, m.Y-abs.Y), max(0, m.X-abs.X)
			if m.Action == tea.MouseActionRelease {
				e.Mouse(button, "release", mods, r, c)
			} else {
				e.Mouse(button, "drag", mods, r, c)
			}
			return nil
		})
		return nil
	})
}

// editorKey routes a key to the editor; a few IntelliJ tool-window shortcuts stay global.
func (a *App) editorKey(k tea.KeyMsg) tea.Cmd {
	// IntelliJ's VCS shortcuts work from the editor too, except plain Ctrl+K/Ctrl+T
	// which vim users need (digraphs, tag pop).
	switch keyName(k) {
	case "ctrl+shift+k", "ctrl+shift+`", "ctrl+shift+a", "ctrl+shift+n", "ctrl+shift+o", "alt+`", "ctrl+alt+y", "ctrl+shift+[", "ctrl+shift+]":
		cmd, _ := a.globalKey(k)
		return cmd
	case "ctrl+alt+z":
		if rel := a.editorPath(); rel != "" && a.Status != nil {
			for _, f := range a.Status.Files {
				if f.Path == rel && f.Kind != git.Untracked {
					OpenRollback(a, []git.FileStatus{f})
					return nil
				}
			}
		}
		return nil
	}
	switch keyName(k) {
	case "alt+1", "ctrl+1":
		a.toggleProject()
		return nil
	case "alt+9", "ctrl+9":
		a.toggleGit()
		return nil
	case "alt+0", "ctrl+0":
		return a.OpenCommit(nil)
	case "shift+shift":
		return a.OpenSearch(0)
	case "alt+f1":
		a.selectOpenedFile(true)
		return nil
	case "ctrl+tab":
		a.cycleFocus()
		return nil
	case "ctrl+f4":
		if a.editor != nil && a.editor.Running() {
			if _, cur := a.editor.Buffers(); cur != "" {
				a.editor.CloseBuffer(cur)
				return nil
			}
		}
	}
	if a.editor != nil && a.editor.Running() {
		if k.Paste {
			a.editor.Paste(string(k.Runes))
			return nil
		}
		a.editor.Input(nvimKey(k))
		return nil
	}
	if a.viewer != nil {
		switch keyName(k) {
		case "esc", "q", "ctrl+w":
			a.closeViewer()
			return nil
		case "e":
			return a.ExternalEdit(a.viewer.path, a.viewer.top+1)
		}
		a.viewer.Key(a, k)
	}
	return nil
}

// Quit asks before discarding unsaved nvim buffers.
func (a *App) Quit() tea.Cmd {
	a.SaveState()
	if a.editor == nil || !a.editor.Running() {
		return tea.Quit
	}
	n := a.editor.ModifiedCount()
	if n == 0 {
		a.editor.Quit(false)
		return tea.Quit
	}
	d := &Dialog{Title: "Unsaved Changes", Width: 60}
	d.Fields = []Field{&LabelField{Text: fmt.Sprintf("%s in the editor %s unsaved changes.", plural(n, "file"), map[bool]string{true: "has", false: "have"}[n == 1])}}
	d.Buttons = []Button{
		{Label: "Cancel"},
		{Label: "Discard", Do: func(a *App, d *Dialog) (tea.Cmd, bool) { a.editor.Quit(false); return tea.Quit, true }},
		{Label: "Save All and Quit", Primary: true, Do: func(a *App, d *Dialog) (tea.Cmd, bool) { a.editor.Quit(true); return tea.Quit, true }},
	}
	d.focus = 2
	a.Push(d)
	return nil
}

func (a *App) editorPath() string {
	if a.editor != nil && a.editor.Running() {
		_, cur := a.editor.Buffers()
		if cur != "" {
			return displayPath(a, cur)
		}
	}
	if a.viewer != nil {
		return a.viewer.path
	}
	return ""
}

func (a *App) workspaceHint() string {
	switch a.focus {
	case focusProject:
		return a.project.Hint()
	case focusEditor:
		if a.viewer != nil && (a.editor == nil || !a.editor.Running()) {
			return "↑↓ scroll · e open in $EDITOR · esc close · alt+1 project · alt+9 git"
		}
		return "keys go to nvim · alt+1 project · alt+9 git · alt+0 commit · click to focus"
	}
	return ""
}

// hideActiveToolWindow is IntelliJ's Shift+Esc.
func (a *App) hideActiveToolWindow() {
	switch {
	case a.focus == focusProject && a.showProject():
		a.toggleProject()
	case a.focus == focusGit && a.editorOpen():
		a.Cfg.HideGitWindow = true
		a.focus = focusEditor
		a.Cfg.Save()
	}
}

// cycleFocus moves between Project, editor and Git tool window (a simple Ctrl+Tab switcher).
func (a *App) cycleFocus() {
	order := []focusArea{focusGit}
	if a.showProject() {
		order = append([]focusArea{focusProject}, order...)
	}
	if a.editorOpen() {
		order = append(order, focusEditor)
	}
	for i, f := range order {
		if f == a.focus {
			a.focus = order[(i+1)%len(order)]
			if a.focus == focusGit {
				a.Cfg.HideGitWindow = false
			}
			return
		}
	}
	a.focus = order[0]
}

func (a *App) selectedPath() string {
	switch a.focus {
	case focusProject:
		return a.project.SelectedPath()
	case focusEditor:
		return a.editorPath()
	}
	if p, ok := a.ActiveTab().(interface{ SelectedPath() string }); ok {
		return p.SelectedPath()
	}
	return ""
}

// showProject is the saved preference, unless this session hid the pane (--pr).
func (a *App) showProject() bool {
	if a.prMode {
		return !a.projectHidden
	}
	return a.Cfg.ShowProject
}

// setProjectShown changes the Project pane's visibility: for this session only in PR
// mode, otherwise as the saved default.
func (a *App) setProjectShown(show bool) {
	if a.prMode {
		a.projectHidden = !show
		return
	}
	a.Cfg.ShowProject = show
	a.Cfg.Save()
}
