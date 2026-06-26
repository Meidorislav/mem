# CLAUDE.md — mem

Local-first CLI "second brain" for developers. Semantic search over terminal commands, debugging sessions, and notes — fully private, powered by Ollama.

## Project overview

`mem` lets developers save and semantically search their terminal history, configs, and notes. No data leaves the machine. Everything runs locally via Ollama.

**Key commands:** `mem save`, `mem ask`, `mem watch`, `mem remember`

## Stack

- **Language:** Go 1.26
- **CLI framework:** cobra + pflag
- **Relational DB:** SQLite via `modernc.org/sqlite` (pure Go, no cgo)
- **Vector DB:** LanceDB (embedded, no daemon)
- **Embeddings:** `nomic-embed-text` or `mxbai-embed-large` via Ollama
- **LLM synthesis:** llama3.2 / qwen2.5 via Ollama

## Project structure

```
cmd/mem/          # CLI entrypoints (cobra commands)
internal/
  storage/        # SQLite layer (Store, SaveMemory, etc.)
  embeddings/     # Ollama embedding client (not yet built)
  vector/         # LanceDB integration (not yet built)
docs/i18n/ru/     # Russian docs
```

## Architecture decisions

### Two-database design
- **SQLite** is the source of truth: stores content, metadata, tags, commands
- **LanceDB** stores only vectors + references back to SQLite IDs
- Never duplicate content in LanceDB — always join back to SQLite for display

### Embedding pipeline
- Encode at **save time**, not search time (SBERT siamese architecture)
- One embedding model = one LanceDB index (vectors from different models are incompatible)
- Track indexing state in `embedding_status` table; use `content_hash` to detect stale chunks

### Chunking strategy (to be implemented)
- Each command is indexed as its own chunk
- Prepend title + tags as context: `"<command> | note: <title> | tags: <tag1>, <tag2>"`
- Keep chunks under ~400 tokens (well within nomic-embed-text's 2048 limit)
- Store `chunk_index` in `embedding_status` for multi-chunk notes

### Model switching
When changing embedding models, a full reindex is required — set `needs_reindex = TRUE` on all `embedding_status` rows and re-encode everything. `embedding_configs` tracks which model produced which vectors.

## SQLite schema (current)

Tables: `memories`, `commands` (with `position` ordering), `tags`, `memory_tags` (many-to-many), `embedding_configs`, `embedding_status`

Key constraints:
- `ON DELETE CASCADE` everywhere
- `PRAGMA foreign_keys = ON` at connection time
- Transactions for all multi-table writes

`content_hash TEXT` in `embedding_status` — sha256 of chunk text, used for automatic stale detection (if hash changes → `needs_reindex = TRUE`).

## Development conventions

- All writes to SQLite go through a transaction with `defer tx.Rollback()`
- Errors always wrapped with `fmt.Errorf("context: %w", err)`
- No external network calls — Ollama runs locally on `localhost:11434`
- Pure Go SQLite driver (no cgo) keeps builds simple and cross-platform

## What's not built yet

- Ollama embedding client (`internal/embeddings/`)
- LanceDB integration (`internal/vector/`)
- `mem ask` semantic search pipeline
- `mem watch` terminal session recorder
- Chunking logic
- Reindex workflow

## Running locally

Requires Ollama running with at least one embedding model pulled:

```bash
ollama pull nomic-embed-text
go run ./cmd/mem
```
