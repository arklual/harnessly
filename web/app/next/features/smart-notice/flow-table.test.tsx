// @vitest-environment jsdom
// The flow spec's table «ввод → действие», row by row: what a hover, a click, a key and the × do to the
// plate, and how the stack keeps its front plate.
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import SmartNotice, {
  SMART_EXIT_MS,
  SMART_KIND,
  type ISmartItem,
  type ISmartNoticeProps,
} from './smart-notice';
import { RISE_MS } from './use-gestures';
import { AFTER_HOLD_MS, NEWS_CLOCK, liveNews, tickNews, useFront, useNewsClock } from './flow';

beforeAll(() => {
  pinLocale('en');
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  window.matchMedia ??= (() => ({ matches: false })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const NOW = 1_800_000_000_000;

const item = (key: string, kind: ISmartItem['kind'], patch: Partial<ISmartItem> = {}): ISmartItem => ({
  key,
  kind,
  chatId: `chat-${key}`,
  icon: 'message-square-text',
  title: `Chat ${key}`,
  text: `What ${key} waits for`,
  at: NOW - 60_000,
  until: null,
  ...patch,
});

const props = (patch: Partial<ISmartNoticeProps> = {}): ISmartNoticeProps => ({
  items: [item('q', SMART_KIND.question)],
  mainKey: null,
  detail: { k: 'question', text: 'Which header?', options: ['Dark', 'Light'] },
  open: false,
  place: 'top-center',
  anchor: null,
  now: NOW,
  onOpen: vi.fn(),
  onMain: vi.fn(),
  onOpenChat: vi.fn(),
  onAnswer: vi.fn(async () => undefined),
  onDecide: vi.fn(async () => undefined),
  onCode: vi.fn(async () => 'ok' as const),
  onCancelCode: vi.fn(async () => undefined),
  onStop: vi.fn(async () => undefined),
  onOpenMachine: vi.fn(),
  onHide: vi.fn(),
  onCloseAll: vi.fn(),
  onMute: vi.fn(),
  onSlot: vi.fn(async () => 'ok' as const),
  onSlotDismiss: vi.fn(async () => undefined),
  onMuteSource: vi.fn(),
  onSettings: vi.fn(),
  announceMs: 5000,
  returnFocus: vi.fn(),
  focusOnOpen: false,
  onHold: vi.fn(),
  reply: {
    text: '',
    setText: vi.fn(),
    recording: null,
    onCancelRecording: vi.fn(),
    onFinishRecording: vi.fn(),
  },
  onReply: vi.fn(async () => undefined),
  ...patch,
});

const host = () => document.querySelector<HTMLElement>('.hx-sn')!;

describe('ввод → действие', () => {
  it('hover: only holds the plate — no other plate, no card, no chat', () => {
    const p = props({ items: [item('q', SMART_KIND.question), item('d', SMART_KIND.done)] });
    render(<SmartNotice {...p} />);
    fireEvent.pointerEnter(host(), { pointerType: 'mouse' });
    fireEvent.pointerMove(host(), { pointerType: 'mouse', clientX: 10, clientY: 10 });
    expect(p.onHold).toHaveBeenCalledWith(true);
    expect(p.onMain).not.toHaveBeenCalled();
    expect(p.onOpen).not.toHaveBeenCalled();
    expect(p.onOpenChat).not.toHaveBeenCalled();
    expect(screen.getByText('Chat q')).toBeTruthy();
  });

  it('click on the line: opens the card, never the chat', () => {
    const p = props();
    render(<SmartNotice {...p} />);
    fireEvent.click(screen.getByText('Chat q'));
    expect(p.onOpen).toHaveBeenCalledWith(true);
    expect(p.onOpenChat).not.toHaveBeenCalled();
  });

  it('click on a finished turn just announced: opens it too, its announcement has no button to the chat', () => {
    const p = props({ items: [item('d', SMART_KIND.done, { at: NOW - 1000 })], detail: { k: 'plain' } });
    render(<SmartNotice {...p} />);
    expect(document.querySelector('.hx-sn__ann .hx-sn__btn')).toBeNull();
    fireEvent.click(screen.getByText('Chat d'));
    expect(p.onOpen).toHaveBeenCalledWith(true);
    expect(p.onOpenChat).not.toHaveBeenCalled();
  });

  it('the chat: only «Open chat» in the open card goes there', () => {
    const p = props({ open: true });
    render(<SmartNotice {...p} />);
    expect(p.onOpenChat).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('Open chat'));
    expect(p.onOpenChat).toHaveBeenCalledTimes(1);
  });

  it('× on one plate puts it away; × on a stack is «Close all»', () => {
    const one = props();
    render(<SmartNotice {...one} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(one.onHide).toHaveBeenCalledTimes(1);
    expect(one.onCloseAll).not.toHaveBeenCalled();
    cleanup();
    const stack = props({ items: [item('q', SMART_KIND.question), item('p', SMART_KIND.permission)] });
    render(<SmartNotice {...stack} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close all' }));
    expect(stack.onCloseAll).toHaveBeenCalledTimes(1);
    expect(stack.onHide).not.toHaveBeenCalled();
  });

  it('«Close all» is over the list of the others in the open card', () => {
    const p = props({
      open: true,
      items: [item('q', SMART_KIND.question), item('p', SMART_KIND.permission)],
    });
    render(<SmartNotice {...p} />);
    fireEvent.click(screen.getAllByText('Close all').find((el) => el.closest('.hx-sn__others-head'))!);
    expect(p.onCloseAll).toHaveBeenCalledTimes(1);
  });

  it('keys: Esc folds, ⌫ puts away the front plate, ⇧⌫ closes all, ↓ in the card takes the next one', () => {
    const items = [item('q', SMART_KIND.question), item('p', SMART_KIND.permission)];
    const p = props({ open: true, items });
    render(<SmartNotice {...p} />);
    const target = screen.getByText('Open chat');
    fireEvent.keyDown(target, { key: 'ArrowDown' });
    expect(p.onMain).toHaveBeenCalledWith('p');
    fireEvent.keyDown(target, { key: 'Backspace', shiftKey: true });
    expect(p.onCloseAll).toHaveBeenCalledTimes(1);
    expect(p.onHide).not.toHaveBeenCalled();
    fireEvent.keyDown(target, { key: 'Backspace' });
    expect(p.onHide).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(target, { key: 'Escape' });
    expect(p.onOpen).toHaveBeenCalledWith(false);
  });
});

describe('the front plate', () => {
  const q = item('q', SMART_KIND.question);
  const done = item('d', SMART_KIND.done);
  const code = item('c', SMART_KIND.code);

  const front = (initial: ISmartItem[], touched: boolean) => {
    let mainKey: string | null = null;
    const hook = renderHook(
      ({ items, touch }: { items: ISmartItem[]; touch: boolean }) =>
        useFront(items, mainKey, (k) => (mainKey = k), touch),
      { initialProps: { items: initial, touch: touched } },
    );
    return { hook, key: () => mainKey };
  };

  it('stays while the person touches the plate: the urgent newcomer goes behind', () => {
    const { hook, key } = front([done], true);
    hook.rerender({ items: [done], touch: true });
    expect(key()).toBe('d');
    hook.rerender({ items: [code, done], touch: true });
    hook.rerender({ items: [code, done], touch: true });
    expect(key()).toBe('d');
  });

  it('nobody touches it: what is more urgent than the front comes forward, what is not goes behind', () => {
    const { hook, key } = front([q], false);
    hook.rerender({ items: [q], touch: false });
    expect(key()).toBe('q');
    hook.rerender({ items: [q, done], touch: false });
    hook.rerender({ items: [q, done], touch: false });
    expect(key()).toBe('q');
    hook.rerender({ items: [code, q, done], touch: false });
    hook.rerender({ items: [code, q, done], touch: false });
    expect(key()).toBe('c');
  });

  it('gone: the most urgent left takes the front', () => {
    const { hook, key } = front([q, done], false);
    hook.rerender({ items: [q, done], touch: false });
    hook.rerender({ items: [done], touch: false });
    hook.rerender({ items: [done], touch: false });
    expect(key()).toBe('d');
  });
});

describe('the news clock: the hover pauses, it hides nothing', () => {
  const LIFE = 7000;
  const at = (key: string, ms: number) => ({ key, at: ms });
  const run = (steps: { news: { key: string; at: number }[]; held: boolean; now: number }[]) => {
    let clock = NEWS_CLOCK;
    return steps.map((step) => {
      clock = tickNews(clock, step.news, step.held, step.now, LIFE);
      return [...liveNews(clock, step.now)].sort();
    });
  };

  it('held past its time, news stays; let go, it goes on from what it had left', () => {
    const news = [at('a', 0)];
    const [start, held, still, after, gone] = run([
      { news, held: false, now: 1000 },
      { news, held: true, now: 2000 },
      { news, held: true, now: 60_000 },
      { news, held: false, now: 61_000 },
      { news, held: false, now: 61_000 + 6000 },
    ]);
    expect(start).toEqual(['a']);
    expect(held).toEqual(['a']);
    expect(still).toEqual(['a']);
    // 5 s were left at the hold: they run from the release.
    expect(after).toEqual(['a']);
    expect(gone).toEqual([]);
  });

  it('each piece on its own clock: they go one by one, not together', () => {
    const news = [at('a', 0), at('b', 3000)];
    const [one, both] = run([
      { news, held: false, now: 7500 },
      { news, held: false, now: 10_500 },
    ]);
    expect(one).toEqual(['b']);
    expect(both).toEqual([]);
  });

  it('news that comes while held starts its time when the hold ends', () => {
    const steps = run([
      { news: [], held: true, now: 0 },
      { news: [at('n', 1000)], held: true, now: 1000 },
      { news: [at('n', 1000)], held: true, now: 30_000 },
      { news: [at('n', 1000)], held: false, now: 40_000 },
      { news: [at('n', 1000)], held: false, now: 40_000 + LIFE - 1 },
    ]);
    expect(steps.slice(1)).toEqual([['n'], ['n'], ['n'], ['n']]);
  });

  it('let go with little left, it still stays a moment; what had gone before the hold does not come back', () => {
    const news = [at('a', 0), at('old', -LIFE - 1)];
    const [, , after, later] = run([
      { news, held: false, now: 6900 },
      { news, held: true, now: 6950 },
      { news, held: false, now: 9000 },
      { news, held: false, now: 9000 + AFTER_HOLD_MS },
    ]);
    expect(after).toEqual(['a']);
    expect(later).toEqual([]);
  });

  it('the hook keeps the clock across renders', () => {
    const news = [at('a', 0)];
    const { result, rerender } = renderHook(
      ({ held, now }: { held: boolean; now: number }) => useNewsClock(news, held, now, LIFE),
      { initialProps: { held: false, now: 1000 } },
    );
    expect([...result.current]).toEqual(['a']);
    rerender({ held: true, now: 2000 });
    rerender({ held: true, now: 20_000 });
    expect([...result.current]).toEqual(['a']);
    rerender({ held: false, now: 30_000 });
    rerender({ held: false, now: 36_000 });
    expect([...result.current]).toEqual([]);
  });
});

describe('a popover opened from the field', () => {
  it('makes the plate over the field give way, and only that one', () => {
    const anchor = { x: 400, bottom: 120 };
    const { rerender } = render(<SmartNotice {...props({ place: 'bottom-center', anchor })} />);
    expect(host().dataset.covered).toBeUndefined();
    rerender(<SmartNotice {...props({ place: 'bottom-center', anchor, covered: true })} />);
    expect(host().dataset.covered).toBe('true');
    rerender(<SmartNotice {...props({ place: 'top-center', anchor, covered: true })} />);
    expect(host().dataset.covered).toBeUndefined();
  });
});

describe('putting one away, and the last one leaving', () => {
  afterEach(() => vi.useRealTimers());

  it('one put away: the next comes forward black in one short move, nothing else is dimmed and nothing sticks', () => {
    vi.useFakeTimers();
    const items = [
      item('q', SMART_KIND.question),
      item('p', SMART_KIND.permission),
      item('d', SMART_KIND.done),
    ];
    const p = props({ open: true, items });
    const view = render(<SmartNotice {...p} />);
    fireEvent.keyDown(screen.getByText('Open chat'), { key: 'Backspace' });
    expect(p.onHide).toHaveBeenCalledTimes(1);
    expect(host().dataset.rise).toBe('next');
    view.rerender(<SmartNotice {...p} open={false} items={items.slice(1)} />);
    act(() => vi.advanceTimersByTime(RISE_MS));
    // The move is over: no state of it is left on the plate, and no plate keeps a look of its own written on it.
    expect(host().dataset.rise).toBeUndefined();
    expect(host().dataset.paging).toBeUndefined();
    for (const el of host().querySelectorAll<HTMLElement>('.hx-sn__shape, .hx-sn__plate, .hx-sn__tint'))
      expect(el.style.opacity).toBe('');
    expect(host().querySelectorAll('.hx-sn__plate[data-depth="1"]')).toHaveLength(1);
  });

  it('the hover holds the plate and takes nothing off it', () => {
    const items = [item('d', SMART_KIND.done), item('f', SMART_KIND.failed)];
    const p = props({ items });
    render(<SmartNotice {...p} />);
    fireEvent.pointerEnter(host(), { pointerType: 'mouse' });
    expect(p.onHold).toHaveBeenLastCalledWith(true);
    expect(p.onHide).not.toHaveBeenCalled();
    expect(p.onMain).not.toHaveBeenCalled();
    expect(host().querySelectorAll('.hx-sn__plate[data-depth="1"]')).toHaveLength(1);
    fireEvent.pointerLeave(host(), { pointerType: 'mouse' });
    expect(p.onHold).toHaveBeenLastCalledWith(false);
  });

  it('the last one leaves in a short exit and is gone after it', () => {
    vi.useFakeTimers();
    const p = props();
    const view = render(<SmartNotice {...p} />);
    view.rerender(<SmartNotice {...p} items={[]} />);
    expect(host()).not.toBeNull();
    expect(SMART_EXIT_MS).toBeLessThanOrEqual(260);
    act(() => vi.advanceTimersByTime(SMART_EXIT_MS));
    expect(document.querySelector('.hx-sn')).toBeNull();
  });
});
