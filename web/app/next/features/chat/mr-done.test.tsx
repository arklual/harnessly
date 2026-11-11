// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import MrDone, { chatMrs, mrPlace } from '@/next/features/chat/mr-done';
import type { FeedItem, TurnOutput } from '@/next/features/chat/types';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const MR = 'https://gitlab.example.com/acme/runtime/-/merge_requests/12';
const PR = 'https://github.com/acme/web/pull/7';

const out = (ref: string, title: string, kind: TurnOutput['kind'] = 'mr'): TurnOutput => ({
  kind,
  op: 'created',
  ref,
  title,
});

const turn = (outputs: TurnOutput[]) => ({ kind: 'turn', turn: { outputs } }) as unknown as FeedItem;

describe('the merge requests under a finished task', () => {
  it('lists each merge request of the chat once, with its latest name', () => {
    const items = [
      turn([out(MR, 'Old name'), out('/notes.md', 'Notes', 'doc')]),
      { kind: 'outgoing' } as unknown as FeedItem,
      turn([out(PR, 'Add the form'), out(MR, 'Keep a clock')]),
    ];
    expect(chatMrs(items).map((m) => [m.output.ref, m.output.title])).toEqual([
      [MR, 'Keep a clock'],
      [PR, 'Add the form'],
    ]);
  });

  it('adds those of its subchats and subagents, once, after its own', () => {
    const items = [turn([out(MR, 'Keep a clock')])];
    const other = 'https://gitlab.example.com/acme/web/-/merge_requests/3';
    const inherited = [
      { output: out(MR, 'Same one'), from: 'subagent' },
      { output: out(other, 'Draw the chart'), from: 'subchat «Chart»' },
    ];
    expect(chatMrs(items, inherited).map((m) => [m.output.title, m.from])).toEqual([
      ['Keep a clock', ''],
      ['Draw the chart', 'subchat «Chart»'],
    ]);
    render(<MrDone mrs={chatMrs(items, inherited)} onOpen={vi.fn()} />);
    expect(screen.getByText('acme/web !3 · subchat «Chart»')).toBeTruthy();
  });

  it('names where each lives', () => {
    expect(mrPlace(MR)).toBe('acme/runtime !12');
    expect(mrPlace(PR)).toBe('acme/web #7');
    expect(mrPlace('https://example.com/x')).toBe('https://example.com/x');
  });

  it('opens a change in the pane or the review app, and leaves with its exit', () => {
    vi.useFakeTimers();
    const onOpen = vi.fn();
    const onReview = vi.fn();
    const mrs = [{ output: out(MR, 'Keep a clock'), from: '' }];
    const { container, rerender } = render(<MrDone mrs={mrs} onOpen={onOpen} onReview={onReview} />);
    expect(screen.getByText('A merge request is ready to look at')).toBeTruthy();
    expect(screen.getByText('acme/runtime !12')).toBeTruthy();
    fireEvent.click(screen.getByText('Review'));
    expect(onOpen).toHaveBeenCalledWith(mrs[0].output);
    fireEvent.click(screen.getByRole('button', { name: 'Open in Code review' }));
    expect(onReview).toHaveBeenCalledWith(MR);

    rerender(<MrDone mrs={null} onOpen={onOpen} onReview={onReview} />);
    expect(container.querySelector('.hx-mrdone')?.getAttribute('data-state')).toBe('closing');
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(container.querySelector('.hx-mrdone')).toBeNull();
  });
});
