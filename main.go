package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/ui"
)

func main() {
	snapshot := flag.String("snapshot", "", "render one frame as plain text (WxH, e.g. 160x45) and exit; for testing")
	tab := flag.Int("tab", 0, "with -snapshot: tab index to render")
	keys := flag.String("keys", "", "with -snapshot: keys to send before rendering (space separated, e.g. \"down enter\")")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ideagit [dir]\n\nAn IntelliJ-style Git tool window for the terminal.\n\n")
		fmt.Fprintf(os.Stderr, "To land in the last worktree you switched to after quitting, add to ~/.zshrc:\n")
		fmt.Fprintf(os.Stderr, "  ig() { local f=$(mktemp); IDEAGIT_CD_FILE=$f ideagit \"$@\"; [ -s \"$f\" ] && cd \"$(cat \"$f\")\"; rm -f \"$f\"; }\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
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

	if *snapshot != "" {
		var w, h int
		fmt.Sscanf(*snapshot, "%dx%d", &w, &h)
		os.Exit(runSnapshot(root, w, h, *tab, *keys))
	}

	p := tea.NewProgram(root, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "ideagit:", err)
		os.Exit(1)
	}
	if f := os.Getenv("IDEAGIT_CD_FILE"); f != "" && root.FinalDir != "" {
		os.WriteFile(f, []byte(root.FinalDir), 0o600)
	}
}

// runSnapshot drives the model synchronously, executing commands until it settles, then prints the frame.
func runSnapshot(root *ui.Root, w, h, tab int, keys string) int {
	root.App.SnapshotMode(w, h)
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
	root.App.SelectTab(tab)
	for _, k := range splitKeys(keys) {
		// Hit regions are registered while rendering, so draw before each event.
		root.View()
		_, cmd := root.Update(k)
		pump(cmd)
	}
	fmt.Println(root.View())
	return 0
}

func runCmd(c tea.Cmd) (msg tea.Msg) {
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case m := <-done:
		return m
	case <-time.After(3 * time.Second):
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
		"left": tea.KeyLeft, "right": tea.KeyRight, "space": tea.KeySpace, "f7": tea.KeyF7, "ctrl+k": tea.KeyCtrlK,
		"pgdown": tea.KeyPgDown, "pgup": tea.KeyPgUp, "shift+tab": tea.KeyShiftTab, "backspace": tea.KeyBackspace,
	}
	for _, k := range splitFields(s) {
		var x, y int
		if n, _ := fmt.Sscanf(k, "click:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			continue
		}
		if n, _ := fmt.Sscanf(k, "wheeldown:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
			continue
		}
		if n, _ := fmt.Sscanf(k, "rclick:%d,%d", &x, &y); n == 2 {
			res = append(res, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
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
