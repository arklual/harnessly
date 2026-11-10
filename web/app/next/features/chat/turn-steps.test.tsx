// @vitest-environment jsdom
// HRN-905: a folded turn unfolds to what the agent did and wrote, a stopped one stays in view.
import { cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import TurnView, { openByDefault, type ITurnHandlers } from '@/next/features/chat/turn';
import type { Entry, TurnItem } from '@/next/features/chat/types';

afterEach(cleanup);

const say = (id: string, md: string) => ({ id, t: 'say', md }) as unknown as Entry;

const handlers = (onLoadSteps: ITurnHandlers['onLoadSteps']): ITurnHandlers => ({
  onToggle: vi.fn(),
  onLoadSteps,
  onContinueTurn: vi.fn(),
  onRetryTurn: vi.fn(),
  onRetryMessage: vi.fn(),
  onDeleteMessage: vi.fn(),
});

const turnItem = (over: Partial<TurnItem>, stepCount: number, status = 'done'): TurnItem =>
  ({
    kind: 'turn',
    key: 'c:1#0',
    turn: {
      id: 'c:1#0',
      author: { provider: 'claude', role: null },
      startedAt: 1,
      endedAt: 2,
      durationMs: 1000,
      toolCount: 0,
      stepCount,
      status,
      failure: null,
      answerEntryId: null,
      files: [],
      outputs: [],
    },
    prompt: null,
    woke: null,
    peer: null,
    note: null,
    steps: [],
    answer: null,
    now: null,
    wait: null,
    endNote: null,
    ...over,
  }) as unknown as TurnItem;

describe('a folded turn', () => {
  it('that holds part of its steps loads the rest when it unfolds', async () => {
    const load = vi.fn(() => Promise.resolve(true));
    render(
      <TurnView
        item={turnItem({ steps: [say('c:3#0', 'the newest')], partial: true }, 3)}
        open
        machineName="vm"
        handlers={handlers(load)}
      />,
    );
    await waitFor(() => expect(load).toHaveBeenCalledWith('c:1#0'));
  });

  it('that holds them all asks for nothing', () => {
    const load = vi.fn(() => Promise.resolve(true));
    render(
      <TurnView
        item={turnItem({ steps: [say('c:3#0', 'all')] }, 1)}
        open
        machineName="vm"
        handlers={handlers(load)}
      />,
    );
    expect(load).not.toHaveBeenCalled();
  });

  it('whose only step is its answer has no work to unfold', () => {
    const { container } = render(
      <TurnView
        item={turnItem({ steps: null, answer: say('c:2#0', 'the answer') as TurnItem['answer'] }, 1)}
        open={false}
        machineName="vm"
        handlers={handlers(vi.fn(() => Promise.resolve(true)))}
      />,
    );
    expect(container.querySelector('.hx-work__head')).toBeNull();
    expect(container.textContent).toContain('the answer');
  });
});

describe('a stopped turn', () => {
  it('is unfolded in the compact feed, so its thoughts and words stay in view', () => {
    expect(openByDefault(turnItem({}, 2, 'interrupted'), 'compact')).toBe(true);
    expect(openByDefault(turnItem({}, 2, 'done'), 'compact')).toBe(false);
    expect(openByDefault(turnItem({}, 2, 'done'), 'full')).toBe(true);
  });
});
