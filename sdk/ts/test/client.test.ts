import { test } from "node:test";
import assert from "node:assert";

import { CallClient } from "../src/client.ts";

class FakeSocket {
  binaryType: string = "arraybuffer";
  readyState = 1; // open
  sent: any[] = [];
  onopen: any = null;
  onmessage: any = null;
  onclose: any = null;
  onerror: any = null;

  send(data: any) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
}

test("CallClient.send includes seq/at and start", async () => {
  const sock = new FakeSocket();
  const client = new CallClient({
    url: "ws://example.com/v1/media",
    socketFactory: () => sock as any,
  });
  await client.connect();
  client.start("c1", "hello");
  assert.equal(sock.sent.length, 1);
  const first = JSON.parse(sock.sent[0]);
  assert.equal(first.type, "start");
  assert.equal(first.call_id, "c1");
  assert.ok(typeof first.seq === "number");
  assert.ok(typeof first.at === "string");
});

test("dispatches message to registered handler", async () => {
  const sock = new FakeSocket();
  const client = new CallClient({
    url: "ws://example.com/v1/media",
    socketFactory: () => sock as any,
  });
  await client.connect();
  let got: any = null;
  client.on("call_started", (m) => {
    got = m;
  });
  // Simulate server message
  sock.onmessage?.({ data: JSON.stringify({ type: "call_started", call_id: "c9", seq: 1, at: "2026-10-17T00:00:00Z" }) });
  assert.equal(got?.type, "call_started");
  assert.equal(got?.call_id, "c9");
});

test("sendDtmf sends correct type", async () => {
  const sock = new FakeSocket();
  const client = new CallClient({
    url: "ws://example.com/v1/media",
    socketFactory: () => sock as any,
  });
  await client.connect();
  client.sendDtmf("1", true);
  const msg = JSON.parse(sock.sent[0]);
  assert.equal(msg.type, "dtmf");
  assert.equal(msg.digit, "1");
  assert.equal(msg.sensitive, true);
});
