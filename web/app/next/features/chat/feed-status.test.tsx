// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Feed, { type IFeedProps } from '@/next/features/chat/feed';
import { CATCHING_UP_DELAY_MS, CATCHING_UP_EXIT_MS } from '@/next/features/chat/feed-status';
import type { FeedItem, ModelSwitchItem } from '@/next/features/chat/types';
import { SKELETON_DELAY_MS } from '@/next/ui/skeleton';
import { t } from '@/next/i18n';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const row = (n: number): ModelSwitchItem => ({
  kind: 'switch',
  key: `switch:${n}`,
  at: n,
  from: { id: 'opus', effort: null },
  to: { id: 'sonnet', effort: null },
});

const done = () => Promise.resolve();

const props = (over: Partial<IFeedProps> = {}): IFeedProps => ({
  items: [row(1), row(2)] as FeedItem[],
  hasOlder: false,
  loadingOlder: false,
  onLoadOlder: () => {},
  density: 'compact',
  offline: false,
  enterAfter: Number.MAX_SAFE_INTEGER,
  machineName: 'm',
  onContinueTurn: done,
  onRetryTurn: done,
  onRetryMessage: done,
  onDeleteMessage: done,
  ...over,
});

const wait = (ms: number) =>
  act(() => {
    vi.advanceTimersByTime(ms);
  });

describe('what the feed says about how much of the conversation it holds', () => {
  it('waits for the conversation with a ring and a line in the middle, no skeleton, after a moment', () => {
    const view = render(<Feed {...props({ items: null })} />);
    expect(view.container.querySelector('.hx-feed__loading')).toBeNull();
    wait(SKELETON_DELAY_MS);
    const loading = view.container.querySelector('.hx-feed__loading');
    expect(loading?.getAttribute('role')).toBe('status');
    expect(loading?.textContent).toBe(t('next.feed.loading'));
    expect(loading?.querySelector('.hx-spin')).not.toBeNull();
    expect(view.container.querySelector('.hx-sk')).toBeNull();
    expect(view.container.querySelector('.hx-feed__start')).toBeNull();
    expect(view.container.querySelector('.hx-feed__progress')).toBeNull();
  });

  it('shows a small ring at the top while older turns come, and no start of the conversation', () => {
    const view = render(<Feed {...props({ hasOlder: true, loadingOlder: true })} />);
    expect(view.container.querySelector('.hx-feed__older .hx-feed__olderspin .hx-spin')).not.toBeNull();
    expect(view.container.querySelector('.hx-sk')).toBeNull();
    expect(view.container.querySelector('.hx-feed__start')).toBeNull();
  });

  it('marks the start of the conversation quietly once nothing is older', () => {
    const view = render(<Feed {...props()} />);
    expect(view.container.querySelector('.hx-feed__start')?.textContent).toBe(t('next.feed.start'));
    expect(view.container.querySelector('.hx-feed__older')).toBeNull();
  });

  it('marks no start in an empty conversation', () => {
    const view = render(<Feed {...props({ items: [] })} />);
    expect(view.container.querySelector('.hx-feed__start')).toBeNull();
  });

  it('asks for older turns without a scroll when the newest ones do not fill the feed', () => {
    const onLoadOlder = vi.fn();
    render(<Feed {...props({ hasOlder: true, onLoadOlder })} />);
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it('does not ask again while a page is on its way or after it failed', () => {
    const onLoadOlder = vi.fn();
    const view = render(<Feed {...props({ hasOlder: true, loadingOlder: true, onLoadOlder })} />);
    view.rerender(<Feed {...props({ hasOlder: true, olderFailed: 'нет связи', onLoadOlder })} />);
    expect(onLoadOlder).not.toHaveBeenCalled();
  });

  it('runs a thin bar over a feed that is still catching up, and lets it fade when it has', () => {
    const view = render(<Feed {...props({ catchingUp: true })} />);
    expect(view.container.querySelector('.hx-feed__progress')).toBeNull();
    wait(CATCHING_UP_DELAY_MS);
    const bar = view.container.querySelector('.hx-feed__progress');
    expect(bar?.getAttribute('role')).toBe('progressbar');
    expect(bar?.getAttribute('data-state')).toBe('open');

    view.rerender(<Feed {...props({ catchingUp: false })} />);
    expect(view.container.querySelector('.hx-feed__progress')?.getAttribute('data-state')).toBe('closing');
    wait(CATCHING_UP_EXIT_MS);
    expect(view.container.querySelector('.hx-feed__progress')).toBeNull();
  });

  it('does not flash the bar when the catch-up ends at once', () => {
    const view = render(<Feed {...props({ catchingUp: true })} />);
    wait(CATCHING_UP_DELAY_MS / 2);
    view.rerender(<Feed {...props({ catchingUp: false })} />);
    wait(CATCHING_UP_DELAY_MS);
    expect(view.container.querySelector('.hx-feed__progress')).toBeNull();
  });

  it('runs no bar while the conversation itself is still on its way', () => {
    const view = render(<Feed {...props({ items: null, catchingUp: true })} />);
    wait(CATCHING_UP_DELAY_MS);
    expect(view.container.querySelector('.hx-feed__progress')).toBeNull();
  });
});
