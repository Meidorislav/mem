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

## ⚡ Quick Start

```bash
# Record a complex fix
mem watch
# ... (do your work) ...
mem remember "fixing postgres connection leak"

# Ask your past self a question later
mem ask "how did I fix postgres?"
```

**MEM grows with you. One week in, it's a notebook. One year in, it's your most valuable asset.**
