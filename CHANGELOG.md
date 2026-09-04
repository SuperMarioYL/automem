# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.0] - 2026-09-04

### Fixed

- **Version-drift lockstep** — the in-repo `VERSION` file, the `main.version`
  var that a `go build` from source reports via `automem --version`, and the
  head of this changelog were out of lockstep with the shipped tags: v0.4.0
  shipped `VERSION=0.3.0` and a changelog that stopped at `0.1.0`. All three
  surfaces now read `0.5.0`, the missing `v0.2.0`/`v0.3.0`/`v0.4.0` entries are
  backfilled below, and a `TestVersionLockstep` regression test asserts
  `VERSION == main.version == CHANGELOG head` going forward (it failed on the
  v0.4.0 tag, proving the drift was real rather than cosmetic).

## [0.4.0] - 2026-08-27

### Fixed

- **Case-insensitive transcript role parsing** — capture's `roleLine` regex was
  compiled case-sensitive, so transcripts using the natural capitalized
  `User:`/`Assistant:` prefixes (the shipped onboarding transcript and the demo
  gif source) did not parse as roles: the whole transcript collapsed into one
  user message instead of the intended last-N user messages, and recall printed
  a run-on blob. The regex is now case-insensitive (`(?i)`).
- **Preserve foreign Claude Code hook fields** — `automem install`'s merge of
  SessionStart/Stop hooks round-tripped foreign (user-owned) hook entries
  through a typed struct that modeled only `matcher`+`hooks`+`type`+`command`,
  silently deleting any extra field (e.g. `timeout`, `note`) on the next install.
  Foreign entries are now kept in their raw form so unknown fields survive
  untouched, honouring the "preserve every key" contract.

## [0.3.0] - 2026-08-21

### Fixed

- **Capture absolute and home-relative paths** — the `pathLike` regex silently
  dropped absolute (`/Users/…/auth.go`) and home (`~/projects/auth.go`) paths
  because its delimiter anchor could not start a match at a leading `/` or `~`.
  Real Claude Code transcripts carry absolute paths in tool calls, so the
  primary integration produced records missing the very path/language tags that
  are recall's strongest signal (`pathTagWeight=2.0`). The regex now anchors at
  a path root so absolute and `~/` paths extract correctly.

## [0.2.0] - 2026-08-02

### Added

- **Claude Code hook JSON stdin parsing** — capture and recall now detect
  Claude Code's JSON hook payload on stdin and extract `transcript_path` (for
  capture) and `cwd` (for recall) instead of reading the JSON blob as text, so
  the wired hooks work end-to-end; recall's cwd-derived tokens also surface
  records captured in the same project via the record's `Cwd`.
- **Concurrency-safe store rewrite** — `MarkInjected`'s Load→rewrite now holds
  a sidecar advisory flock (`syscall.Flock`) across the read-modify-write so a
  concurrent capture's append (or another recall's `Injected` bump) is no
  longer silently clobbered by the atomic temp+rename.

### Changed

- Adopted the Apache 2.0 license.

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

[Unreleased]: https://github.com/SuperMarioYL/automem/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/SuperMarioYL/automem/releases/tag/v0.5.0
[0.4.0]: https://github.com/SuperMarioYL/automem/releases/tag/v0.4.0
[0.3.0]: https://github.com/SuperMarioYL/automem/releases/tag/v0.3.0
[0.2.0]: https://github.com/SuperMarioYL/automem/releases/tag/v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/automem/releases/tag/v0.1.0
