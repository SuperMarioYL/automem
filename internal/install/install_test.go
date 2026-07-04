package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReport renders the summary for a mix of outcomes.
func TestReport(t *testing.T) {
	results := []Result{
		{Agent: "claude-code", Status: StatusWired},
		{Agent: "aider", Status: StatusWired, Unverified: true},
		{Agent: "cursor", Status: StatusNotFound},
	}
	got := Report(results)
	want := "wired claude-code ✓, aider ✓ (unverified)"
	if got != want {
		t.Fatalf("Report() = %q, want %q", got, want)
	}
}

// TestReportNoAgents falls back to a clear "nothing detected" line.
func TestReportNoAgents(t *testing.T) {
	results := []Result{
		{Agent: "claude-code", Status: StatusNotFound},
		{Agent: "aider", Status: StatusNotFound},
	}
	got := Report(results)
	if !strings.Contains(got, "no supported agents detected") {
		t.Fatalf("Report() = %q, want a no-agents line", got)
	}
	if !strings.Contains(got, "claude-code") || !strings.Contains(got, "aider") {
		t.Fatalf("no-agents line should list what was looked for: %q", got)
	}
}

// TestGlyph maps each status to the right marker.
func TestGlyph(t *testing.T) {
	cases := map[Status]string{
		StatusWired:        "✓",
		StatusAlreadyWired: "✓",
		StatusWouldWire:    "✓",
		StatusNotFound:     "•",
		StatusFailed:       "✗",
	}
	for status, want := range cases {
		if got := (Result{Status: status}).Glyph(); got != want {
			t.Errorf("Glyph(%v) = %q, want %q", status, got, want)
		}
	}
}

// TestWriteFileAtomic writes, then verifies mode and content, then overwrites.
func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.sh")

	if err := writeFileAtomic(path, []byte("first\n"), 0o755); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after write: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("mode = %o, want 0755", perm)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "first\n" {
		t.Errorf("content = %q, want %q", data, "first\n")
	}

	// Overwrite must fully replace, not append.
	if err := writeFileAtomic(path, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic overwrite: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "second\n" {
		t.Errorf("after overwrite content = %q, want %q", data, "second\n")
	}
	// No stray temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".automem-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// TestFileContains covers the idempotency probe.
func TestFileContains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	os.WriteFile(path, []byte("hello marker world"), 0o644)

	if !fileContains(path, "marker") {
		t.Error("fileContains should find present needle")
	}
	if fileContains(path, "absent") {
		t.Error("fileContains should not find absent needle")
	}
	if fileContains(filepath.Join(dir, "missing"), "x") {
		t.Error("fileContains on a missing file should be false, not panic")
	}
}

// TestRunUnsupportedOS can't change runtime.GOOS, but Supported() must be true
// on the platforms we build/test on (darwin/linux CI), so Run should not bail
// with the unsupported-OS error there.
func TestRunResolvesEnv(t *testing.T) {
	if !Supported() {
		t.Skip("test host is not a supported OS")
	}
	home := t.TempDir()
	t.Setenv("AUTOMEM_HOME", home)
	t.Setenv("AUTOMEM_BIN", "/opt/automem")

	results, err := Run(Config{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The registry always has at least the aider installer.
	if len(results) == 0 {
		t.Fatalf("Run returned no results; registry empty?")
	}
	names := map[string]bool{}
	for _, r := range results {
		names[r.Agent] = true
	}
	if !names["aider"] {
		t.Errorf("expected aider in results, got %v", names)
	}
}

// TestBinaryPathHonorsOverride checks the $AUTOMEM_BIN escape hatch.
func TestBinaryPathHonorsOverride(t *testing.T) {
	t.Setenv("AUTOMEM_BIN", "/custom/automem")
	got, err := BinaryPath()
	if err != nil {
		t.Fatalf("BinaryPath: %v", err)
	}
	if got != "/custom/automem" {
		t.Errorf("BinaryPath() = %q, want /custom/automem", got)
	}
}
