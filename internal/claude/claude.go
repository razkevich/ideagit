// Package claude detects Claude Code sessions working inside directories.
package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Session struct {
	PID int
	Cwd string
}

type Activity struct {
	Open        int
	LastWrite   time.Time
	LastPrompt  string
	Transcripts int
}

// Working reports whether a session is mid-turn (transcript written very recently).
func (a Activity) Working() bool { return a.Open > 0 && time.Since(a.LastWrite) < 20*time.Second }

// Running returns live Claude Code CLI processes with their working directories.
func Running() []Session {
	if runtime.GOOS == "linux" {
		return runningProc()
	}
	out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil
	}
	var pids []string
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && isClaude(f[1:]) {
			pids = append(pids, f[0])
		}
	}
	if len(pids) == 0 {
		return nil
	}
	lo, err := exec.Command("lsof", "-a", "-p", strings.Join(pids, ","), "-d", "cwd", "-Fpn").Output()
	if err != nil && len(lo) == 0 {
		return nil
	}
	var res []Session
	cur := 0
	for _, l := range strings.Split(string(lo), "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			cur, _ = strconv.Atoi(l[1:])
		case strings.HasPrefix(l, "n") && cur != 0:
			res = append(res, Session{PID: cur, Cwd: l[1:]})
		}
	}
	return res
}

// runningProc reads /proc directly so Linux doesn't need lsof.
func runningProc() []Session {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var res []Session
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if !isClaude(args) {
			continue
		}
		cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		if err == nil {
			res = append(res, Session{PID: pid, Cwd: cwd})
		}
	}
	return res
}

// isClaude matches the native `claude` binary and npm installs (`node …/claude-code/cli.js`).
func isClaude(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if filepath.Base(args[0]) == "claude" {
		// Background helpers (bg-pty-host, bg-spare) aren't user sessions.
		return len(args) < 2 || !strings.HasPrefix(args[1], "bg-")
	}
	if strings.HasPrefix(filepath.Base(args[0]), "node") && len(args) > 1 {
		return strings.Contains(args[1], "@anthropic-ai/claude-code") || filepath.Base(args[1]) == "claude"
	}
	return false
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9-]`)

// ProjectDir is where Claude Code keeps transcripts for sessions started in dir.
func ProjectDir(dir string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects", unsafe.ReplaceAllString(dir, "-"))
}

// ActivityFor summarizes sessions whose cwd is dir or below it, excluding nested
// directories listed in exclude (other worktrees living inside this one).
func ActivityFor(dir string, sessions []Session, exclude []string) Activity {
	var a Activity
	var cwds []string
	for _, s := range sessions {
		if !within(s.Cwd, dir) {
			continue
		}
		skip := false
		for _, e := range exclude {
			// Only worktrees nested inside dir steal its sessions, not enclosing ones.
			if e != dir && within(e, dir) && within(s.Cwd, e) {
				skip = true
			}
		}
		if !skip {
			a.Open++
			cwds = append(cwds, s.Cwd)
		}
	}
	dirs := []string{ProjectDir(dir)}
	for _, c := range cwds {
		if c != dir {
			dirs = append(dirs, ProjectDir(c))
		}
	}
	var newest string
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			a.Transcripts++
			if fi, err := e.Info(); err == nil && fi.ModTime().After(a.LastWrite) {
				a.LastWrite = fi.ModTime()
				newest = filepath.Join(d, e.Name())
			}
		}
	}
	if newest != "" {
		a.LastPrompt = lastPrompt(newest)
	}
	return a
}

func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}

func lastPrompt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const tail = 256 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		f.Seek(fi.Size()-tail, io.SeekStart)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	last := ""
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line[:min(len(line), 200)]), `"type":"user"`) {
			continue
		}
		var m struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &m) != nil || m.Type != "user" || m.IsMeta {
			continue
		}
		if t := contentText(m.Message.Content); t != "" {
			last = t
		}
	}
	return last
}

func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return clean(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				return clean(p.Text)
			}
		}
	}
	return ""
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") {
		// Command wrappers, hook output and system reminders aren't prompts.
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
