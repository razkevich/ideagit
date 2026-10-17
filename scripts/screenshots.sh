#!/usr/bin/env bash
# Builds a throwaway demo project and renders README screenshots as SVG into docs/.
# Claude Code sessions are simulated with a renamed `sleep` and a fake $HOME.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d)
trap 'kill $(jobs -p) 2>/dev/null || true; chmod -R u+w "$T"; rm -rf "$T"' EXIT
mkdir -p "$T/bin"
go build -o "$T/ideagit" "$ROOT"
export HOME="$T/home" IDEAGIT_THEME=dark
mkdir -p "$HOME"

R="$HOME/code/acme-api"
mkdir -p "$HOME/code"
git init -q -b main "$R"
cd "$R"
git config user.name "Alex Razkevich"
git config user.email alex@example.com

commit() { # author, days-ago-spec, message
  GIT_AUTHOR_NAME="$1" GIT_AUTHOR_EMAIL="$(echo "$1" | tr 'A-Z ' 'a-z.')@example.com" \
  GIT_AUTHOR_DATE="$2" GIT_COMMITTER_DATE="$2" git commit -q -m "$3"
}
ago() { date -v-"$1" "+%Y-%m-%dT%H:%M:%S" 2>/dev/null || date -d "-${1/d/ days}" "+%Y-%m-%dT%H:%M:%S"; }

mkdir -p cmd/api internal/server internal/auth docs
cat > internal/server/server.go <<'GO'
package server

import (
	"fmt"
	"net/http"
	"time"
)

type Server struct {
	addr    string
	timeout time.Duration
	mux     *http.ServeMux
}

func New(addr string) *Server {
	return &Server{addr: addr, timeout: 5 * time.Second, mux: http.NewServeMux()}
}

func (s *Server) Routes() {
	s.mux.HandleFunc("/health", s.health)
	s.mux.HandleFunc("/users", s.users)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `[]`)
}

func (s *Server) Start() error {
	srv := &http.Server{Addr: s.addr, Handler: s.mux, ReadTimeout: s.timeout}
	return srv.ListenAndServe()
}
GO
cat > cmd/api/main.go <<'GO'
package main

import (
	"log"

	"acme/internal/server"
)

func main() {
	s := server.New(":8080")
	s.Routes()
	log.Fatal(s.Start())
}
GO
printf '# acme-api\n\nHTTP API for Acme.\n' > README.md
printf 'module acme\n\ngo 1.22\n' > go.mod
git add -A && commit "Alice Chen" "$(ago 9d)" "Initial HTTP server skeleton"
printf 'package auth\n\nfunc Check(token string) bool { return token != "" }\n' > internal/auth/auth.go
git add -A && commit "Bob Martin" "$(ago 7d)" "[API-12] Add token auth check"
git tag v1.0.0
git checkout -q -b fix/auth-timeout
printf 'package auth\n\nimport "time"\n\nconst Timeout = 3 * time.Second\n\nfunc Check(token string) bool { return token != "" }\n' > internal/auth/auth.go
git add -A && commit "Carol Diaz" "$(ago 6d)" "[API-19] Time out slow auth backends"
git checkout -q main
printf '# acme-api\n\nHTTP API for Acme.\n\n## Running\n\n    go run ./cmd/api\n' > README.md
git add -A && commit "Alice Chen" "$(ago 5d)" "docs: how to run locally"
GIT_AUTHOR_DATE="$(ago 4d)" GIT_COMMITTER_DATE="$(ago 4d)" git merge -q --no-ff fix/auth-timeout -m "Merge branch 'fix/auth-timeout'"
git tag v1.1.0
git checkout -q -b feature/rate-limit
mkdir -p internal/ratelimit
printf 'package ratelimit\n\ntype Bucket struct{ tokens, max int }\n' > internal/ratelimit/bucket.go
git add -A && commit "Alex Razkevich" "$(ago 3d)" "[API-31] Token bucket skeleton"
printf 'package ratelimit\n\ntype Bucket struct{ tokens, max int }\n\nfunc (b *Bucket) Take() bool {\n\tif b.tokens == 0 {\n\t\treturn false\n\t}\n\tb.tokens--\n\treturn true\n}\n' > internal/ratelimit/bucket.go
git add -A && commit "Alex Razkevich" "$(ago 2d)" "[API-31] Take() consumes a token"
git checkout -q main
printf 'package auth\n\nimport "time"\n\nconst Timeout = 2 * time.Second\n\nfunc Check(token string) bool { return token != "" }\n' > internal/auth/auth.go
git add -A && commit "Bob Martin" "$(ago 1d)" "[API-40] Lower auth timeout to 2s"
printf '\nSee docs/ for the API reference.\n' >> README.md
git add -A && commit "Carol Diaz" "$(ago 3H)" "docs: link API reference"

