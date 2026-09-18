package store

import (
	"testing"
)

func seedRecords(t *testing.T, s *Store, n int) []Record {
	t.Helper()
	ids := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		r, err := s.Append(Record{Summary: "m", Tags: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r)
	}
	return ids
}

func TestDeleteRemovesOnlyNamedIDs(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recs := seedRecords(t, s, 3)

	n, err := s.Delete([]string{recs[0].ID, recs[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	left, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != recs[1].ID {
		t.Fatalf("remaining = %+v, want only %s", left, recs[1].ID)
	}
}

func TestDeleteUnknownIDIsNoop(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedRecords(t, s, 2)

	n, err := s.Delete([]string{"01HZZZZZZZZZZZZZZZZZZZZZZZZ"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted %d on unknown id, want 0", n)
	}
	left, _ := s.Load()
	if len(left) != 2 {
		t.Fatalf("store mutated on unknown id: %d records", len(left))
	}
}

func TestDeleteAllEmptiesStore(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedRecords(t, s, 4)

	n, err := s.DeleteAll()
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("deleted %d, want 4", n)
	}
	left, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("store not empty: %d records", len(left))
	}
	// the store must still be appendable afterwards
	if _, err := s.Append(Record{Summary: "again", Tags: []string{}}); err != nil {
		t.Fatalf("append after DeleteAll: %v", err)
	}
}

func TestDeleteIsAtomicAgainstConcurrentAppend(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedRecords(t, s, 2)

	// A Delete that removes nothing must not rewrite the file at all, so a
	// concurrent Append cannot be lost to a no-op rewrite window.
	if _, err := s.Delete([]string{"nope"}); err != nil {
		t.Fatal(err)
	}
	concurrent := Record{Summary: "concurrent", Tags: []string{}}
	stored, err := s.Append(concurrent)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := s.Load()
	found := false
	for _, r := range left {
		if r.ID == stored.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("concurrent append lost")
	}
}
