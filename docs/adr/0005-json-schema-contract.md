# ADR-0005: JSON Schema contract now, protobuf later

- Status: accepted
- Date: 2026-10-17
- Deciders: project owner

## Context

The contract is the seam between languages (ADR-0002). Protobuf is a strong binary schema with codegen,
but it adds `protoc`, plugins, and a generate-before-build step to every contributor and to CI. Phase 0
exchanges control messages as JSON text over a WebSocket whose audio is already binary — the JSON
overhead is negligible and the wire is not yet stable.

## Decision

Define v1 in JSON Schema (`schema/ivrvg.v1.schema.json`) and mirror it with hand-written types in Go and
TypeScript. Adopt protobuf only if and when the wire stabilises and message volume or schema drift
justifies the toolchain.

## Consequences

- Positive: zero codegen; anyone can read and validate the contract with common tools.
- Positive: a call's text messages are human-readable in the event log.
- Negative / trade-offs: hand-written types in two languages can drift; mitigated by a shared fixture
  both languages validate against, and by revisiting codegen at the end of Phase 0.
- Follow-ups: add a cross-language contract test (same fixture, both SDKs) before Phase 1.

## Alternatives considered

| Option | Why not |
|---|---|
| Protobuf from the start | Toolchain cost and a codegen gate before the wire is even stable. |
| Ad-hoc JSON, no schema | No source of truth; the seam between languages rots immediately. |
