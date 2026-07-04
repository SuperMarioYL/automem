# examples

A 60-second round-trip you can run against a scratch store, no agent required.

```bash
# Build (or `brew install automem` once published)
go build -o automem ./cmd/automem

# Use a throwaway store so you don't touch ~/.automem
export AUTOMEM_DIR="$(mktemp -d)/.automem"

# 1. Capture a session transcript (what a Claude Code Stop hook does)
./automem capture --agent claude-code --cwd ~/proj/api examples/session.transcript

# 2. Recall the most relevant prior memory (what a SessionStart hook does)
./automem recall "what did we decide about auth.py?"

# 3. Prove it was actually used — stored vs injected
./automem stats
```

Expected `recall` output:

```
# memory 1/1  (score 1.000)
User: refactor auth.py to use dataclasses ... keep the old constructor working
files: auth.py
```

Expected `stats` output:

```
1 stored, 1 injected
  injection rate: 100% (1 of 1 memories recalled at least once)
```

To wire it into your real agents instead of piping transcripts by hand, run
`automem install` — it adds Claude Code `SessionStart`/`Stop` hooks and an Aider
wrapper so every session captures and recalls automatically.
