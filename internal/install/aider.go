package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// aiderInstaller wires automem into Aider by installing a small wrapper script
// on PATH. Aider has no native session-hook surface the way Claude Code does,
// so instead of editing an Aider config we ship an `automem-aider` command that
// brackets a real `aider` run with recall (before) and capture (after):
//
//  1. `automem recall` prints the top-K prior summaries relevant to the repo;
//     the wrapper drops them into a read-only context file Aider loads via
//     `--read`, so the model starts the session already remembering.
//  2. it execs the real `aider`, forwarding every argument, so `automem-aider`
//     is a drop-in replacement for `aider`.
//  3. on exit it captures the session: it feeds Aider's own chat-history file
//     (`.aider.chat.history.md`, which Aider writes by default) to
//     `automem capture`, appending one extractive record.
//
// IMPORTANT — this integration is UNVERIFIED. The build machine had no Aider
// install to test the wrapper end-to-end against, so we ship it best-effort and
// flag it plainly (Result.Unverified) rather than claiming a tested check. The
// Claude Code path, by contrast, is verified.
type aiderInstaller struct{}

func init() { register(aiderInstaller{}) }

func (aiderInstaller) Name() string { return "aider" }

// aiderWrapperName is the wrapper command installed on PATH.
const aiderWrapperName = "automem-aider"

// aiderWrapperMarker is a unique string baked into the wrapper so a re-run can
// tell "automem already wired this" from "the user has their own script here".
const aiderWrapperMarker = "# automem-aider wrapper (managed by `automem install`)"

// Detect reports whether Aider is installed. We look for the `aider` binary on
// PATH; that's the only reliable cross-distro signal (Aider is a pip/pipx tool
// with no fixed config directory we can count on).
func (aiderInstaller) Detect(cfg Config) (bool, string) {
	if p, err := exec.LookPath("aider"); err == nil {
		return true, "found aider at " + p
	}
	return false, ""
}

// aiderWrapperDir returns the directory the wrapper is installed into: ~/.local/bin,
// which is on PATH for most macOS/Linux dev setups (and the conventional home
// for user-scoped binaries). Derived from cfg.Home so tests can sandbox it.
func aiderWrapperDir(cfg Config) string {
	return filepath.Join(cfg.Home, ".local", "bin")
}

// Wire installs (or, when Force, refreshes) the automem-aider wrapper.
func (a aiderInstaller) Wire(cfg Config) Result {
	res := Result{Agent: a.Name(), Unverified: true}

	dir := aiderWrapperDir(cfg)
	target := filepath.Join(dir, aiderWrapperName)
	res.Path = target

	script := aiderWrapperScript(cfg.BinPath)

	// Idempotency: if our managed wrapper is already there, don't rewrite it
	// unless Force. If a *different* file already occupies the name, refuse to
	// clobber it — that's the user's, not ours.
	if info, err := os.Stat(target); err == nil {
		if fileContains(target, aiderWrapperMarker) {
			if !cfg.Force {
				res.Status = StatusAlreadyWired
				res.Detail = "wrapper already installed (use --force to refresh)"
				return res
			}
		} else if info.Mode().IsRegular() {
			res.Status = StatusFailed
			res.Err = fmt.Errorf("%s already exists and was not created by automem; refusing to overwrite", target)
			return res
		}
	}

	if cfg.DryRun {
		res.Status = StatusWouldWire
		res.Detail = "would install wrapper (dry-run)"
		return res
	}

	if err := writeFileAtomic(target, []byte(script), 0o755); err != nil {
		res.Status = StatusFailed
		res.Err = err
		return res
	}

	res.Status = StatusWired
	res.Detail = pathHint(dir)
	return res
}

// pathHint nudges the user to add the wrapper dir to PATH when it isn't already
// there, so `automem-aider` is actually invokable after install.
func pathHint(dir string) string {
	if onPath(dir) {
		return "run `automem-aider` in place of `aider`"
	}
	return fmt.Sprintf("add %s to PATH, then run `automem-aider` in place of `aider`", dir)
}

// onPath reports whether dir is a component of $PATH.
func onPath(dir string) bool {
	clean := filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == clean {
			return true
		}
	}
	return false
}

// aiderWrapperScript returns the POSIX-sh wrapper source, with the resolved
// automem binary path baked in so it works no matter what PATH the wrapped
// session runs with. The script is deliberately dependency-free (sh + the
// automem binary itself); it degrades gracefully if a step has no data.
func aiderWrapperScript(binPath string) string {
	// Quote the binary path once for safe embedding in the generated script.
	bin := shellSingleQuote(binPath)
	return strings.Join([]string{
		"#!/bin/sh",
		aiderWrapperMarker,
		"# Brackets a real `aider` run with automem recall (before) and capture",
		"# (after) so each session remembers the last. Forwards all arguments to",
		"# aider unchanged, so this is a drop-in replacement for `aider`.",
		"#",
		"# UNVERIFIED integration: shipped best-effort, not tested against a real",
		"# aider install. Please open an issue if it misbehaves.",
		"set -eu",
		"",
		"AUTOMEM=" + bin,
		"",
		"# --- recall: seed the session with relevant prior memory -------------",
		"# Query automem with the current directory name + any git repo hint, and",
		"# stash the top matches in a read-only context file aider loads.",
		"CTX_DIR=\"${TMPDIR:-/tmp}\"",
		"CTX_FILE=\"$CTX_DIR/automem-aider-context.$$.md\"",
		"QUERY=\"$(basename \"$PWD\")\"",
		"if command -v git >/dev/null 2>&1; then",
		"  BRANCH=\"$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)\"",
		"  QUERY=\"$QUERY $BRANCH\"",
		"fi",
		"if \"$AUTOMEM\" recall \"$QUERY\" >\"$CTX_FILE\" 2>/dev/null && \\",
		"   ! grep -q 'no relevant memories' \"$CTX_FILE\"; then",
		"  set -- --read \"$CTX_FILE\" \"$@\"",
		"else",
		"  rm -f \"$CTX_FILE\"",
		"  CTX_FILE=\"\"",
		"fi",
		"",
		"# --- run aider, forwarding every argument ---------------------------",
		"STATUS=0",
		"aider \"$@\" || STATUS=$?",
		"",
		"# --- capture: append an extractive record from aider's chat history --",
		"# Aider writes .aider.chat.history.md in the repo by default; feed it to",
		"# automem capture. If it isn't there (nothing happened), skip silently.",
		"HISTORY=\".aider.chat.history.md\"",
		"if [ -f \"$HISTORY\" ]; then",
		"  \"$AUTOMEM\" capture --agent aider \"$HISTORY\" >/dev/null 2>&1 || true",
		"fi",
		"",
		"[ -n \"$CTX_FILE\" ] && rm -f \"$CTX_FILE\"",
		"exit \"$STATUS\"",
		"",
	}, "\n")
}

// shellSingleQuote wraps s in single quotes for safe embedding in a POSIX shell
// script, escaping any embedded single quote the standard '\” way.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
