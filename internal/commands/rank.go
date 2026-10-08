package commands

import (
	"sort"

	"github.com/meidori/mem/internal/vector"
)

const (
	// defaultMinScore is the cosine similarity below which a memory is not
	// shown at all. Deliberately low: nomic-embed-text scores for short
	// commands (and non-English queries) sit well below 1 even when relevant.
	defaultMinScore = 0.4

	// scoreGap drops results that trail the best match by more than this,
	// so one strong hit is not padded out with unrelated memories.
	scoreGap = 0.15
)

// rankHits collapses per-chunk hits into one score per memory (its best
// chunk), drops weak matches and returns at most limit memories, best first.
// A minScore of 0 or less disables filtering. hits must already be sorted by
// score, best first.
func rankHits(hits []vector.Hit, minScore float64, limit int) []vector.Hit {
	var ranked []vector.Hit
	seen := make(map[int64]bool, len(hits))
	for _, h := range hits {
		if seen[h.MemoryID] {
			continue
		}
		seen[h.MemoryID] = true

		if minScore > 0 {
			if h.Score < minScore {
				break
			}
			if len(ranked) > 0 && h.Score < ranked[0].Score-scoreGap {
				break
			}
		}
		ranked = append(ranked, h)
		if len(ranked) == limit {
			break
		}
	}
	return ranked
}

// rrfK damps reciprocal rank fusion so that being near the top of either
// list matters more than the exact position (the usual value from the
// original RRF paper).
const rrfK = 60

// result is one memory in the final hybrid ranking.
type result struct {
	MemoryID int64
	Score    float64 // cosine similarity, or -1 if semantic search didn't see it
	Keyword  bool    // matched the keyword index
}

// fuse merges semantic results (already filtered by rankHits) with keyword
// matches using reciprocal rank fusion: a memory found by both lists rises to
// the top, and an exact keyword hit can surface a memory the embedding
// missed. allHits supplies cosine scores for keyword-only memories when
// semantic search saw them below the threshold.
func fuse(semantic, allHits []vector.Hit, keyword []int64, limit int) []result {
	fused := map[int64]float64{}
	byID := map[int64]*result{}
	var order []int64

	add := func(id int64, rank int) *result {
		fused[id] += 1 / float64(rrfK+rank+1)
		if r, ok := byID[id]; ok {
			return r
		}
		r := &result{MemoryID: id, Score: -1}
		byID[id] = r
		order = append(order, id)
		return r
	}

	for i, h := range semantic {
		add(h.MemoryID, i).Score = h.Score
	}
	for i, id := range keyword {
		add(id, i).Keyword = true
	}
	for _, h := range allHits {
		if r, ok := byID[h.MemoryID]; ok && r.Score < 0 {
			r.Score = h.Score // best chunk: allHits is sorted best first
		}
	}

	// Stable sort keeps semantic order on ties.
	sort.SliceStable(order, func(i, j int) bool { return fused[order[i]] > fused[order[j]] })
	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]result, len(order))
	for i, id := range order {
		out[i] = *byID[id]
	}
	return out
}
