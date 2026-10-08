package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meidori/mem/internal/paths"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested memory does not exist.
var ErrNotFound = errors.New("memory not found")

const schema = `
-- Memory entry
CREATE TABLE IF NOT EXISTS memories (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  title       TEXT NOT NULL,
  source      TEXT NOT NULL CHECK(source IN ('save', 'remember', 'watch')),
  description TEXT,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Commands associated with a memory
CREATE TABLE IF NOT EXISTS commands (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  memory_id   INTEGER NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  position    INTEGER NOT NULL,
  command     TEXT NOT NULL,
  output      TEXT,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Tags
CREATE TABLE IF NOT EXISTS tags (
  id    INTEGER PRIMARY KEY AUTOINCREMENT,
  name  TEXT NOT NULL UNIQUE
);

-- Junction table for memories and tags (many-to-many)
CREATE TABLE IF NOT EXISTS memory_tags (
  memory_id INTEGER NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  tag_id    INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
  PRIMARY KEY (memory_id, tag_id)
);

-- Embedding model configuration
CREATE TABLE IF NOT EXISTS embedding_configs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  model_name TEXT NOT NULL,
  version    TEXT,
  dimensions INTEGER NOT NULL,
  is_active  BOOLEAN DEFAULT TRUE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexing status for chunks
CREATE TABLE IF NOT EXISTS embedding_status (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  memory_id           INTEGER NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  command_id          INTEGER REFERENCES commands(id) ON DELETE CASCADE,
  embedding_config_id INTEGER NOT NULL REFERENCES embedding_configs(id),
  chunk_index         INTEGER NOT NULL DEFAULT 0,
  content_hash        TEXT,
  indexed_at          DATETIME,
  needs_reindex       BOOLEAN DEFAULT FALSE,
  UNIQUE(memory_id, command_id, chunk_index, embedding_config_id)
);

-- Indices
CREATE INDEX IF NOT EXISTS idx_commands_memory_id    ON commands(memory_id);
CREATE INDEX IF NOT EXISTS idx_memory_tags_memory_id ON memory_tags(memory_id);
CREATE INDEX IF NOT EXISTS idx_memory_tags_tag_id    ON memory_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_embedding_status      ON embedding_status(memory_id, needs_reindex);
CREATE INDEX IF NOT EXISTS idx_memories_created_at   ON memories(created_at);

-- Full-text index for keyword search; rowid is the memory id. Rows are
-- written by SaveMemory and removed by the trigger below.
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
  title, description, tags, commands,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
  DELETE FROM memory_fts WHERE rowid = old.id;
END;
`

type Store struct {
	db *sql.DB
}

func NewStore() (*Store, error) {
	appDir, err := paths.AppDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("creating app dir: %w", err)
	}

	return NewStoreAt(filepath.Join(appDir, "mem.db"))
}

func NewStoreAt(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	store := &Store{db: db}
	if err := store.syncFTS(); err != nil {
		return nil, err
	}
	return store, nil
}

// ftsVersion is stored in PRAGMA user_version. Bump it when the text
// written to memory_fts changes, so existing databases rebuild the index.
// 1: words stemmed (see ftsText).
const ftsVersion = 1

// syncFTS keeps memory_fts complete: it rebuilds the index when it was
// written by an older version, and otherwise indexes memories missing from
// it (e.g. ones saved before keyword search existed).
func (s *Store) syncFTS() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	query := "SELECT id FROM memories WHERE id NOT IN (SELECT rowid FROM memory_fts) ORDER BY id"
	if version < ftsVersion {
		query = "SELECT id FROM memories ORDER BY id"
	} else {
		var missing bool
		err := s.db.QueryRow(`
			SELECT (SELECT COUNT(*) FROM memories) != (SELECT COUNT(*) FROM memory_fts)
		`).Scan(&missing)
		if err != nil {
			return fmt.Errorf("checking keyword index: %w", err)
		}
		if !missing {
			return nil
		}
	}

	ids, err := s.queryIDs(query)
	if err != nil {
		return err
	}
	memories, err := s.GetMemoriesByIDs(ids)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	prune := "DELETE FROM memory_fts WHERE rowid NOT IN (SELECT id FROM memories)"
	if version < ftsVersion {
		prune = "DELETE FROM memory_fts"
	}
	if _, err := tx.Exec(prune); err != nil {
		return fmt.Errorf("pruning keyword index: %w", err)
	}
	for i := range memories {
		if err := insertFTS(tx, &memories[i]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", ftsVersion)); err != nil {
		return fmt.Errorf("writing schema version: %w", err)
	}
	return tx.Commit()
}

