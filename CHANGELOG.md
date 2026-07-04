# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-07-04

### Added

- **Store, capture & recall core** — append-only JSONL store (`~/.automem/store.jsonl`),
  deterministic extractive capture (no API key), lexical-overlap × recency-decay
  recall (top-K, no vector DB), and a stored-vs-injected stats counter.
- **Auto-install for coding agents** — `automem install` wires Claude Code
  `SessionStart`/`Stop` hooks and an Aider wrapper on macOS and Linux so a fresh
  two-session flow remembers across restarts with no manual config.
- **Demo & paid-tier stubs** — `automem sync` and `automem team` paid-tier stubs,
  a vhs `demo.tape` rendering the install → two-sessions → `stats` flow, and a
  bilingual README (English primary, Simplified Chinese sibling).

[Unreleased]: https://github.com/SuperMarioYL/automem/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/SuperMarioYL/automem/releases/tag/v0.1.0
