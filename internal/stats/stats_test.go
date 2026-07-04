package stats

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/automem/internal/store"
)

func TestComputeCountsStoredAndInjected(t *testing.T) {
	records := []store.Record{
		{ID: "a", Agent: "claude-code", Injected: 0},
		{ID: "b", Agent: "claude-code", Injected: 1},
		{ID: "c", Agent: "aider", Injected: 3},
	}
	s := Compute(records)

	if s.Stored != 3 {
		t.Errorf("Stored = %d, want 3", s.Stored)
	}
	if s.Injected != 2 {
		t.Errorf("Injected (distinct) = %d, want 2", s.Injected)
	}
	if s.TotalInjections != 4 {
		t.Errorf("TotalInjections = %d, want 4", s.TotalInjections)
	}
	if s.ByAgent["claude-code"] != 2 || s.ByAgent["aider"] != 1 {
		t.Errorf("ByAgent wrong: %v", s.ByAgent)
	}
}

func TestHeadlineFormat(t *testing.T) {
	s := Compute([]store.Record{
		{ID: "a", Injected: 1},
		{ID: "b", Injected: 0},
		{ID: "c", Injected: 0},
	})
	if got := s.Headline(); got != "3 stored, 1 injected" {
		t.Errorf("Headline = %q, want %q", got, "3 stored, 1 injected")
	}
}

func TestInjectionRate(t *testing.T) {
	s := Compute([]store.Record{
		{ID: "a", Injected: 1},
		{ID: "b", Injected: 0},
	})
	if r := s.InjectionRate(); r != 0.5 {
		t.Errorf("InjectionRate = %.3f, want 0.5", r)
	}
	// Empty store must not divide by zero.
	if r := Compute(nil).InjectionRate(); r != 0 {
		t.Errorf("empty InjectionRate = %.3f, want 0", r)
	}
}

func TestFormatEmptyStoreHint(t *testing.T) {
	out := Compute(nil).Format()
	if !strings.HasPrefix(out, "0 stored, 0 injected") {
		t.Errorf("empty Format should start with the headline, got %q", out)
	}
	if !strings.Contains(out, "no memories yet") {
		t.Errorf("empty Format should hint first-run, got %q", out)
	}
}

func TestFormatFullReport(t *testing.T) {
	s := Compute([]store.Record{
		{ID: "a", Agent: "claude-code", Injected: 2},
		{ID: "b", Agent: "claude-code", Injected: 0},
		{ID: "c", Agent: "aider", Injected: 1},
	})
	out := s.Format()
	if !strings.HasPrefix(out, "3 stored, 2 injected") {
		t.Errorf("Format should lead with the headline, got:\n%s", out)
	}
	for _, want := range []string{"injection rate", "total injections: 3", "claude-code", "aider"} {
		if !strings.Contains(out, want) {
			t.Errorf("Format missing %q, got:\n%s", want, out)
		}
	}
}

func TestByAgentUnknownLabel(t *testing.T) {
	s := Compute([]store.Record{{ID: "a", Agent: ""}})
	if s.ByAgent["unknown"] != 1 {
		t.Errorf("blank agent should count as 'unknown', got %v", s.ByAgent)
	}
}
