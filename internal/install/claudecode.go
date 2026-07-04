package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// claudeCodeInstaller wires automem into Claude Code by adding two hooks to the
// user's ~/.claude/settings.json:
//
//   - SessionStart → `automem recall` : on every new session, recall the top-K
//     prior summaries and inject them into the session context, so the agent
//     starts already remembering the last session.
//   - Stop         → `automem capture --agent claude-code` : when a session
//     ends, append one extractive record to ~/.automem/store.jsonl.
//
// Claude Code exposes a first-class hook surface in settings.json (unlike Aider,
// which has none — hence the wrapper there), so this integration is the verified
// path: it edits a documented config file with documented event names and never
// shells out to an unverified surface. The edit is a JSON merge, not a rewrite —
// we preserve every key and every non-automem hook the user already has, and are
// idempotent: a second `automem install` without --force leaves an existing
// automem wiring exactly as-is.
type claudeCodeInstaller struct{}

func init() { register(claudeCodeInstaller{}) }

func (claudeCodeInstaller) Name() string { return "claude-code" }

// claudeMarker is baked into every automem-managed hook command so a re-run can
// distinguish "automem already wired this" from "the user wrote their own hook".
// It rides along as an env-style prefix that Claude Code passes through to the
// shell unchanged, so it is inert at runtime but greppable for idempotency.
const claudeMarker = "AUTOMEM_HOOK=1"

// claudeConfigPath returns the path to Claude Code's settings.json under the
// wiring home (~/.claude/settings.json), derived from cfg.Home so tests sandbox
// it.
func claudeConfigPath(cfg Config) string {
	return filepath.Join(cfg.Home, ".claude", "settings.json")
}

// claudeConfigDir returns ~/.claude, the directory Claude Code keeps its
// settings in. Its existence is our detection signal.
func claudeConfigDir(cfg Config) string {
	return filepath.Join(cfg.Home, ".claude")
}

// Detect reports whether Claude Code is present by looking for its config
// directory (~/.claude). Claude Code creates this on first run, so its presence
// is the reliable, cross-platform signal — we do not depend on a `claude` binary
// being on PATH (it may be installed under a name or path we can't predict).
func (claudeCodeInstaller) Detect(cfg Config) (bool, string) {
	dir := claudeConfigDir(cfg)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return true, "found Claude Code config at " + dir
	}
	return false, ""
}

// hookEntry is one entry in a Claude Code hook array: a matcher plus the list of
// hook commands to run for it. We model only the fields we touch and preserve
// everything else via the raw settings map, so an unknown future field on a
// user's own hook survives our merge untouched.
type hookEntry struct {
	Matcher string     `json:"matcher,omitempty"`
	Hooks   []hookSpec `json:"hooks"`
}

// hookSpec is a single command hook.
type hookSpec struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// Wire adds (or, with Force, refreshes) automem's SessionStart + Stop hooks in
// settings.json without disturbing any other setting or hook the user has.
func (c claudeCodeInstaller) Wire(cfg Config) Result {
	res := Result{Agent: c.Name()}

	path := claudeConfigPath(cfg)
	res.Path = path

	settings, err := loadClaudeSettings(path)
	if err != nil {
		res.Status = StatusFailed
		res.Err = err
		return res
	}

	// Idempotency: if our marker is already present anywhere in the settings,
	// automem is wired. Leave it alone unless Force.
	if claudeAlreadyWired(settings) && !cfg.Force {
		res.Status = StatusAlreadyWired
		res.Detail = "hooks already installed (use --force to refresh)"
		return res
	}

	if cfg.DryRun {
		res.Status = StatusWouldWire
		res.Detail = "would add SessionStart + Stop hooks (dry-run)"
		return res
	}

	// Merge our hooks in, preserving every foreign hook.
	if err := mergeAutomemHooks(settings, cfg.BinPath); err != nil {
		res.Status = StatusFailed
		res.Err = err
		return res
	}

	data, err := marshalClaudeSettings(settings)
	if err != nil {
		res.Status = StatusFailed
		res.Err = err
		return res
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		res.Status = StatusFailed
		res.Err = err
		return res
	}

	res.Status = StatusWired
	res.Detail = "wired SessionStart (recall) + Stop (capture) hooks"
	return res
}

