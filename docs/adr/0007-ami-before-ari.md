# ADR-0007: AMI for Phase 1 control, ARI when bridges are needed

- Status: accepted
- Date: 2026-10-19
- Deciders: project owner

## Context

ADR-0004 established that actions travel on a control channel separate from media. Asterisk offers
two: **AMI**, a line-based TCP protocol, and **ARI**, a REST + WebSocket API backed by a Stasis
application. Phase 1 needs `send_dtmf`, `route`, `transfer`, `park`, and `hangup` against a call whose
audio is already flowing over AudioSocket.

The two differ in who owns the call. With AMI the call stays in the dialplan and we act on it from
outside. With ARI the call is handed to our Stasis application and we own its channels and bridges.

AudioSocket turns out to carry DTMF natively (frame type `0x03`), so digits arrive on the media
socket. The control channel is therefore needed for *outbound* actions only, which narrows what
Phase 1 must choose.

## Decision

Use AMI for Phase 1, behind the `control.Controller` interface, and add an ARI controller in Phase 3
when warm transfer needs bridges.

Mapping onto AMI actions:

| Action | AMI | Note |
|---|---|---|
| `send_dtmf` | `PlayDTMF` | Leaves the channel in AudioSocket. |
| `route`, `transfer` | `Redirect` | Deliberately moves the call away; the AI leg ends. |
| `park` | `Park` | Same — the call leaves on purpose. |
| `hangup` | `Hangup` | |
| `read_digits` | *(none)* | The engine collects digits from AudioSocket; no switch action. |
| `play_prompt` | **unsupported** | See below. |

`play_prompt` has no honest AMI implementation. `Redirect` would pull the channel out of the
AudioSocket application to reach `Playback()`, ending the media session the prompt was meant to
interrupt. The AMI controller returns a typed `ErrUnsupported` for it, which is recorded in the event
log like any other outcome, rather than silently doing the wrong thing.

## Consequences

- Positive: no new dependency — AMI is line-based TCP over the standard library, where ARI would need
  an HTTP client, a WebSocket, and a Stasis app to be configured before anything works at all.
- Positive: the call stays in the dialplan, so an integrator wires one line and keeps their existing
  phone tree, which is the commitment in ADR-0001.
- Positive: the `Controller` seam means the ARI adapter is additive, not a rewrite.
- Negative / trade-offs: `play_prompt` is unavailable until Phase 3. Agents can speak through the
  media path, so this blocks only switch-side prompt files.
- Negative / trade-offs: AMI gives no bridge control, so warm transfer and the whisper leg are not
  expressible. That is Phase 3's work and the reason ARI arrives then.
- Follow-ups: implement `control.ARI` in Phase 3; revisit whether AMI remains the default once it
  exists.

## Alternatives considered

| Option | Why not |
|---|---|
| ARI from the start | More machinery before the first call works, and it takes ownership of the channel, which conflicts with attaching to an IVR that already owns its dialplan. |
| Both adapters now | Doubles the Phase 1 control surface and its integration tests for capability Phase 1 cannot yet use. |
| A dialplan HTTP callback instead of either | Another contract to version, and no path to bridges later. |
