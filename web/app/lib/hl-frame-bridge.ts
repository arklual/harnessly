import { OPEN_PANE_TAB_EVENT, isPaneTabRequest, type IPaneTabRequest } from '@/lib/hl-pane-tabs';

/**
 * Messages between the shell page and the runtime iframes of remote machines. Both sides check
 * `event.origin === location.origin` and the expected `event.source`.
 */

/** Frame → shell: a shell shortcut pressed while focus was inside the frame. */
export const SHORTCUT_MESSAGE = 'hl:shortcut' as const;

const DIGITS = ['1', '2', '3', '4', '5', '6', '7', '8', '9'] as const;
export type TShellShortcutKey = (typeof DIGITS)[number] | 'k' | 'b' | 'o';

/** ⌘1…⌘9, ⌘K and ⌘B come without Shift; ⌘⇧O only with it. */
export interface IShellShortcut {
  key: TShellShortcutKey;
  shift: boolean;
}

/** Stable name of a shortcut in lists: `k`, `3`, `shift+o`. */
export type TShellShortcutId = TShellShortcutKey | `shift+${TShellShortcutKey}`;

export const shortcutId = ({ key, shift }: IShellShortcut): TShellShortcutId =>
  shift ? `shift+${key}` : key;

/** Shortcuts the shell handles on every page, pinned ⌘1…⌘9 and ⌘⇧O among them. */
export const BASE_SHELL_SHORTCUTS = ['k', 'b'] as const satisfies readonly TShellShortcutId[];

const isValidShortcut = (key: TShellShortcutKey, shift: boolean): boolean => (key === 'o' ? shift : !shift);

type TKeyInput = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'altKey' | 'shiftKey'>;

/**
 * Digits are read from `code` so ⌘1 works whatever the layout; letters from `key`, falling back to `code`
 * only when the layout types a non-Latin letter (⌘K on the Russian layout reports a Cyrillic letter).
 */
export const shellShortcutFromEvent = (event: TKeyInput): IShellShortcut | null => {
  if (!(event.metaKey || event.ctrlKey) || event.altKey) return null;
  const digit = /^Digit([1-9])$/.exec(event.code)?.[1] as TShellShortcutKey | undefined;
  if (digit) return event.shiftKey ? null : { key: digit, shift: false };
  const typed = event.key.toLowerCase();
  const letter = /^[a-z]$/.test(typed) ? typed : /^Key([A-Z])$/.exec(event.code)?.[1].toLowerCase();
  if (letter !== 'k' && letter !== 'b' && letter !== 'o') return null;
  return isValidShortcut(letter, event.shiftKey) ? { key: letter, shift: event.shiftKey } : null;
};

export const isShortcutMessage = (
  data: unknown,
): data is { type: typeof SHORTCUT_MESSAGE } & IShellShortcut => {
  if (typeof data !== 'object' || data === null) return false;
  const { type, key, shift } = data as Record<string, unknown>;
  if (type !== SHORTCUT_MESSAGE || typeof shift !== 'boolean') return false;
  const known = key === 'k' || key === 'b' || key === 'o' || (DIGITS as readonly unknown[]).includes(key);
  return known && isValidShortcut(key as TShellShortcutKey, shift);
};

/** Shell → frame: a tab requested in the shell (`openPaneTab`) for the machine's conversation on screen. */
export const isOpenPaneTabMessage = (
  data: unknown,
): data is { type: typeof OPEN_PANE_TAB_EVENT } & IPaneTabRequest =>
  typeof data === 'object' &&
  data !== null &&
  (data as { type?: unknown }).type === OPEN_PANE_TAB_EVENT &&
  isPaneTabRequest(data);

export const isShortcutIdList = (value: unknown): value is TShellShortcutId[] =>
  Array.isArray(value) &&
  value.every((item) => typeof item === 'string' && /^(shift\+)?([1-9kbo])$/.test(item));

/** A sideways swipe inside a machine frame; the shell owns both drawers, so it decides what opens. */
export const SWIPE_MESSAGE = 'hl:swipe' as const;
export type TSwipeDirection = 'left' | 'right';

export const isSwipeMessage = (
  data: unknown,
): data is { type: typeof SWIPE_MESSAGE; direction: TSwipeDirection } => {
  if (typeof data !== 'object' || data === null) return false;
  const { type, direction } = data as Record<string, unknown>;
  return type === SWIPE_MESSAGE && (direction === 'left' || direction === 'right');
};

export interface IFrameSplit {
  /** Height of the shell header and banners the frame's conversation starts under. */
  top: number;
  /** Width of the right panel column. */
  right: number;
}

export const isFrameSplit = (value: unknown): value is IFrameSplit => {
  if (typeof value !== 'object' || value === null) return false;
  const { top, right } = value as Record<string, unknown>;
  return (
    typeof top === 'number' &&
    typeof right === 'number' &&
    top >= 0 &&
    top < 400 &&
    right >= 200 &&
    right < 2000
  );
};

export interface IFrameView {
  inspectorOpen: boolean;
  split: IFrameSplit | null;
}

/**
 * The view a machine frame opens with, in its address. `hl:runtime-view` arrives only after the
 * frame has painted, and until then the conversation stood full width and under the shell header: the right panel
 * opening and the split moving it down shifted every reply of a reloaded page (CLS 0.2–0.3 on `tw-turn`).
 */
export const frameViewParams = ({ inspectorOpen, split }: IFrameView): string => {
  const params = new URLSearchParams();
  if (inspectorOpen) params.set('pane', '1');
  if (split) params.set('split', `${Math.round(split.top)}:${Math.round(split.right)}`);
  const query = params.toString();
  return query ? `&${query}` : '';
};

export const parseFrameView = (query: Record<string, string | string[] | undefined>): IFrameView => {
  const raw = typeof query.split === 'string' ? query.split.split(':').map(Number) : [];
  const split = raw.length === 2 ? { top: raw[0], right: raw[1] } : null;
  return { inspectorOpen: query.pane === '1', split: isFrameSplit(split) ? split : null };
};

/** Shell → frame: the summary button in the shell header (Outputs, Side chats, Sources) for the frame's conversation. */
export const PANE_SUMMARY_MESSAGE = 'hl:pane-summary' as const;

/** Frame → shell: the right panel was dragged to this width; the shell keeps its column for it. */
export const PANE_WIDTH_MESSAGE = 'hl:pane-width' as const;

export const isPaneWidthMessage = (
  data: unknown,
): data is { type: typeof PANE_WIDTH_MESSAGE; width: number } => {
  if (typeof data !== 'object' || data === null) return false;
  const { type, width } = data as Record<string, unknown>;
  return (
    type === PANE_WIDTH_MESSAGE &&
    typeof width === 'number' &&
    Number.isFinite(width) &&
    width >= 200 &&
    width < 2000
  );
};
