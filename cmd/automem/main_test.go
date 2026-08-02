package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the automem root command with the given args and stdin, using a
// fresh command tree each time, and returns stdout. It fails the test on error.
func run(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("automem %v: %v\noutput:\n%s", args, err, out.String())
	}
	return out.String()
}

// TestM1RoundTrip is the milestone's acceptance test: pipe a fake transcript
// into capture, recall a relevant query out, and watch stats increment the
// injected counter — all against an isolated store dir.
func TestM1RoundTrip(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())

	transcript := `user: refactor auth.py to use dataclasses
assistant: editing src/auth.py
2 files changed, 30 insertions(+), 5 deletions(-)`

	// 1. capture
	capOut := run(t, transcript, "capture", "--agent", "claude-code")
	if !strings.Contains(capOut, "captured") {
		t.Fatalf("capture output unexpected: %q", capOut)
	}

	// Capture a second, unrelated session so recall has to discriminate.
	run(t, "user: write the deployment kubernetes yaml", "capture", "--agent", "aider")

	// 2. stats before recall: 2 stored, 0 injected
	pre := run(t, "", "stats")
	if !strings.HasPrefix(pre, "2 stored, 0 injected") {
		t.Fatalf("pre-recall stats wrong: %q", firstLine(pre))
	}

	// 3. recall the auth query — should surface the auth session
	recOut := run(t, "", "recall", "what did we decide about auth?")
	if !strings.Contains(recOut, "auth.py") {
		t.Fatalf("recall did not surface the auth memory:\n%s", recOut)
	}
	if strings.Contains(recOut, "kubernetes") {
		t.Fatalf("recall surfaced the irrelevant kubernetes memory:\n%s", recOut)
	}

	// 4. stats after recall: injected incremented
	post := run(t, "", "stats")
	if !strings.HasPrefix(post, "2 stored, 1 injected") {
		t.Fatalf("post-recall stats should show 1 injected, got: %q", firstLine(post))
	}
}

func TestRecallNoMarkDoesNotIncrement(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())
	run(t, "user: fix the auth bug in auth.go", "capture")

	run(t, "", "recall", "--no-mark", "auth")
	post := run(t, "", "stats")
	if !strings.HasPrefix(post, "1 stored, 0 injected") {
		t.Fatalf("--no-mark should leave injected at 0, got: %q", firstLine(post))
	}
}

func TestRecallEmptyStore(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())
	out := run(t, "", "recall", "anything")
	if !strings.Contains(out, "no relevant memories") {
		t.Errorf("empty-store recall should say so, got %q", out)
	}
}

func TestStatsEmptyStore(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())
	out := run(t, "", "stats")
	if !strings.HasPrefix(out, "0 stored, 0 injected") {
		t.Errorf("empty-store stats wrong: %q", out)
	}
}

func TestCaptureFromStdinQueryFromStdin(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())
	// roleless transcript on stdin
	run(t, "improve error handling in server.go", "capture")
	// query on stdin (no positional arg)
	out := run(t, "server error handling", "recall")
	if !strings.Contains(out, "server.go") {
		t.Errorf("recall with stdin query should surface server.go, got:\n%s", out)
	}
}

