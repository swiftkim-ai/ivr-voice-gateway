export type ActionKind =
  | "send_dtmf"
  | "route"
  | "transfer"
  | "play_prompt"
  | "park"
  | "hangup"
  | "read_digits";

export type WireMessage =
  | CallStarted
  | Transcript
  | Reply
  | Dtmf
  | Action
  | Stop
  | Ended
  | Error;

export interface BaseMessage {
  type: string;
  seq: number;
  at: string;
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

export interface Dtmf extends BaseMessage {
  type: "dtmf";
  digit: string;
  sensitive?: boolean;
}

export interface Action extends BaseMessage {
  type: "action";
  kind: ActionKind;
  args?: Record<string, unknown>;
}

export interface Stop {
  type: "stop";
  reason?: string;
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
