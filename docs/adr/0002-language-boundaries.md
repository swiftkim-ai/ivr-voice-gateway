# ADR-0002: Language boundaries — Go core, TypeScript SDK/console, Python evals

- Status: accepted
- Date: 2026-10-17
- Deciders: project owner

## Context

The system has three distinct workloads: a latency-sensitive realtime audio engine, a typed client
consumed by web and integration teams, and an evaluation/statistics workload. Forcing one language
across all three means compromising at least two of them.

## Decision

Use the best language per workload, with the contract as the seam between them:

- **Go** — call engine, gateway adapters, control plane. Goroutine-per-call, 20 ms frames, one static
  binary, minimal dependencies.
- **TypeScript** — the integration SDK and the operator console. One schema, one typed client;
  Node 22+ runs the SDK natively and the browser runs the console.
- **Python** — the scenario runner and eval scoring (Phase 2/4). The LLM-eval ecosystem lives here and
  scenarios are authored as YAML.

The wire contract (`schema/ivrvg.v1.schema.json`) is the only coupling; no language imports another.

## Consequences

- Positive: each part uses the right runtime; the contract keeps them independent and swappable.
- Negative / trade-offs: three toolchains to install and run in CI; contributors need more than one.
- Follow-ups: generate client types from the schema so the copies cannot drift; keep P0 free of a
  codegen step until the wire is stable.

## Alternatives considered

| Option | Why not |
|---|---|
| All-Go | The eval/stats work and scenario authoring are markedly better in Python; the web SDK in TS. |
| All-TypeScript (Node) | Per-call timers and audio buffers are more predictable in Go; deploy is messier. |
| All-Python | Weaker concurrency story for many simultaneous calls; heavier deploy. |
