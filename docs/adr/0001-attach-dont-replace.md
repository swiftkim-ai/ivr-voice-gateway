# ADR-0001: Attach to the IVR; do not replace it

- Status: accepted
- Date: 2026-10-17
- Deciders: project owner

## Context

Almost every "voice AI" product targets replacing the phone tree. That forces the customer to migrate
routing, payment gating, queueing, and compliance off infrastructure they already trust and that
already works. It also puts the vendor in competition with the customer's own switch.

## Decision

The gateway attaches to an existing IVR through one documented contract instead of replacing it. The
IVR keeps its job (routing, account lookup, payment gating, queues). The AI is one participant the IVR
can hand a call to and take back.

## Consequences

- Positive: near-zero adoption friction; the caller hears no change except a better conversation.
- Positive: the customer's compliance and payment boundaries are preserved by construction.
- Negative / trade-offs: we must support whatever control channel each switch exposes (AMI, ARI, ESL,
  REST, an HTTP callback), which is real integration work per platform.
- Follow-ups: Phase 1 delivers the Asterisk path end to end; later phases add ESL and a generic HTTP
  control callback.

## Alternatives considered

| Option | Why not |
|---|---|
| Replace the IVR with an all-in-one voice platform | Highest friction; competes with the customer's infrastructure instead of complementing it. |
| Cloud-only, no self-hosting | Excludes customers (like the reference one) who must keep audio and SQL on their own hardware. |
