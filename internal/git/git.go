package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ConsoleEntry struct {
	Time   time.Time
	Cmd    string
	Output string
	Failed bool
}

type Repo struct {
	Root   string
	GitDir string
	Name   string

	mu           sync.Mutex
	console      []ConsoleEntry
	consoleTotal int
	mainRoot     string
}

func Open(dir string) (*Repo, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel", "--absolute-git-dir").Output()
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository", dir)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, errors.New("unexpected rev-parse output")
	}
	return &Repo{Root: lines[0], GitDir: lines[1], Name: filepath.Base(lines[0])}, nil
}

type runOpts struct {
	stdin   string
	env     []string
	logged  bool
	allowRC []int
}

func (r *Repo) run(o runOpts, args ...string) (string, error) {
	full := append([]string{"-c", "core.quotepath=false", "-c", "color.ui=false", "-c", "log.showSignature=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "LC_ALL=C")
	if !o.logged {
		cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	}
	cmd.Env = append(cmd.Env, o.env...)
	if o.stdin != "" {
		cmd.Stdin = strings.NewReader(o.stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			for _, rc := range o.allowRC {
				if ee.ExitCode() == rc {
					err = nil
				}
			}
		}
	}
	if o.logged {
		text := strings.TrimRight(stdout.String()+stderr.String(), "\n")
		r.mu.Lock()
		r.console = append(r.console, ConsoleEntry{Time: time.Now(), Cmd: "git " + strings.Join(args, " "), Output: text, Failed: err != nil})
		r.consoleTotal++
		if len(r.console) > 2000 {
			r.console = r.console[len(r.console)-2000:]
		}
		r.mu.Unlock()
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

func (r *Repo) Read(args ...string) (string, error) { return r.run(runOpts{}, args...) }

func (r *Repo) Exec(args ...string) (string, error) { return r.run(runOpts{logged: true}, args...) }

func (r *Repo) ExecInput(stdin string, args ...string) (string, error) {
	return r.run(runOpts{logged: true, stdin: stdin}, args...)
}

func (r *Repo) Console() []ConsoleEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ConsoleEntry(nil), r.console...)
}

// ConsoleSince returns entries logged after the first seen ones, and the new total.
// Old entries are trimmed, so seen counts everything ever logged.
func (r *Repo) ConsoleSince(seen int) ([]ConsoleEntry, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	first := r.consoleTotal - len(r.console)
	from := max(0, seen-first)
	if from >= len(r.console) {
		return nil, r.consoleTotal
	}
	return append([]ConsoleEntry(nil), r.console[from:]...), r.consoleTotal
}

func (r *Repo) Abs(path string) string { return filepath.Join(r.Root, filepath.FromSlash(path)) }

func (r *Repo) Config(key string) string {
	out, _ := r.Read("config", "--get", key)
	return strings.TrimSpace(out)
}

func (r *Repo) HeadHash() string {
	out, err := r.Read("rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Show returns the content of path at rev; missing paths yield ("", false).
func (r *Repo) Show(rev, path string) ([]byte, bool) {
	cmd := exec.Command("git", "-c", "core.quotepath=false", "cat-file", "blob", rev+":"+path)
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return out, true
}

func (r *Repo) ReadWorktree(path string) ([]byte, bool) {
	p := r.Abs(path)
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t, err := os.Readlink(p)
		if err != nil {
			return nil, false
		}
		return []byte(t), true
	}
	if fi.IsDir() {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}

func (r *Repo) RevParse(rev string) (string, error) {
	out, err := r.Read("rev-parse", "--verify", "-q", rev)
	return strings.TrimSpace(out), err
}

func (r *Repo) UserName() string  { return r.Config("user.name") }
func (r *Repo) UserEmail() string { return r.Config("user.email") }

func (r *Repo) HasHooks() bool {
	hooksDir := r.Config("core.hooksPath")
	if hooksDir == "" {
		hooksDir = filepath.Join(r.GitDir, "hooks")
	} else if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(r.Root, hooksDir)
	}
	for _, h := range []string{"pre-commit", "commit-msg", "prepare-commit-msg", "post-commit"} {
		if fi, err := os.Stat(filepath.Join(hooksDir, h)); err == nil && fi.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// LogExternal records a non-git command (gh) in the Console.
func (r *Repo) LogExternal(cmd, output string, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.console = append(r.console, ConsoleEntry{Time: time.Now(), Cmd: cmd, Output: output, Failed: failed})
	r.consoleTotal++
	if len(r.console) > 2000 {
		r.console = r.console[len(r.console)-2000:]
	}
}
