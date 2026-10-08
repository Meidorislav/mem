# MEM: Your Local Technical Oracle

**Stop searching the internet for problems you've already solved.**

MEM is a terminal-native "second brain" that indexes your technical life. It captures your terminal sessions, configuration snippets, and project notes, making them instantly searchable through local, private AI.

---

## 🚀 Key Features

*   **`mem ask` (The Magic)**: Search your experience using natural language. Ask *"how did I fix the nginx 502 error last year?"* and get the exact steps.
*   **`mem watch` (Zero Effort)**: Record your terminal session. Solve a problem, then use `mem remember` to turn your command history into a searchable recipe.
*   **`mem save`**: Instantly store code snippets, one-liners, or architecture notes.
*   **CLI-First**: Built for developers who live in the terminal. No context-switching to heavy GUI apps.

## 🧠 Why it's different

### Meaning, not just Keywords
Traditional tools like `grep` fail if you don't remember the exact words. MEM uses **Semantic Search**: it understands the *intent* behind your query. A search for "deployment" will find a note titled "Server Setup".

### Total Privacy
Everything stays on your machine. MEM uses **Local Vector Embeddings** and a **Local LLM** to process your data. Your proprietary code and private server configs never touch the cloud.

---

## 🛠 How the AI Works

1.  **Embeddings**: Your data is converted into high-dimensional vectors (mathematical representations of meaning) using a local model.
2.  **Semantic Retrieval**: When you ask a question, MEM finds the most mathematically similar "memories" in your local database.
3.  **Local Synthesis**: A local LLM reads those memories and provides a concise, relevant answer based solely on *your* experience.

---

## ⚡ Practical Examples

### 1. Complex Infrastructure Commands
```bash
# Save a specific setup sequence
mem save "Postgres Replication Setup" -c "apt install postgresql-15; sed -i 's/wal_level = replica/wal_level = logical/' /etc/postgresql/15/main/postgresql.conf"

# Find it 6 months later
mem ask "how did I enable logical replication?"
```

### 2. The "I'll Forget This" One-Liner
```bash
# Save that 3-line find/xargs combo
mem save "delete all empty directories" -c "find . -type d -empty -delete"

# Recall it instantly
mem ask "how to clean empty folders?"
```

### 3. Recording a Debugging Session
```bash
# Record your flow while fixing a bug
mem watch
# $ ssh production-server
# $ journalctl -u api-service -n 100
# $ systemctl restart api-service
mem remember "restart api-service on prod"

# Later, ask how you handled the crash
mem ask "how did I fix the api-service crash?"
```

---

## 📦 Installation

Requirements: Go 1.26+, a C toolchain (LanceDB is linked via cgo), and [Ollama](https://ollama.com) running locally.

```bash
ollama pull bge-m3             # embeddings, multilingual (required)
ollama pull llama3.2           # answers for `mem ask --answer` (optional)

make build                     # downloads LanceDB native libs on first run
./mem --help
```

Data lives in `~/.mem/` (`mem.db` for SQLite, `vectors/` for LanceDB, `sessions/` for `mem watch`); set `MEM_HOME` to use another directory, e.g. a scratch database.

The default embedding model is `bge-m3`, which handles English and Russian in one index. English-only models such as `nomic-embed-text` work for English notes but score non-English questions close to noise; switch with `mem reindex --model <name>`.

MEM talks to Ollama at `127.0.0.1:11434`, or wherever `OLLAMA_HOST` points (same format as the `ollama` CLI: `host`, `host:port`, `http(s)://host:port`). Pointing it at another machine sends your notes and queries there.

After upgrading, `mem ask` may report memories that are not indexed with the current settings; run `mem reindex` once to re-embed them.

## 📖 Commands

| Command | What it does |
|---|---|
| `mem save "title" -c "cmd" [-c ...] [-t tag] [-d "description"] [--force]` | Save commands/notes as a memory. An exact copy of an existing memory (same title and commands) is refused unless `--force` |
| `mem ask "question" [-n 5] [--min-score 0.42] [--answer] [--llm llama3.2]` | Hybrid search: semantic similarity plus exact keywords (error codes, flags, service names). Each result shows its similarity (0–1) and `keyword` if words matched; weak semantic matches are hidden (the default threshold is tuned per embedding model; `--min-score 0` shows everything). Without Ollama it falls back to keyword matches. `--answer` also asks a local LLM to summarize the results |
| `mem watch [--shell bash\|zsh]` | Start a recorded subshell; every command is logged until `exit` |
| `mem remember "title" [-t tag] [-d "..."] [-n N]` | Turn the recorded session (or its last N commands) into a memory. Works inside the watched shell too |
| `mem list [-t tag] [-n 20]` | List recent memories |
| `mem show <id>` / `mem delete <id>...` | Inspect a memory, or remove one or more |
| `mem edit <id> [--title ...] [-d ...] [-c ...] [-t ...]` | Change a memory with flags (`-c` / `-t` replace all commands / tags), or without flags in `$EDITOR`. The memory is re-indexed |
| `mem reindex [--all] [--model name]` | Embed memories saved while Ollama was down, rebuild the index, or switch embedding model |

Commands starting with a space are not recorded by `mem watch` if your shell ignores them for history (`HISTCONTROL=ignorespace` in bash, `setopt HIST_IGNORE_SPACE` in zsh).

---

## 🏁 Quick Start

```bash
# Save your first command
mem save "list files by size" -c "ls -lhS"

# Ask your past self
mem ask "how to list large files?"

ls -lhS # list files by size 
```

**MEM grows with you. One week in, it's a notebook. One year in, it's your most valuable asset.**
