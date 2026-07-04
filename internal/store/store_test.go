package store

import (
	"os"
	"path/filepath"
	"testing"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestAppendAssignsIDAndTS(t *testing.T) {
	s := newTempStore(t)

	got, err := s.Append(Record{Summary: "hello", Tags: []string{"go"}})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got.ID == "" {
		t.Error("Append did not assign an ID")
	}
	if got.TS == 0 {
		t.Error("Append did not assign a timestamp")
	}
}

func TestAppendThenLoadRoundTrip(t *testing.T) {
	s := newTempStore(t)

	in := []Record{
		{Summary: "refactor auth", Tags: []string{"auth", "go"}},
		{Summary: "add tests", Tags: []string{"test"}},
	}
	for _, r := range in {
		if _, err := s.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("Load returned %d records, want %d", len(out), len(in))
	}
	// Stored order must be preserved.
	if out[0].Summary != "refactor auth" || out[1].Summary != "add tests" {
		t.Errorf("record order not preserved: %q, %q", out[0].Summary, out[1].Summary)
	}
	if len(out[0].Tags) != 2 || out[0].Tags[0] != "auth" {
		t.Errorf("tags not round-tripped: %v", out[0].Tags)
	}
}

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	s := newTempStore(t) // dir exists, file does not

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load on missing file should not error, got: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("Load on missing file should be empty, got %d", len(out))
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	s := newTempStore(t)
	if _, err := s.Append(Record{Summary: "good one"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Inject a corrupt line and a blank line by hand.
	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("this is not json\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()
	if _, err := s.Append(Record{Summary: "good two"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 valid records past the corrupt line, got %d", len(out))
	}
	if out[0].Summary != "good one" || out[1].Summary != "good two" {
		t.Errorf("corrupt-line skip lost good records: %+v", out)
	}
}

func TestMarkInjectedBumpsAndPersists(t *testing.T) {
	s := newTempStore(t)
	a, _ := s.Append(Record{Summary: "a"})
	b, _ := s.Append(Record{Summary: "b"})
	_, _ = s.Append(Record{Summary: "c"})

	bumped, err := s.MarkInjected([]string{a.ID, b.ID, "nonexistent"})
	if err != nil {
		t.Fatalf("MarkInjected: %v", err)
	}
	if bumped != 2 {
		t.Fatalf("expected 2 bumped, got %d", bumped)
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byID := map[string]Record{}
	for _, r := range out {
		byID[r.ID] = r
	}
	if byID[a.ID].Injected != 1 || byID[b.ID].Injected != 1 {
		t.Errorf("injected not persisted: a=%d b=%d", byID[a.ID].Injected, byID[b.ID].Injected)
	}
	// Second mark of the same id increments again (counter, not a flag).
	if _, err := s.MarkInjected([]string{a.ID}); err != nil {
		t.Fatalf("MarkInjected 2: %v", err)
	}
	out2, _ := s.Load()
	for _, r := range out2 {
		if r.ID == a.ID && r.Injected != 2 {
			t.Errorf("expected a.Injected==2 after second mark, got %d", r.Injected)
		}
	}
}

func TestMarkInjectedEmptyIsNoOp(t *testing.T) {
	s := newTempStore(t)
	s.Append(Record{Summary: "a"})
	n, err := s.MarkInjected(nil)
	if err != nil || n != 0 {
		t.Fatalf("empty MarkInjected should be a no-op, got n=%d err=%v", n, err)
	}
}

func TestRewriteIsAtomicNoTempLeft(t *testing.T) {
	s := newTempStore(t)
	a, _ := s.Append(Record{Summary: "a"})
	if _, err := s.MarkInjected([]string{a.ID}); err != nil {
		t.Fatalf("MarkInjected: %v", err)
	}
	// No leftover temp files in the store dir.
	entries, err := os.ReadDir(filepath.Dir(s.Path()))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || filepath.Base(e.Name()) != "store.jsonl" {
			if e.Name() != "store.jsonl" {
				t.Errorf("unexpected leftover file after atomic rewrite: %s", e.Name())
			}
		}
	}
}

func TestDefaultDirHonorsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AUTOMEM_DIR", dir)
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if got != dir {
		t.Errorf("DefaultDir = %q, want override %q", got, dir)
	}
}

func TestCount(t *testing.T) {
	s := newTempStore(t)
	if n, _ := s.Count(); n != 0 {
		t.Errorf("empty Count = %d, want 0", n)
	}
	s.Append(Record{Summary: "x"})
	s.Append(Record{Summary: "y"})
	if n, _ := s.Count(); n != 2 {
		t.Errorf("Count = %d, want 2", n)
	}
}
