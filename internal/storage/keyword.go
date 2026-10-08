package storage

import (
	"fmt"
	"strings"
	"unicode"
)

// bm25 column weights for memory_fts (title, description, tags, commands):
// a hit in the title or a command says more than one in a long description.
const ftsRank = "bm25(memory_fts, 3.0, 1.0, 2.0, 2.0)"

// KeywordSearch returns IDs of memories whose text contains words from
// query, best match first. It complements semantic search for exact tokens
// such as error codes, flags and service names.
func (s *Store) KeywordSearch(query string, limit int) ([]int64, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}

	rows, err := s.db.Query(`
		SELECT rowid FROM memory_fts
		WHERE memory_fts MATCH ?
		ORDER BY `+ftsRank+`
		LIMIT ?
	`, match, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan keyword hit: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate keyword hits: %w", err)
	}
	return ids, nil
}

// ftsQuery turns free text into an FTS5 query: every meaningful word,
// quoted (so user input can't inject FTS syntax) and prefix-matched, OR-ed
// together and left to bm25 to rank.
func ftsQuery(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var terms []string
	seen := make(map[string]bool, len(words))
	for _, w := range words {
		if len([]rune(w)) < 2 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, `"`+w+`"*`)
	}
	return strings.Join(terms, " OR ")
}

// stopWords are dropped from keyword queries: they appear in almost every
// question ("how did I fix…", "как я чинил…") and would match noise.
var stopWords = setOf(
	// English
	"a", "an", "the", "and", "or", "of", "to", "in", "on", "at", "for", "with",
	"by", "from", "is", "are", "was", "were", "be", "it", "this", "that", "my",
	"me", "do", "did", "does", "how", "what", "when", "where", "which", "why",
	"can", "could", "should", "last", "again", "use", "used",
	// Russian
	"как", "что", "где", "когда", "почему", "зачем", "какой", "какая", "какие",
	"это", "этот", "эта", "то", "та", "те", "и", "или", "а", "но", "в", "во",
	"на", "по", "из", "за", "от", "до", "для", "с", "со", "у", "о", "об",
	"я", "мы", "ты", "мне", "меня", "мой", "моя", "мои", "свой", "был", "была",
	"было", "были", "не", "ли", "же", "бы", "уже", "ещё", "еще", "раз",
	"можно", "надо", "нужно",
)

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}
