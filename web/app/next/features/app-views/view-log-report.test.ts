import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  bridgeLine,
  consoleLine,
  flushViewLog,
  queueViewLog,
  setViewLogSender,
  VIEW_LOG_BATCH,
  VIEW_LOG_QUEUE,
} from '@/next/features/app-views/view-log-report';
import type { MachineId } from '@/next/protocol/v2';

const m = 'local' as MachineId;

describe('view log report (HRN-913)', () => {
  afterEach(() => {
    flushViewLog();
    setViewLogSender(null);
    vi.useRealTimers();
  });

  it('gathers the lines of a view and sends them in batches, bounded', () => {
    vi.useFakeTimers();
    const sent: { app: string; view: string; n: number }[] = [];
    setViewLogSender((_m, app, view, lines) => {
      sent.push({ app, view, n: lines.length });
      return Promise.resolve({});
    });
    for (let i = 0; i < VIEW_LOG_QUEUE + 30; i++)
      queueViewLog(m, 'example.a', 'main', { level: 'error', text: `e${i}`, at: i });
    queueViewLog(m, 'example.b', 'card', { level: 'warn', text: 'x'.repeat(5000), at: 1 });
    expect(sent).toEqual([]);
    vi.runAllTimers();
    const a = sent.filter((s) => s.app === 'example.a');
    expect(a.reduce((n, s) => n + s.n, 0)).toBe(VIEW_LOG_QUEUE);
    expect(Math.max(...a.map((s) => s.n))).toBe(VIEW_LOG_BATCH);
    expect(sent.some((s) => s.app === 'example.b' && s.view === 'card')).toBe(true);
  });

  it('keeps errors, warnings and failed calls only', () => {
    expect(consoleLine('error', 'boom', 1)).toEqual({ level: 'error', text: 'boom', at: 1 });
    expect(consoleLine('log', 'x', 1)).toBeNull();
    expect(bridgeLine({ method: 'storage.get', permission: 'storage', ok: true, at: 1 })).toBeNull();
    expect(
      bridgeLine({ method: 'console.error', permission: '', ok: false, message: 'x', at: 1 }),
    ).toBeNull();
    expect(
      bridgeLine({
        method: 'files.read',
        permission: 'files.read',
        ok: false,
        code: 'permission_denied',
        message: 'no',
        at: 2,
      }),
    ).toEqual({ level: 'bridge', text: 'files.read — permission_denied: no', at: 2 });
  });
});
