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
- **Embeddings:** `bge-m3` (default, multilingual) via Ollama; `nomic-embed-text` / `mxbai-embed-large` also supported but English-only
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
  ollama/         # shared Ollama settings (OLLAMA_HOST parsing, error messages)
  paths/          # data directory: $MEM_HOME or ~/.mem
scripts/
  calibrate.sh    # seeds a scratch DB and prints scores to tune --min-score per model
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
- Task prefixes: documents go through `EmbedDocument`, queries through `EmbedQuery` (`search_document:` / `search_query:` for nomic-embed-text, a query instruction for mxbai-embed-large, nothing for other models). `Embed` sends text as is
- `embeddings.Scheme` is stored in `embedding_configs.version`; `activeConfig` → `ensureScheme` flags all of the config's rows `needs_reindex` when it differs. Bump `Scheme` whenever the text sent to the model changes

### Chunking strategy
- Each command is indexed as its own chunk
- Prepend title + tags as context: `"<command> | note: <title> | tags: <tag1>, <tag2>"`
- Keep chunks under ~400 tokens (well within the models' context limits)
- Store `chunk_index` in `embedding_status` for multi-chunk notes
- A memory with a description (or no commands) also gets a context chunk with `command_id = NULL`
- LanceDB rows only carry `memory_id`, so re-indexing replaces *all* vectors of a memory (`indexer.go`); embed first, then delete + insert, so an Ollama failure keeps old vectors
- `embedding_status` upserts match on `command_id IS ?` — SQLite treats NULLs as distinct in UNIQUE, so `ON CONFLICT` would duplicate context-chunk rows

### Search scoring
- Vectors are L2-normalized on insert and query, so LanceDB's L2 ranking equals cosine ranking. `vector.Search` returns `Hit{MemoryID, Score}` where `Score` is cosine similarity computed in Go from the returned embedding (correct even for vectors indexed before normalization)
- `rankHits` (`commands/rank.go`) keeps one hit per memory (best chunk), drops scores below `--min-score` and anything more than `scoreGap` (0.08) behind the best hit; `--min-score 0` disables both
- The default `--min-score` comes from `embeddings.MinScore(model)` (per-model table next to the task prefixes; 0.45 for bge-m3 from `scripts/calibrate.sh`; 0.4 fallback). Score distributions differ a lot between models, so calibrate a new model before adding it there: `scripts/calibrate.sh` (or `MODEL=<name> scripts/calibrate.sh`) seeds a throwaway `MEM_HOME` with realistic memories and asks paraphrased questions with known answers plus unrelated ones; set the threshold between the worst right answer and the best unrelated one
- `ask` warns on stderr when `CountUnindexedMemories` > 0
- Ollama errors go through `ollama.RequestError` / `ollama.StatusError` so the user sees "not running at <url>: start it with `ollama serve`" or "run `ollama pull <model>`" instead of raw dial errors

### Keyword search (hybrid)
- `memory_fts` (SQLite FTS5, `unicode61 remove_diacritics 2`, rowid = memory id) indexes title, description, tags and commands. `SaveMemory` inserts the row in its transaction; the `memories_fts_delete` trigger removes it; `NewStoreAt` backfills missing rows (`syncFTS`). `UpdateMemory` (used by `mem edit`) rewrites the row; anything else that changes a memory's text must too
- `ftsQuery` turns the question into quoted prefix terms OR-ed together (`"nginx"* OR "502"*`), dropping en/ru stop words, so user text can't inject FTS syntax; results are ranked by `bm25` with title/tags/commands weighted above description
- `fuse` (`commands/rank.go`) merges `rankHits` output with keyword hits by reciprocal rank fusion (k = 60): memories found by both rank first, and keyword hits are shown even below `--min-score`. If embedding the query fails, `ask` falls back to keyword results with a warning

### Editing and duplicates
- `UpdateMemory` replaces title, description, commands and tags in one transaction (commands get new IDs), rewrites the `memory_fts` row and deletes the memory's `embedding_status` rows, so the following `indexNewMemory` re-embeds it; other memories are untouched
- `mem edit` without flags round-trips the memory through `$VISUAL`/`$EDITOR` (`formatForEdit` / `parseEdit`): `key: value` header, then commands starting with `$ ` with continuation lines indented by two spaces
- `save` and `remember` refuse an exact duplicate (same title, same commands in order; `FindDuplicate`) unless `--force`

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
- No external network calls — Ollama runs locally on `127.0.0.1:11434`, overridable with `OLLAMA_HOST` (parsed like the ollama CLI in `internal/ollama`); never hard-code the address in clients
- Pure Go SQLite driver (no cgo) keeps builds simple and cross-platform

## Commits

- Author every commit as the repo owner so it counts on their GitHub contribution graph: run `git config user.name "Vladislav Bakin"` and `git config user.email "meidorislav@gmail.com"` in the clone before the first commit (a fresh clone does not carry this)
- Credit the AI assistant with a `Co-Authored-By:` trailer at the end of the message instead of as the author
- Work on `dev` and open PRs into `main`; PRs are merged with a merge commit (not squash), so individual commits land on `main` with their author intact

## What's not built yet

- Capturing command output in `mem watch` (`commands.output` column is unused)
- Shells other than bash/zsh for `mem watch`

## Running locally

Requires Ollama running with at least one embedding model pulled, and the LanceDB native libraries (cgo). The Makefile downloads them into `lib/` and `include/` (gitignored) and sets `CGO_CFLAGS`/`CGO_LDFLAGS`:

```bash
ollama pull bge-m3
make build   # or: make download-artifacts, then make test
./mem --help
```

`make lint` runs the gofmt check and `go vet`; CI (`.github/workflows/ci.yml`) runs `make lint` and `make test` on Linux and macOS for PRs and pushes to `main`, caching `lib/` and `include/`.

Plain `go build`/`go test` fail at link time for packages that import `internal/vector` unless the CGO variables from the Makefile are exported.
