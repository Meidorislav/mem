# AGENTS.md — mem

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
cmd/mem/          # main package
internal/
  commands/       # cobra commands, chunker, indexer (save/ask/watch/remember/reindex/...)
  storage/        # SQLite layer (Store, SaveMemory, embedding configs/status)
  embeddings/     # Ollama embedding client
  vector/         # LanceDB integration (vectors + memory_id only)
  session/        # `mem watch` shell recorder (bash/zsh hooks, session files)
  llm/            # Ollama generate client for `mem ask --answer`
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

### Chunking strategy
- Each command is indexed as its own chunk
- Prepend title + tags as context: `"<command> | note: <title> | tags: <tag1>, <tag2>"`
- Keep chunks under ~400 tokens (well within nomic-embed-text's 2048 limit)
- Store `chunk_index` in `embedding_status` for multi-chunk notes
- A memory with a description (or no commands) also gets a context chunk with `command_id = NULL`
- LanceDB rows only carry `memory_id`, so re-indexing replaces *all* vectors of a memory (`indexer.go`); embed first, then delete + insert, so an Ollama failure keeps old vectors
- `embedding_status` upserts match on `command_id IS ?` — SQLite treats NULLs as distinct in UNIQUE, so `ON CONFLICT` would duplicate context-chunk rows

### Model switching
When changing embedding models, a full reindex is required — set `needs_reindex = TRUE` on all `embedding_status` rows and re-encode everything. `embedding_configs` tracks which model produced which vectors; exactly one row is `is_active`.

`mem reindex --model <name>` probes the model's dimensions, calls `ActivateEmbeddingConfig` (deactivates others, flags all rows stale), drops and recreates the LanceDB table, then re-embeds. Rows are flagged before vectors are dropped, so an interrupted reindex resumes on the next run. Opening the vector store with the wrong dimensions returns `vector.ErrDimsMismatch`.

### Watch sessions
- `mem watch` starts `$SHELL` (bash or zsh) with a generated rc file that sources the user's config and appends each command to `~/.mem/sessions/<timestamp>.session`, NUL-terminated (multi-line safe); `MEM_SESSION` holds the path
- bash records from `PROMPT_COMMAND` via `history 1`; zsh from a `preexec` hook. Both skip space-prefixed commands when the shell ignores them for history
- `mem remember` inside the shell reads `MEM_SESSION` and truncates it after saving; outside it uses the newest session file and deletes it. `session.Clean` first splits multi-line records into per-line commands with `mvdan.cc/sh` (zsh runs a pasted block as one line; loops, `\` continuations and heredocs stay whole), then drops `mem` invocations, `exit`, blanks and consecutive duplicates

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

- Editing existing memories (`mem edit`); chunk hashes already support stale detection for it
- Capturing command output in `mem watch` (`commands.output` column is unused)
- Shells other than bash/zsh for `mem watch`

## Running locally

Requires Ollama running with at least one embedding model pulled, and the LanceDB native libraries (cgo). The Makefile downloads them into `lib/` and `include/` (gitignored) and sets `CGO_CFLAGS`/`CGO_LDFLAGS`:

```bash
ollama pull nomic-embed-text
make build   # or: make download-artifacts, then make test
./mem --help
```

Plain `go build`/`go test` fail at link time for packages that import `internal/vector` unless the CGO variables from the Makefile are exported.
