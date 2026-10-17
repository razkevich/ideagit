package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/neovim/go-client/nvim"

	. "github.com/razkevich/ideagit/internal/canvas"
)

// Editor hosts an embedded Neovim (nvim --embed) and renders its linegrid UI into a pane.
type Editor struct {
	mu      sync.Mutex
	v       *nvim.Nvim
	send    chan func(v *nvim.Nvim)
	events  chan editorEvent
	cells   []gcell
	gw, gh  int
	curRow  int
	curCol  int
	hl      map[int]hlAttr
	defFg   Color
	defBg   Color
	mode    string
	bufs    []editorBuf
	current string
	reqW    int
	reqH    int
	running bool
	exited  bool
	// nativeTabs: nvim renders its own tabline, so ours would duplicate it.
	nativeTabs bool
	hadFiles   bool
}

func (e *Editor) NativeTabs() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nativeTabs
}

type gcell struct {
	text string
	hl   int
}

type hlAttr struct {
	fg, bg                               Color
	bold, italic, underline, strike, rev bool
}

type editorBuf struct {
	path     string
	modified bool
	line     int
}

type editorEvent int

const (
	evFlush editorEvent = iota
	evSaved
	evExit
	// evEmpty: the last file buffer was closed.
	evEmpty
)

func NvimAvailable() bool {
	_, err := exec.LookPath("nvim")
	return err == nil
}

func NewEditor() *Editor {
	return &Editor{hl: map[int]hlAttr{}, events: make(chan editorEvent, 64), send: make(chan func(v *nvim.Nvim), 256)}
}

func (e *Editor) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// Start launches nvim in dir with a w×h UI. Events arrive through Wait.
func (e *Editor) Start(dir string, w, h int) error {
	v, err := nvim.NewChildProcess(
		nvim.ChildProcessArgs("--embed"),
		nvim.ChildProcessDir(dir),
		nvim.ChildProcessServe(false),
		nvim.ChildProcessLogf(func(string, ...interface{}) {}),
	)
	if err != nil {
		return err
	}
	v.RegisterHandler("redraw", func(updates ...[]interface{}) { e.redraw(updates) })
	v.RegisterHandler("ideagit_bufs", func(args ...interface{}) { e.onBufs(args) })
	v.RegisterHandler("ideagit_saved", func(args ...interface{}) { e.post(evSaved) })
	go func() {
		v.Serve()
		e.mu.Lock()
		e.running = false
		e.exited = true
		e.mu.Unlock()
		e.post(evExit)
	}()
	e.mu.Lock()
	e.v = v
	e.running = true
	e.exited = false
	e.reqW, e.reqH = w, h
	e.mu.Unlock()
	if err := v.AttachUI(max(10, w), max(3, h), map[string]interface{}{"ext_linegrid": true, "rgb": true}); err != nil {
		v.Close()
		return err
	}
	// The third value tells whether nvim draws its own tabline (e.g. bufferline sets
	// showtabline=2); it is re-sent when plugins change the option after lazy loading.
	v.Exec(`function! IdeagitNotify() abort
  call rpcnotify(0, 'ideagit_bufs', map(getbufinfo({'buflisted': 1}), {_, b -> [b.name, b.changed, b.lnum]}), expand('%:p'), &showtabline == 2 || (&showtabline == 1 && tabpagenr('$') > 1))
endfunction
augroup ideagit
  autocmd!
  autocmd BufEnter,BufAdd,BufDelete,BufModifiedSet,BufWritePost,BufFilePost,TabNew,TabClosed,VimEnter,UIEnter * call IdeagitNotify()
  autocmd OptionSet showtabline call IdeagitNotify()
  autocmd CursorHold,BufLeave * call IdeagitNotify()
  " BufDelete fires while the buffer is still listed; report again once it's gone.
  autocmd BufDelete,BufWipeout,BufUnload * call timer_start(20, {-> IdeagitNotify()})
  autocmd User VeryLazy,LazyDone call IdeagitNotify()
  autocmd BufWritePost * call rpcnotify(0, 'ideagit_saved')
augroup END
call timer_start(1500, {-> IdeagitNotify()})`, false)
	go func() {
		for fn := range e.send {
			fn(v)
		}
	}()
	return nil
}

func (e *Editor) post(ev editorEvent) {
	select {
	case e.events <- ev:
	default:
	}
}

// Wait blocks until nvim has something new to show; re-issue it after each event.
func (e *Editor) Wait() tea.Cmd {
	ep := epoch.Load()
	return func() tea.Msg {
		ev := <-e.events
		return applyMsg{ep, func(a *App) tea.Cmd { return a.onEditorEvent(ev) }}
	}
}

