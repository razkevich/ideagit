package claude

import "testing"

func TestIsClaude(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"/Users/x/.local/bin/claude"}, true},
		{[]string{"claude", "--resume"}, true},
		{[]string{"claude", "bg-pty-host"}, false},
		{[]string{"node", "/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js"}, true},
		{[]string{"node", "server.js"}, false},
		{[]string{"vim", "claude"}, false},
	}
	for _, c := range cases {
		if got := isClaude(c.args); got != c.want {
			t.Errorf("isClaude(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestProjectDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	if got := ProjectDir("/home/u/code/chat/.claude/worktrees/a"); got != "/home/u/.claude/projects/-home-u-code-chat--claude-worktrees-a" {
		t.Fatal(got)
	}
}
