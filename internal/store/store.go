// Package store is automem's append-only JSONL persistence layer. Each memory
// is one Record, one JSON object per line, in ~/.automem/store.jsonl. There is
// no index, no daemon, and no database — the file IS the store. Reads scan the
// whole file (small by construction: a few thousand short summaries), which is
// fast enough for the recall workload and keeps the "single binary, no server"
// promise honest.
//
// The store owns three operations the rest of automem builds on:
//   - Append: add one captured Record (assigns a ULID + timestamp if unset).
//   - Load:   read every Record back, in stored order.
//   - MarkInjected: bump the Injected counter on a set of records (the
//     "is-it-used" signal) and rewrite the file atomically.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
)

// Record is one captured memory. It maps 1:1 to a line in store.jsonl.
//
// The struct is intentionally flat and JSON-tagged with short keys so the file
// stays compact and diff-friendly. Injected is the load-bearing "is-it-used"
// counter: it starts at 0 on capture and increments every time recall surfaces
// the record into a later session.
type Record struct {
	ID       string   `json:"id"`       // ULID, monotonic — stable identity across rewrites
	TS       int64    `json:"ts"`       // unix-milli capture time — recency-decay input
	Cwd      string   `json:"cwd"`      // working dir at capture — scope hint
	Agent    string   `json:"agent"`    // "claude-code" | "aider" | "" (unknown)
	Summary  string   `json:"summary"`  // deterministic extractive summary
	Tags     []string `json:"tags"`     // path + lang tokens for lexical match
	Injected int      `json:"injected"` // times recall surfaced this record
}

// DefaultDirName is the per-user store directory under the home dir.
const DefaultDirName = ".automem"

// storeFileName is the JSONL file inside the store directory.
const storeFileName = "store.jsonl"

// Store is a handle to one JSONL memory file. It holds no open descriptors and
// no in-memory cache — every method opens, works, and closes, so concurrent
// hook invocations from different agent processes never fight over a lock.
type Store struct {
	path string
}

// Open returns a Store rooted at the given directory, creating the directory
// (mode 0700 — memory is private) if it does not exist. The JSONL file itself
// is created lazily on first Append.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create dir %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, storeFileName)}, nil
}

// DefaultDir returns the store directory for the current user
// (~/.automem), honoring the AUTOMEM_DIR override so tests and power users can
// redirect the store without touching the real one.
func DefaultDir() (string, error) {
	if override := os.Getenv("AUTOMEM_DIR"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: resolve home dir: %w", err)
	}
	return filepath.Join(home, DefaultDirName), nil
}

// OpenDefault opens the store at DefaultDir(). It is the convenience entry
// point every command uses.
func OpenDefault() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return Open(dir)
}

// Path returns the absolute path of the backing JSONL file.
func (s *Store) Path() string { return s.path }

// entropySource is the ULID entropy reader. It is a package var so tests can
// make ID generation deterministic; production uses crypto-seeded monotonic
// entropy for collision-free, sortable IDs.
var newULID = func(ts int64) string {
	return ulid.MustNew(uint64(ts), ulid.DefaultEntropy()).String()
}

// Append writes one Record to the end of the store. It fills in a ULID and a
// millisecond timestamp when the caller left them zero, so callers can hand in
// a bare {Summary, Tags} record and get a well-formed, sortable entry back.
//
// The returned Record is the stored form (with ID/TS populated), so callers can
// report the assigned ID. Writes are line-atomic: one Marshal, one Write of a
// single "<json>\n" line appended with O_APPEND, which the OS guarantees is
// atomic for writes under PIPE_BUF — safe against concurrent hook fires.
func (s *Store) Append(r Record) (Record, error) {
	if r.TS == 0 {
		r.TS = time.Now().UnixMilli()
	}
	if r.ID == "" {
		r.ID = newULID(r.TS)
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}

	line, err := json.Marshal(r)
	if err != nil {
		return Record{}, fmt.Errorf("store: marshal record: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Record{}, fmt.Errorf("store: open for append: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return Record{}, fmt.Errorf("store: append: %w", err)
	}
	return r, nil
}

// Load reads every Record from the store, in file (stored) order. A missing
// store file is not an error — it means "no memories yet" and yields an empty
// slice, so first-run recall/stats degrade gracefully instead of failing.
//
// Malformed lines (should never happen for a store automem itself wrote, but
// possible after a partial disk write or hand-edit) are skipped rather than
// aborting the whole load, so one bad line can't blind the tool to every good
// memory. Blank lines are ignored.
func (s *Store) Load() ([]Record, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Record{}, nil
		}
		return nil, fmt.Errorf("store: open for read: %w", err)
	}
	defer f.Close()

	var records []Record
	sc := bufio.NewScanner(f)
	// Summaries can be long; raise the line cap well above the 64KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // skip a corrupt line, keep the rest
		}
		records = append(records, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("store: scan: %w", err)
	}
	if records == nil {
		records = []Record{}
	}
	return records, nil
}

// MarkInjected increments the Injected counter for every record whose ID is in
// ids, then rewrites the store atomically (write temp, fsync, rename) so a
// crash mid-write can never truncate the memory file. IDs not present in the
// store are ignored. It returns the number of records actually bumped.
//
// Rewrite-in-place is acceptable here because recall runs at session start
// (not in a hot loop) and the store is small; the atomic rename keeps the
// invariant that store.jsonl is always a complete, valid file.
func (s *Store) MarkInjected(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}

	records, err := s.Load()
	if err != nil {
		return 0, err
	}

	bumped := 0
	for i := range records {
		if _, ok := want[records[i].ID]; ok {
			records[i].Injected++
			bumped++
		}
	}
	if bumped == 0 {
		return 0, nil
	}
	if err := s.rewrite(records); err != nil {
		return 0, err
	}
	return bumped, nil
}

// rewrite replaces the store file with the given records, atomically. It writes
// to a temp file in the same directory (so rename is a same-filesystem move),
// fsyncs, then renames over the target.
func (s *Store) rewrite(records []Record) error {
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "store-*.jsonl.tmp")
	if err != nil {
		return fmt.Errorf("store: create temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename.
	defer func() { _ = os.Remove(tmpName) }()

	w := bufio.NewWriter(tmp)
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			tmp.Close()
			return fmt.Errorf("store: marshal on rewrite: %w", err)
		}
		if _, err := w.Write(line); err != nil {
			tmp.Close()
			return fmt.Errorf("store: write on rewrite: %w", err)
		}
		if err := w.WriteByte('\n'); err != nil {
			tmp.Close()
			return fmt.Errorf("store: write on rewrite: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: flush temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close temp: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("store: chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("store: atomic rename: %w", err)
	}
	return nil
}

// Count reports how many records are stored. Convenience for stats; equivalent
// to len(Load()) but documents intent.
func (s *Store) Count() (int, error) {
	records, err := s.Load()
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

// trimSpace trims ASCII whitespace from both ends of b without allocating.
// (Avoids importing bytes just for one call and keeps the hot Load path lean.)
func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && isSpace(b[start]) {
		start++
	}
	end := len(b)
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f'
}
