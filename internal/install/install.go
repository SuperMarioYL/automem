// Package install auto-wires automem into the coding-agent CLIs a developer
// already runs, so that after a single `automem install` the next session
// remembers the last — with no manual config file editing.
//
// The design is a small registry of per-agent installers. Each supported agent
// lives in its own file (claudecode.go, aider.go) and self-registers into the
// package-level installer list from an init function. install.Run walks the
// registry, detects which agents are present on the machine, wires the ones it
// finds, and returns a per-agent Result slice the CLI renders as the
// "wired claude-code ✓, aider ✓" summary.
//
// Everything here is macOS + Linux only (the v0.1 target); Windows is out of
// scope. Nothing runs as a daemon — install writes hook/wrapper files once and
// exits. The agent's own process invokes the automem binary per hook fire.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Config controls an install run.
type Config struct {
	// BinPath is the absolute path to the automem binary the wired hooks and
	// wrappers should invoke. Empty means "resolve the running executable"
	// (install.Run fills it in via BinaryPath).
	BinPath string

	// Home is the user home directory to wire into. Empty means "resolve the
	// current user's home" (honoring $AUTOMEM_HOME for tests). Agent config
	// paths (~/.claude, ~/.local/bin, …) are derived from this.
	Home string

	// DryRun previews what would be wired without writing any file. Every
	// installer reports what it *would* do and leaves the disk untouched.
	DryRun bool

	// Force overwrites an existing automem wiring instead of leaving a
	// hand-customized config alone. Installers that find automem already wired
	// are idempotent by default and only rewrite when Force is set.
	Force bool
}

// Status is the outcome of wiring one agent.
type Status int

const (
	// StatusNotFound means the agent isn't installed on this machine, so there
	// was nothing to wire. Not an error — most machines run a subset of agents.
	StatusNotFound Status = iota
	// StatusWired means automem was newly wired into the agent.
	StatusWired
	// StatusAlreadyWired means automem was already wired and left as-is (no
	// Force). Idempotent re-runs land here.
	StatusAlreadyWired
	// StatusWouldWire is the DryRun outcome for an agent that would be wired.
	StatusWouldWire
	// StatusFailed means detection succeeded but wiring hit an error; see Err.
	StatusFailed
)

// Result is the per-agent outcome of an install run.
type Result struct {
	// Agent is the agent label ("claude-code", "aider").
	Agent string
	// Status is what happened for this agent.
	Status Status
	// Detail is a human-readable note (the path written, why skipped, etc.).
	Detail string
	// Path is the config/wrapper file that was (or would be) written, if any.
	Path string
	// Unverified flags an integration automem ships but could not validate on
	// the build machine (the Aider wrapper). The CLI surfaces this honestly
	// rather than claiming a green check it can't stand behind.
	Unverified bool
	// Err is set when Status is StatusFailed.
	Err error
}

// wired reports whether this result represents automem actually being active
// for the agent (freshly wired or already wired). Used for the summary glyph.
func (r Result) wired() bool {
	return r.Status == StatusWired || r.Status == StatusAlreadyWired || r.Status == StatusWouldWire
}

// Glyph is the status marker for the one-line summary: ✓ wired, • not found,
// ✗ failed. Kept ASCII-safe apart from the check/cross so it renders in any
// terminal.
func (r Result) Glyph() string {
	switch {
	case r.Status == StatusFailed:
		return "✗"
	case r.Status == StatusNotFound:
		return "•"
	case r.wired():
		return "✓"
	default:
		return "•"
	}
}

// agentInstaller is implemented once per supported agent. Detect is cheap and
// side-effect-free; Wire is the only method allowed to touch disk (and only
// when cfg.DryRun is false).
type agentInstaller interface {
	// Name is the agent label used in Results and the summary.
	Name() string
	// Detect reports whether the agent is present on the machine and, if so, a
	// short location hint for the Detail line.
	Detect(cfg Config) (found bool, where string)
	// Wire installs automem into the agent per cfg and returns a Result. Wire is
	// only called when Detect returned true. It must be idempotent: a second
	// call without Force must not double-write.
	Wire(cfg Config) Result
}

// registry holds every installer, populated by each agent file's init(). Order
// is normalized (see sortedInstallers) so the summary is deterministic
// regardless of file init order.
var registry []agentInstaller

// register adds an installer to the package registry. Called from agent files'
// init functions.
func register(a agentInstaller) { registry = append(registry, a) }

// sortedInstallers returns the registry in a stable, human-friendly order:
// claude-code first (the verified, primary integration), then the rest
// alphabetically.
func sortedInstallers() []agentInstaller {
	out := append([]agentInstaller(nil), registry...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := installerRank(out[i].Name()), installerRank(out[j].Name())
		if pi != pj {
			return pi < pj
		}
		return out[i].Name() < out[j].Name()
	})
	return out
}

// installerRank pins the primary integration to the front of the summary.
func installerRank(name string) int {
	if name == "claude-code" {
		return 0
	}
	return 1
}