// loadClaudeSettings reads and parses settings.json, tolerating a missing file
// (a fresh Claude Code that has a config dir but no settings.json yet) as an
// empty object. A malformed settings.json is an error we surface rather than
// overwrite — clobbering a user's hand-edited config would be worse than failing.
func loadClaudeSettings(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("install: read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("install: %s is not valid JSON (%w); refusing to overwrite — fix or move it, then re-run", path, err)
	}
	return settings, nil
}

// marshalClaudeSettings renders settings back to disk as indented JSON with a
// trailing newline, matching the shape Claude Code itself writes so a diff after
// install is minimal.
func marshalClaudeSettings(settings map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("install: encode settings: %w", err)
	}
	return append(data, '\n'), nil
}

// claudeAlreadyWired reports whether an automem hook (identified by claudeMarker)
// is already present anywhere in the hooks tree.
func claudeAlreadyWired(settings map[string]any) bool {
	return bytes.Contains(mustJSON(settings), []byte(claudeMarker))
}

// mustJSON marshals v for a substring probe; on the (unreachable for a map[string]any)
// error path it returns nil so the probe simply reports "not found".
func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

// mergeAutomemHooks inserts automem's SessionStart and Stop hooks into the
// settings' "hooks" map, first stripping any prior automem-managed entry (so a
// --force refresh replaces rather than duplicates) while preserving every
// foreign hook and matcher untouched.
func mergeAutomemHooks(settings map[string]any, binPath string) error {
	hooksAny, ok := settings["hooks"]
	if !ok {
		hooksAny = map[string]any{}
	}
	hooks, ok := hooksAny.(map[string]any)
	if !ok {
		return fmt.Errorf("install: settings \"hooks\" is %T, expected an object; refusing to overwrite", hooksAny)
	}

	// SessionStart → recall; Stop → capture. The marker rides as a leading
	// env-assignment so it's inert at runtime but detectable for idempotency.
	recallCmd := fmt.Sprintf("%s %s recall", claudeMarker, shellSingleQuote(binPath))
	captureCmd := fmt.Sprintf("%s %s capture --agent claude-code", claudeMarker, shellSingleQuote(binPath))

	for event, cmd := range map[string]string{
		"SessionStart": recallCmd,
		"Stop":         captureCmd,
	} {
		kept := stripAutomemEntries(hooks[event])
		kept = append(kept, hookEntry{
			Hooks: []hookSpec{{Type: "command", Command: cmd}},
		})
		// Re-encode via JSON round-trip so the value is a plain []any of
		// map[string]any (matching what a fresh Unmarshal of the file would
		// yield), keeping the on-disk shape uniform whether or not the user
		// already had hooks for this event.
		normalized, err := normalizeHookEntries(kept)
		if err != nil {
			return err
		}
		hooks[event] = normalized
	}

	settings["hooks"] = hooks
	return nil
}

// stripAutomemEntries returns the hook entries for one event with every
// automem-managed entry removed (identified by claudeMarker in a hook command),
// leaving the user's own hooks in place. It tolerates any shape the raw JSON
// might hold and simply drops entries it can't understand as automem's.
func stripAutomemEntries(raw any) []hookEntry {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	var kept []hookEntry
	for _, item := range arr {
		if entryIsAutomem(item) {
			continue
		}
		if e, ok := decodeHookEntry(item); ok {
			kept = append(kept, e)
		}
	}
	return kept
}

// entryIsAutomem reports whether a raw hook entry is one automem manages,
// detected by the marker in any of its hook commands.
func entryIsAutomem(item any) bool {
	return bytes.Contains(mustJSON(item), []byte(claudeMarker))
}

// decodeHookEntry converts a raw JSON hook entry into a typed hookEntry,
// preserving the matcher and every command. Entries it can't decode are dropped
// from the typed view but — because they were foreign and non-automem — this
// only happens for genuinely malformed data; well-formed foreign hooks decode
// cleanly and are preserved.
func decodeHookEntry(item any) (hookEntry, bool) {
	data, err := json.Marshal(item)
	if err != nil {
		return hookEntry{}, false
	}
	var e hookEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return hookEntry{}, false
	}
	return e, true
}

// normalizeHookEntries round-trips the typed entries back through JSON into the
// generic []any / map[string]any representation, so the value stored in the
// settings map has the same concrete types a fresh file read would produce.
func normalizeHookEntries(entries []hookEntry) (any, error) {
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("install: encode hook entries: %w", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("install: normalize hook entries: %w", err)
	}
	return out, nil
}
