import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setPendingFirstCommand, takePendingFirstCommand } from '@/lib/hl-pending-command';

beforeEach(() => {
  const data = new Map<string, string>();
  vi.stubGlobal('sessionStorage', {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
    removeItem: (key: string) => void data.delete(key),
  });
});
afterEach(() => vi.unstubAllGlobals());

describe('the first slash command of a new conversation', () => {
  it('goes to the composer of that conversation once', () => {
    expect(setPendingFirstCommand('m1', 'tab-1', '  /review  ')).toBe(true);
    expect(takePendingFirstCommand('m2', 'tab-1')).toBeNull();
    expect(takePendingFirstCommand('m1', 'tab-2')).toBeNull();
    expect(takePendingFirstCommand('m1', 'tab-1')).toBe('/review');
    expect(takePendingFirstCommand('m1', 'tab-1')).toBeNull();
  });

  it('is not sent when it is older than a start can take', () => {
    setPendingFirstCommand(undefined, 'tab-1', '/review');
    expect(takePendingFirstCommand(undefined, 'tab-1', Date.now() + 11 * 60_000)).toBeNull();
    expect(takePendingFirstCommand(undefined, 'tab-1')).toBeNull();
  });

  it('refuses an empty line', () => {
    expect(setPendingFirstCommand(undefined, 'tab-1', '   ')).toBe(false);
  });
});