// Run detects every registered agent and wires the ones it finds, returning a
// Result per registered agent (including not-found agents, so the summary shows
// what was considered). It returns an error only for a machine-level problem
// (unsupported OS, unresolvable home/binary) — a single agent failing to wire
// is reported in that agent's Result, not as a hard error, so one broken agent
// never blocks the others.
func Run(cfg Config) ([]Result, error) {
	if !Supported() {
		return nil, fmt.Errorf("install: unsupported OS %q (automem v0.1 supports macOS and Linux)", runtime.GOOS)
	}

	if cfg.Home == "" {
		home, err := resolveHome()
		if err != nil {
			return nil, err
		}
		cfg.Home = home
	}
	if cfg.BinPath == "" {
		bin, err := BinaryPath()
		if err != nil {
			return nil, err
		}
		cfg.BinPath = bin
	}

	installers := sortedInstallers()
	results := make([]Result, 0, len(installers))
	for _, a := range installers {
		found, where := a.Detect(cfg)
		if !found {
			results = append(results, Result{
				Agent:  a.Name(),
				Status: StatusNotFound,
				Detail: "not installed",
			})
			continue
		}
		r := a.Wire(cfg)
		if r.Agent == "" {
			r.Agent = a.Name()
		}
		if where != "" && r.Detail == "" {
			r.Detail = where
		}
		results = append(results, r)
	}
	return results, nil
}

// Report renders the one-line install summary the happy path prints, e.g.
//
//	wired claude-code ✓, aider ✓ (unverified)
//
// Not-found agents are omitted from the headline; if nothing was found at all
// the caller gets a clear "no supported agents detected" line instead.
func Report(results []Result) string {
	var parts []string
	anyFound := false
	for _, r := range results {
		if r.Status == StatusNotFound {
			continue
		}
		anyFound = true
		seg := r.Agent + " " + r.Glyph()
		switch {
		case r.Unverified && r.wired():
			seg += " (unverified)"
		case r.Status == StatusAlreadyWired:
			seg += " (already wired)"
		case r.Status == StatusWouldWire:
			seg += " (dry-run)"
		case r.Status == StatusFailed:
			seg += " (failed)"
		}
		parts = append(parts, seg)
	}
	if !anyFound {
		return "no supported agents detected (looked for: " + strings.Join(agentNames(results), ", ") + ")"
	}
	return "wired " + strings.Join(parts, ", ")
}

// Detail renders the multi-line breakdown printed under the summary: one line
// per agent with its path and any note. Returns "" when there's nothing worth
// elaborating (all agents not found).
func Detail(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		switch r.Status {
		case StatusNotFound:
			continue
		case StatusFailed:
			fmt.Fprintf(&b, "  %s ✗ %s\n", r.Agent, errText(r))
		default:
			line := fmt.Sprintf("  %s %s", r.Agent, r.Glyph())
			if r.Path != "" {
				line += " " + r.Path
			}
			if r.Detail != "" {
				line += " — " + r.Detail
			}
			if r.Unverified {
				line += " [unverified: no aider on the build machine — please report if it misbehaves]"
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func errText(r Result) string {
	if r.Err != nil {
		return r.Err.Error()
	}
	if r.Detail != "" {
		return r.Detail
	}
	return "failed"
}

func agentNames(results []Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Agent)
	}
	return out
}

// ---- environment resolution --------------------------------------------

// Supported reports whether the current OS is a v0.1 install target
// (macOS or Linux). Windows is out of scope.
func Supported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

// resolveHome returns the home directory to wire into, honoring $AUTOMEM_HOME
// (used by tests and power users to redirect the whole wiring to a sandbox)
// before falling back to the OS user home.
func resolveHome() (string, error) {
	if override := os.Getenv("AUTOMEM_HOME"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("install: resolve home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("install: empty home dir")
	}
	return home, nil
}

// BinaryPath returns the absolute path of the running automem executable, which
// is what the wired hooks and wrappers invoke. It honors $AUTOMEM_BIN so tests
// (and users who keep the binary somewhere unusual) can pin it explicitly.
func BinaryPath() (string, error) {
	if override := os.Getenv("AUTOMEM_BIN"); override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("install: resolve automem binary path: %w", err)
	}
	// Resolve symlinks so a wrapper written today keeps working if the symlink
	// (e.g. a Homebrew shim) is later repointed — we bake in the real target.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// ---- shared filesystem helpers (used by the per-agent installers) -------

// writeFileAtomic writes data to path via a same-directory temp file + rename,
// creating parent dirs (mode 0700) as needed. The rename is atomic on the same
// filesystem, so a crash mid-write can never leave a half-written hook/wrapper
// that the agent would then try to execute.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("install: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".automem-*.tmp")
	if err != nil {
		return fmt.Errorf("install: create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("install: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("install: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("install: close temp: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("install: chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install: rename into place %s: %w", path, err)
	}
	return nil
}

// fileContains reports whether the file at path exists and contains needle.
// A missing file is not an error — it just isn't wired yet. Used by installers
// to detect an existing automem wiring for idempotency.
func fileContains(path, needle string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), needle)
}
