# Attaching to Asterisk

The gateway attaches to a call over two connections, because no media transport
carries call control (ADR-0004):

```
   caller ──SIP──▶ Asterisk ──AudioSocket (TCP)──▶ ivrvg      audio + DTMF in,
                       ▲                                       agent voice out
                       └────────── AMI (TCP) ──────────────┘   transfer, route,
                                                               send_dtmf, park,
                                                               hangup
```

Audio on the AudioSocket leg is **8 kHz mono 16-bit little-endian PCM** — the
engine's native format — so nothing is resampled or transcoded here. Asterisk
handles G.711 on the trunk side.

## 1. Prerequisites

**Version matters more than it looks.** AudioSocket exists from Asterisk 16, but
it only carries DTMF from **20.15** and **22.4** onward. On an older build the
audio path works and the caller's keypresses go nowhere, which is easy to
mistake for a bug in the gateway:

| Asterisk | AudioSocket audio | DTMF over AudioSocket |
|---|---|---|
| 16 – 20.14 | yes | **no** |
| 20.15+ | yes | yes |
| 21.x, 22.0 – 22.3 | yes | **no** |
| 22.4+ | yes | yes |

Ubuntu 24.04 LTS ships 20.6, which cannot carry DTMF. Ubuntu 25.10 ships 22.5,
which can. Verify your own build before blaming the gateway:

```bash
asterisk -rx "module show like audiosocket"   # expect res_ and app_ Running
asterisk -V                                   # then check the table above
```

You also need a SIP endpoint to call from — a softphone (Linphone, Zoiper)
registered to your Asterisk is enough. **No phone number or carrier is needed.**

### The quickest check: the containerised test

If you have Docker, this builds a real Asterisk, places a real call, and
asserts the whole Phase 1 path without any configuration of your own:

```bash
./deploy/asterisk/docker/run-integration-test.sh
```

It drives the caller from a dialplan context, so it needs no phone at all.

## 2. Configure Asterisk

Copy the two sample files and reload:

```bash
sudo cp deploy/asterisk/extensions.conf /etc/asterisk/extensions.conf
sudo cp deploy/asterisk/manager.conf    /etc/asterisk/manager.conf
sudo asterisk -rx "dialplan reload"
sudo asterisk -rx "manager reload"
```

Set a real password in `manager.conf` first. Confirm the manager is listening:

```bash
asterisk -rx "manager show settings" | head
```

## 3. Run the gateway

```bash
export IVRVG_AMI_SECRET='the password from manager.conf'
export IVRVG_REDACTION_SALT="$(openssl rand -hex 32)"   # see §6

go run ./cmd/ivrvg serve \
  -audiosocket 127.0.0.1:8090 \
  -ami 127.0.0.1:5038 \
  -ami-user ivrvg \
  -ami-context default \
  -logdir data/calls
```

`-ami-context` is the dialplan context that `route` and `transfer` redirect
into. Point it at whichever context holds your real extensions.

Leaving `-ami` off runs the call media-only: the agent still talks and hears and
every action is recorded, but nothing carries the actions out.

## 4. Place a call

Dial **7000** from your softphone. You should hear the agent greet you, and see
the call appear under `data/calls/<uuid>.jsonl`. Press `1` and the sample brain
transfers you to extension 200.

Replay the call afterwards — this is the point of the event log:

```bash
go run ./cmd/ivrvg replay data/calls/<uuid>.jsonl
```

## 5. How the pieces find each other

AudioSocket identifies a call by a UUID and carries nothing else about it, so
the dialplan announces the rest:

```
same => n,Set(RAW=${MD5(${UNIQUEID})})
same => n,Set(IVRVG_UUID=${RAW:0:8}-${RAW:8:4}-${RAW:12:4}-${RAW:16:4}-${RAW:20:12})
same => n,UserEvent(IvrvgStart,UUID: ${IVRVG_UUID},Hint: ${CALLERID(num)})
same => n,AudioSocket(${IVRVG_UUID},${IVRVG_MEDIA})
```

