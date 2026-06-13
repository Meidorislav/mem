package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

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
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
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

	dbPath := filepath.Join(appDir, "mem.db")
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
		_, err := tx.Exec(`
			INSERT INTO commands (memory_id, position, command, output)
			VALUES (?, ?, ?, ?)
		`, memID, i, cmd.Command, cmd.Output)
		if err != nil {
			return fmt.Errorf("insert command %d: %w", i, err)
		}
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
