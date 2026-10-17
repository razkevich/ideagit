package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/github"
	"github.com/razkevich/ideagit/internal/kitty"
	"github.com/razkevich/ideagit/internal/ui"
)

// Set by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = ""
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	prRef := flag.String("pr", "", "open the PRs tab: on your open PRs, or on this pull request (number, #number or URL)")
	snapshot := flag.String("snapshot", "", "render one frame as plain text (WxH, e.g. 160x45) and exit; for testing")
	tab := flag.Int("tab", -1, "with -snapshot: tab index to render")
	svg := flag.String("svg", "", "with -snapshot: write the frame as an SVG image to this file")
	keys := flag.String("keys", "", "with -snapshot: keys to send before rendering (space separated, e.g. \"down enter\")")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ideagit [dir]\n\nAn IntelliJ-style Git tool window for the terminal.\n\n")
		fmt.Fprintf(os.Stderr, "To land in the last worktree you switched to after quitting, add to ~/.zshrc:\n")
		fmt.Fprintf(os.Stderr, "  ig() { local f=$(mktemp); IDEAGIT_CD_FILE=$f ideagit \"$@\"; [ -s \"$f\" ] && cd \"$(cat \"$f\")\"; rm -f \"$f\"; }\n\n")
		flag.PrintDefaults()
	}
	os.Args = normalizePRFlag(os.Args)
	flag.Parse()
	if *showVersion {
		fmt.Println("ideagit", version, commit)
		return
	}
	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	repo, err := git.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ideagit:", err)
		os.Exit(1)
	}
	root := ui.NewRoot(repo)
	switch {
	case *prRef == prListOnly:
		root.InitialPR = -1
	case *prRef != "":
		n, ok := github.ParseRef(*prRef)
		if !ok {
			fmt.Fprintln(os.Stderr, "ideagit: --pr expects a number or a pull request URL")
			os.Exit(2)
		}
		root.InitialPR = n
	}

	if *snapshot != "" {
		var w, h int
		fmt.Sscanf(*snapshot, "%dx%d", &w, &h)
		os.Exit(runSnapshot(root, w, h, *tab, *keys, *svg))
	}

	// XTSHIFTESCAPE: ask the terminal (Ghostty, xterm) to report Shift+click to us
	// instead of using it for its own text selection.
	os.Stdout.WriteString("\x1b[>1s")
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion(), tea.WithReportFocus()}
	var p *tea.Program
	// Input always goes through the translator: it also names modified F-keys (Shift+F10)
	// that Bubble Tea's legacy table lacks. The kitty protocol itself, needed for bare
	// Shift and Ctrl+Shift chords, is only switched on where the terminal supports it.
	ui.KittyKeys = kitty.Supported()
	opts = append(opts, tea.WithInput(kitty.NewInput(os.Stdin, ui.KittyKeys, func() {
		if p != nil {
			p.Send(ui.DoubleShiftMsg{})
		}
	})))
	p = tea.NewProgram(root, opts...)
	_, err = p.Run()
	if ui.KittyKeys {
		os.Stdout.WriteString(kitty.Disable)
	}
	os.Stdout.WriteString("\x1b[>0s")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ideagit:", err)
		os.Exit(1)
	}
	if f := os.Getenv("IDEAGIT_CD_FILE"); f != "" && root.FinalDir != "" {
		os.WriteFile(f, []byte(root.FinalDir), 0o600)
	}
}

