package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAiderWrapperScript checks the generated wrapper is a well-formed sh
// script that brackets aider with recall + capture and bakes the binary path in
// safely.
func TestAiderWrapperScript(t *testing.T) {
	script := aiderWrapperScript("/usr/local/bin/automem")

	mustContain := []string{
		"#!/bin/sh",
		aiderWrapperMarker,
		"AUTOMEM='/usr/local/bin/automem'",
		"recall",
		"capture --agent aider",
		"aider \"$@\"",
		"exit \"$STATUS\"",
	}
	for _, want := range mustContain {
		if !strings.Contains(script, want) {
			t.Errorf("wrapper script missing %q\n---\n%s", want, script)
		}
	}
}

// TestShellSingleQuote escapes embedded single quotes so a weird binary path
// can't break out of the quoting and inject shell.
func TestShellSingleQuote(t *testing.T) {
	cases := map[string]string{
		`/usr/bin/automem`:  `'/usr/bin/automem'`,
		`/o'ddball/automem`: `'/o'\''ddball/automem'`,
		`/a b/automem`:      `'/a b/automem'`,
	}
	for in, want := range cases {
		if got := shellSingleQuote(in); got != want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAiderWireInstallsWrapper wires into a sandboxed home and checks the
// wrapper lands executable, is flagged unverified, and is idempotent.
func TestAiderWireInstallsWrapper(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	res := aiderInstaller{}.Wire(cfg)
	if res.Status != StatusWired {
		t.Fatalf("first Wire status = %v (%v), want StatusWired", res.Status, res.Err)
	}
	if !res.Unverified {
		t.Errorf("aider result should be flagged Unverified")
	}

	target := filepath.Join(home, ".local", "bin", aiderWrapperName)
	if res.Path != target {
		t.Errorf("Path = %q, want %q", res.Path, target)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("wrapper not written: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("wrapper not executable: mode %o", info.Mode().Perm())
	}

	// Second Wire without Force is a no-op (already wired).
	res2 := aiderInstaller{}.Wire(cfg)
	if res2.Status != StatusAlreadyWired {
		t.Errorf("second Wire status = %v, want StatusAlreadyWired", res2.Status)
	}
}

// TestAiderWireRefusesToClobberForeignFile protects a user's own script that
// happens to share the wrapper name.
func TestAiderWireRefusesToClobberForeignFile(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".local", "bin", aiderWrapperName)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho not ours\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := aiderInstaller{}.Wire(Config{Home: home, BinPath: "/opt/automem"})
	if res.Status != StatusFailed {
		t.Fatalf("Wire over a foreign file status = %v, want StatusFailed", res.Status)
	}
	// The user's file must be untouched.
	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "not ours") {
		t.Errorf("foreign file was clobbered: %q", data)
	}
}

// TestAiderWireDryRun writes nothing.
func TestAiderWireDryRun(t *testing.T) {
	home := t.TempDir()
	res := aiderInstaller{}.Wire(Config{Home: home, BinPath: "/opt/automem", DryRun: true})
	if res.Status != StatusWouldWire {
		t.Fatalf("dry-run status = %v, want StatusWouldWire", res.Status)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote a file at %s", res.Path)
	}
}

// TestAiderWireForceRefreshes overwrites automem's own managed wrapper when
// asked, and the second write is a full replacement (still one marker).
func TestAiderWireForceRefreshes(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Home: home, BinPath: "/opt/automem"}

	if res := (aiderInstaller{}).Wire(cfg); res.Status != StatusWired {
		t.Fatalf("initial wire failed: %v", res.Err)
	}
	cfg.Force = true
	res := aiderInstaller{}.Wire(cfg)
	if res.Status != StatusWired {
		t.Fatalf("force refresh status = %v, want StatusWired", res.Status)
	}
	data, _ := os.ReadFile(res.Path)
	if n := strings.Count(string(data), aiderWrapperMarker); n != 1 {
		t.Errorf("wrapper should contain exactly one marker after refresh, got %d", n)
	}
}
