package capture

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTranscriptRolesAndPaths(t *testing.T) {
	raw := `user: refactor auth.py to use dataclasses
assistant: sure, editing src/auth.py now
user: also add a test in tests/test_auth.py
system: done
3 files changed, 40 insertions(+), 12 deletions(-)`

	tr, err := ParseTranscript(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseTranscript: %v", err)
	}

	if len(tr.UserMessages) != 2 {
		t.Fatalf("expected 2 user messages, got %d: %v", len(tr.UserMessages), tr.UserMessages)
	}
	if !strings.Contains(tr.UserMessages[0], "refactor auth.py") {
		t.Errorf("first user message wrong: %q", tr.UserMessages[0])
	}
	if !containsAll(tr.Paths, "auth.py", "src/auth.py", "tests/test_auth.py") {
		t.Errorf("paths not all extracted: %v", tr.Paths)
	}
	if tr.DiffStat == "" || !strings.Contains(tr.DiffStat, "3 files changed") {
		t.Errorf("diff stat not captured: %q", tr.DiffStat)
	}
}

func TestParseTranscriptRolelessIsOneUserMessage(t *testing.T) {
	tr, err := ParseTranscript(strings.NewReader("fix the login bug in auth.go"))
	if err != nil {
		t.Fatalf("ParseTranscript: %v", err)
	}
	if len(tr.UserMessages) != 1 {
		t.Fatalf("roleless transcript should be 1 user message, got %d", len(tr.UserMessages))
	}
	if !containsAll(tr.Paths, "auth.go") {
		t.Errorf("expected auth.go path, got %v", tr.Paths)
	}
}

func TestExtractIsDeterministic(t *testing.T) {
	tr := Transcript{
		UserMessages: []string{"first", "second", "third"},
		Paths:        []string{"src/auth.py", "src/auth.py"},
		DiffStat:     "1 file changed, 2 insertions(+)",
	}
	a := Extract(tr, Options{Agent: "claude-code", Cwd: "/tmp"})
	b := Extract(tr, Options{Agent: "claude-code", Cwd: "/tmp"})

	if a.Summary != b.Summary {
		t.Errorf("summary not deterministic:\n%q\n%q", a.Summary, b.Summary)
	}
	if !reflect.DeepEqual(a.Tags, b.Tags) {
		t.Errorf("tags not deterministic: %v vs %v", a.Tags, b.Tags)
	}
	// ID/TS must be left for the store to assign (pure extraction).
	if a.ID != "" || a.TS != 0 {
		t.Errorf("Extract should not assign ID/TS, got ID=%q TS=%d", a.ID, a.TS)
	}
}

func TestExtractSummaryNewestFirstAndSignals(t *testing.T) {
	tr := Transcript{
		UserMessages: []string{"old message", "newest message"},
		Paths:        []string{"a/b.go"},
		DiffStat:     "1 file changed",
	}
	rec := Extract(tr, Options{})
	lines := strings.Split(rec.Summary, "\n")
	if lines[0] != "newest message" {
		t.Errorf("summary should lead with newest user message, got %q", lines[0])
	}
	if !strings.Contains(rec.Summary, "files: a/b.go") {
		t.Errorf("summary missing files line: %q", rec.Summary)
	}
	if !strings.Contains(rec.Summary, "diff: 1 file changed") {
		t.Errorf("summary missing diff line: %q", rec.Summary)
	}
}

func TestExtractTagsHavePathAndLangTokens(t *testing.T) {
	tr := Transcript{Paths: []string{"src/user_auth.py"}}
	rec := Extract(tr, Options{})

	if !containsAll(rec.Tags, "python") {
		t.Errorf("expected language tag 'python', got %v", rec.Tags)
	}
	// Path stem split on '_' should yield both "user" and "auth".
	if !containsAll(rec.Tags, "auth", "user") {
		t.Errorf("expected split path tokens auth+user, got %v", rec.Tags)
	}
	// Tags must be sorted (deterministic).
	if !isSorted(rec.Tags) {
		t.Errorf("tags not sorted: %v", rec.Tags)
	}
}

func TestExtractMaxUserMsgsBounded(t *testing.T) {
	msgs := []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7"}
	rec := Extract(Transcript{UserMessages: msgs}, Options{MaxUserMsgs: 2})
	// Only the two most recent messages, newest first.
	lines := strings.Split(rec.Summary, "\n")
	if len(lines) != 2 || lines[0] != "m7" || lines[1] != "m6" {
		t.Errorf("MaxUserMsgs=2 should keep m7,m6, got %v", lines)
	}
}

func TestExtractEmptyTranscriptDoesNotPanic(t *testing.T) {
	rec := Extract(Transcript{}, Options{})
	if rec.Summary != "" {
		t.Errorf("empty transcript should give empty summary, got %q", rec.Summary)
	}
	if rec.Tags == nil {
		t.Error("Tags should be non-nil empty slice, got nil")
	}
}

// --- helpers ---

func containsAll(haystack []string, wants ...string) bool {
	set := map[string]struct{}{}
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, w := range wants {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

func isSorted(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}
