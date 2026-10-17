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
| Phase 0 verified end-to-end | ✅ done | live call → log → replay, CI green |
| Control plane (actions) | 🟡 in progress | kinds defined; executes via loopback |
| Asterisk adapter (media + AMI/ARI) | ⬜ not started | Phase 1 |
| DTMF in | ⬜ not started | Phase 1 |
| Test-driver (`dial`/`map`) | ⬜ not started | Phase 2 |
| Warm transfer + console | ⬜ not started | Phase 3 |
| Real STT/LLM/TTS providers | ⬜ not started | Phase 4 |

## Done (most recent first)
- 2026-10-17 Phase 0 verified end to end. A live call was driven through the running gateway with
  the TypeScript SDK over `/v1/media`: greeting → DTMF `1` → `transfer` action → hangup, six events
  written with monotonic `seq`, and that log replayed with 0 divergences. `go test -race ./...`,
  `tsc --noEmit` and the SDK tests are all green.
- 2026-10-17 fix(media): the mu-law compressor assigned every sample one segment too low, because
  each exponent threshold was doubled (`ss < 0x200` for segment 0 instead of `ss < 0x100`). The 4-bit
  mantissa then dropped the leading bit it should have kept, so PCM 1000 came back as 492 and 32767
  as 15996 — roughly half amplitude with wraparound distortion. The codec is now bit-exact with
  Asterisk's `ulaw.c` across all 65536 inputs. It has no callers yet, so nothing recorded was
  affected; it would have garbled audio the moment the Phase 1 adapter used it.
- 2026-10-17 fix(sdk): `ActionKind` is a type-only union and was re-exported as a value, and the
  tests import `../src/client.ts` because that is the specifier Node needs to run them directly.
  `tsc --noEmit` failed on both; `allowImportingTsExtensions` + `rewriteRelativeImportExtensions`
  let tsc accept the `.ts` specifier and rewrite it to `.js` on emit.

## Next (ordered)
1. Phase 1: Asterisk dialplan (AudioSocket media) + AMI/ARI control adapter.
2. Phase 1: DTMF event in, `send_dtmf`/`route`/`transfer` out, sensitive-digit redaction.
3. Cross-language contract test: one fixture validated by both Go and TS.

## Decisions (link ADRs)
- ADR-0001 Attach to the IVR; do not replace it.
- ADR-0002 Language boundaries: Go core, TypeScript SDK/console, Python evals.
- ADR-0003 Append-only JSONL event log, not a database.
- ADR-0004 A separate control channel, because media carriers have no actions.
- ADR-0005 JSON Schema contract now, protobuf later.
- Mu-law follows the 16-bit-domain form in Asterisk's `ulaw.c` (bias 0x84, clip 32635), not the
  14-bit Sun/ITU reference. The two differ on 381 segment-boundary samples because the reference
  truncates to 14 bits first; we keep the precision. Asterisk is the interop target.

## Assumptions (made without confirmation — revisit)
- v1 always has a second control channel available (AMI/ARI/ESL/REST/HTTP).
- 8 kHz mono PCM16 is sufficient for the phone path; higher rates are resampled at the edge.
- Phase 0 runs locally only; the shared production server is out of scope.

## Known issues / deferred
- `schema/` is not yet enforced at runtime (messages are trusted); add validation in Phase 1.
- No auth beyond a `token` query param placeholder.
- Hand-written Go/TS types can drift until codegen (ADR-0005).
- `internal/media` mu-law has no callers yet; it is tested but unexercised until the Phase 1
  adapter uses it, and no real Asterisk instance has been dialled.

## How to run right now
- Prereqs: Go 1.23+, Node 22+ (for the SDK).
- `go run ./cmd/ivrvg serve` — gateway on `:8099`.
- `go run ./cmd/ivrvg replay testdata/calls/example.jsonl`
- `go test ./...` · `cd sdk/ts && npm install && npm test`
