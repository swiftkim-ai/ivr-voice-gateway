import { test } from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";

import { ContractSchema, type Contract } from "./schema.ts";
import { samples } from "./contract.fixture.ts";

// Both languages read these two files. Go's half lives in
// internal/wire/contract_test.go.
const schemaUrl = new URL("../../../schema/ivrvg.v1.schema.json", import.meta.url);
const fixtureUrl = new URL("../../../testdata/contract/messages.jsonl", import.meta.url);

const schema = new ContractSchema(JSON.parse(readFileSync(schemaUrl, "utf8")) as Contract);

const fixture: unknown[] = readFileSync(fixtureUrl, "utf8")
  .split("\n")
  .filter((line) => line.trim() !== "")
  .map((line) => JSON.parse(line) as unknown);

test("every fixture message matches the schema", () => {
  fixture.forEach((msg, i) => {
    const fault = schema.validate(msg);
    assert.equal(fault, null, `line ${i + 1}: ${fault}\n  ${JSON.stringify(msg)}`);
  });
});

test("the typed samples are exactly the shared fixture", () => {
  // If these drift, the compile-time check below stops describing the file the
  // Go side validates, and the two languages quietly stop agreeing.
  assert.deepEqual(
    JSON.parse(JSON.stringify(samples)),
    fixture,
    "sdk/ts/test/contract.fixture.ts and testdata/contract/messages.jsonl have diverged",
  );
});

test("the SDK types cover every message type in the contract", () => {
  // samples is typed WireMessage[], so tsc has already proven the union can
  // express each one. This checks none is missing.
  const covered = new Set(
    samples.map((m) => (m as { type: string }).type),
  );
  for (const type of schema.types) {
    assert.ok(covered.has(type), `the contract defines "${type}" but the SDK types have no sample`);
  }
});

test("the SDK types carry every field the contract declares", () => {
  const byType = new Map<string, Set<string>>();
  for (const sample of samples) {
    const type = (sample as { type: string }).type;
    const seen = byType.get(type) ?? new Set<string>();
    for (const key of Object.keys(sample)) seen.add(key);
    byType.set(type, seen);
  }

  for (const type of schema.types) {
    const seen = byType.get(type) ?? new Set<string>();
    for (const prop of schema.propertiesOf(type)) {
      assert.ok(
        seen.has(prop),
        `${type}.${prop} is in the contract but no typed sample carries it, ` +
          `so the SDK interface is not proven to declare it`,
      );
    }
  }
});

test("the validator rejects malformed messages", () => {
  // A validator that accepts anything is worse than none. These mirror the
  // cases in internal/wire/contract_test.go.
  const cases: [string, unknown, string][] = [
    ["no type", { seq: 1 }, "no type"],
    ["unknown type", { type: "whisper", seq: 1 }, "not a message type"],
    [
      "missing required field",
      { type: "call_ended", seq: 1, at: "2026-10-19T08:15:16Z" },
      'required field "reason"',
    ],
    [
      "undefined field",
      { type: "reply", seq: 1, at: "2026-10-19T08:15:16Z", text: "hi", mood: "warm" },
      "does not define",
    ],
    [
      "wrong field type",
      { type: "reply", seq: 1, at: "2026-10-19T08:15:16Z", text: 42 },
      "must be a string",
    ],
    [
      "unknown action kind",
      { type: "action", seq: 1, at: "2026-10-19T08:15:16Z", kind: "launch_rocket" },
      "is not one of",
    ],
    ["digit outside the keypad", { type: "dtmf", digit: "Z" }, "does not match"],
    [
      "negative seq",
      { type: "reply", seq: -1, at: "2026-10-19T08:15:16Z", text: "hi" },
      "at least",
    ],
    [
      "timestamp not RFC 3339",
      { type: "reply", seq: 1, at: "last Tuesday", text: "hi" },
      "RFC 3339",
    ],
    ["not an object", "hello", "not an object"],
  ];

  for (const [name, msg, want] of cases) {
    const fault = schema.validate(msg);
    assert.ok(fault !== null, `${name}: accepted a malformed message`);
    assert.ok(
      fault.includes(want),
      `${name}: fault was "${fault}", expected it to mention "${want}"`,
    );
  }
});
