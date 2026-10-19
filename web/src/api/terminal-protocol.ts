// The terminal socket's binary frames: the first byte names the message, the rest is its payload.

export const TermMsg = {
  input: 0x00,
  output: 0x01,
  resize: 0x02,
  heartbeat: 0x03,
  kill: 0x04,
  composerText: 0x05,
  composerMark: 0x06,
} as const;

const encoder = new TextEncoder();

function frame(type: number, payload: Uint8Array = new Uint8Array()): Uint8Array {
  const out = new Uint8Array(payload.length + 1);
  out[0] = type;
  out.set(payload, 1);
  return out;
}

export const encodeInput = (data: string) => frame(TermMsg.input, encoder.encode(data));

export function encodeResize(cols: number, rows: number): Uint8Array {
  const c = Math.max(1, Math.min(10000, Math.floor(cols)));
  const r = Math.max(1, Math.min(10000, Math.floor(rows)));
  return frame(TermMsg.resize, new Uint8Array([c >> 8, c & 255, r >> 8, r & 255]));
}

export const encodeHeartbeat = () => frame(TermMsg.heartbeat);

export type Decoded = { type: 'output'; data: Uint8Array } | { type: 'heartbeat' } | { type: 'unknown' };

export function decodeFrame(buf: ArrayBuffer): Decoded {
  const bytes = new Uint8Array(buf);
  if (bytes.length === 0) return { type: 'unknown' };
  if (bytes[0] === TermMsg.output) return { type: 'output', data: bytes.subarray(1) };
  if (bytes[0] === TermMsg.heartbeat) return { type: 'heartbeat' };
  return { type: 'unknown' };
}

/** Close codes after which reopening the same session is pointless. */
export function terminalClosedForGood(code: number, reason: string): boolean {
  return code === 1000 || (code === 1011 && reason === 'Session not found') || code === 1008;
}
