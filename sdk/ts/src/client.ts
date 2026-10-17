import type { WireMessage } from "./types.js";

export type EventCallback = (msg: WireMessage) => void;

export type SocketLike = {
  binaryType?: string;
  readyState: number;
  send(data: string | ArrayBuffer | Uint8Array): void;
  close(): void;
  onopen?: ((ev: any) => any) | null;
  onmessage?: ((ev: any) => any) | null;
  onclose?: ((ev: any) => any) | null;
  onerror?: ((ev: any) => any) | null;
};

export type SocketFactory = (url: string) => SocketLike;

const isOpen = (s: SocketLike) => s.readyState === 1; // WebSocket.OPEN is 1

export class CallClient {
  url: string;
  token?: string;
  socketFactory: SocketFactory;

  private ws: SocketLike | null = null;
  private handlers = new Map<string, Set<EventCallback>>();
  private seq = 0;

  constructor(opts: { url: string; token?: string; socketFactory?: SocketFactory }) {
    this.url = opts.url;
    this.token = opts.token;
    const f = opts.socketFactory;
    if (f) {
      this.socketFactory = f;
    } else {
      // Use global WebSocket if available (Node 24+); in browsers it's global.
      const g = (globalThis as any).WebSocket;
      if (!g) {
        throw new Error("WebSocket not available; provide socketFactory");
      }
      this.socketFactory = (u: string) => new g(u);
    }
  }

  connect(): Promise<void> {
    const url = this.token ? `${this.url}${this.url.includes("?") ? "&" : "?"}token=${encodeURIComponent(this.token)}` : this.url;
    this.ws = this.socketFactory(url);
    this.ws.binaryType = "arraybuffer";
    this.ws.onmessage = (ev) => {
      const data = ev.data;
      if (typeof data === "string") {
        try {
          const msg = JSON.parse(data) as WireMessage;
          this.dispatch(msg);
        } catch (e) {
          // Ignore malformed JSON
        }
        return;
      }
      // Binary audio from server: ignore in the event stream (client handles as needed)
    };
    return new Promise((resolve, reject) => {
      if (!this.ws) return reject(new Error("no socket"));
      const ws = this.ws;
      const onOpen = () => {
        ws.onopen = null;
        ws.onerror = null;
        resolve();
      };
      const onErr = (e: any) => {
        ws.onopen = null;
        ws.onerror = null;
        reject(e);
      };
      ws.onopen = onOpen;
      ws.onerror = onErr;
      if (ws.readyState === 1) {
        onOpen();
      }
    });
  }

  on(type: string, cb: EventCallback) {
    let set = this.handlers.get(type);
    if (!set) {
      set = new Set();
      this.handlers.set(type, set);
    }
    set.add(cb);
    return () => set!.delete(cb);
  }

  start(callId: string, hint?: string) {
    this.send({ type: "start", call_id: callId, hint });
  }

  sendDtmf(digit: string, sensitive = false) {
    this.send({ type: "dtmf", digit, sensitive });
  }

  stop(reason?: string) {
    this.send({ type: "stop", reason });
  }

  sendAudio(pcm: Uint8Array | ArrayBuffer) {
    const ws = this.ws;
    if (!ws || !isOpen(ws)) return;
    ws.send(pcm);
  }

  send(msg: any) {
    const ws = this.ws;
    if (!ws || !isOpen(ws)) return;
    ws.send(JSON.stringify({ seq: ++this.seq, at: new Date().toISOString(), ...msg }));
  }

  close() {
    if (this.ws) {
      try {
        this.ws.close();
      } catch {}
      this.ws = null;
    }
  }

  private dispatch(msg: WireMessage) {
    const set = this.handlers.get(msg.type);
    if (set) {
      for (const cb of set) {
        try {
          cb(msg);
        } catch {}
      }
    }
    // Also dispatch to wildcard "message" if added? No wildcard in v1.
  }
}