// TestCaptureReadsTranscriptFromHookPayload exercises the Claude Code Stop hook
// path (fix-cc-hooks-read-json-stdin-as-text): the hook pipes a JSON metadata
// object on stdin (with transcript_path), and capture must read that file as
// the transcript rather than parse the JSON blob as a roleless transcript.
func TestCaptureReadsTranscriptFromHookPayload(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())

	// Write a transcript file the way a Claude Code session would leave one.
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	transcript := "user: refactor auth.py to use dataclasses\n2 files changed, 30 insertions(+), 5 deletions(-)"
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	// The JSON payload Claude Code pipes on stdin (no positional arg).
	payload := fmt.Sprintf(`{"session_id":"abc","transcript_path":%q,"cwd":%q,"hook_event_name":"Stop"}`, transcriptPath, dir)
	capOut := run(t, payload, "capture", "--agent", "claude-code")
	if !strings.Contains(capOut, "captured") {
		t.Fatalf("capture from hook payload failed: %q", capOut)
	}

	// The stored summary must reflect the transcript file's content, not the
	// JSON payload (which has no "user:" role line). Prove it by recalling the
	// auth path token that only the transcript file carries.
	stats := run(t, "", "stats")
	if !strings.HasPrefix(stats, "1 stored, 0 injected") {
		t.Fatalf("stats after capture: %q", stats)
	}
	recOut := run(t, "auth", "recall", "--no-mark")
	if !strings.Contains(recOut, "auth.py") {
		t.Fatalf("recall should surface the transcript's auth.py, got:\n%s", recOut)
	}
	if strings.Contains(recOut, "session_id") {
		t.Fatalf("capture stored the hook JSON blob instead of the transcript:\n%s", recOut)
	}
}

// TestRecallUsesCwdFromHookPayload exercises the Claude Code SessionStart hook
// path (fix-cc-hooks-read-json-stdin-as-text): the hook pipes a JSON metadata
// object on stdin (with cwd), and recall must use cwd-derived tokens as the
// query and surface records captured in the same project via the record's Cwd.
func TestRecallUsesCwdFromHookPayload(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())

	// Capture a record in a specific project cwd.
	projectDir := "/Users/example/workspace/myproject"
	run(t, "user: refactor auth.py", "capture", "--agent", "claude-code", "--cwd", projectDir)

	// SessionStart hook pipes JSON with cwd; recall uses cwd tokens as query.
	payload := fmt.Sprintf(`{"session_id":"abc","transcript_path":"/tmp/x.jsonl","cwd":%q,"hook_event_name":"SessionStart"}`, projectDir)
	out := run(t, payload, "recall")
	if strings.Contains(out, "no relevant memories") {
		t.Fatalf("recall via hook cwd should surface the same-project record, got:\n%s", out)
	}
	if !strings.Contains(out, "auth.py") {
		t.Fatalf("recall via hook cwd should surface the auth.py record, got:\n%s", out)
	}
}

// TestRecallPlainStdinStillWorks ensures the JSON-detection branch doesn't
// break the pre-v0.2.0 behaviour: a plain-text query on stdin is used verbatim.
func TestRecallPlainStdinStillWorks(t *testing.T) {
	t.Setenv("AUTOMEM_DIR", t.TempDir())
	run(t, "user: wire up the postgres connection pool", "capture")
	out := run(t, "postgres connection", "recall", "--no-mark")
	if !strings.Contains(out, "postgres") {
		t.Fatalf("plain-text stdin query should still work, got:\n%s", out)
	}
}

// TestPaidTierStubs checks that sync and team return the paid-tier hint (not an
// error) — the local substrate is fully usable, the paid features just aren't
// free, and each invocation is a demand signal.
func TestPaidTierStubs(t *testing.T) {
	for _, name := range []string{"sync", "team"} {
		out := run(t, "", name)
		if !strings.Contains(out, "requires automem cloud") {
			t.Errorf("%s should print the paid-tier hint, got %q", name, out)
		}
	}
}

// TestInstallRunsAndReports checks that `automem install` runs to completion
// against a sandboxed home and prints a summary line, without touching the real
// ~/.claude or ~/.local/bin. Which agents are detected depends on the machine,
// so we assert on the shape of the output, not a specific agent.
func TestInstallRunsAndReports(t *testing.T) {
	t.Setenv("AUTOMEM_HOME", t.TempDir())
	t.Setenv("AUTOMEM_BIN", "/usr/local/bin/automem")
	out := run(t, "", "install")
	if strings.TrimSpace(out) == "" {
		t.Fatalf("install printed nothing")
	}
	// Every run prints either a "wired …" summary or an explicit no-agents line.
	if !strings.Contains(out, "wired") && !strings.Contains(out, "no supported agents") {
		t.Errorf("install summary unexpected: %q", firstLine(out))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
