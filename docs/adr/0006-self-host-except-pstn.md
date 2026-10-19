# ADR-0006: Self-host everything except PSTN interconnect

- Status: proposed
- Date: 2026-10-18
- Deciders: project owner

## Context

We own a server and want to avoid recurring third-party fees. A voice-AI deployment normally rents
four things:

| Rented thing | Can we run it ourselves? |
|---|---|
| The switch / platform (3CX, Twilio, Vapi, Retell) | Yes — Asterisk is free, and this project is the platform. |
| STT, TTS, and the LLM | Yes — they are software, and this is where per-minute billing concentrates. |
| The operator console and call storage | Yes — already ours by design (ADR-0003). |
| Terminating a call on Safaricom / Airtel / Telkom | **No.** |

The fourth is not a software problem. Delivering a call to a Kenyan mobile subscriber is a licensed
interconnect between carriers, overseen by the Communications Authority. Owning hardware does not
grant access to it, and the usual shortcut — a GSM gateway loaded with SIMs — is the practice the CA
bans and seizes equipment for. So the question is not *whether* to pay a carrier, but how small and
how swappable that payment is.

Separately, not every call needs the PSTN at all. A caller already inside our app, our website, or
our network can reach the server over SIP or WebRTC, where the marginal cost of a call is zero.

## Decision

Self-host the switch, the gateway, the console, and — from Phase 4 — STT, TTS, and the model. Buy
only PSTN termination, pay-as-you-go, from a licensed Kenyan SIP trunk, and keep it behind a single
adapter so the provider is a config change rather than a port.

Prefer IP call paths wherever the caller's journey allows one:

1. **Softphone or WebRTC → our Asterisk** — no carrier, no per-minute cost. This is the default for
   development, internal lines, and any caller who starts in our own app or web page.
2. **SIP trunk → our Asterisk** — only for calls that must originate or land on a real phone number.

## Consequences

- Positive: the dominant recurring cost of a voice agent — per-minute STT + LLM + TTS — becomes fixed
  cost on hardware we already pay for.
- Positive: audio, transcripts, and the digits the Phase 1 redaction rule protects never leave our
  server. That is the easier position under the Data Protection Act, 2019.
- Positive: the trunk sits behind one adapter, so a provider's price rise or outage is survivable.
- Negative / trade-offs: we own the operations — SIP and NAT, codecs, uptime, backups, model updates.
  A hosted platform absorbs all of that, and we are choosing not to buy that relief.
- Negative / trade-offs: self-hosted STT and TTS for Swahili, Sheng, and code-switched speech is
  materially weaker than the hosted options. Quality may force a provider for that leg specifically;
  the provider interface exists so that stays a per-leg choice, not a rewrite.
- Negative / trade-offs: a trunk needs a reachable, static address, and Safaricom is reported to
  require the PBX to sit in Kenya. This constrains where the server may live.
- Follow-ups: benchmark self-hosted STT on real Kenyan call audio before committing in Phase 4;
  choose a trunk provider before Phase 1 ends; revisit the local-only rule in `AGENTS.md` once the
  project has its own host.

## Alternatives considered

| Option | Why not |
|---|---|
| A hosted voice-AI platform (Twilio, Vapi, Retell) | Bills per minute for precisely the control plane and eval loop this project exists to own, and call audio leaves our infrastructure. |
| A GSM gateway with SIM cards instead of a trunk | Unlicensed voice provision; the CA bans and enforces against it, and it breaches the SIM terms. Not viable for anything meant to keep running. |
| Self-host the interconnect too | Not open to an unlicensed party. PSTN interconnect needs a CA licence and carrier agreements, not a server. |
| Pay for a hosted model but self-host the rest | Still an option per leg, and the provider interface keeps it open. Rejected as the *default* because per-minute model fees are the single largest line item. |
