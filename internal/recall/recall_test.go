package recall

import (
	"testing"
	"time"

	"github.com/SuperMarioYL/automem/internal/store"
)

func rec(id, summary string, tags []string, ageDays float64, now time.Time) store.Record {
	return store.Record{
		ID:      id,
		TS:      now.Add(-time.Duration(ageDays*24) * time.Hour).UnixMilli(),
		Summary: summary,
		Tags:    tags,
	}
}

func TestRecallSurfacesRelevantMemory(t *testing.T) {
	now := time.Now()
	records := []store.Record{
		rec("a", "refactor auth to use dataclasses", []string{"auth", "python"}, 1, now),
		rec("b", "update the readme and changelog", []string{"markdown"}, 1, now),
		rec("c", "add pagination to the api list endpoint", []string{"api"}, 1, now),
	}

	res := Recall(records, "what did we decide about auth?", Options{TopK: 2, Now: now})
	if len(res) == 0 {
		t.Fatal("expected at least one recall result")
	}
	if res[0].Record.ID != "a" {
		t.Errorf("expected the auth memory (a) to rank first, got %q", res[0].Record.ID)
	}
}

func TestRecallZeroOverlapExcluded(t *testing.T) {
	now := time.Now()
	records := []store.Record{
		rec("a", "kubernetes deployment yaml", []string{"yaml"}, 1, now),
	}
	res := Recall(records, "quantum entanglement lecture", Options{Now: now})
	if len(res) != 0 {
		t.Errorf("query with no shared token should return nothing, got %d", len(res))
	}
}

func TestRecencyBreaksTextualTies(t *testing.T) {
	now := time.Now()
	// Same summary/tags → identical overlap; recency must decide.
	records := []store.Record{
		rec("old", "fix the auth bug", []string{"auth"}, 60, now),
		rec("new", "fix the auth bug", []string{"auth"}, 1, now),
	}
	res := Recall(records, "auth bug", Options{TopK: 2, Now: now})
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	if res[0].Record.ID != "new" {
		t.Errorf("more recent equal-overlap memory should rank first, got %q", res[0].Record.ID)
	}
	if res[0].Score <= res[1].Score {
		t.Errorf("recent score %.4f should exceed old score %.4f", res[0].Score, res[1].Score)
	}
}

func TestPathTagBoost(t *testing.T) {
	now := time.Now()
	// Both mention "auth" but only "tagged" carries it as a path tag.
	tagged := rec("tagged", "some session about login flow", []string{"auth"}, 1, now)
	inline := rec("inline", "some session about auth login flow", nil, 1, now)
	res := Recall([]store.Record{tagged, inline}, "auth", Options{TopK: 2, Now: now})
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	if res[0].Record.ID != "tagged" {
		t.Errorf("path-tag match should outrank a plain summary match, got %q first", res[0].Record.ID)
	}
	if res[0].Overlap <= res[1].Overlap {
		t.Errorf("tag overlap %.3f should exceed inline overlap %.3f", res[0].Overlap, res[1].Overlap)
	}
}

func TestTopKCaps(t *testing.T) {
	now := time.Now()
	var records []store.Record
	for i := 0; i < 10; i++ {
		records = append(records, rec(string(rune('a'+i)), "auth session", []string{"auth"}, float64(i), now))
	}
	res := Recall(records, "auth", Options{TopK: 3, Now: now})
	if len(res) != 3 {
		t.Errorf("TopK=3 should cap results at 3, got %d", len(res))
	}
	// Newest three (ages 0,1,2 → ids a,b,c) should win.
	for i, want := range []string{"a", "b", "c"} {
		if res[i].Record.ID != want {
			t.Errorf("rank %d = %q, want %q", i, res[i].Record.ID, want)
		}
	}
}

func TestEmptyQueryReturnsNothing(t *testing.T) {
	now := time.Now()
	records := []store.Record{rec("a", "auth", []string{"auth"}, 1, now)}
	if res := Recall(records, "   ", Options{Now: now}); len(res) != 0 {
		t.Errorf("blank query should return nothing, got %d", len(res))
	}
	// A query of only stopwords is also empty.
	if res := Recall(records, "what did we do", Options{Now: now}); len(res) != 0 {
		t.Errorf("all-stopword query should return nothing, got %d", len(res))
	}
}

func TestRecencyDecayHalfLife(t *testing.T) {
	now := time.Now()
	hl := DefaultHalfLife
	full := recencyDecay(now, now.UnixMilli(), hl)
	half := recencyDecay(now, now.Add(-hl).UnixMilli(), hl)
	if full < 0.999 {
		t.Errorf("age 0 decay should be ~1, got %.4f", full)
	}
	if half < 0.49 || half > 0.51 {
		t.Errorf("age == half-life decay should be ~0.5, got %.4f", half)
	}
	// Missing/zero timestamp is treated as fresh, not penalized.
	if recencyDecay(now, 0, hl) != 1 {
		t.Errorf("zero timestamp should decay to 1, got %.4f", recencyDecay(now, 0, hl))
	}
}

func TestIDsExtractsInRankOrder(t *testing.T) {
	results := []Result{
		{Record: store.Record{ID: "x"}},
		{Record: store.Record{ID: "y"}},
	}
	ids := IDs(results)
	if len(ids) != 2 || ids[0] != "x" || ids[1] != "y" {
		t.Errorf("IDs = %v, want [x y]", ids)
	}
}
