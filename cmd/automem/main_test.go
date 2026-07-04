package main

import (
	"bytes"
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
