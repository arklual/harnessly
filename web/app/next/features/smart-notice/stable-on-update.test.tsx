// @vitest-environment jsdom
// Updating one notice touches only it: the other plates and the front card keep their nodes, so nothing
// mounts again and no entry animation runs again.
import { cleanup, render } from '@testing-library/react';
import { afterEach, beforeAll, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import SmartNotice, { SMART_KIND, type ISmartItem, type ISmartNoticeProps } from './smart-notice';

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

const item = (key: string, text = `What ${key} waits for`): ISmartItem => ({
  key,
  kind: SMART_KIND.question,
  chatId: `chat-${key}`,
  icon: 'message-square-text',
  title: `Chat ${key}`,
  text,
  at: NOW - 60_000,
  until: null,
});

const props = (items: ISmartItem[]): ISmartNoticeProps => ({
  items,
  mainKey: null,
  detail: { k: 'plain' },
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
});

it('a new line in one notice changes only that notice; the rest keep their nodes and stay untouched', () => {
  const items = [item('a'), item('b'), item('c'), item('d')];
  const view = render(<SmartNotice {...props(items)} />);
  const body = view.container.querySelector<HTMLElement>('.hx-sn__body')!;
  const plates = [...view.container.querySelectorAll<HTMLElement>('.hx-sn__plate')];
  // The one to change: not on the front card and not on a peeking plate.
  const target = items.find((it) => !view.container.textContent!.includes(it.title))!;
  expect(target).toBeDefined();

  const changed: Node[] = [];
  const watch = new MutationObserver((records) => changed.push(...records.map((r) => r.target)));
  watch.observe(view.container, { subtree: true, childList: true, characterData: true, attributes: true });

  const next = items.map((it) => (it.key === target.key ? item(it.key, 'Another line') : it));
  view.rerender(<SmartNotice {...props(next)} />);
  watch.takeRecords().forEach((r) => changed.push(r.target));
  watch.disconnect();

  expect(view.container.querySelector('.hx-sn__body')).toBe(body);
  expect([...view.container.querySelectorAll('.hx-sn__plate')]).toEqual(plates);
  expect(changed.filter((node) => body.contains(node) || plates.some((p) => p.contains(node)))).toEqual([]);
});
