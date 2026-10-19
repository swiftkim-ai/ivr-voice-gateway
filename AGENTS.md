# AGENTS.md — IVR Voice Gateway

## Read first (in this order)
1. ~/.engineering/STANDARDS.md — global rules (if missing, use ./STANDARDS.md)
2. This file — project rules
3. docs/CONTEXT.md — where the work currently stands
4. docs/CONTRACT.md and docs/adr/ — the interface and why it is shaped this way
5. docs/ASTERISK.md — attaching to a real switch (Phase 1 onward)
6. README.md — the phased roadmap

## Project in one paragraph
A self-hosted sidecar that lets any IVR hand a live call to a voice AI and take it back. The value is
the **control plane** (the agent can act on the call) and the **eval loop** (every call replays
deterministically), not voice quality. See README.md for the phase-by-phase plan.

## Stack
Go 1.23+ (engine, gateway, control) · TypeScript / Node 22+ (SDK, console) · Python (scenarios/evals,
Phase 2/4). Contract in JSON Schema. No database in Phase 0 — the event log is append-only JSONL.

## Commands
| Task | Command |
|---|---|
| Everything CI runs | `make check` |
| Run gateway (no telephony) | `make run` |
| Run attached to Asterisk | `make run-asterisk` (needs `IVRVG_AMI_SECRET`) |
| Replay a call | `make replay CALL=<file.jsonl>` |
| Go tests | `make test-go` |
| Go vet / format | `make vet` / `make fmt` (`make fmt-check` to verify) |
| SDK type-check | `make typecheck` |
| SDK tests | `make test-ts` |

Environment variables are documented in `.env.example`. Secrets are read from the environment, never
from flags, which any user who can list processes can read.

## Project-specific rules
- The contract is the seam: change `schema/ivrvg.v1.schema.json` and `docs/CONTRACT.md` together, and
  update both Go and TypeScript types in the same change.
- Additive optional fields are allowed within v1; anything that changes meaning is a new `/v2`.
- The event log is append-only. Never mutate or delete an event; never renumber `seq`.
- Audio is always 8 kHz mono PCM16 at the engine boundary; resample only at the adapter edge.
- Control actions are emitted as typed messages and recorded; the switch-specific translation lives
  only in the adapter, never in the engine.
- Providers sit behind interfaces; tests use fakes. Phase 0 ships a scripted brain, not a real model.
- No new dependency without a written justification; prefer the standard library.

## Do not touch without asking
- `schema/ivrvg.v1.schema.json` semantics (additive-only without a version bump).
- Anything on the shared production server — this project is local-only until it has its own host.

## Definition of Done
Global Definition of Done + Anti-Slop checklist, then update docs/CONTEXT.md.
