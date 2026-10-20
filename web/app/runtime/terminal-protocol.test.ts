import { describe, expect, it } from 'vitest';

import {
  decodeFrame,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
  terminalClosedForGood,
} from '@/runtime/terminal-protocol';

describe('terminal frames', () => {
  it('prefixes keystrokes with 0x00 and sends UTF-8', () => {
    expect([...encodeInput('aé\r')]).toEqual([0, 0x61, 0xc3, 0xa9, 0x0d]);
  });

  it('writes the size big-endian', () => {
    expect([...encodeResize(300, 40)]).toEqual([2, 1, 44, 0, 40]);
  });

  it('clamps the size to what the runtime accepts', () => {
    expect([...encodeResize(0, 20000)]).toEqual([2, 0, 1, 0x27, 0x10]);
  });

  it('sends a bare heartbeat byte', () => {
    expect([...encodeHeartbeat()]).toEqual([3]);
  });

  it('reads output and heartbeat frames', () => {
    const out = decodeFrame(new Uint8Array([1, 0x68, 0x69]).buffer);
    expect(out.type).toBe('output');
    expect(out.type === 'output' && new TextDecoder().decode(out.data)).toBe('hi');
    expect(decodeFrame(new Uint8Array([3]).buffer).type).toBe('heartbeat');
    expect(decodeFrame(new ArrayBuffer(0)).type).toBe('unknown');
  });

  it('knows which closes end the session', () => {
    expect(terminalClosedForGood(1000, 'Session exited')).toBe(true);
    expect(terminalClosedForGood(1011, 'Session not found')).toBe(true);
    expect(terminalClosedForGood(1001, 'Heartbeat timeout')).toBe(false);
    expect(terminalClosedForGood(1006, '')).toBe(false);
  });
});
