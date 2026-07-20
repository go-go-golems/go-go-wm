package launcher

import (
	"math"
	"strings"
)

// Fuzzy matching: a pure subsequence scorer with bonuses, per the
// design (no dependency). Query chars must appear in order,
// case-insensitively; score favors word-boundary hits, consecutive
// runs, early matches, and shorter fields.

const (
	boundaryBonus    = 3.0
	consecutiveBonus = 2.0
	leadingPenalty   = 0.1 // per unmatched char before the first hit
	baseHit          = 1.0
)

// field weights: a label hit beats a keyword hit beats an id hit.
const (
	weightLabel   = 1.0
	weightKeyword = 0.8
	weightID      = 0.6
)

// scoreCommand returns the best weighted field score for query against
// cmd, and whether any field matched.
func scoreCommand(query string, cmd Command) (float64, bool) {
	best := math.Inf(-1)
	ok := false
	if s, m := subsequenceScore(query, cmd.Label); m {
		best, ok = math.Max(best, s*weightLabel), true
	}
	for _, k := range cmd.Keywords {
		if s, m := subsequenceScore(query, k); m {
			best, ok = math.Max(best, s*weightKeyword), true
		}
	}
	if s, m := subsequenceScore(query, cmd.ID); m {
		best, ok = math.Max(best, s*weightID), true
	}
	return best, ok
}

// subsequenceScore matches query as a subsequence of field (both
// lowercased). Greedy left-to-right with per-hit bonuses; not an
// optimal-alignment search — good enough for launcher-sized fields,
// and deterministic.
func subsequenceScore(query, field string) (float64, bool) {
	q := strings.ToLower(query)
	f := strings.ToLower(field)
	if q == "" || f == "" {
		return 0, false
	}
	score := 0.0
	qi := 0
	prevHit := -2 // last matched index in f
	firstHit := -1
	for fi := 0; fi < len(f) && qi < len(q); fi++ {
		if f[fi] != q[qi] {
			continue
		}
		hit := baseHit
		if fi == 0 || isBoundary(f[fi-1]) {
			hit += boundaryBonus
		}
		if fi == prevHit+1 {
			hit += consecutiveBonus
		}
		if firstHit < 0 {
			firstHit = fi
		}
		score += hit
		prevHit = fi
		qi++
	}
	if qi < len(q) {
		return 0, false
	}
	score -= leadingPenalty * float64(firstHit)
	// Slight preference for shorter fields (tighter matches).
	score -= 0.01 * float64(len(f))
	return score, true
}

func isBoundary(prev byte) bool {
	switch prev {
	case ' ', '-', '_', '.', ':', '/':
		return true
	}
	return false
}

func log1p(x float64) float64 { return math.Log1p(x) }