git init -q --bare "$T/origin.git"
git remote add origin "$T/origin.git"
git push -q origin main feature/rate-limit
git branch -q --set-upstream-to=origin/main main
printf '\n## Contributing\n\nOpen a PR.\n' >> README.md
git add -A && commit "Alex Razkevich" "$(ago 1H)" "docs: contributing section"
git branch -q release/v1.2 v1.1.0

# Worktrees: a Claude agent (working), a named Claude worktree (idle), and a merged sibling.
git worktree add -q .claude/worktrees/agent-a7f3c21 -b agent/rate-limit-tests feature/rate-limit
git worktree add -q .claude/worktrees/auth-metrics -b feature/auth-metrics
git worktree add -q "$HOME/code/acme-api-hotfix" fix/auth-timeout
printf '.claude/\n.idea/\n' > .git/info/exclude

# Local changes across two changelists.
perl -0pi -e 's/timeout: 5 \* time.Second/timeout: 10 * time.Second/; s/fmt.Fprintln\(w, "ok"\)/w.WriteHeader(http.StatusOK)\n\tfmt.Fprintln(w, "ok")/; s#s.mux.HandleFunc\("/users", s.users\)#s.mux.HandleFunc("/users", s.users)\n\ts.mux.HandleFunc("/metrics", s.metrics)#' internal/server/server.go
cat >> internal/server/server.go <<'GO'

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "uptime_seconds %d\n", int(time.Since(start).Seconds()))
}

var start = time.Now()
GO
printf 'package server\n\nimport "net/http"\n\nfunc Logging(next http.Handler) http.Handler { return next }\n' > internal/server/middleware.go
git add internal/server/middleware.go
printf '\nAPI reference: docs/api.md\n' >> README.md
printf 'todo: benchmark the limiter\n' > notes.txt
mkdir -p tmp && echo x > tmp/scratch.log
mkdir -p .git/ideagit
cat > .git/ideagit/changelists.json <<'JSON'
{"active":"Changes","lists":[{"name":"Changes","files":["internal/server/server.go","internal/server/middleware.go"]},{"name":"Docs","comment":"README polish","files":["README.md"]}]}
JSON

# Stashes and an IntelliJ shelf.
echo "// experimental" >> cmd/api/main.go
git stash push -q -m "wip: graceful shutdown" cmd/api/main.go
printf 'go 1.23\n' >> go.mod
git stash push -q -m "try go 1.23" go.mod
mkdir -p .idea/shelf/Retry_policy
cat > .idea/shelf/Retry_policy.xml <<XML
<changelist name="Retry_policy" date="$(( $(date +%s) * 1000 - 86400000 ))" recycled="false">
  <option name="PATH" value="\$PROJECT_DIR\$/.idea/shelf/Retry_policy/shelved.patch" />
  <option name="DESCRIPTION" value="Retry policy" />
</changelist>
XML
{ printf 'Index: internal/auth/auth.go\n===================================================================\n'
  printf 'diff --git a/internal/auth/auth.go b/internal/auth/auth.go\n--- a/internal/auth/auth.go\t(revision %s)\n+++ b/internal/auth/auth.go\t(date 1)\n' "$(git rev-parse HEAD)"
  printf '@@ -4,4 +4,6 @@\n \n const Timeout = 2 * time.Second\n \n+const Retries = 3\n+\n func Check(token string) bool { return token != "" }\n'; } > .idea/shelf/Retry_policy/shelved.patch

# Fake Claude Code sessions.
encode() { echo "$1" | sed 's/[^A-Za-z0-9-]/-/g'; }
session() { # dir, prompt, age-seconds
  local p="$HOME/.claude/projects/$(encode "$1")"
  mkdir -p "$p"
  printf '{"type":"user","message":{"content":"%s"}}\n' "$2" > "$p/s.jsonl"
  touch -t "$(date -r $(( $(date +%s) - $3 )) +%Y%m%d%H%M.%S 2>/dev/null || date -d "@$(( $(date +%s) - $3 ))" +%Y%m%d%H%M.%S)" "$p/s.jsonl"
  # argv[0] "claude" is what session detection matches; the process is plain sleep.
  (cd "$1" && exec -a claude sleep 600) &
}
session "$(cd .claude/worktrees/agent-a7f3c21 && pwd -P)" "Add table-driven tests for Bucket.Take, including the empty bucket case" 2
session "$(cd .claude/worktrees/auth-metrics && pwd -P)" "Expose auth latency as a Prometheus histogram" 420
sleep 1

shot() { # name, tab, keys
  "$T/ideagit" -snapshot 150x34 -tab "$2" -keys "$3" -svg "$ROOT/docs/$1.svg" "$R"
  echo "docs/$1.svg"
}
shot local-changes 0 "down"
shot commit 0 "ctrl+k"
shot log 3 "down"
shot worktrees 0 "W"
shot branches 0 "b"
shot stash 2 ""
shot diff 0 "down enter"
