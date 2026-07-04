// Package capture turns a raw agent-session transcript into one store.Record —
// deterministically, with no AI provider, no network, and no key. That
// keyless-by-construction summary is automem's core bet: the "compression" is
// plain extraction, not an LLM call.
//
// The extractive summary is assembled from three signals the transcript
// already contains:
//
//   - the last N user messages (what the human actually asked for),
//   - the file paths touched during the session (the "what code" anchor),
//   - a one-line diff stat if the transcript reports one.
//
// Tags are the union of path tokens and language tokens derived from those
// paths, which is what recall does lexical matching against. Everything here is
// pure text processing: identical input always yields an identical Record body
// (modulo the ULID/timestamp the store assigns), which is what makes captures
// reproducible and testable.
package capture

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/SuperMarioYL/automem/internal/store"
)

// defaultMaxUserMsgs is how many trailing user messages the summary keeps.
// Enough to carry the intent of a session without bloating the store line.
const defaultMaxUserMsgs = 5

// maxSummaryRunes caps the summary length so one pathological session can't
// write a megabyte line. Truncation is on a rune boundary with an ellipsis.
const maxSummaryRunes = 2000

// Options tunes extraction. The zero value is valid and uses the defaults.
type Options struct {
	// Agent labels the capture source ("claude-code" | "aider" | ""). Stored
	// verbatim on the Record.
	Agent string
	// Cwd is the working directory at capture time. Stored verbatim.
	Cwd string
	// MaxUserMsgs overrides how many trailing user messages to keep (<=0 uses
	// the default).
	MaxUserMsgs int
}

// Transcript is the parsed, structured view of a session the extractor works
// from. Callers may hand ParseTranscript a raw text stream, or build this
// directly (e.g. an agent hook that already has structured turns).
type Transcript struct {
	// UserMessages are the human turns, in chronological order.
	UserMessages []string
	// Paths are file paths referenced during the session, de-duplicated and in
	// first-seen order.
	Paths []string
	// DiffStat is an optional one-line change summary (e.g. "3 files changed,
	// 40 insertions(+), 12 deletions(-)").
	DiffStat string
}

// Extract builds a store.Record body from a Transcript. The returned Record has
// Summary, Tags, Agent and Cwd populated; ID and TS are left zero for the store
// to assign at Append time (keeping capture pure and deterministic).
func Extract(t Transcript, opts Options) store.Record {
	maxMsgs := opts.MaxUserMsgs
	if maxMsgs <= 0 {
		maxMsgs = defaultMaxUserMsgs
	}

	summary := buildSummary(t, maxMsgs)
	tags := buildTags(t)

	return store.Record{
		Cwd:     opts.Cwd,
		Agent:   opts.Agent,
		Summary: summary,
		Tags:    tags,
	}
}

// buildSummary assembles the human-readable extractive summary. Structure is
// fixed so it is stable and skimmable:
//
//	<last user msg>
//	<earlier user msg>
//	files: a.go, b/c.py
//	diff: 3 files changed, 40 insertions(+)
//
// The most recent user message comes first because that's the freshest intent —
// the thing a future session most wants recalled.
func buildSummary(t Transcript, maxMsgs int) string {
	var b strings.Builder

	msgs := lastN(t.UserMessages, maxMsgs)
	// Newest first.
	for i := len(msgs) - 1; i >= 0; i-- {
		m := collapseSpace(msgs[i])
		if m == "" {
			continue
		}
		b.WriteString(m)
		b.WriteByte('\n')
	}

	if len(t.Paths) > 0 {
		b.WriteString("files: ")
		b.WriteString(strings.Join(dedupePreserveOrder(t.Paths), ", "))
		b.WriteByte('\n')
	}

	if ds := collapseSpace(t.DiffStat); ds != "" {
		b.WriteString("diff: ")
		b.WriteString(ds)
		b.WriteByte('\n')
	}

	return truncateRunes(strings.TrimRight(b.String(), "\n"), maxSummaryRunes)
}

// buildTags returns the lexical-match tokens: path segments (without extension
// noise) plus the language token implied by each file extension. Tags are
// lowercased, de-duplicated, and sorted so the stored form is deterministic.
func buildTags(t Transcript) []string {
	set := map[string]struct{}{}
	for _, p := range t.Paths {
		for _, tok := range pathTokens(p) {
			set[tok] = struct{}{}
		}
		if lang := langForPath(p); lang != "" {
			set[lang] = struct{}{}
		}
	}
	tags := make([]string, 0, len(set))
	for tok := range set {
		tags = append(tags, tok)
	}
	sort.Strings(tags)
	if tags == nil {
		tags = []string{}
	}
	return tags
}

// ---- transcript parsing -------------------------------------------------

// roleLine matches a "role: message" prefix at the start of a transcript line.
// Recognized roles: user/human (kept), assistant/ai/system (ignored for the
// user-message extraction but their bodies are still scanned for paths).
var roleLine = regexp.MustCompile(`^\s*(user|human|assistant|ai|system)\s*:\s*(.*)$`)

