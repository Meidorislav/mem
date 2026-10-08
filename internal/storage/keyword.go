package storage

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/kljensen/snowball"
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

// minPrefixLen is the shortest term matched as a prefix. Shorter stems
// match whole words only: "лог"* (from "логи") would also hit "логическую",
// and "go"* every "google".
const minPrefixLen = 4

// ftsQuery turns free text into an FTS5 query: every meaningful word,
// stemmed and quoted (so user input can't inject FTS syntax), longer ones
// prefix-matched, OR-ed together and left to bm25 to rank.
func ftsQuery(text string) string {
	var terms []string
	seen := map[string]bool{}
	for _, w := range words(text) {
		if len([]rune(w)) < 2 || stopWords[w] {
			continue
		}
		t := stem(w)
		if seen[t] {
			continue
		}
		seen[t] = true
		term := `"` + t + `"`
		if len([]rune(t)) >= minPrefixLen {
			term += "*"
		}
		terms = append(terms, term)
	}
	return strings.Join(terms, " OR ")
}

// ftsText is what memory_fts stores for a field: each word followed by its
// stem when that differs, so "ошибку" in a command is found by "ошибка"
// (both stem to "ошибк") and "коммита" by "коммит" (prefix of the word).
func ftsText(text string) string {
	var out []string
	for _, w := range words(text) {
		out = append(out, w)
		if s := stem(w); s != w {
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}

// words splits text into lowercase letter/digit runs, the way the
// unicode61 tokenizer does. ё is folded to е, as people type both.
func words(text string) []string {
	text = strings.ReplaceAll(strings.ToLower(text), "ё", "е")
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// stem reduces a word to its Snowball stem: Russian for Cyrillic words,
// English for Latin ones. Words it can't handle, and stems shorter than two
// letters, come back unchanged.
func stem(w string) string {
	lang := ""
	for _, r := range w {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			lang = "russian"
		case lang == "" && r >= 'a' && r <= 'z':
			lang = "english"
		}
	}
	if lang == "" {
		return w
	}
	s, err := snowball.Stem(w, lang, false)
	if err != nil || len([]rune(s)) < 2 {
		return w
	}
	return s
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
