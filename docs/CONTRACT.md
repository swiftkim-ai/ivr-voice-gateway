# Integration Contract — v1

This is the interface another team integrates against. It is deliberately small: one transport, one
framing rule, one message set. If you can open a WebSocket and push bytes, you can attach the gateway
to your phone system.

The machine-readable source of truth is [`../schema/ivrvg.v1.schema.json`](../schema/ivrvg.v1.schema.json).
This document explains the intent and the integration.

## 1. Transport

- A single WebSocket per call: `GET /v1/media?call_id=<id>&token=<token>`.
- **Binary frames** carry audio. **Text frames** carry JSON messages.
- Audio is always **8 kHz mono, 16-bit little-endian PCM**, in 20 ms frames (160 samples, 320 bytes).
  Anything else is resampled at the edge by the adapter, never inside the engine.

## 2. Lifecycle

```
client                              gateway
  │  connect /v1/media                 │
  │  ── text {"type":"start",...} ───▶ │   opens the call, writes call_started
  │  ◀── text {"type":"call_started"} ─│
  │  ── binary audio (caller) ───────▶ │
  │  ◀── text {"type":"transcript"} ───│   caller speech recognised
  │  ◀── text {"type":"reply"} ────────│   what the agent will say
  │  ◀── binary audio (agent) ─────────│
  │  ── text {"type":"dtmf","digit"} ─▶│   caller pressed a key
  │  ◀── text {"type":"action",...} ───│   agent acts on the call
  │  ── text {"type":"stop"} ─────────▶ │
  │  ◀── text {"type":"call_ended"} ───│   event log flushed
```

## 3. Messages

| Direction | type | Purpose |
|---|---|---|
| → | `start` | Open the call. `hint` carries IVR context (caller id, menu choice, account). |
| ← | `call_started` | Call accepted; `seq` begins. |
| → | *binary* | Caller audio (PCM16, 20 ms). |
| → | `dtmf` | A keypad digit (`sensitive: true` keeps it out of the transcript). |
| → | `stop` | Caller hung up / leg ending. |
| ← | `transcript` | Recognised caller speech (`final` false while refining). |
| ← | `reply` | A sentence the agent will speak; its audio frames follow. |
| ← | `action` | The agent acts: `send_dtmf`, `route`, `transfer`, `play_prompt`, `park`, `hangup`, `read_digits`. |
| ← | `call_ended` | Call closed, log flushed. |
| ← | `error` | Protocol or provider error (`code`, `message`). |

Every server message carries a monotonically increasing `seq` and an `at` (RFC 3339). The sequence is
what a replay relies on.

## 4. The control channel (important)

Audio transports — AudioSocket, Twilio Media Streams, raw RTP — carry **audio only**. DTMF and call
actions do not travel on them. Therefore v1 assumes a **second, bidirectional control channel**
alongside the media socket:

- **Asterisk:** AMI or ARI to send DTMF, redirect, and hang up a channel.
- **FreeSWITCH:** the Event Socket (ESL).
- **Twilio:** the REST API plus `<Redirect>`.
- **A generic IVR (e.g. VBVoice):** an HTTP callback the dialplan polls, or a small HTTPS endpoint the
  IVR calls when it wants to act.

The gateway emits `action` messages on the media socket as its *intent*; the adapter translates them
onto whichever control channel the deployment uses. Phase 0 wires this to a loopback; Phase 1 wires it
to a real Asterisk.

## 5. Attaching to an existing IVR (the one-line promise)

Minimal Asterisk dialplan — media over AudioSocket, context passed up front:

```
exten => 7000,1,Answer()
 same => n,Set(CALL_ID=${UNIQUEID})
 same => n,Set(HINT=${CALLERID(num)})
 same => n,CURL(http://gateway:8099/v1/attach?call_id=${CALL_ID}&hint=${HINT})
 same => n,AudioSocket(${CALL_ID},gateway:8090)
 same => n,Hangup()
```

(Exact adapter syntax lands in Phase 1; the contract above is fixed now.)

## 6. SDKs

- **TypeScript:** [`../sdk/ts`](../sdk/ts) — typed message models and a `CallClient` that speaks this
  contract. Use it in the web console and in integration tests.
- **Go:** the reference implementation lives in `internal/`.
- **Python:** planned with the scenario runner (Phase 2/4).

## 7. Versioning

The path prefix `/v1` and `ivrvg.v1.schema.json` are the version. Additive optional fields are
allowed within v1; anything that changes meaning gets `/v2` and a new schema file.
