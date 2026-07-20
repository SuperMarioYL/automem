<div align="right"><sub><b>EN</b>&nbsp;&nbsp;⇄&nbsp;&nbsp;<a href="./README.zh-CN.md">中文</a></sub></div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/hero-light.svg">
  <img src="./assets/hero-light.svg" width="880" alt="automem — the offline memory layer so your coding agent remembers across restarts">
</picture>

<p><sub>automem is the offline memory layer that makes any coding agent remember across restarts — one binary, no vector DB, no account, no key.</sub></p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License: Apache-2.0"></a>
  <a href="https://github.com/SuperMarioYL/automem/releases"><img src="https://img.shields.io/github/v/release/SuperMarioYL/automem?color=5E5CE6" alt="Latest release"></a>
  <a href="https://github.com/SuperMarioYL/automem/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/automem/ci.yml?branch=main&label=ci" alt="CI status"></a>
  <img src="https://img.shields.io/badge/go-1.24-00ADD8.svg" alt="Go 1.24">
  <img src="https://img.shields.io/badge/Coding%20Agent-ready-10A37F.svg" alt="Coding Agent ready">
  <img src="https://img.shields.io/badge/Agent-offline-5E5CE6.svg" alt="Agent offline">
</p>

**Your coding agent restarts stateless every session — you re-paste context, re-explain the codebase, re-state the decision you made two hours ago. `automem` captures each session and injects the relevant bits into the next one, so the agent picks up where it left off.**

<h2><img src="https://api.iconify.design/tabler:topology-star-3.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Architecture</h2>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
  <img src="./assets/atlas-light.svg" width="880" alt="Architecture: agent-CLI hooks call automem capture (Stop) and recall (SessionStart) against an append-only JSONL store; recall injects the top-K memories back into the session">
</picture>

One binary, one append-only file, no daemon, no network, no account. The `automem` binary is short-lived — the agent's own process invokes it once per hook fire and it exits; nothing runs between sessions. Capture is deterministic extraction (last-N user messages, touched paths, diff stat), and recall scores every stored record by `lexical_overlap × recency_decay` and injects the top-K — no embeddings, no vector DB, no API key.

## Table of contents

