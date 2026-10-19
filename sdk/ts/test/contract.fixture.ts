/**
 * The golden fixture, written as typed literals.
 *
 * TypeScript types vanish at runtime, so reading the JSONL file proves nothing
 * about them: a field missing from an interface would go unnoticed. These
 * literals are checked by tsc instead — object literals assigned to a union get
 * excess-property checking, so a field the interfaces do not declare fails the
 * type-check, and a missing required one fails too.
 *
 * contract.test.ts asserts this list is identical to
 * testdata/contract/messages.jsonl, so the two cannot drift apart.
 */
import type { WireMessage } from "../src/types.ts";

export const samples: WireMessage[] = [
  { type: "start", call_id: "contract-0001", hint: "+254722123456" },
  // The same, as the reference SDK sends it: clients may stamp their own
  // seq and at, and the server does not rely on them.
  {
    type: "start",
    seq: 1,
    at: "2026-10-19T08:14:59.900Z",
    call_id: "contract-0001",
    hint: "+254722123456",
  },
  { type: "call_started", call_id: "contract-0001", seq: 1, at: "2026-10-19T08:15:00.100Z" },
  {
    type: "reply",
    seq: 2,
    at: "2026-10-19T08:15:00.200Z",
    text: "Hello. Press 1 for sales, or 2 to check an account.",
  },
  {
    type: "transcript",
    seq: 3,
    at: "2026-10-19T08:15:03.000Z",
    text: "I want to check my acc",
    final: false,
  },
  {
    type: "transcript",
    seq: 4,
    at: "2026-10-19T08:15:03.400Z",
    text: "I want to check my account",
    final: true,
  },
  // Sent by the client: no seq or at, because the client assigns neither.
  { type: "dtmf", digit: "2" },
  // The same keypress echoed back as an event.
  { type: "dtmf", seq: 5, at: "2026-10-19T08:15:04.500Z", digit: "2" },
  { type: "dtmf", digit: "7", sensitive: true },
  {
    type: "action",
    seq: 6,
    at: "2026-10-19T08:15:04.000Z",
    kind: "read_digits",
    args: { label: "account", terminator: "#" },
  },
  {
    type: "digits",
    seq: 7,
    at: "2026-10-19T08:15:09.000Z",
    label: "account",
    count: 5,
    digits: "48217",
  },
  // A sensitive group: a count and a token, never the digits.
  {
    type: "digits",
    seq: 8,
    at: "2026-10-19T08:15:14.000Z",
    label: "card",
    count: 16,
    redacted: true,
    hash: "9f2c1b7e4a0d3c55",
  },
  {
    type: "action",
    seq: 9,
    at: "2026-10-19T08:15:15.000Z",
    kind: "transfer",
    args: { dest: "200" },
  },
  {
    type: "error",
    seq: 10,
    at: "2026-10-19T08:15:15.500Z",
    code: "action_failed",
    message: "play_prompt needs ARI",
  },
  { type: "stop", reason: "caller hung up" },
  { type: "stop", seq: 3, at: "2026-10-19T08:15:15.900Z", reason: "caller hung up" },
  { type: "call_ended", seq: 11, at: "2026-10-19T08:15:16.000Z", reason: "hangup" },
];
