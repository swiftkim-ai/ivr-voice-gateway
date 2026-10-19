# CONTEXT — IVR Voice Gateway
_Last updated: 2026-10-19 by project owner_

## Current goal
Phase 1 — the control plane: attach to a real Asterisk, carry the agent's actions onto the switch,
and collect keypad digits (redacting the sensitive ones) without losing deterministic replay.

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
| Control plane (actions) | ✅ done | `Controller` seam; AMI adapter carries them out |
| Asterisk adapter (AudioSocket + AMI) | ✅ done | `deploy/asterisk/`, `docs/ASTERISK.md` |
| DTMF in | ✅ done | AudioSocket frame `0x03`; no AMI correlation needed |
| Sensitive digit redaction | ✅ done | count + keyed token; replays without the digits |
| Test-driver (`dial`/`map`) | ⬜ not started | Phase 2 |
| Warm transfer + console | ⬜ not started | Phase 3 |
| Real STT/LLM/TTS providers | ⬜ not started | Phase 4 |

## Done (most recent first)
- 2026-10-19 Phase 1 shipped and proved end to end against the real binary. A scripted stand-in for
  Asterisk served AMI and placed an AudioSocket call: greeting → DTMF `2` → account number `48217#`
  keyed mid-call → the agent read it back → `Redirect` to extension 200 arrived on the manager
  interface with the right channel and context. The call then replayed with 0 divergences.
- 2026-10-19 AudioSocket turned out to carry DTMF natively (frame `0x03`), so digits do not need the
  AMI channel correlation that was planned. The correlation is still needed to address actions.
- 2026-10-19 `play_prompt` has no honest AMI implementation: `Redirect` would pull the channel out of
  AudioSocket and kill the media session. The adapter returns `control.ErrUnsupported` and the
  refusal is recorded; ARI closes this in Phase 3 (ADR-0007).
- 2026-10-19 Adding the `digits` event broke replay until `internal/replay` learned it — a Phase 1
  call replayed as 2 divergences. Caught by running the real binary, not by the unit tests.
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
1. Test against a real Asterisk, not the scripted stand-in: module load, dialplan reload, softphone.
2. Cross-language contract test: one fixture validated by both Go and TS.
3. Phase 2: `ivrvg dial` and `ivrvg map` — the same tools in reverse.

## Decisions (link ADRs)
- ADR-0001 Attach to the IVR; do not replace it.
- ADR-0002 Language boundaries: Go core, TypeScript SDK/console, Python evals.
- ADR-0003 Append-only JSONL event log, not a database.
- ADR-0004 A separate control channel, because media carriers have no actions.
- ADR-0005 JSON Schema contract now, protobuf later.
- ADR-0006 Self-host everything except PSTN interconnect (proposed).
- ADR-0007 AMI for Phase 1 control, ARI when bridges are needed.
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
- `internal/media` mu-law still has no callers: AudioSocket carries SLIN, which is already the
  engine's format. It becomes load-bearing for a Twilio Media Streams adapter or raw RTP.
- **No real Asterisk has been dialled.** Phase 1 is proved against a scripted stand-in that speaks
  the AMI and AudioSocket protocols; module loading, dialplan reloads and NAT are untested.
- The AMI connection does not reconnect. If it drops, actions fail until the gateway restarts.
- A digit group has no timeout: it closes on its count or terminator, nothing else. A caller who
  stops halfway leaves it open. Adding one means a timer in the engine, which is currently
  event-driven and single-threaded per call.
- Audio is written paced at 20 ms from the same goroutine that reads, so a digit pressed while the
  agent is speaking waits up to a sentence to be processed. Barge-in needs a separate read goroutine.

## How to run right now
- Prereqs: Go 1.23+, Node 22+ (for the SDK). Asterisk 16+ only for a real call.
- `go run ./cmd/ivrvg serve` — gateway on `:8099`.
- With Asterisk: `IVRVG_AMI_SECRET=… go run ./cmd/ivrvg serve -audiosocket 127.0.0.1:8090 -ami 127.0.0.1:5038`
  then dial 7000 from a softphone. Full setup in `docs/ASTERISK.md`.
- `IVRVG_REDACTION_SALT` keys sensitive digit tokens; a random one is generated per run when unset.
- `go run ./cmd/ivrvg replay testdata/calls/example.jsonl`
- `go test ./...` · `cd sdk/ts && npm install && npm test`