// diffStatLine matches a git-style shortstat line anywhere in the transcript.
var diffStatLine = regexp.MustCompile(`\d+ files? changed(?:, \d+ insertions?\(\+\))?(?:, \d+ deletions?\(-\))?`)

// pathLike matches path-ish tokens: something with a slash or a known code
// extension. Deliberately conservative to avoid tagging prose.
var pathLike = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])((?:[\w.-]+/)+[\w.-]+|[\w.-]+\.` + knownExtAlternation + `)`)

// ParseTranscript reads a plain-text transcript from r and structures it. The
// format is line-oriented and forgiving:
//
//   - Lines beginning "user:" / "human:" contribute to UserMessages (the text
//     after the colon; a following indented/continuation line is appended).
//   - Any line may contribute file paths (matched by pathLike).
//   - A git shortstat line anywhere sets DiffStat (last one wins).
//
// Transcripts that carry no explicit roles (e.g. a bare prompt piped in) are
// treated as a single user message — so `echo "fix auth" | automem capture`
// still produces a useful record.
func ParseTranscript(r io.Reader) (Transcript, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var t Transcript
	seenPath := map[string]struct{}{}
	var curRole string
	var curMsg strings.Builder
	sawRole := false

	flush := func() {
		if curRole == "user" || curRole == "human" {
			msg := strings.TrimSpace(curMsg.String())
			if msg != "" {
				t.UserMessages = append(t.UserMessages, msg)
			}
		}
		curMsg.Reset()
	}

	for sc.Scan() {
		line := sc.Text()

		// Collect paths from every line, regardless of role.
		for _, p := range extractPaths(line) {
			if _, ok := seenPath[p]; !ok {
				seenPath[p] = struct{}{}
				t.Paths = append(t.Paths, p)
			}
		}
		// Track the latest diff stat.
		if m := diffStatLine.FindString(line); m != "" {
			t.DiffStat = m
		}

		if m := roleLine.FindStringSubmatch(line); m != nil {
			sawRole = true
			flush()
			curRole = strings.ToLower(m[1])
			curMsg.WriteString(m[2])
			continue
		}
		// Continuation of the current turn.
		if curMsg.Len() > 0 {
			curMsg.WriteByte('\n')
		}
		curMsg.WriteString(line)
	}
	// Snapshot the final buffer before flush resets it — needed for the
	// roleless fallback below.
	trailing := curMsg.String()
	flush()
	if err := sc.Err(); err != nil {
		return Transcript{}, fmt.Errorf("capture: read transcript: %w", err)
	}

	// No explicit roles at all → the whole thing is one user message.
	if !sawRole && len(t.UserMessages) == 0 {
		if whole := strings.TrimSpace(trailing); whole != "" {
			t.UserMessages = []string{whole}
		}
	}

	return t, nil
}

// extractPaths pulls path-like tokens from a single line.
func extractPaths(line string) []string {
	matches := pathLike.FindAllStringSubmatch(line, -1)
	if matches == nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		p := strings.Trim(m[1], `"'`+"`"+`(),`)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ---- token helpers ------------------------------------------------------

// pathTokens splits a path into lowercased segment/word tokens usable for
// lexical matching: directory names, the base name (with and without
// extension), so a query mentioning "auth" matches a record touching
// "src/auth.py".
func pathTokens(p string) []string {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	var toks []string
	base := path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(s) >= 2 {
			toks = append(toks, s)
		}
	}
	add(base)
	add(stem)
	for _, seg := range strings.Split(path.Dir(p), "/") {
		add(seg)
	}
	// Split the stem on common separators so "user_auth" also yields "user"/"auth".
	for _, part := range splitAny(stem, "_-.") {
		add(part)
	}
	return toks
}

// langForPath maps a file extension to a coarse language token.
func langForPath(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".md":
		return "markdown"
	case ".sh", ".bash":
		return "shell"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return ""
	}
}

// knownExtAlternation is the regex alternation of extensions pathLike will
// recognize on an extension-only (slashless) token, e.g. "auth.py".
const knownExtAlternation = `(?:go|py|js|mjs|cjs|ts|tsx|rs|java|rb|c|h|cpp|cc|hpp|md|sh|bash|yaml|yml|json|toml|cfg|ini)\b`

// ---- small string utilities --------------------------------------------

func lastN(s []string, n int) []string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func dedupePreserveOrder(s []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

var wsRun = regexp.MustCompile(`\s+`)

// collapseSpace trims and collapses internal whitespace runs to single spaces,
// so multi-line user turns become one tidy summary line.
func collapseSpace(s string) string {
	return strings.TrimSpace(wsRun.ReplaceAllString(s, " "))
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func splitAny(s, cutset string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune(cutset, r)
	})
}
