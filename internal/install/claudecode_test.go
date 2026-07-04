package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClaudeConfig seeds a sandboxed ~/.claude/settings.json and returns its path.
func writeClaudeConfig(t *testing.T, home, body string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// TestClaudeDetect finds Claude Code by its config directory and misses when absent.
func TestClaudeDetect(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	if found, _ := (claudeCodeInstaller{}).Detect(cfg); found {
		t.Error("Detect should be false before ~/.claude exists")
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, where := claudeCodeInstaller{}.Detect(cfg)
	if !found {
		t.Fatalf("Detect should find ~/.claude, where=%q", where)
	}
	if !strings.Contains(where, ".claude") {
		t.Errorf("Detect hint = %q, want a path mentioning .claude", where)
	}
}

// TestClaudeWireFreshConfig wires into a config dir that has no settings.json yet.
func TestClaudeWireFreshConfig(t *testing.T) {
	home := t.TempDir()
	path := writeClaudeConfig(t, home, "") // dir only, no file
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	res := claudeCodeInstaller{}.Wire(cfg)
	if res.Status != StatusWired {
		t.Fatalf("Wire status = %v (%v), want StatusWired", res.Status, res.Err)
	}
	if res.Path != path {
		t.Errorf("Path = %q, want %q", res.Path, path)
	}

	s := readSettings(t, path)
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing/not an object: %#v", s["hooks"])
	}
	if hooks["SessionStart"] == nil || hooks["Stop"] == nil {
		t.Fatalf("both SessionStart and Stop hooks must be present: %#v", hooks)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), " recall") {
		t.Error("SessionStart hook should call recall")
	}
	if !strings.Contains(string(raw), "capture --agent claude-code") {
		t.Error("Stop hook should call capture --agent claude-code")
	}
}

// TestClaudeWirePreservesForeignConfig is the core safety guarantee: automem's
// merge must not lose any pre-existing setting or user hook.
func TestClaudeWirePreservesForeignConfig(t *testing.T) {
	home := t.TempDir()
	body := `{
	  "theme": "dark",
	  "permissions": {"allow": ["Bash(ls)"]},
	  "hooks": {
	    "SessionStart": [{"hooks": [{"type": "command", "command": "echo user-hook"}]}]
	  }
	}`
	path := writeClaudeConfig(t, home, body)
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	if res := (claudeCodeInstaller{}).Wire(cfg); res.Status != StatusWired {
		t.Fatalf("Wire status = %v (%v)", res.Status, res.Err)
	}

	raw, _ := os.ReadFile(path)
	str := string(raw)
	for _, want := range []string{"dark", "Bash(ls)", "echo user-hook", "recall", "capture --agent claude-code"} {
		if !strings.Contains(str, want) {
			t.Errorf("merged settings dropped %q:\n%s", want, str)
		}
	}
	// The result must still be valid JSON.
	readSettings(t, path)
}

// TestClaudeWireIdempotent leaves an existing automem wiring untouched (no Force).
func TestClaudeWireIdempotent(t *testing.T) {
	home := t.TempDir()
	writeClaudeConfig(t, home, "{}")
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	if res := (claudeCodeInstaller{}).Wire(cfg); res.Status != StatusWired {
		t.Fatalf("first Wire status = %v", res.Status)
	}
	res2 := claudeCodeInstaller{}.Wire(cfg)
	if res2.Status != StatusAlreadyWired {
		t.Errorf("second Wire status = %v, want StatusAlreadyWired", res2.Status)
	}
}

// TestClaudeWireForceRefreshNoDuplicate re-wires with Force and must not
// duplicate automem's hooks.
func TestClaudeWireForceRefreshNoDuplicate(t *testing.T) {
	home := t.TempDir()
	path := writeClaudeConfig(t, home, "{}")
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	if res := (claudeCodeInstaller{}).Wire(cfg); res.Status != StatusWired {
		t.Fatalf("initial Wire status = %v", res.Status)
	}
	cfg.Force = true
	if res := (claudeCodeInstaller{}).Wire(cfg); res.Status != StatusWired {
		t.Fatalf("force Wire status = %v", res.Status)
	}
	raw, _ := os.ReadFile(path)
	// Exactly two markers: one SessionStart, one Stop — not four.
	if n := strings.Count(string(raw), claudeMarker); n != 2 {
		t.Errorf("after force refresh want 2 automem markers, got %d:\n%s", n, raw)
	}
}

// TestClaudeWireDryRun writes nothing.
func TestClaudeWireDryRun(t *testing.T) {
	home := t.TempDir()
	path := writeClaudeConfig(t, home, "") // dir only
	cfg := Config{Home: home, BinPath: "/opt/automem", DryRun: true}

	res := claudeCodeInstaller{}.Wire(cfg)
	if res.Status != StatusWouldWire {
		t.Fatalf("dry-run status = %v, want StatusWouldWire", res.Status)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote settings.json at %s", path)
	}
}

// TestClaudeWireRejectsMalformedConfig refuses to overwrite a settings.json that
// isn't valid JSON — clobbering a user's hand-edited config would be worse than
// failing loudly.
func TestClaudeWireRejectsMalformedConfig(t *testing.T) {
	home := t.TempDir()
	path := writeClaudeConfig(t, home, "{ this is not json ")
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	res := claudeCodeInstaller{}.Wire(cfg)
	if res.Status != StatusFailed {
		t.Fatalf("Wire over malformed JSON status = %v, want StatusFailed", res.Status)
	}
	// The original bytes must be untouched.
	raw, _ := os.ReadFile(path)
	if string(raw) != "{ this is not json " {
		t.Errorf("malformed config was modified: %q", raw)
	}
}

// TestClaudeRegisteredInRegistry confirms the installer self-registers via init,
// so `automem install` considers Claude Code.
func TestClaudeRegisteredInRegistry(t *testing.T) {
	found := false
	for _, a := range registry {
		if a.Name() == "claude-code" {
			found = true
			break
		}
	}
	if !found {
		t.Error("claude-code installer not registered; init() should register it")
	}
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, raw)
	}
	return s
}
