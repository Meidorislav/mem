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

func TestFuse(t *testing.T) {
	allHits := []vector.Hit{
		{MemoryID: 1, Score: 0.80},
		{MemoryID: 2, Score: 0.75},
		{MemoryID: 3, Score: 0.30}, // below threshold semantically
	}
	semantic := rankHits(allHits, 0.4, 5) // 1, 2

	tests := []struct {
		name    string
		keyword []int64
		limit   int
		want    []result
	}{
		{
			name:  "semantic only",
			limit: 5,
			want:  []result{{1, 0.80, false}, {2, 0.75, false}},
		},
		{
			name:    "found by both rises to the top",
			keyword: []int64{2},
			limit:   5,
			want:    []result{{2, 0.75, true}, {1, 0.80, false}},
		},
		{
			name:    "keyword rescues a weak semantic match and keeps its score",
			keyword: []int64{3, 9},
			limit:   5,
			want:    []result{{1, 0.80, false}, {3, 0.30, true}, {2, 0.75, false}, {9, -1, true}},
		},
		{
			name:    "limit",
			keyword: []int64{3},
			limit:   1,
			want:    []result{{1, 0.80, false}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fuse(semantic, allHits, tt.keyword, tt.limit); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fuse = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResultNote(t *testing.T) {
	for _, tt := range []struct {
		r    result
		want string
	}{
		{result{Score: 0.734}, "0.73"},
		{result{Score: 0.5, Keyword: true}, "0.50, keyword"},
		{result{Score: -1, Keyword: true}, "keyword"},
	} {
		if got := resultNote(tt.r); got != tt.want {
			t.Errorf("resultNote(%+v) = %q, want %q", tt.r, got, tt.want)
		}
	}
}
