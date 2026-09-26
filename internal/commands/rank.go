package commands

import "github.com/meidori/mem/internal/vector"

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
