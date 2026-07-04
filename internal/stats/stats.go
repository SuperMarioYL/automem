// Package stats computes and formats the "is-it-used" numbers — automem's proof
// that the memory it stores is actually surfaced into later sessions, not just
// written and forgotten. This is the PMB "is-it-used" hook made concrete: a
// memory tool nobody can measure is a memory tool nobody trusts.
//
// The two headline numbers are:
//
//   - Stored:   how many records exist.
//   - Injected: how many DISTINCT records have been surfaced by recall at least
//     once (Injected > 0). The tagline "3 stored, 2 injected" reads as "of 3
//     memories, 2 have actually been recalled into a later session".
//
// A handful of secondary numbers (total injections, per-agent breakdown) round
// out `automem stats` without turning it into a dashboard.
package stats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SuperMarioYL/automem/internal/store"
)

// Stats is the computed summary of a memory store.
type Stats struct {
	// Stored is the number of records in the store.
	Stored int
	// Injected is the number of DISTINCT records with Injected > 0 — i.e. how
	// many stored memories have been surfaced into a later session at least
	// once. This is the headline "is-it-used" number.
	Injected int
	// TotalInjections is the sum of every record's Injected counter — how many
	// times, in total, recall has surfaced a memory (a record recalled 3 times
	// counts 3 here but 1 in Injected).
	TotalInjections int
	// ByAgent breaks the stored count down by the capturing agent.
	ByAgent map[string]int
}

// Compute derives Stats from a slice of records (as returned by store.Load).
func Compute(records []store.Record) Stats {
	s := Stats{ByAgent: map[string]int{}}
	s.Stored = len(records)
	for _, r := range records {
		if r.Injected > 0 {
			s.Injected++
			s.TotalInjections += r.Injected
		}
		agent := r.Agent
		if agent == "" {
			agent = "unknown"
		}
		s.ByAgent[agent]++
	}
	return s
}

// InjectionRate is the fraction of stored memories that have been injected at
// least once, in [0,1]. Zero stored records yields 0 (not NaN).
func (s Stats) InjectionRate() float64 {
	if s.Stored == 0 {
		return 0
	}
	return float64(s.Injected) / float64(s.Stored)
}

// Headline is the one-line "is-it-used" tagline, e.g. "3 stored, 2 injected".
// It is the single most important line automem prints, so it gets its own
// method (the demo asserts on it).
func (s Stats) Headline() string {
	return fmt.Sprintf("%d stored, %d injected", s.Stored, s.Injected)
}

// Format renders the full human-readable report for `automem stats`. The first
// line is always Headline() so scripts and the demo can grep it; the rest is
// indented detail. Empty stores print a friendly first-run hint instead of a
// wall of zeros.
func (s Stats) Format() string {
	if s.Stored == 0 {
		return "0 stored, 0 injected\n(no memories yet — run a session with a supported agent, then check back)"
	}

	var b strings.Builder
	b.WriteString(s.Headline())
	b.WriteByte('\n')
	fmt.Fprintf(&b, "  injection rate: %.0f%% (%d of %d memories recalled at least once)\n",
		s.InjectionRate()*100, s.Injected, s.Stored)
	fmt.Fprintf(&b, "  total injections: %d\n", s.TotalInjections)

	if len(s.ByAgent) > 0 {
		b.WriteString("  by agent:\n")
		for _, a := range sortedAgents(s.ByAgent) {
			fmt.Fprintf(&b, "    %-12s %d\n", a, s.ByAgent[a])
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// sortedAgents returns the agent keys in a stable order: highest count first,
// then alphabetical, so the report is deterministic.
func sortedAgents(byAgent map[string]int) []string {
	agents := make([]string, 0, len(byAgent))
	for a := range byAgent {
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool {
		if byAgent[agents[i]] != byAgent[agents[j]] {
			return byAgent[agents[i]] > byAgent[agents[j]]
		}
		return agents[i] < agents[j]
	})
	return agents
}
