import { describe, expect, it } from 'vitest';
import { parsePushNavigation, pushNavKey, readWorkerMessage } from '@/lib/push-bridge';

describe('push bridge', () => {
  it('derives the cache key from the base path like the worker does', () => {
    expect(pushNavKey('/agents/')).toBe('/agents/_push-pending');
    expect(pushNavKey('/')).toBe('/_push-pending');
    expect(pushNavKey('')).toBe('/_push-pending');
  });

  it('accepts a click only with a tab to open', () => {
    expect(parsePushNavigation({ tabId: 't1', workspaceId: 'w1' })).toEqual({
      tabId: 't1',
      workspaceId: 'w1',
    });
    expect(parsePushNavigation({ tabId: 't1' })).toEqual({ tabId: 't1', workspaceId: null });
    expect(parsePushNavigation({ tabId: '' })).toBeNull();
    expect(parsePushNavigation({ tabId: 5 })).toBeNull();
    expect(parsePushNavigation(null)).toBeNull();
    expect(parsePushNavigation('x')).toBeNull();
  });

  it('sorts worker messages and ignores foreign ones', () => {
    const message = { kind: 'done', title: 'Done', tabId: 't1' };
    expect(readWorkerMessage({ type: 'hl:push-message', message })).toEqual({ kind: 'push', message });
    expect(readWorkerMessage({ type: 'hl:open-conversation', tabId: 't1', workspaceId: 'w1' })).toEqual({
      kind: 'open',
      navigation: { tabId: 't1', workspaceId: 'w1' },
    });
    expect(readWorkerMessage({ type: 'hl:push-message', message: { title: 'no kind' } })).toBeNull();
    expect(readWorkerMessage({ type: 'hl:open-conversation' })).toBeNull();
    expect(readWorkerMessage({ type: 'something-else' })).toBeNull();
    expect(readWorkerMessage(undefined)).toBeNull();
  });
});
