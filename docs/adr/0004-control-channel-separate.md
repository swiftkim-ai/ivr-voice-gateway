# ADR-0004: A separate control channel, because media carriers have no actions

- Status: accepted
- Date: 2026-10-17
- Deciders: project owner

## Context

AudioSocket, Twilio Media Streams, and raw RTP carry audio. They do not carry DTMF or call-control
actions. A voice agent that can only produce audio cannot route, transfer, press keys, or hang up — it
is a passenger, not a participant.

## Decision

Treat control as a first-class, separate concern from media. The engine emits typed `action` messages;
a per-deployment adapter translates them onto the switch's control channel (AMI/ARI, ESL, Twilio REST,
or a generic HTTP callback). Phase 0 backs this with a loopback and records every action in the event
log; Phase 1 wires Asterisk.

## Consequences

- Positive: the engine never depends on a specific switch's control API; only the adapter does.
- Positive: actions are recorded, so "did the agent transfer correctly?" is a replayable assertion.
- Negative / trade-offs: every supported platform needs its own adapter and an integration test.
- Follow-ups: define the adapter interface in Phase 1 and ship the Asterisk one first.

## Alternatives considered

| Option | Why not |
|---|---|
| Infer control from audio only (in-band DTMF decoding) | No call actions at all; unreliable on compressed or noisy lines. |
| Depend on Asterisk ARI throughout | Couples the engine to one vendor and to its REST model. |
