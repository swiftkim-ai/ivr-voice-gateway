# CONTEXT — IVR Voice Gateway
_Last updated: 2026-10-25_

## Current goal
Phase 2 — the test-driver: originate outbound calls, drive IVR menus, and produce deterministic,
diffable flow maps using the existing call engine and event log.

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
| Asterisk adapter (AudioSocket + AMI) | ✅ done | verified against real Asterisk 22.5.2 |
| Real-switch integration test | ✅ done | `make integration-test` (Docker) |
| DTMF in | ✅ done | AudioSocket frame `0x03`; no AMI correlation needed |
| Sensitive digit redaction | ✅ done | count + keyed token; replays without the digits |
| AMI outbound originate (`ivrvg dial`) | 🟡 started | Originate an Asterisk channel into `ivrvg-out`; answered calls use AudioSocket and the existing JSONL logging/replay path. |
| IVR mapping (`ivrvg map`) | ⬜ not started | Removed the empty-map command; implement menu traversal before exposing it again. |
| Warm transfer + console | ⬜ not started | Phase 3 |
| Real STT/LLM/TTS providers | ⬜ not started | Phase 4 |

## Done (most recent first)
- 2026-10-25 Started Phase 2. The pushed `ivrvg dial` command requests an asynchronous AMI originate;
  it now reports AMI refusals as errors and describes the result as accepted (AMI acceptance does not
  prove the remote endpoint answered). Removed the `map` command because it only returned an empty
  map and did not explore an IVR.
- 2026-10-25 Connected outbound origination to the existing call path. `ivrvg dial` now accepts an
  Asterisk channel template, creates a UUID, and enters a configurable dialplan context after answer.
  The shipped `ivrvg-out` context announces that UUID and opens AudioSocket, so the existing gateway
  writes the call log and serves/replays the outbound session. The channel template is deployment
  specific (for example, `PJSIP/{number}@my-trunk`); speech recognition and menu traversal remain
  later Phase 2 work.
- 2026-10-19 **Tested against a real Asterisk at last** (22.5.2 in a container) and the full Phase 1
  proof passes: greeting → DTMF `2` → account number `48217#` keyed mid-call → the agent reads it
  back → AMI `Redirect` moves the real channel into `200@default`, which the dialplan confirms →
  the recorded call replays with 0 divergences. Reproducible with `make integration-test`.
- 2026-10-19 The real switch found three bugs the stand-in could not:
  (1) **The media stream must never stop.** Asterisk hangs up after 2 s without traffic
  (`MAX_WAIT_TIMEOUT_MSEC`, hard-coded). The adapter only wrote while the agent spoke, so every
  call died two seconds after the greeting. It now streams continuously, sending silence when idle.
  (2) **The shipped dialplan was broken.** It used `${UUID()}`, which does not exist before
  Asterisk 22 — on 20.x it expands to nothing and the call fails. Replaced with an MD5 of the
  channel uniqueid, which works on every version.
  (3) **DTMF over AudioSocket needs Asterisk 20.15 / 22.4 or newer.** Ubuntu 24.04 LTS ships 20.6,
  where the audio path works and keypresses silently go nowhere. Documented with a version table.
- 2026-10-19 Verified the documented minimum manager permissions (`read = call,user`,
  `write = call`) are genuinely sufficient, by narrowing the test switch to exactly those.
- 2026-10-19 CI cleaned up. Every job had been carrying Node 20 deprecation warnings because the
  actions were two to three majors behind (checkout/setup-node v4, setup-go v5, all now v7), which
  is what the annotations on every run reported. The runner is pinned to ubuntu-24.04 rather than
  ubuntu-latest, which began migrating to Ubuntu 26 on 2026-10-19. Go and Node versions now come
  from go.mod and .nvmrc instead of being repeated in the workflow. Verified: run 39247610330 is
  green with no annotations.
  Note for anyone reading the Actions tab: the one failed run belongs to the initial commit
  62ae876, which failed `tsc --noEmit` on two type errors fixed in 27ea8c4. Everything since is
  green.
- 2026-10-19 Cross-language contract test. One golden fixture, `testdata/contract/messages.jsonl`,
  validated by Go and TypeScript against the same schema file. Go proves `wire.Message` carries every
  field; TypeScript proves the same at compile time, since its types vanish at runtime. Both drift
  guards were checked by deliberately adding a field to the schema and watching each fail.
- 2026-10-19 The fixtures found two real contract bugs. The schema forbade `seq`/`at` on `dtmf`,
  `start` and `stop`, yet the engine stamps them on every message it emits and the reference SDK
  stamps them on everything it sends — so the gateway and its own client were both emitting messages
  the contract rejected. Fixed additively by allowing them, which v1 permits.
- 2026-10-19 The WebSocket boundary now validates inbound messages instead of trusting them, closing
  the known issue carried since Phase 0. A rejected message is answered and the call continues.
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
1. Add a repeatable Asterisk integration path that simulates an answered outbound channel and verifies
   the UUID join, media session, event log, and replay.
2. Implement traversal over observed DTMF/speech choices and emit a real flow map; add release-to-
   release map comparison after the map format is established.
3. Consider generating the Go and TS types from the schema (ADR-0005), now that the fixture proves
   where they drift.

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
- Phase 2 outbound calls require a configured Asterisk channel template and `ivrvg-out` context.
  Speech recognition, automated menu traversal, and a flow map are not implemented yet; `map` stays
  unavailable until it can produce real observations.
- AudioSocket frames are not schema-checked: that transport carries audio and single digits, not
  JSON, so there is nothing to validate. The WebSocket path is checked.
- No auth beyond a `token` query param placeholder.
- Hand-written Go/TS types still are not generated (ADR-0005), but a golden fixture now pins them:
  `testdata/contract/messages.jsonl` is validated by both languages, and both fail if a contract
  field is missing on their side.
- `internal/media` mu-law still has no callers: AudioSocket carries SLIN, which is already the
  engine's format. It becomes load-bearing for a Twilio Media Streams adapter or raw RTP.
- Real Asterisk is now tested, but only in a container on one host: NAT between separate machines,
  SIP trunk interop and a registered softphone are all still untested.
- DTMF does not work on Asterisk older than 20.15 / 22.4, where AudioSocket cannot carry it. The
  audio path works, so the failure is silent. See the version table in `docs/ASTERISK.md`.
- The AMI connection does not reconnect. If it drops, actions fail until the gateway restarts.
- A digit group has no timeout: it closes on its count or terminator, nothing else. A caller who
  stops halfway leaves it open. Adding one means a timer in the engine, which is currently
  event-driven and single-threaded per call.
- Audio is written by its own goroutine now, but the engine still processes one event at a time, so a
  digit pressed mid-sentence waits for the sentence to finish. Barge-in needs the engine to be
  interruptible, not just the writer to be separate.

## How to run right now
- Prereqs: Go 1.23+, Node 22+ (for the SDK). Asterisk 16+ only for a real call.
- `go run ./cmd/ivrvg serve` — gateway on `:8099`.
- With Asterisk: `IVRVG_AMI_SECRET=… go run ./cmd/ivrvg serve -audiosocket 127.0.0.1:8090 -ami 127.0.0.1:5038`
  then dial 7000 from a softphone. Full setup in `docs/ASTERISK.md`.
- `IVRVG_REDACTION_SALT` keys sensitive digit tokens; a random one is generated per run when unset.
- `go run ./cmd/ivrvg replay testdata/calls/example.jsonl`
- `go test ./...` · `cd sdk/ts && npm install && npm test`