func (e *Editor) do(fn func(v *nvim.Nvim)) {
	e.mu.Lock()
	ok := e.running
	e.mu.Unlock()
	if ok {
		select {
		case e.send <- fn:
		default:
		}
	}
}

func vimString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func (e *Editor) Open(path string, line int) {
	e.do(func(v *nvim.Nvim) {
		src := fmt.Sprintf("execute 'edit' fnameescape(%s)", vimString(path))
		if line > 0 {
			src += fmt.Sprintf(" | call cursor(%d, 1) | normal! zz", line)
		}
		v.Exec(src, false)
	})
}

func (e *Editor) SwitchTo(path string) {
	e.do(func(v *nvim.Nvim) { v.Exec(fmt.Sprintf("execute 'buffer' fnameescape(%s)", vimString(path)), false) })
}

func (e *Editor) CloseBuffer(path string) {
	e.do(func(v *nvim.Nvim) {
		// :bdelete refuses modified buffers; nvim shows the error in its own cmdline.
		v.Exec(fmt.Sprintf("execute 'confirm bdelete' fnameescape(%s)", vimString(path)), false)
	})
}

func (e *Editor) Input(keys string) {
	if keys == "" {
		return
	}
	e.do(func(v *nvim.Nvim) { v.Input(keys) })
}

func (e *Editor) Paste(text string) {
	e.do(func(v *nvim.Nvim) { v.Paste(text, true, -1) })
}

func (e *Editor) Mouse(button, action, mods string, row, col int) {
	e.do(func(v *nvim.Nvim) { v.InputMouse(button, action, mods, 0, row, col) })
}

func (e *Editor) Resize(w, h int) {
	e.mu.Lock()
	if !e.running || (w == e.reqW && h == e.reqH) || w < 10 || h < 3 {
		e.mu.Unlock()
		return
	}
	e.reqW, e.reqH = w, h
	e.mu.Unlock()
	e.do(func(v *nvim.Nvim) { v.TryResizeUI(w, h) })
}

// ModifiedCount asks nvim how many buffers have unsaved changes.
func (e *Editor) ModifiedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, b := range e.bufs {
		if b.modified {
			n++
		}
	}
	return n
}

func (e *Editor) Quit(save bool) {
	e.mu.Lock()
	v := e.v
	running := e.running
	e.mu.Unlock()
	if !running || v == nil {
		return
	}
	if save {
		v.Command("silent! wall")
	}
	v.Command("qall!")
}

// QuitIfIdle quits nvim without forcing, so an unsaved scratch buffer keeps it open.
func (e *Editor) QuitIfIdle() {
	e.do(func(v *nvim.Nvim) { v.Command("qall") })
}

// Lines returns each open file's last known cursor line, repo-relative.
func (e *Editor) Lines(a *App) map[string]int {
	bufs, _ := e.Buffers()
	res := map[string]int{}
	for _, b := range bufs {
		if rel := a.repoRel(b.path); rel != "" && b.line > 0 {
			res[rel] = b.line
		}
	}
	return res
}

func (e *Editor) Buffers() ([]editorBuf, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]editorBuf(nil), e.bufs...), e.current
}

func (e *Editor) onBufs(args []interface{}) {
	if len(args) < 2 {
		return
	}
	var bufs []editorBuf
	if list, ok := args[0].([]interface{}); ok {
		for _, it := range list {
			pair, ok := it.([]interface{})
			if !ok || len(pair) < 2 {
				continue
			}
			name, _ := pair[0].(string)
			// Plugin scratch buffers ("miniicons://…", terminals, help) aren't files.
			if !filepath.IsAbs(name) {
				continue
			}
			b := editorBuf{path: name, modified: toInt(pair[1]) != 0}
			if len(pair) >= 3 {
				b.line = toInt(pair[2])
			}
			bufs = append(bufs, b)
		}
	}
	cur, _ := args[1].(string)
	e.mu.Lock()
	if len(args) >= 3 {
		e.nativeTabs = toInt(args[2]) != 0
	}
	e.bufs = bufs
	for _, b := range bufs {
		if b.path == cur {
			e.current = cur
		}
	}
	empty := e.hadFiles && len(bufs) == 0
	if len(bufs) > 0 {
		e.hadFiles = true
	}
	e.mu.Unlock()
	if empty {
		e.post(evEmpty)
		return
	}
	e.post(evFlush)
}

func toInt(x interface{}) int {
	switch n := x.(type) {
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case uint32:
		return int(n)
	case int8:
		return int(n)
	case uint8:
		return int(n)
	case int16:
		return int(n)
	case uint16:
		return int(n)
	case float64:
		return int(n)
	case bool:
		if n {
			return 1
		}
	}
	return 0
}

