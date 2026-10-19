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
| ← | `digits` | A completed `read_digits` group. Redacted groups carry `count` and `hash` only. |
| ← | `action` | The agent acts: `send_dtmf`, `route`, `transfer`, `play_prompt`, `park`, `hangup`, `read_digits`. |
| ← | `call_ended` | Call closed, log flushed. |
| ← | `error` | Protocol or provider error (`code`, `message`). |

Every server message carries a monotonically increasing `seq` and an `at` (RFC 3339). The sequence is
what a replay relies on.

A client may stamp its own `seq` and `at` on the messages it sends — `start`, `dtmf`, `stop` — as the
reference SDK does. The server accepts them and relies on neither; the two sequences are independent
counters. `dtmf` travels in both directions: the client sends one without `seq`/`at`, and the gateway
echoes it back as an event carrying both.

**The gateway validates every inbound text frame against the schema.** A message that does not match
is answered with an `error` (`code: "invalid_message"`, or `"bad_start"` for the opening message) and
is not acted on. A rejected message does not end the call — only a rejected `start` does, since
without it there is no call.

Worked examples of every message, in both forms, are in
[`../testdata/contract/messages.jsonl`](../testdata/contract/messages.jsonl). Both the Go and the
TypeScript test suites validate that file against the schema, so it cannot drift from either.

## 3a. Collecting digits, and redaction

`read_digits` asks the engine to collect a group of keypad digits rather than
report each one. Its `args`:

| Arg | Meaning |
|---|---|
| `label` | What is being collected (`"account"`, `"card"`). Echoed back on the result. |
| `count` | How many digits to expect. |
| `terminator` | A digit that ends the group early. Defaults to `#` when no `count` is given, so a group always has a way to close. |
| `sensitive` | When true the digits are redacted — see below. |

The group is reported once, as a `digits` message, when it closes:

```json
{ "type": "digits", "seq": 7, "at": "…", "label": "account", "count": 5, "digits": "12345" }
```

A **sensitive** group never yields its digits. The engine reduces it to a count
and a keyed token before anything is written, so the digits reach neither the
append-only event log nor the model:

```json
{ "type": "digits", "seq": 7, "at": "…", "label": "card", "count": 16,
  "redacted": true, "hash": "9f2c1b7e4a0d3c55" }
```

The token is a truncated HMAC, not a bare hash: a card number or PIN is short
enough that an unkeyed digest is recoverable by brute force. It exists so the
same entry can be recognised across calls, and it is not a secret. The key is
deployment configuration and never appears in the log.

Replay stays deterministic because the model is fed the same surrogate the
recording holds — the digits themselves are not needed to reproduce the
decision.

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

Minimal Asterisk dialplan — media over AudioSocket, control over AMI:

```
exten => 7000,1,Answer()
 same => n,Set(IVRVG_UUID=${UUID()})
 same => n,UserEvent(IvrvgStart,UUID: ${IVRVG_UUID},Hint: ${CALLERID(num)})
 same => n,AudioSocket(${IVRVG_UUID},gateway:8090)
 same => n,Hangup()
```

`AudioSocket` carries the audio and the caller's keypresses. The `UserEvent`
tells the gateway which channel the call is on, since AudioSocket does not, and
actions have to be addressed somewhere; `Hint` passes the IVR's context to the
model. Asterisk adds the channel name to the event itself.

The full setup, including the manager account the control channel needs, is in
[`ASTERISK.md`](ASTERISK.md). Working configuration files are in
[`../deploy/asterisk/`](../deploy/asterisk/).

## 6. SDKs

- **TypeScript:** [`../sdk/ts`](../sdk/ts) — typed message models and a `CallClient` that speaks this
  contract. Use it in the web console and in integration tests.
- **Go:** the reference implementation lives in `internal/`.
- **Python:** planned with the scenario runner (Phase 2/4).

## 7. Versioning

The path prefix `/v1` and `ivrvg.v1.schema.json` are the version. Additive optional fields are
allowed within v1; anything that changes meaning gets `/v2` and a new schema file.
