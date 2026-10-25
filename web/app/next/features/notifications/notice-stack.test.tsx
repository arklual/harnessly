// @vitest-environment jsdom
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import NoticeStack, { NOTICE_EXIT_MS } from '@/next/features/notifications/notice-stack';
import type { INotice } from '@/next/stores/notices';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const notice = (chatId: string): INotice => ({
  key: `${chatId}:1`,
  chatId,
  kind: 'waiting',
  icon: 'circle-question-mark',
  title: `Chat ${chatId}`,
  text: 'Waits for your answer',
  at: 0,
});

const props = (notices: readonly INotice[]) => ({
  notices,
  now: 0,
  onOpen: vi.fn(),
  onDismiss: vi.fn(),
  onDismissAll: vi.fn(),
});

describe('the pile of notices', () => {
  it('opens a chat and takes one away', () => {
    const p = props([notice('a')]);
    render(<NoticeStack {...p} />);
    fireEvent.click(screen.getAllByRole('button', { name: /open|открыть/i })[0]);
    expect(p.onOpen).toHaveBeenCalledWith(expect.objectContaining({ chatId: 'a' }));
    fireEvent.click(screen.getByRole('button', { name: /dismiss|скрыть/i }));
    expect(p.onDismiss).toHaveBeenCalledWith('a:1');
  });

  it('spreads as the pointer comes over it', () => {
    const p = props([notice('a'), notice('b')]);
    const { container } = render(<NoticeStack {...p} />);
    const region = container.querySelector('.hx-notices') as HTMLElement;
    expect(region.dataset.spread).toBeUndefined();
    fireEvent.pointerEnter(region, { pointerType: 'mouse' });
    expect(region.dataset.spread).toBe('true');
  });

  it('keeps a leaving notice for its exit, then lets it go', () => {
    const p = props([notice('a'), notice('b')]);
    const { container, rerender } = render(<NoticeStack {...p} />);
    rerender(<NoticeStack {...p} notices={[notice('b')]} />);
    expect(container.querySelectorAll('.hx-notice-slot[data-leaving]')).toHaveLength(1);
    act(() => void vi.advanceTimersByTime(NOTICE_EXIT_MS));
    expect(container.querySelectorAll('.hx-notice-slot')).toHaveLength(1);
  });
});
