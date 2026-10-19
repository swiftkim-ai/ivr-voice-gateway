# IVR Voice Gateway

A self-hosted sidecar that lets any IVR (Asterisk, FreeSWITCH, VBVoice, Twilio, Genesys, a homegrown
switch) hand a live call to a voice AI — and take it back. The AI can hear, speak, **press digits,
route, transfer, and hang up**, and every call is written as a replayable event log so a failed call
becomes a regression test.

The point of this project is not voice quality. Voice quality is a solved commodity. The point is the
**control plane** — acting on the call — and the **eval loop** — proving a change did not break a call.

> Status: **Phase 1 (control plane)** — see [Roadmap](#roadmap). The gateway attaches to a real
> Asterisk: audio and DTMF over AudioSocket, actions over AMI, every call replayable. Point a
> softphone at it and no carrier is needed — see [`docs/ASTERISK.md`](docs/ASTERISK.md).

---

## Why this exists

Most "AI voice agent" projects are one-way: caller talks, agent talks back. Real telephony needs the
other direction too — the agent must *do* things inside a phone system. AudioSocket and raw media
streams carry audio only; DTMF and call actions need a **control channel** (AMI, ARI, or a dialplan
call-back). That asymmetry is the hard part, and it is where this project lives.

Two design commitments follow from it:

1. **The IVR stays. The AI plugs in.** We do not replace the phone tree. We attach to it through a
   single documented contract, so an integrator wires one line into their dialplan and nothing else
   changes for the caller.
2. **One engine, five jobs.** The same call engine, tool registry, and event log serve *answering* a
   call and *placing* one, and serve both production traffic and automated tests.

---

## What it does (by phase)

- **Inbound** — an IVR routes a call to the gateway; the AI greets, listens, replies, and acts.
- **Control** — the AI's tools drive the call: `send_dtmf`, `route`, `transfer`, `play_prompt`,
  `park`, `hangup`, `read_digits`.
- **DTMF + speech** — a caller can press keys or speak at any time; digits arrive as model input, and
  a redaction rule keeps card/PIN digits out of the transcript.
- **Warm transfer** — the AI escalates to a human (or another persona) with a structured handoff
  packet; a web console screen-pops the packet and the live transcript.
- **Outbound / test-driver** — the same engine *dials out*, navigates a menu tree, maps every path,
  asserts outcomes, and diffs the map between releases.
- **Replay / eval** — every call is an append-only event log; `ivrvg replay` re-runs the decisions
  offline and deterministically, and scenarios run in CI.

---

## Roadmap

Each phase is independently demonstrable and ships a working slice — no stubs left behind.

### Phase 0 — Spine ✅
Prove the foundation without needing a phone system.
- The integration contract, versioned (`schema/`, `docs/CONTRACT.md`).
- Append-only JSONL event log per call.
- The call engine and an adapter that carries audio over a plain WebSocket (testable with no telephony).
- A scripted brain behind the same interface a real model will use.
- `ivrvg serve` and `ivrvg replay`, plus a TypeScript SDK that speaks the contract.
- **Proof:** a call connects, turns are exchanged, the log is written, and a replay reproduces the
  exact turn sequence.

### Phase 1 — Control plane ✅ (this release)
The thesis of the project, made real.
- DTMF in over AudioSocket (which carries digits natively) and actions out over AMI:
  `send_dtmf`, `route`, `transfer`, `park`, `hangup`. `play_prompt` waits for ARI (ADR-0007).
- A real Asterisk dialplan — [`deploy/asterisk/`](deploy/asterisk/), setup in
  [`docs/ASTERISK.md`](docs/ASTERISK.md). Verified against Asterisk 22.5 with
  `make integration-test`, which builds a switch in Docker and places a real call.
- `read_digits` collects a group; a sensitive one is reduced to a count and a keyed token before
  anything is written, so it reaches neither the log nor the model — and still replays.
- **Proof:** a caller enters an account number by keypad mid-sentence; the agent reads it back,
  routes the call, and transfers it.

### Phase 2 — Test-driver
The sellable, outward-facing product, reusing the Phase 1 tools in reverse.
- `ivrvg dial` places outbound calls against any reachable IVR.
- `ivrvg map` walks a menu tree via DTMF + speech and emits a flow map.
- Assertions on outcomes; a release-to-release diff of the map.
- **Proof:** map a sample IVR automatically and catch a seeded regression.

### Phase 3 — Warm transfer + console
- Handoff packet (identity, intent, entities, actions taken, sentiment, escalation code).
- Whisper leg to the human; the caller never hears it.
- Web screen-pop console with live transcript and browser take-over.
- **Proof:** an AI host qualifies a caller, then merges a human leg with a whisper and a screen-pop.

### Phase 4 — Real providers + eval CI
- STT / LLM / TTS behind the existing interfaces; recorded fixtures for tests.
- A scenario bank that runs in CI; production failures promoted to scenarios.
- **Proof:** a prompt or model change runs the scenario bank and reports pass/fail per scenario.

---

## Architecture

```
        ┌──────────────┐   media (PCM16 8k)   ┌───────────────────────────────┐
        │   IVR / SIP  │◀────────────────────▶│  gateway adapters             │
        │  Asterisk    │   control (AMI/ARI)  │  wsmedia · audiosocket ·      │
        │  FreeSWITCH  │◀────────────────────▶│  twilio · httphook            │
        │  VBVoice …   │                      └──────────────┬────────────────┘
        └──────────────┘                                     │
                                                             ▼
                        ┌──────────────────────────────────────────────────────┐
                        │  call engine  ──  event log (append-only JSONL)       │
                        │      │                                                │
                        │      ├─ media plane:  VAD → STT → brain → TTS → out   │
                        │      └─ control plane: tool registry → actions        │
                        └──────────────────────────────────────────────────────┘
                                             │
                                   replay · scorer · console
```

The same registry of tools runs **inward** (manage this call) and **outward** (pilot another IVR). The
same event log feeds the live path and offline replay.

## Languages and why

| Part | Language | Why |
|---|---|---|
| Call engine, gateway, control plane | **Go** | Goroutine-per-call, 20 ms frames, one static binary, trivial deploys. |
| Integration SDK + web console | **TypeScript** | One schema, a typed client any web/IVR team can consume; Node 24 runs it natively. |
| Scenario runner + eval scoring | **Python** *(Phase 2/4)* | The LLM-eval ecosystem lives here; scenarios authored as YAML. |
| Contract | **JSON Schema** (`.proto` later) | Language-neutral source of truth; clients generated per language. |

Deliberately no protobuf/build-codegen in Phase 0 — the JSON contract is enough until the wire needs it.

---

## Quickstart

Prerequisites: Go 1.23+, Node 22+ (for the SDK). Asterisk is needed only for a real phone call —
see [`docs/ASTERISK.md`](docs/ASTERISK.md).

```bash
# Build and run the gateway (WebSocket media endpoint on :8099)
go run ./cmd/ivrvg serve

# Attach to Asterisk: media on 8090, control over the manager interface
IVRVG_AMI_SECRET=... go run ./cmd/ivrvg serve -audiosocket 127.0.0.1:8090 -ami 127.0.0.1:5038

# Replay a recorded call deterministically
go run ./cmd/ivrvg replay testdata/calls/example.jsonl

# Go tests
go test ./...

# TypeScript SDK: type-check and test
cd sdk/ts && npm install && npm test
```

## Repository layout

```
cmd/ivrvg/         CLI: serve · replay · (dial · map in Phase 2)
internal/call/     call engine + turn loop
internal/eventlog/ append-only JSONL event log
internal/media/    PCM16, 20 ms frames, G.711 mulaw
internal/brain/    brain interface + scripted brain + tool registry
internal/gateway/  wsmedia + audiosocket adapters
internal/control/  control actions, the Controller seam, AMI client
internal/replay/   deterministic replay
schema/            the v1 contract (JSON Schema)
deploy/asterisk/   dialplan and manager config that work as shipped
docs/              CONTRACT.md · ARCHITECTURE · adr/ · CONTEXT.md
sdk/ts/            TypeScript SDK (types + CallClient)
testdata/calls/    recorded calls and golden fixtures
```

## Documentation

- [`docs/CONTRACT.md`](docs/CONTRACT.md) — the integration contract (v1).
- [`docs/ASTERISK.md`](docs/ASTERISK.md) — attaching to a real Asterisk, end to end.
- [`schema/ivrvg.v1.schema.json`](schema/ivrvg.v1.schema.json) — message schema (source of truth).
- [`docs/adr/`](docs/adr/) — architecture decisions.
- [`docs/CONTEXT.md`](docs/CONTEXT.md) — where the work currently stands.

## Licence

TBD.
