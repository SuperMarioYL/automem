// Package recall scores stored memories against a query and returns the top-K —
// with no embeddings, no vector DB, and no network. That is automem's
// load-bearing bet: a path-weighted bag-of-words overlap times a recency-decay
// kernel is "good enough" to resurface the right prior session context.
//
// The score is:
//
//	score(query, r) = lexical_overlap(query, r.Summary+r.Tags) * recency_decay(now - r.TS)
//
// lexical_overlap is a token-overlap measure where tokens that also appear in
// the record's Tags (i.e. file paths / languages) count extra, because a query
// mentioning "auth.py" should strongly prefer the session that actually touched
// auth.py. recency_decay is an exponential half-life kernel so a fresh memory
// outranks a stale one of equal textual relevance.
package recall

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SuperMarioYL/automem/internal/store"
)

// DefaultTopK is how many memories recall surfaces when the caller doesn't
// specify. Small on purpose: injecting too much stale context is its own kind
// of noise.
const DefaultTopK = 3

// DefaultHalfLife is the recency half-life: a memory this old scores half what
// an otherwise-identical brand-new one would. Two weeks matches a typical
// coding cadence — last sprint's context is still relevant, last quarter's is
// mostly not.
const DefaultHalfLife = 14 * 24 * time.Hour

// pathTagWeight is how much more a token that matches a record Tag (a path /
// language token) counts than a plain summary-word match.
const pathTagWeight = 2.0

// Options tunes scoring. The zero value is valid: TopK, HalfLife and Now fall
// back to sensible defaults.
type Options struct {
	TopK     int           // number of results (<=0 → DefaultTopK)
	HalfLife time.Duration // recency half-life (<=0 → DefaultHalfLife)
	Now      time.Time     // "now" for recency (zero → time.Now())
	// MinScore drops results scoring at or below this threshold. Default 0
	// keeps any positive-overlap match; raise it to demand stronger relevance.
	MinScore float64
}

// Result is one scored, ranked memory. Score is the final combined value;
// Overlap and Recency are exposed so callers/tests can see why it ranked where
// it did.
type Result struct {
	Record  store.Record
	Score   float64
	Overlap float64
	Recency float64
}

// Recall scores every record against the query and returns the top-K by score,
// highest first. Records with zero lexical overlap are never returned (a memory
// that shares no word with the query is not "relevant" no matter how recent).
// Ties break by recency, then by ID, so the ordering is deterministic.
func Recall(records []store.Record, query string, opts Options) []Result {
	topK := opts.TopK
	if topK <= 0 {
		topK = DefaultTopK
	}
	halfLife := opts.HalfLife
	if halfLife <= 0 {
		halfLife = DefaultHalfLife
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	qTokens := tokenSet(query)
	if len(qTokens) == 0 {
		return nil
	}

	results := make([]Result, 0, len(records))
	for _, r := range records {
		overlap := lexicalOverlap(qTokens, r)
		if overlap <= 0 {
			continue
		}
		recency := recencyDecay(now, r.TS, halfLife)
		score := overlap * recency
		if score <= opts.MinScore {
			continue
		}
		results = append(results, Result{
			Record:  r,
			Score:   score,
			Overlap: overlap,
			Recency: recency,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Record.TS != results[j].Record.TS {
			return results[i].Record.TS > results[j].Record.TS // newer first
		}
		return results[i].Record.ID > results[j].Record.ID // stable tiebreak
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

// IDs is a convenience that pulls the record IDs out of a result slice, in
// rank order — exactly the argument store.MarkInjected wants.
func IDs(results []Result) []string {
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.Record.ID)
	}
	return ids
}

// lexicalOverlap measures how much of the query is covered by the record.
// Plain summary-word matches count 1.0; matches that also hit a record Tag
// (path/language token) count pathTagWeight. The raw weighted hit count is
// normalized by the query size so a two-word query and a ten-word query live on
// the same 0..~pathTagWeight scale — otherwise long queries would dominate
// purely by having more chances to match.
func lexicalOverlap(qTokens map[string]int, r store.Record) float64 {
	docTokens := tokenSet(r.Summary)
	tagSet := make(map[string]struct{}, len(r.Tags))
	for _, t := range r.Tags {
		for tok := range tokenSet(t) {
			tagSet[tok] = struct{}{}
		}
	}

	var hits float64
	var qTotal float64
	for tok, qCount := range qTokens {
		qTotal += float64(qCount)
		_, inDoc := docTokens[tok]
		_, inTag := tagSet[tok]
		switch {
		case inTag:
			hits += pathTagWeight * float64(qCount)
		case inDoc:
			hits += float64(qCount)
		}
	}
	if qTotal == 0 {
		return 0
	}
	return hits / qTotal
}

// recencyDecay is an exponential half-life kernel in [0,1]. At age 0 it is 1;
// at age == halfLife it is 0.5; it never goes negative. A record with a missing
// or future timestamp (age <= 0) gets the full 1.0 rather than being penalized.
func recencyDecay(now time.Time, tsMilli int64, halfLife time.Duration) float64 {
	if tsMilli <= 0 {
		return 1
	}
	age := now.Sub(time.UnixMilli(tsMilli))
	if age <= 0 {
		return 1
	}
	return math.Exp2(-float64(age) / float64(halfLife))
}

// tokenRe splits on any non-alphanumeric run, so "auth.py", "user_auth" and
// "auth,py" all yield the same word tokens.
var tokenRe = regexp.MustCompile(`[a-z0-9]+`)

// tokenSet lowercases, tokenizes, drops stopwords and 1-char tokens, and
// returns each token mapped to its count in the text.
func tokenSet(text string) map[string]int {
	text = strings.ToLower(text)
	out := map[string]int{}
	for _, tok := range tokenRe.FindAllString(text, -1) {
		if len(tok) < 2 {
			continue
		}
		if _, stop := stopwords[tok]; stop {
			continue
		}
		out[tok]++
	}
	return out
}

// stopwords are high-frequency English words that carry no matching signal;
// dropping them keeps "what did we decide about auth?" from matching every
// memory on "what"/"did"/"we"/"about".
var stopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "you": {}, "your": {}, "did": {},
	"was": {}, "are": {}, "our": {}, "out": {}, "with": {}, "what": {},
	"that": {}, "this": {}, "from": {}, "have": {}, "about": {}, "into": {},
	"they": {}, "them": {}, "then": {}, "than": {}, "were": {}, "will": {},
	"can": {}, "how": {}, "why": {}, "who": {}, "when": {}, "where": {},
	"has": {}, "had": {}, "not": {}, "but": {}, "all": {}, "any": {},
	"use": {}, "using": {}, "get": {}, "got": {}, "let": {}, "its": {},
	"it": {}, "is": {}, "we": {}, "to": {}, "of": {}, "in": {}, "on": {},
	"at": {}, "by": {}, "an": {}, "as": {}, "be": {}, "do": {}, "or": {},
	"if": {}, "so": {}, "up": {}, "me": {}, "my": {}, "no": {},
}
