package commands

import (
	"reflect"
	"testing"

	"github.com/meidori/mem/internal/vector"
)

func TestRankHits(t *testing.T) {
	hits := []vector.Hit{
		{MemoryID: 1, Score: 0.82},
		{MemoryID: 1, Score: 0.80}, // second chunk of the same memory
		{MemoryID: 2, Score: 0.74},
		{MemoryID: 3, Score: 0.66}, // more than scoreGap behind the best
		{MemoryID: 4, Score: 0.30},
	}

	tests := []struct {
		name     string
		minScore float64
		limit    int
		want     []int64
	}{
		{"gap cuts the tail", 0.4, 5, []int64{1, 2}},
		{"limit", 0.4, 1, []int64{1}},
		{"min score cuts everything", 0.9, 5, nil},
		{"zero disables filtering", 0, 5, []int64{1, 2, 3, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []int64
			for _, h := range rankHits(hits, tt.minScore, tt.limit) {
				got = append(got, h.MemoryID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rankHits = %v, want %v", got, tt.want)
			}
		})
	}
}