`${UUID()}` would be the obvious way to make that id, and it is what most
examples show — but the function does not exist before Asterisk 22. On 20.x it
expands to nothing and the call fails with no useful error. MD5 of the
channel's uniqueid is unique per call and works on every version.

The gateway joins the `UserEvent` (which Asterisk stamps with the channel name)
to the AudioSocket connection by UUID. `Hint` is passed straight to the brain as
the IVR's context — caller id here, but it can carry a menu choice or an account
number just as well.

The two arrive in either order, so the gateway waits briefly for the event. If
it never comes the call still runs, with actions unaddressable and a line in the
log saying so.

## 5a. The media stream must never stop

Asterisk hangs up an AudioSocket connection after **two seconds without
traffic**. That is `MAX_WAIT_TIMEOUT_MSEC` in `app_audiosocket.c`, and the
source notes it is hard-coded — there is no dialplan argument or config key for
it.

So AudioSocket is a real-time media stream, not a message channel. The gateway
sends a frame every 20 ms for the whole call, carrying the agent's voice when
there is something to say and silence when there is not. A server that speaks
only when spoken to gets hung up on a couple of seconds after its greeting.

If you write another adapter against this contract, this is the part to get
right first. The symptom in the Asterisk log is:

```
ERROR app_audiosocket.c:221 audiosocket_run: Reached timeout after 2000 ms
of no activity on AudioSocket connection
```

## 6. Sensitive digits

`read_digits` with `sensitive: true` collects a group — a card number, a PIN —
that must not survive the call. The engine reduces it to a count and a keyed
token before anything is written, so the digits reach neither the append-only
log nor the model.

`IVRVG_REDACTION_SALT` is that key. Set it to a long random value and keep it
out of the log directory. When it is unset the gateway generates one per run:
the digits are still never logged, but tokens stop matching across restarts.

## 7. What works, and what does not yet

| Action | Status |
|---|---|
| `send_dtmf` | Works — AMI `PlayDTMF`. |
| `route`, `transfer` | Works — AMI `Redirect`. The call leaves the agent, by design. |
| `park` | Works — AMI `Park`. |
| `hangup` | Works — AMI `Hangup`. |
| `read_digits` | Works — digits arrive over AudioSocket; the engine collects them. |
| `play_prompt` | **Not yet.** Reaching `Playback()` means redirecting the channel out of AudioSocket, which would end the call's media. ARI does this properly in Phase 3 (ADR-0007). The refusal is recorded in the event log. |

## 8. Troubleshooting

| Symptom | Likely cause |
|---|---|
| `No such application 'AudioSocket'` | The module is not built or not loaded. `asterisk -rx "module load app_audiosocket.so"`. |
| Call connects, silence both ways | The gateway is not listening on `IVRVG_MEDIA`, or a firewall sits between them. Check the gateway log for `audiosocket on …`. |
| Agent speaks, but transfers do nothing | AMI is not connected, or the account lacks `write = call`. The gateway logs `manager interface connected` at startup when it is. |
| `no channel announced` in the log | The `UserEvent` line is missing from the dialplan, or the account lacks `read = user`. |
| Choppy or fast audio | Audio is paced at 20 ms per frame. If you changed `FramePace`, put it back. |
| `Reached timeout after 2000 ms of no activity` | The media stream stopped. See §5a — AudioSocket must be fed continuously. |
| Audio works, keypresses do nothing | Your Asterisk predates DTMF over AudioSocket. See the table in §1. |
| `AudioSocket` fails with an empty or invalid id | The dialplan used `${UUID()}` on Asterisk 20.x, where it does not exist. Use the MD5 form in §5. |
| Replay reports divergences | The brain's decisions changed since the call was recorded. That is the eval loop working, not a bug. |
