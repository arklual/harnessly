import { describe, expect, it } from 'vitest';
import { pagePoint, parseBrowserFrame } from './agent-browser-frame';
import { runtimeSocket, setApiOrigin } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

describe('browser frame contract', () => {
  it('reads CSS dimensions and generations before JPEG bytes', () => {
    const data = new ArrayBuffer(15),
      view = new DataView(data);
    view.setUint32(0, 390);
    view.setUint32(4, 844);
    view.setUint32(8, 47);
    const frame = parseBrowserFrame(data);
    expect(frame).toMatchObject({ w: 390, h: 844, gen: 47 });
    expect(frame?.jpeg.size).toBe(3);
    expect(parseBrowserFrame(new ArrayBuffer(12))).toBeNull();
    expect(parseBrowserFrame(new ArrayBuffer(15))).toBeNull();
  });
  it('maps a scaled image and refuses the letterbox', () => {
    const rect = { left: 100, top: 20, width: 195, height: 422 },
      page = { w: 390, h: 844 };
    expect(pagePoint(rect, page, { x: 150, y: 120 })).toEqual({ x: 100, y: 200 });
    expect(pagePoint(rect, page, { x: 90, y: 120 })).toBeNull();
  });
  it('keeps conversation and machine routing through the hub path', () => {
    setApiOrigin({ base: '/m/machine-a', host: 'hub.example', protocol: 'https:' });
    expect(runtimeSocket('/api/agent-browser?conv=chat-a', 'machine-a' as MachineId)).toBe(
      'wss://hub.example/m/machine-a/api/agent-browser?conv=chat-a&machine=machine-a',
    );
    setApiOrigin({ base: '', host: 'localhost', protocol: 'http:' });
  });
});
