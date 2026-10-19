export type ActionKind =
  | "send_dtmf"
  | "route"
  | "transfer"
  | "play_prompt"
  | "park"
  | "hangup"
  | "read_digits";

export type WireMessage =
  | Start
  | CallStarted
  | Transcript
  | Reply
  | Dtmf
  | Digits
  | Action
  | Stop
  | Ended
  | Error;

export interface BaseMessage {
  type: string;
  seq: number;
  at: string;
}

/** client -> server. The first message after the socket opens. */
export interface Start {
  type: "start";
  call_id: string;
  /** Context from the IVR: caller id, menu choice, account. */
  hint?: string;
  /** A client may number its own messages; the server does not rely on it. */
  seq?: number;
  at?: string;
}

export interface CallStarted extends BaseMessage {
  type: "call_started";
  call_id: string;
}

export interface Transcript extends BaseMessage {
  type: "transcript";
  text: string;
  final: boolean;
}

export interface Reply extends BaseMessage {
  type: "reply";
  text: string;
}

/**
 * A keypad digit. Travels both ways: the client sends one without seq/at, and
 * the gateway echoes it back as an event carrying both.
 */
export interface Dtmf {
  type: "dtmf";
  digit: string;
  sensitive?: boolean;
  seq?: number;
  at?: string;
}

export interface Digits extends BaseMessage {
  type: "digits";
  /** How many digits the caller entered. */
  count: number;
  /** What was collected, echoed from the read_digits action. */
  label?: string;
  /** The digits entered. Absent when redacted is true. */
  digits?: string;
  /** True when the group was collected as sensitive. */
  redacted?: boolean;
  /** Keyed correlation token standing in for a redacted group. */
  hash?: string;
}

export interface Action extends BaseMessage {
  type: "action";
  kind: ActionKind;
  args?: Record<string, unknown>;
}

export interface Stop {
  type: "stop";
  reason?: string;
  /** A client may number its own messages; the server does not rely on it. */
  seq?: number;
  at?: string;
}

export interface Ended extends BaseMessage {
  type: "call_ended";
  reason: string;
}

export interface Error extends BaseMessage {
  type: "error";
  code: string;
  message: string;
}