func toColor(x interface{}) Color {
	n := toInt(x)
	if n < 0 {
		return NoColor
	}
	return Color(1<<24 | uint32(n)&0xFFFFFF)
}

func (e *Editor) redraw(updates [][]interface{}) {
	e.mu.Lock()
	flushed := false
	for _, u := range updates {
		if len(u) == 0 {
			continue
		}
		name, _ := u[0].(string)
		for _, raw := range u[1:] {
			args, _ := raw.([]interface{})
			switch name {
			case "grid_resize":
				if len(args) >= 3 {
					e.resizeGrid(toInt(args[1]), toInt(args[2]))
				}
			case "grid_clear":
				for i := range e.cells {
					e.cells[i] = gcell{text: " "}
				}
			case "grid_cursor_goto":
				if len(args) >= 3 {
					e.curRow, e.curCol = toInt(args[1]), toInt(args[2])
				}
			case "grid_line":
				e.gridLine(args)
			case "grid_scroll":
				e.gridScroll(args)
			case "default_colors_set":
				if len(args) >= 2 {
					e.defFg, e.defBg = toColor(args[0]), toColor(args[1])
				}
			case "hl_attr_define":
				if len(args) >= 2 {
					e.hl[toInt(args[0])] = parseHL(args[1])
				}
			case "mode_change":
				if len(args) >= 1 {
					e.mode, _ = args[0].(string)
				}
			case "flush":
				flushed = true
			}
		}
	}
	e.mu.Unlock()
	if flushed {
		e.post(evFlush)
	}
}

func parseHL(x interface{}) hlAttr {
	h := hlAttr{fg: NoColor, bg: NoColor}
	m, ok := x.(map[string]interface{})
	if !ok {
		return h
	}
	for k, v := range m {
		switch k {
		case "foreground":
			h.fg = toColor(v)
		case "background":
			h.bg = toColor(v)
		case "bold":
			h.bold = toInt(v) != 0
		case "italic":
			h.italic = toInt(v) != 0
		case "underline", "undercurl", "underdouble", "underdotted", "underdashed":
			h.underline = toInt(v) != 0 || h.underline
		case "strikethrough":
			h.strike = toInt(v) != 0
		case "reverse", "standout":
			h.rev = toInt(v) != 0 || h.rev
		}
	}
	return h
}

func (e *Editor) resizeGrid(w, h int) {
	cells := make([]gcell, w*h)
	for i := range cells {
		cells[i] = gcell{text: " "}
	}
	for y := 0; y < min(h, e.gh); y++ {
		for x := 0; x < min(w, e.gw); x++ {
			cells[y*w+x] = e.cells[y*e.gw+x]
		}
	}
	e.cells, e.gw, e.gh = cells, w, h
}

func (e *Editor) gridLine(args []interface{}) {
	if len(args) < 4 {
		return
	}
	row, col := toInt(args[1]), toInt(args[2])
	cells, _ := args[3].([]interface{})
	if row < 0 || row >= e.gh {
		return
	}
	hl := 0
	for _, c := range cells {
		cell, _ := c.([]interface{})
		if len(cell) == 0 {
			continue
		}
		text, _ := cell[0].(string)
		if len(cell) >= 2 {
			hl = toInt(cell[1])
		}
		repeat := 1
		if len(cell) >= 3 {
			repeat = toInt(cell[2])
		}
		for i := 0; i < repeat && col < e.gw; i++ {
			e.cells[row*e.gw+col] = gcell{text: text, hl: hl}
			col++
		}
	}
}

func (e *Editor) gridScroll(args []interface{}) {
	if len(args) < 7 {
		return
	}
	top, bot, left, right, rows := toInt(args[1]), toInt(args[2]), toInt(args[3]), toInt(args[4]), toInt(args[5])
	bot, right = min(bot, e.gh), min(right, e.gw)
	copyRow := func(dst, src int) {
		for x := left; x < right; x++ {
			e.cells[dst*e.gw+x] = e.cells[src*e.gw+x]
		}
	}
	if rows > 0 {
		for y := top; y < bot-rows; y++ {
			copyRow(y, y+rows)
		}
	} else if rows < 0 {
		for y := bot - 1; y >= top-rows; y-- {
			copyRow(y, y+rows)
		}
	}
}