- [Why this exists](#why-this-exists)
- [Install](#install)
- [Quickstart](#quickstart)
- [Usage](#usage)
- [Demo](#demo)
- [Configuration](#configuration)
- [vs claude-mem](#vs-claude-mem)
- [Pricing](#pricing)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

<h2><img src="https://api.iconify.design/tabler:help-circle.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Why this exists</h2>

Getting cross-session memory today means picking a service (Mem0, Zep, ContextNest), running it, wiring an embedding provider, and creating an account — a gauntlet where the friction *is* the setup, not the recall quality. The category leader, [claude-mem](https://github.com/thedotmack/claude-mem) (85k★), proved the demand but compresses via an AI-provider call, so it needs a key and a network. `automem` removes all of that: drop one binary on `PATH`, run `automem install`, and every supported agent auto-captures and auto-recalls — offline, keyless, and not tied to a single agent.

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Install</h2>

```bash
# Homebrew (macOS / Linux)
brew install SuperMarioYL/tap/automem

# …or one-line curl installer
curl -fsSL https://lei6393.com/automem/install | sh

# …or from source (Go 1.24+)
go install github.com/SuperMarioYL/automem/cmd/automem@latest
```

> macOS and Linux for v0.1. Windows is on the [roadmap](#roadmap).

<h2><img src="https://api.iconify.design/tabler:player-play.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Quickstart</h2>

Three commands from cold clone to "it remembers":

```bash
automem install                                   # wire your agents (Claude Code + Aider)
claude                                            # work a session, then exit — the Stop hook captures it
claude                                            # new session: SessionStart recalls the last one, automem stats proves it
```

<details><summary>What <code>automem install</code> prints</summary>

```text
wired claude-code ✓, aider ✓ (unverified)
  claude-code ✓ ~/.claude/settings.json — wired SessionStart (recall) + Stop (capture) hooks
  aider ✓ ~/.local/bin/automem-aider — run `automem-aider` in place of `aider`
    [unverified: no aider on the build machine — please report if it misbehaves]
```

</details>

<h2><img src="https://api.iconify.design/tabler:terminal-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Usage</h2>

`automem install` wires everything automatically, but each subcommand also works standalone — handy for scripting agents, CI warm-ups, or piping a transcript by hand. See [`examples/`](./examples) for a copy-paste round-trip.

```bash
# Capture: append one extractive record from a session transcript (file or stdin)
automem capture --agent claude-code --cwd ~/proj/api session.transcript
printf 'User: refactor auth.py to use dataclasses\n' | automem capture --agent claude-code

# Recall: print the top-K prior summaries most relevant to a query
automem recall "what did we decide about auth.py last session?"
automem recall --top 3 --no-mark "http client retries"   # preview without counting an injection

# Stats: stored-vs-injected — proof the memory is actually used
automem stats
```

<details><summary>Sample <code>recall</code> + <code>stats</code> output</summary>

```text
$ automem recall "what did we decide about auth.py last session?"
# memory 1/2  (score 0.800)
User: refactor auth.py to use dataclasses  User: keep the old constructor working
files: auth.py

# memory 2/2  (score 0.400)
User: add retry logic to the http client in client.py
files: client.py

$ automem stats
2 stored, 2 injected
  injection rate: 100% (2 of 2 memories recalled at least once)
  total injections: 2
  by agent:
    claude-code  2
```

</details>

The paid-tier commands are present as stubs so their demand is measurable:

```bash
automem sync    # cross-machine sync — requires automem cloud (paid tier)
automem team    # shared team memory — requires automem cloud (paid tier)
```

<h2><img src="https://api.iconify.design/tabler:photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Demo</h2>

Two sessions, and the second one remembers the first — captured live from the real binary:

![automem demo: capture two sessions, recall the relevant one, stats proves it was injected](./assets/demo.gif)

<h2><img src="https://api.iconify.design/tabler:adjustments.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Configuration</h2>

`automem` needs no config file — it works out of the box. A few environment variables let you redirect it (used by tests, sandboxes, and unusual setups):

| Variable | Type | Default | Meaning |
|---|---|---|---|
| `AUTOMEM_DIR` | path | `~/.automem` | Directory holding `store.jsonl`. |
| `AUTOMEM_HOME` | path | OS home dir | Home root that `automem install` wires into (agent config paths derive from it). |
| `AUTOMEM_BIN` | path | resolved executable | Absolute path baked into the hooks/wrapper that `automem install` writes. |

<h2><img src="https://api.iconify.design/tabler:git-compare.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> vs claude-mem</h2>

Honest positioning against the incumbent that proved the demand — it beats `automem` on recall quality, and that's the deliberate trade for zero setup:

| | automem | [claude-mem](https://github.com/thedotmack/claude-mem) |
|---|:---:|:---:|
| Works offline, no API key | ✓ | — (compresses via an AI-provider call) |
| No account, no vector DB to stand up | ✓ | partial |
| Multi-agent (Claude Code **and** Aider) | ✓ | — (Claude Code-specific) |
| Recall quality on large stores | partial (lexical + recency) | ✓ (semantic compression) |
| Proven distribution / community | — (new) | ✓ (85k★) |

`automem` isn't competing for claude-mem's users — it serves the segment that bounced off the API-key and cloud dependency.

<h2><img src="https://api.iconify.design/tabler:currency-dollar.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Pricing</h2>

The local substrate is **free and open source (Apache 2.0), forever** — capture, recall, stats, and agent wiring all run fully offline with no account. The commercial tier is the one thing the substrate deliberately does *not* do for free: leaving your machine.

| Tier | Price | What you get |
|---|---|---|
| **Local** | Free · Apache-2.0 | Single offline binary: capture, recall, stats, `automem install` for Claude Code + Aider. No account, no key, no network. |
| **Sync** | Paid | Cross-machine memory sync (`automem sync`) — the same store, on every machine you code from. |
| **Team** | **$8 / seat / month** | Shared team scopes + cross-machine sync + audit log (`automem team`). One memory layer for the whole team's decisions, quirks, and conventions. |

`automem sync` and `automem team` ship as stubs today; each invocation is a demand signal. The hosted backend goes live once the interest does — see `lei6393.com/automem`.

<h2><img src="https://api.iconify.design/tabler:map-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Roadmap</h2>

- [x] **m1 — store, capture & recall.** Append-only JSONL store, deterministic extractive capture (no key), `lexical × recency` top-K recall, and the stored-vs-injected stats counter.
- [x] **m2 — auto-install for agents.** `automem install` wires Claude Code `SessionStart`/`Stop` hooks and an Aider wrapper on macOS + Linux; a fresh two-session flow remembers with no manual config.
- [x] **m3 — demo & paid-tier stubs.** `automem sync` / `team` stubs, the vhs demo, and this bilingual README.
- [ ] Local-embedding fallback (still no account, no cloud key) for larger stores.
- [ ] MCP-server universal transport so Cursor, Codex CLI, and Gemini CLI auto-discover the same substrate.
- [ ] Hosted `sync` / `team` backend (the paid tier).
- [ ] Windows support.

<h2><img src="https://api.iconify.design/tabler:heart-handshake.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Contributing</h2>

Issues and PRs welcome. Please [open an issue](https://github.com/SuperMarioYL/automem/issues) for bugs or ideas. The Aider wrapper in particular is shipped **unverified** (it wasn't tested against a live Aider install) — if you run Aider, a report either way is genuinely useful.

<h2><img src="https://api.iconify.design/tabler:license.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> License</h2>

Released under the [Apache License, Version 2.0](./LICENSE).

## Share this

```text
automem — the offline memory layer that makes any Coding Agent remember across
restarts. One binary, no vector DB, no account, no key. Your Agent picks up where
the last session left off. https://github.com/SuperMarioYL/automem
```

<p align="center"><sub><a href="./LICENSE">Apache-2.0</a> © 2026 SuperMarioYL</sub></p>