// runSnapshot drives the model synchronously, executing commands until it settles, then prints the frame.
func runSnapshot(root *ui.Root, w, h, tab int, keys, svg string) int {
	root.App.SnapshotMode(w, h)
	defer root.Shutdown()
	pump := func(cmd tea.Cmd) {
		queue := []tea.Cmd{cmd}
		deadline := time.Now().Add(10 * time.Second)
		for len(queue) > 0 && time.Now().Before(deadline) {
			c := queue[0]
			queue = queue[1:]
			if c == nil {
				continue
			}
			msg := runCmd(c)
			switch m := msg.(type) {
			case nil:
			case tea.BatchMsg:
				queue = append(queue, m...)
			default:
				if isTick(msg) {
					continue
				}
				_, next := root.Update(msg)
				queue = append(queue, next)
			}
		}
	}
	pump(root.Init())
	if tab >= 0 {
		root.App.SelectTab(tab)
	}
	for _, k := range splitKeys(keys) {
		if d, ok := k.(waitMsg); ok {
			time.Sleep(time.Duration(d))
			continue
		}
		// Hit regions are registered while rendering, so draw before each event.
		root.View()
		_, cmd := root.Update(k)
		pump(cmd)
		root.SettleEditor()
	}
	if svg != "" {
		th := root.App.Th
		if err := os.WriteFile(svg, []byte(root.App.Render().SVG(th.Fg, th.Bg)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Println(root.View())
	return 0
}

const prListOnly = "\x00list"

// normalizePRFlag lets a bare --pr (no number) mean "open the PRs tab", which the flag
// package can't express for a string flag.
func normalizePRFlag(args []string) []string {
	out := []string{args[0]}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--pr" || a == "-pr" {
			if i+1 < len(args) {
				if _, ok := github.ParseRef(args[i+1]); ok {
					out = append(out, a, args[i+1])
					i++
					continue
				}
			}
			out = append(out, "--pr="+prListOnly)
			continue
		}
		out = append(out, a)
	}
	return out
}

type waitMsg time.Duration

func runCmd(c tea.Cmd) (msg tea.Msg) {
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case m := <-done:
		return m
	case <-time.After(20 * time.Second):
		return nil
	}
}

func isTick(msg tea.Msg) bool {
	return fmt.Sprintf("%T", msg) == "ui.tickMsg"
}

func splitKeys(s string) []tea.Msg {
	var res []tea.Msg
	names := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "space": tea.KeySpace, "f7": tea.KeyF7, "ctrl+k": tea.KeyCtrlK, "ctrl+z": tea.KeyCtrlZ,
		"pgdown": tea.KeyPgDown, "pgup": tea.KeyPgUp, "shift+tab": tea.KeyShiftTab, "backspace": tea.KeyBackspace,
	}
	for _, k := range splitFields(s) {
		var x, y int
		if n, _ := fmt.Sscanf(k, "wait:%d", &x); n == 1 {
			res = append(res, waitMsg(time.Duration(x)*time.Millisecond))
			continue
		}
		if n, _ := fmt.Sscanf(k, "click:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			continue
		}
		var x2, y2 int
		if n, _ := fmt.Sscanf(k, "drag:%d,%d,%d,%d", &x, &y, &x2, &y2); n == 4 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
				tea.MouseMsg{X: x2, Y: y2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
				tea.MouseMsg{X: x2, Y: y2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
			continue
		}
		if n, _ := fmt.Sscanf(k, "sdrag:%d,%d,%d,%d", &x, &y, &x2, &y2); n == 4 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Shift: true, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
				tea.MouseMsg{X: x2, Y: y2, Shift: true, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
				tea.MouseMsg{X: x2, Y: y2, Shift: true, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
			continue
		}
		if n, _ := fmt.Sscanf(k, "wheeldown:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
			continue
		}
		if n, _ := fmt.Sscanf(k, "sclick:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Shift: true, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
				tea.MouseMsg{X: x, Y: y, Shift: true, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
			continue
		}
		if n, _ := fmt.Sscanf(k, "rclick:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
			continue
		}
		if strings.HasPrefix(k, "alt+") && len(k) == 5 {
			res = append(res, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k[4:]), Alt: true})
			continue
		}
		if t, ok := names[k]; ok {
			res = append(res, tea.KeyMsg{Type: t})
		} else {
			res = append(res, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
	return res
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