// Draw paints nvim's grid; the cursor is shown only while the editor has focus.
func (e *Editor) Draw(a *App, v View, focused bool) {
	th := a.Th
	e.Resize(v.W(), v.H())
	e.mu.Lock()
	defer e.mu.Unlock()
	defFg, defBg := e.defFg, e.defBg
	if defFg == NoColor {
		defFg = th.Fg
	}
	if defBg == NoColor {
		defBg = th.Bg
	}
	v.Fill(Style{Fg: defFg, Bg: defBg})
	for y := 0; y < min(e.gh, v.H()); y++ {
		for x := 0; x < min(e.gw, v.W()); x++ {
			c := e.cells[y*e.gw+x]
			if c.text == "" {
				continue
			}
			h := e.hl[c.hl]
			fg, bg := h.fg, h.bg
			if fg == NoColor {
				fg = defFg
			}
			if bg == NoColor {
				bg = defBg
			}
			if h.rev {
				fg, bg = bg, fg
			}
			if focused && y == e.curRow && x == e.curCol {
				fg, bg = bg, fg
			}
			r := []rune(c.text)
			ch := ' '
			if len(r) > 0 {
				ch = r[0]
			}
			if RuneWidth(ch) == 0 {
				ch = ' '
			}
			v.Set(x, y, ch, Style{Fg: fg, Bg: bg, Bold: h.bold, Italic: h.italic, Underline: h.underline, Strike: h.strike})
		}
	}
}

// Settle waits until queued input has been delivered to nvim and it has had a moment
// to redraw; used by snapshot tests.
func (e *Editor) Settle(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for len(e.send) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(250 * time.Millisecond)
}

func (e *Editor) InsertMode() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.HasPrefix(e.mode, "insert")
}

// nvimKey converts a Bubble Tea key into Neovim's key notation.
func nvimKey(k tea.KeyMsg) string {
	if name, ok := chordOf(k); ok {
		return chordToNvim(name)
	}
	if k.Type == tea.KeyRunes {
		var b strings.Builder
		for _, r := range k.Runes {
			s := string(r)
			if r == '<' {
				s = "lt"
			}
			if k.Alt {
				b.WriteString("<M-" + s + ">")
			} else if r == '<' {
				b.WriteString("<lt>")
			} else {
				b.WriteString(s)
			}
		}
		return b.String()
	}
	s := keyName(k)
	mods := ""
	for {
		switch {
		case strings.HasPrefix(s, "alt+"):
			mods += "M-"
			s = s[4:]
			continue
		case strings.HasPrefix(s, "ctrl+"):
			mods += "C-"
			s = s[5:]
			continue
		case strings.HasPrefix(s, "shift+"):
			mods += "S-"
			s = s[6:]
			continue
		}
		break
	}
	names := map[string]string{
		"enter": "CR", "tab": "Tab", "backspace": "BS", "esc": "Esc", "delete": "Del", "insert": "Insert",
		"up": "Up", "down": "Down", "left": "Left", "right": "Right", "home": "Home", "end": "End",
		"pgup": "PageUp", "pgdown": "PageDown", " ": "Space", "@": "Space", "\\": "Bslash", "]": "]", "^": "^", "_": "_",
	}
	if n, ok := names[s]; ok {
		return "<" + mods + n + ">"
	}
	if strings.HasPrefix(s, "f") && len(s) > 1 {
		var n int
		if _, err := fmt.Sscanf(s, "f%d", &n); err == nil {
			if n > 12 && n <= 20 {
				return fmt.Sprintf("<%sS-F%d>", mods, n-12)
			}
			return fmt.Sprintf("<%sF%d>", mods, n)
		}
	}
	if len([]rune(s)) == 1 {
		if mods == "" {
			return s
		}
		return "<" + mods + s + ">"
	}
	return ""
}

// chordToNvim turns "ctrl+shift+k" into "<C-S-k>" and "shift+f6" into "<S-F6>".
func chordToNvim(name string) string {
	parts := strings.Split(name, "+")
	key := parts[len(parts)-1]
	if key == "" && len(parts) >= 2 { // "ctrl+kp+" splits into [..., "kp", ""]
		key = parts[len(parts)-2] + "+"
		parts = parts[:len(parts)-1]
	}
	mods := ""
	for _, m := range parts[:len(parts)-1] {
		mods += map[string]string{"ctrl": "C-", "alt": "M-", "shift": "S-"}[m]
	}
	switch {
	case key == "esc":
		key = "Esc"
	case key == "enter":
		key = "CR"
	case key == "insert":
		key = "Insert"
	case key == "kp+":
		key = "kPlus"
	case key == "kp-":
		key = "kMinus"
	case strings.HasPrefix(key, "f") && len(key) > 1:
		key = "F" + key[1:]
	case key == "<":
		key = "lt"
	}
	return "<" + mods + key + ">"
}

func displayPath(a *App, p string) string {
	if rel, err := filepath.Rel(a.Repo.Root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return relHome(p)
}
