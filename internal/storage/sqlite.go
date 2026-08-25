package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
`

type Store struct {
	db *sql.DB
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	appDir := filepath.Join(home, ".mem")
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

	return &Store{db: db}, nil
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

	res, err := tx.Exec(`
		INSERT INTO memories (title, source, description)
		VALUES (?, ?, ?)
	`, m.Title, m.Source, m.Description)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}

	memID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	m.ID = memID

	for i, cmd := range m.Commands {
		cmdRes, err := tx.Exec(`
			INSERT INTO commands (memory_id, position, command, output)
			VALUES (?, ?, ?, ?)
		`, memID, i, cmd.Command, cmd.Output)
		if err != nil {
			return fmt.Errorf("insert command %d: %w", i, err)
		}
		cmdID, err := cmdRes.LastInsertId()
		if err != nil {
			return err
		}
		m.Commands[i].ID = cmdID
	}

	for _, tagName := range m.Tags {
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

		_, err = tx.Exec("INSERT INTO memory_tags (memory_id, tag_id) VALUES (?, ?)", memID, tagID)
		if err != nil {
			return fmt.Errorf("insert memory_tag: %w", err)
		}
	}

	return tx.Commit()
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

func (s *Store) UpsertEmbeddingStatus(status *EmbeddingStatus) error {
	res, err := s.db.Exec(`
		INSERT INTO embedding_status (memory_id, command_id, embedding_config_id, chunk_index, content_hash, indexed_at, needs_reindex)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, FALSE)
		ON CONFLICT(memory_id, command_id, chunk_index, embedding_config_id) DO UPDATE SET
			content_hash = excluded.content_hash,
			indexed_at = CURRENT_TIMESTAMP,
			needs_reindex = FALSE
	`, status.MemoryID, status.CommandID, status.EmbeddingConfigID, status.ChunkIndex, status.ContentHash)
	if err != nil {
		return fmt.Errorf("upsert embedding status: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if id > 0 {
		status.ID = id
	}
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
