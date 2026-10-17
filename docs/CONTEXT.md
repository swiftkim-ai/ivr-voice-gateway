# CONTEXT — IVR Voice Gateway
_Last updated: 2026-10-17 by project owner_

## Current goal
Phase 0 — the spine: contract, append-only event log, WebSocket media adapter, scripted brain, and
deterministic replay. Prove a call connects, turns exchange, the log is written, and replay reproduces
the exact turn sequence.

## Status at a glance
| Area | State | Notes |
|---|---|---|
| Contract (v1 schema + doc) | ✅ done | `schema/`, `docs/CONTRACT.md` |
| Event log | ✅ done | append-only JSONL, monotonic `seq` |
| Media plane (WS) | ✅ done | `/v1/media`, PCM16 8k, 20 ms frames |
| Scripted brain | ✅ done | same interface a real model will use |
| Deterministic replay | ✅ done | re-runs decisions from the log |
| TypeScript SDK | ✅ done | types + `CallClient` |
| Control plane (actions) | 🟡 in progress | kinds defined; executes via loopback |
| Asterisk adapter (media + AMI/ARI) | ⬜ not started | Phase 1 |
| DTMF in | ⬜ not started | Phase 1 |
| Test-driver (`dial`/`map`) | ⬜ not started | Phase 2 |
| Warm transfer + console | ⬜ not started | Phase 3 |
| Real STT/LLM/TTS providers | ⬜ not started | Phase 4 |

## Next (ordered)
1. Finish Phase 0 verification: `go test ./...`, SDK `npm test`, replay a recorded call.
2. Phase 1: Asterisk dialplan (AudioSocket media) + AMI/ARI control adapter.
3. Phase 1: DTMF event in, `send_dtmf`/`route`/`transfer` out, sensitive-digit redaction.
4. Cross-language contract test: one fixture validated by both Go and TS.

## Decisions (link ADRs)
- ADR-0001 Attach to the IVR; do not replace it.
- ADR-0002 Language boundaries: Go core, TypeScript SDK/console, Python evals.
- ADR-0003 Append-only JSONL event log, not a database.
- ADR-0004 A separate control channel, because media carriers have no actions.
- ADR-0005 JSON Schema contract now, protobuf later.

## Assumptions (made without confirmation — revisit)
- v1 always has a second control channel available (AMI/ARI/ESL/REST/HTTP).
- 8 kHz mono PCM16 is sufficient for the phone path; higher rates are resampled at the edge.
- Phase 0 runs locally only; the shared production server is out of scope.

## Known issues / deferred
- `schema/` is not yet enforced at runtime (messages are trusted); add validation in Phase 1.
- No auth beyond a `token` query param placeholder.
- Hand-written Go/TS types can drift until codegen (ADR-0005).

## How to run right now
- Prereqs: Go 1.23+, Node 22+ (for the SDK).
- `go run ./cmd/ivrvg serve` — gateway on `:8099`.
- `go run ./cmd/ivrvg replay testdata/calls/example.jsonl`
- `go test ./...` · `cd sdk/ts && npm install && npm test`