func (s *Store) queryIDs(query string, args ...any) ([]int64, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query memory ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan memory id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory ids: %w", err)
	}
	return ids, nil
}

// insertFTS writes m's keyword index row.
func insertFTS(tx *sql.Tx, m *Memory) error {
	cmds := make([]string, len(m.Commands))
	for i, c := range m.Commands {
		cmds[i] = c.Command
	}
	_, err := tx.Exec(`
		INSERT INTO memory_fts (rowid, title, description, tags, commands)
		VALUES (?, ?, ?, ?, ?)
	`, m.ID, ftsText(m.Title), ftsText(m.Description), ftsText(strings.Join(m.Tags, " ")), ftsText(strings.Join(cmds, "\n")))
	if err != nil {
		return fmt.Errorf("index memory %d for keyword search: %w", m.ID, err)
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveMemory(m *Memory) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Zero timestamps mean "now"; imports pass the original ones.
	res, err := tx.Exec(`
		INSERT INTO memories (title, source, description, created_at, updated_at)
		VALUES (?, ?, ?, COALESCE(?, CURRENT_TIMESTAMP), COALESCE(?, CURRENT_TIMESTAMP))
	`, m.Title, m.Source, m.Description, nullTime(m.CreatedAt), nullTime(m.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}

	memID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	m.ID = memID

	if err := writeContents(tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// UpdateMemory replaces m's title, description, commands and tags. Its
// vectors become stale: the embedding_status rows are dropped so the next
// index run re-embeds it from scratch.
func (s *Store) UpdateMemory(m *Memory) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		UPDATE memories SET title = ?, description = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, m.Title, m.Description, m.ID)
	if err != nil {
		return fmt.Errorf("update memory %d: %w", m.ID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}

	for _, q := range []string{
		"DELETE FROM commands WHERE memory_id = ?",
		"DELETE FROM memory_tags WHERE memory_id = ?",
		"DELETE FROM embedding_status WHERE memory_id = ?",
		"DELETE FROM memory_fts WHERE rowid = ?",
	} {
		if _, err := tx.Exec(q, m.ID); err != nil {
			return fmt.Errorf("clear memory %d: %w", m.ID, err)
		}
	}

	if err := writeContents(tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

// writeContents inserts m's commands and tags and its keyword index row.
func writeContents(tx *sql.Tx, m *Memory) error {
	for i, cmd := range m.Commands {
		cmdRes, err := tx.Exec(`
			INSERT INTO commands (memory_id, position, command, output)
			VALUES (?, ?, ?, ?)
		`, m.ID, i, cmd.Command, cmd.Output)
		if err != nil {
			return fmt.Errorf("insert command %d: %w", i, err)
		}
		cmdID, err := cmdRes.LastInsertId()
		if err != nil {
			return err
		}
		m.Commands[i].ID = cmdID
		m.Commands[i].MemoryID = m.ID
		m.Commands[i].Position = i
	}

	seen := make(map[string]bool, len(m.Tags))
	for _, tagName := range m.Tags {
		if seen[tagName] {
			continue
		}
		seen[tagName] = true

		var tagID int64
		err := tx.QueryRow("SELECT id FROM tags WHERE name = ?", tagName).Scan(&tagID)
		if err == sql.ErrNoRows {
			res, err := tx.Exec("INSERT INTO tags (name) VALUES (?)", tagName)
			if err != nil {
				return fmt.Errorf("insert tag %s: %w", tagName, err)
			}
			tagID, err = res.LastInsertId()
			if err != nil {
				return err
			}
		} else if err != nil {
			return fmt.Errorf("query tag %s: %w", tagName, err)
		}

		_, err = tx.Exec("INSERT INTO memory_tags (memory_id, tag_id) VALUES (?, ?)", m.ID, tagID)
		if err != nil {
			return fmt.Errorf("insert memory_tag: %w", err)
		}
	}

	return insertFTS(tx, m)
}

// FindDuplicate returns the ID of a memory with the same title and the same
// commands in the same order, or 0 if there is none.
func (s *Store) FindDuplicate(title string, commands []string) (int64, error) {
	rows, err := s.db.Query("SELECT id FROM memories WHERE title = ? ORDER BY id", title)
	if err != nil {
		return 0, fmt.Errorf("query duplicates: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan duplicate id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate duplicates: %w", err)
	}

	candidates, err := s.GetMemoriesByIDs(ids)
	if err != nil {
		return 0, err
	}
	for _, c := range candidates {
		if len(c.Commands) != len(commands) {
			continue
		}
		same := true
		for i, cmd := range c.Commands {
			if cmd.Command != commands[i] {
				same = false
				break
			}
		}
		if same {
			return c.ID, nil
		}
	}
	return 0, nil
}

func (s *Store) DeleteMemory(id int64) error {
	res, err := s.db.Exec("DELETE FROM memories WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete memory %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListMemories(tag string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []any

	if tag != "" {
		query = `
			SELECT m.id
			FROM memories m
			JOIN memory_tags mt ON mt.memory_id = m.id
			JOIN tags t ON t.id = mt.tag_id
			WHERE t.name = ?
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT ?
		`
		args = []any{tag, limit}
	} else {
		query = `
			SELECT id
			FROM memories
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		`
		args = []any{limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query list memories: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan memory id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory ids: %w", err)
	}

	return s.GetMemoriesByIDs(ids)
}

func (s *Store) GetMemory(id int64) (*Memory, error) {
	memories, err := s.GetMemoriesByIDs([]int64{id})
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, ErrNotFound
	}
	return &memories[0], nil
}

// GetMemoriesByIDs loads memories with their commands and tags.
// Results preserve the order of ids; unknown ids are skipped so callers
// can pass vector-search results directly without existence checks.
func (s *Store) GetMemoriesByIDs(ids []int64) ([]Memory, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	byID := make(map[int64]*Memory, len(ids))

	rows, err := s.db.Query(`
		SELECT id, title, source, description, created_at, updated_at
		FROM memories
		WHERE id IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query memories: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m Memory
		var description sql.NullString
		if err := rows.Scan(&m.ID, &m.Title, &m.Source, &description, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		m.Description = description.String
		byID[m.ID] = &m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}

	if err := s.loadCommands(placeholders, args, byID); err != nil {
		return nil, err
	}
	if err := s.loadTags(placeholders, args, byID); err != nil {
		return nil, err
	}

	memories := make([]Memory, 0, len(byID))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok && !seen[id] {
			memories = append(memories, *m)
			seen[id] = true
		}
	}
	return memories, nil
}

func (s *Store) loadCommands(placeholders string, args []any, byID map[int64]*Memory) error {
	rows, err := s.db.Query(`
		SELECT id, memory_id, position, command, output, created_at
		FROM commands
		WHERE memory_id IN (`+placeholders+`)
		ORDER BY memory_id, position
	`, args...)
	if err != nil {
		return fmt.Errorf("query commands: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var c Command
		if err := rows.Scan(&c.ID, &c.MemoryID, &c.Position, &c.Command, &c.Output, &c.CreatedAt); err != nil {
			return fmt.Errorf("scan command: %w", err)
		}
		if m, ok := byID[c.MemoryID]; ok {
			m.Commands = append(m.Commands, c)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate commands: %w", err)
	}
	return nil
}

func (s *Store) loadTags(placeholders string, args []any, byID map[int64]*Memory) error {
	rows, err := s.db.Query(`
		SELECT mt.memory_id, t.name
		FROM memory_tags mt
		JOIN tags t ON t.id = mt.tag_id
		WHERE mt.memory_id IN (`+placeholders+`)
		ORDER BY mt.memory_id, t.name
	`, args...)
	if err != nil {
		return fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var memoryID int64
		var name string
		if err := rows.Scan(&memoryID, &name); err != nil {
			return fmt.Errorf("scan tag: %w", err)
		}
		if m, ok := byID[memoryID]; ok {
			m.Tags = append(m.Tags, name)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate tags: %w", err)
	}
	return nil
}

func (s *Store) GetOrCreateActiveEmbeddingConfig(modelName string, dims int) (*EmbeddingConfig, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var ec EmbeddingConfig
	err = tx.QueryRow(`
		SELECT id, model_name, version, dimensions, is_active, created_at
		FROM embedding_configs
		WHERE model_name = ? AND is_active = TRUE
		LIMIT 1
	`, modelName).Scan(&ec.ID, &ec.ModelName, &ec.Version, &ec.Dimensions, &ec.IsActive, &ec.CreatedAt)

	if err == sql.ErrNoRows {
		res, err := tx.Exec(`
			INSERT INTO embedding_configs (model_name, dimensions, is_active)
			VALUES (?, ?, TRUE)
		`, modelName, dims)
		if err != nil {
			return nil, fmt.Errorf("insert embedding config: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ec.ID = id
		ec.ModelName = modelName
		ec.Dimensions = dims
		ec.IsActive = true
		ec.CreatedAt = time.Now()
	} else if err != nil {
		return nil, fmt.Errorf("query embedding config: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &ec, nil
}

// UpsertEmbeddingStatus records that a chunk has been indexed. The update is
// matched with IS rather than ON CONFLICT because SQLite treats NULLs as
// distinct in UNIQUE constraints, so context chunks (command_id NULL) would
// otherwise be inserted again on every reindex.
func (s *Store) UpsertEmbeddingStatus(status *EmbeddingStatus) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(`
		UPDATE embedding_status
		SET content_hash = ?, indexed_at = CURRENT_TIMESTAMP, needs_reindex = FALSE
		WHERE memory_id = ? AND command_id IS ? AND chunk_index = ? AND embedding_config_id = ?
		RETURNING id
	`, status.ContentHash, status.MemoryID, status.CommandID, status.ChunkIndex, status.EmbeddingConfigID).Scan(&id)

	if err == sql.ErrNoRows {
		res, err := tx.Exec(`
			INSERT INTO embedding_status (memory_id, command_id, embedding_config_id, chunk_index, content_hash, indexed_at, needs_reindex)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, FALSE)
		`, status.MemoryID, status.CommandID, status.EmbeddingConfigID, status.ChunkIndex, status.ContentHash)
		if err != nil {
			return fmt.Errorf("insert embedding status: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("update embedding status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	status.ID = id
	return nil
}

func (s *Store) GetEmbeddingStatus(memoryID int64, commandID *int64, chunkIndex int, configID int64) (*EmbeddingStatus, error) {
	var st EmbeddingStatus

	// handle nullable command_id for sqlite
	var cmdQuery string
	var args []any
	if commandID == nil {
		cmdQuery = "command_id IS NULL"
		args = []any{memoryID, chunkIndex, configID}
	} else {
		cmdQuery = "command_id = ?"
		args = []any{memoryID, *commandID, chunkIndex, configID}
	}

	err := s.db.QueryRow(fmt.Sprintf(`
		SELECT id, memory_id, command_id, embedding_config_id, chunk_index, content_hash, indexed_at, needs_reindex
		FROM embedding_status
		WHERE memory_id = ? AND %s AND chunk_index = ? AND embedding_config_id = ?
	`, cmdQuery), args...).Scan(&st.ID, &st.MemoryID, &st.CommandID, &st.EmbeddingConfigID, &st.ChunkIndex, &st.ContentHash, &st.IndexedAt, &st.NeedsReindex)

	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetActiveEmbeddingConfig returns the most recently created active config,
// or nil if no model has been configured yet.
func (s *Store) GetActiveEmbeddingConfig() (*EmbeddingConfig, error) {
	var ec EmbeddingConfig
	err := s.db.QueryRow(`
		SELECT id, model_name, version, dimensions, is_active, created_at
		FROM embedding_configs
		WHERE is_active = TRUE
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&ec.ID, &ec.ModelName, &ec.Version, &ec.Dimensions, &ec.IsActive, &ec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query active embedding config: %w", err)
	}
	return &ec, nil
}

// ActivateEmbeddingConfig makes modelName the only active embedding model.
// Vectors from different models are incompatible, so every embedding_status
// row is flagged for reindexing.
func (s *Store) ActivateEmbeddingConfig(modelName string, dims int) (*EmbeddingConfig, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE embedding_configs SET is_active = FALSE"); err != nil {
		return nil, fmt.Errorf("deactivate embedding configs: %w", err)
	}

	var ec EmbeddingConfig
	err = tx.QueryRow(`
		SELECT id, model_name, version, dimensions, created_at
		FROM embedding_configs
		WHERE model_name = ? AND dimensions = ?
		ORDER BY id DESC
		LIMIT 1
	`, modelName, dims).Scan(&ec.ID, &ec.ModelName, &ec.Version, &ec.Dimensions, &ec.CreatedAt)

	if err == sql.ErrNoRows {
		res, err := tx.Exec(`
			INSERT INTO embedding_configs (model_name, dimensions, is_active)
			VALUES (?, ?, TRUE)
		`, modelName, dims)
		if err != nil {
			return nil, fmt.Errorf("insert embedding config: %w", err)
		}
		if ec.ID, err = res.LastInsertId(); err != nil {
			return nil, err
		}
		ec.ModelName = modelName
		ec.Dimensions = dims
		ec.CreatedAt = time.Now()
	} else if err != nil {
		return nil, fmt.Errorf("query embedding config: %w", err)
	} else if _, err := tx.Exec("UPDATE embedding_configs SET is_active = TRUE WHERE id = ?", ec.ID); err != nil {
		return nil, fmt.Errorf("activate embedding config: %w", err)
	}
	ec.IsActive = true

	if _, err := tx.Exec("UPDATE embedding_status SET needs_reindex = TRUE"); err != nil {
		return nil, fmt.Errorf("mark embeddings for reindex: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ec, nil
}

// MarkAllForReindex flags every indexed chunk as stale.
func (s *Store) MarkAllForReindex() error {
	if _, err := s.db.Exec("UPDATE embedding_status SET needs_reindex = TRUE"); err != nil {
		return fmt.Errorf("mark embeddings for reindex: %w", err)
	}
	return nil
}

// AllMemoryIDs returns the IDs of every stored memory, oldest first.
func (s *Store) AllMemoryIDs() ([]int64, error) {
	return s.queryIDs("SELECT id FROM memories ORDER BY id")
}

// CountUnindexedMemories counts memories that have no up-to-date vectors for
// the given embedding config (never indexed, or flagged needs_reindex).
func (s *Store) CountUnindexedMemories(configID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM memories m
		WHERE NOT EXISTS (
			SELECT 1 FROM embedding_status es
			WHERE es.memory_id = m.id AND es.embedding_config_id = ? AND es.needs_reindex = FALSE
		) OR EXISTS (
			SELECT 1 FROM embedding_status es
			WHERE es.memory_id = m.id AND es.embedding_config_id = ? AND es.needs_reindex = TRUE
		)
	`, configID, configID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unindexed memories: %w", err)
	}
	return n, nil
}

// SetEmbeddingScheme records how text is prepared for config's model and
// flags its vectors for reindexing, since vectors built under a different
// scheme are not comparable with new queries.
func (s *Store) SetEmbeddingScheme(configID int64, scheme string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE embedding_configs SET version = ? WHERE id = ?", scheme, configID); err != nil {
		return fmt.Errorf("update embedding scheme: %w", err)
	}
	if _, err := tx.Exec("UPDATE embedding_status SET needs_reindex = TRUE WHERE embedding_config_id = ?", configID); err != nil {
		return fmt.Errorf("mark embeddings for reindex: %w", err)
	}
	return tx.Commit()
}
