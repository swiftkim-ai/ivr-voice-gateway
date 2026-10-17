# ADR-0003: Append-only JSONL event log, not a database

- Status: accepted
- Date: 2026-10-17
- Deciders: project owner

## Context

We need to reconstruct exactly what happened on a call, run it again offline, and turn a failure into a
regression test. A relational store invites mutable rows (a turn's text edited in place) and makes
"re-run this call" mean re-implementing the engine against the schema.

## Decision

Every call is one append-only JSONL file: one event per line, each with a per-call monotonic `seq` and a
UTC `at`. The live path and the replay path both write/read the same events. No database in Phase 0.

## Consequences

- Positive: a call is a file you can copy, diff, and attach to a bug report.
- Positive: replay is the same engine reading recorded inputs — no parallel implementation to drift.
- Positive: append-only means no in-place mutation, so ordering is trustworthy.
- Negative / trade-offs: querying across many calls means scanning files; a later index or a projection
  into SQLite can be added without changing the source of truth.
- Follow-ups: Phase 2 promotes failed calls into scenarios; Phase 4 runs a scenario bank in CI.

## Alternatives considered

| Option | Why not |
|---|---|
| SQLite as the primary store | Mutable rows blur the append-only guarantee replay depends on. |
| Structured logs to stdout only | Not self-contained; hard to replay a single call deterministically. |
