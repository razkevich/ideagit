package ui

import (
	"encoding/base64"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func runWithInput(argv []string, input string) error {
	if _, err := exec.LookPath(argv[0]); err != nil {
		return err
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin = strings.NewReader(input)
	return c.Run()
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// OpenShell suspends the UI and runs an interactive shell in dir.
func (a *App) OpenShell(dir string) tea.Cmd {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	c := exec.Command(sh)
	c.Dir = dir
	c.Env = append(os.Environ(), "IDEAGIT_SHELL=1")
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return apply(func(a *App) tea.Cmd { return tea.Batch(a.Refresh(), a.reloadLogs(), enableKittyKeys()) })
	})
}

func (a *App) OpenInIDE(dir string) tea.Cmd {
	return func() tea.Msg {
		for _, bin := range []string{"idea", "goland"} {
			if _, err := exec.LookPath(bin); err == nil {
				if err := exec.Command(bin, dir).Start(); err == nil {
					return apply(func(a *App) tea.Cmd { a.Notify("Opening in "+bin, relHome(dir)); return nil })
				}
			}
		}
		err := exec.Command("open", "-a", "IntelliJ IDEA", dir).Run()
		return apply(func(a *App) tea.Cmd {
			if err != nil {
				a.Error("Cannot open IntelliJ", "Install the 'idea' command-line launcher (Tools › Create Command-line Launcher)")
			}
			return nil
		})
	}
}

func writeFile(path string, b []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, b, mode)
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
