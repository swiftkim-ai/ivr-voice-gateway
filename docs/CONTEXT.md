# CONTEXT — IVR Voice Gateway
_Last updated: 2026-10-26_

## Current goal
Phase 2 — the test-driver: originate outbound calls, drive IVR menus via timed scenarios, and
produce deterministic, diffable flow maps.

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
| AMI outbound originate (`ivrvg dial`) | ✅ done | fire-and-forget originate into `ivrvg-out` |
| Scenario-driven outbound calls | ✅ done | `internal/scenario` + `internal/gateway/outbound.go` |
| Flow map from event logs | ✅ done | `internal/flowmap`: build nodes/edges from step events |
| Map comparison (`ivrvg diff`) | ✅ done | exit 0 = identical, exit 1 = changes (JSON diff) |
| `ivrvg map` command | ✅ done | wired: scenarios → AMI originate → AudioSocket → event log → map |
| Outbound integration test | ✅ done | `make outbound-test` — stub IVR, two paths, diff |
| STT-driven traversal | ⬜ not started | Phase 4; scenarios use timed waits for now |
| Warm transfer + console | ⬜ not started | Phase 3 |
| Real STT/LLM/TTS providers | ⬜ not started | Phase 4 |

## Done (most recent first)
- 2026-10-26 Outbound integration test (`make outbound-test`): stub IVR in the Docker dialplan,
  two scenarios mapping both paths, flow map verification, `ivrvg diff` regression detection.
  AMI manager permissions updated to include `originate` write.
- 2026-10-26 Phase 2 core delivered. New packages: `internal/scenario` (timed scenario scripts),
  `internal/flowmap` (flow map graph: build from event logs, diff between releases),
  `internal/gateway/outbound.go` (outbound scenario handler over AudioSocket). New commands:
  `ivrvg map` (runs scenarios, produces a flow map), `ivrvg diff` (compares two maps, exit 1 on
  changes). Sample scenario in `testdata/scenarios/sample.json`. All tests pass with race
  detection.
- 2026-10-25 Started Phase 2. The pushed `ivrvg dial` command requests an asynchronous AMI originate;
  it now reports AMI refusals as errors and describes the result as accepted (AMI acceptance does not
  prove the remote endpoint answered). Removed the `map` command because it only returned an empty
  map and did not explore an IVR.
- 2026-10-25 Connected outbound origination to the existing call path. `ivrvg dial` now accepts an
  Asterisk channel template, creates a UUID, and enters a configurable dialplan context after answer.
  The shipped `ivrvg-out` context announces that UUID and opens AudioSocket, so the existing gateway
  writes the call log and serves/replays the outbound session.
- 2026-10-19 **Tested against a real Asterisk at last** (22.5.2 in a container) and the full Phase 1
  proof passes. Reproducible with `make integration-test`.
- 2026-10-19 CI cleaned up. Runner pinned to ubuntu-24.04. Go and Node versions from go.mod and .nvmrc.
- 2026-10-19 Cross-language contract test. Golden fixture `testdata/contract/messages.jsonl`.
- 2026-10-19 Phase 1 shipped and proved end to end against the real binary.
- 2026-10-17 Phase 0 verified end to end. A live call was driven through the running gateway with
  the TypeScript SDK over `/v1/media`: greeting → DTMF `1` → `transfer` action → hangup.

## Next (ordered)
1. Run `make outbound-test` with Docker to verify the outbound integration test passes end-to-end.
2. Verify that `PlayDTMF` via AMI reaches the remote party through AudioSocket. If it does not,
   fall back to generating DTMF audio tones inline.
3. Add a CI job that runs both integration tests on push.
4. Consider generating the Go and TS types from the schema (ADR-0005).

## Decisions (link ADRs)
- ADR-0001 Attach to the IVR; do not replace it.
- ADR-0002 Language boundaries: Go core, TypeScript SDK/console, Python evals.
- ADR-0003 Append-only JSONL event log, not a database.
- ADR-0004 A separate control channel, because media carriers have no actions.
- ADR-0005 JSON Schema contract now, protobuf later.
- ADR-0006 Self-host everything except PSTN interconnect (proposed).
- ADR-0007 AMI for Phase 1 control, ARI when bridges are needed.
- Mu-law follows the 16-bit-domain form in Asterisk's `ulaw.c` (bias 0x84, clip 32635), not the
  14-bit Sun/ITU reference.
- Phase 2 flow maps are scenario-driven (timed DTMF) rather than STT-driven. STT traversal is
  Phase 4 work that plugs into the same scenario/map infrastructure.

## Assumptions (made without confirmation — revisit)
- v1 always has a second control channel available (AMI/ARI/ESL/REST/HTTP).
- 8 kHz mono PCM16 is sufficient for the phone path; higher rates are resampled at the edge.
- Phase 0 runs locally only; the shared production server is out of scope.
- AMI `PlayDTMF` sends DTMF to the remote party during an AudioSocket session. Needs verification
  against a real switch.

## Known issues / deferred
- `PlayDTMF` does not cross the Local channel bridge when AudioSocket consumes the originating
  channel. The outbound integration test works around this with a stub IVR that times out to a
  default path. Generating in-band DTMF tones via the audio stream is the planned fallback.
- AudioSocket frames are not schema-checked (binary transport, not JSON).
- No auth beyond a `token` query param placeholder.
- Hand-written Go/TS types are not generated from the schema (ADR-0005).
- `internal/media` mu-law still has no callers.
- The AMI connection does not reconnect.
- A digit group has no timeout.
- Barge-in needs the engine to be interruptible.

## How to run right now
- Prereqs: Go 1.23+, Node 22+ (for the SDK). Asterisk 16+ only for a real call.
- `go run ./cmd/ivrvg serve` — gateway on `:8099`.
- With Asterisk: `IVRVG_AMI_SECRET=… go run ./cmd/ivrvg serve -audiosocket 127.0.0.1:8090 -ami 127.0.0.1:5038`
  then dial 7000 from a softphone. Full setup in `docs/ASTERISK.md`.
- `IVRVG_REDACTION_SALT` keys sensitive digit tokens; a random one is generated per run when unset.
- `go run ./cmd/ivrvg replay testdata/calls/example.jsonl`
- Outbound mapping: `IVRVG_AMI_SECRET=… go run ./cmd/ivrvg map -ami host:5038 -channel 'PJSIP/{number}@trunk' -scenario testdata/scenarios/sample.json`
- Compare two maps: `go run ./cmd/ivrvg diff before.json after.json`
- `go test ./...` · `cd sdk/ts && npm install && npm test`
