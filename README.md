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

## 🏁 Quick Start

```bash
# Save your first command
mem save "list files by size" -c "ls -lhS"

# Ask your past self
mem ask "how to list large files?"
```,old_string:,old_string:

**MEM grows with you. One week in, it's a notebook. One year in, it's your most valuable asset.**
