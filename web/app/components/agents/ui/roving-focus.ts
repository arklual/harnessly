/**
 * Keyboard logic shared by the Hl UI primitives (menu, listbox, radio groups).
 * Pure functions only: no DOM, no React. Components translate DOM events into these calls,
 * so every rule here is unit-tested in node (tests/unit/lib/hl-ui-roving-focus.test.ts).
 */

export type TValueOf<T> = T[keyof T];

export const NAV_ORIENTATION = {
  vertical: 'vertical',
  horizontal: 'horizontal',
  both: 'both',
} as const;

export const NAV_KEY = {
  up: 'ArrowUp',
  down: 'ArrowDown',
  left: 'ArrowLeft',
  right: 'ArrowRight',
  home: 'Home',
  end: 'End',
  pageUp: 'PageUp',
  pageDown: 'PageDown',
} as const;

export const TEXT_DIRECTION = { ltr: 'ltr', rtl: 'rtl' } as const;

/** How many rows PageUp/PageDown jump. */
export const PAGE_STEP = 10;

/** A typed letter is joined with the previous one if it comes within this window. */
export const TYPEAHEAD_RESET_MS = 700;

export interface INavOptions {
  count: number;
  orientation: TValueOf<typeof NAV_ORIENTATION>;
  loop: boolean;
  /** Rows that navigation jumps over (disabled radios). Menus keep disabled rows reachable. */
  isSkipped?: (index: number) => boolean;
  dir?: TValueOf<typeof TEXT_DIRECTION>;
  pageStep?: number;
  /** Home/End belong to a text field (combobox) and must move the caret, not the list. */
  textInput?: boolean;
}

const stepFor = (key: string, orientation: INavOptions['orientation'], dir: INavOptions['dir']): number => {
  const vertical = orientation !== NAV_ORIENTATION.horizontal;
  const horizontal = orientation !== NAV_ORIENTATION.vertical;
  const forward = dir === TEXT_DIRECTION.rtl ? NAV_KEY.left : NAV_KEY.right;
  const backward = dir === TEXT_DIRECTION.rtl ? NAV_KEY.right : NAV_KEY.left;
  if (vertical && key === NAV_KEY.down) return 1;
  if (vertical && key === NAV_KEY.up) return -1;
  if (horizontal && key === forward) return 1;
  if (horizontal && key === backward) return -1;
  return 0;
};

const firstFrom = (
  start: number,
  step: 1 | -1,
  count: number,
  isSkipped: (index: number) => boolean,
): number => {
  for (let index = start; index >= 0 && index < count; index += step) {
    if (!isSkipped(index)) return index;
  }
  return -1;
};

export const firstNavigable = (count: number, isSkipped: (index: number) => boolean = () => false): number =>
  firstFrom(0, 1, count, isSkipped);

export const lastNavigable = (count: number, isSkipped: (index: number) => boolean = () => false): number =>
  firstFrom(count - 1, -1, count, isSkipped);

/**
 * Index the active row moves to for `key`.
 * Returns null when the key is not a navigation key here (the caller lets it through),
 * -1 when it is one but nothing is navigable, otherwise the new index (it may equal `current`).
 * `current` may be -1: nothing active yet, the first press lands on the first or last row.
 */
export const nextIndex = (current: number, key: string, options: INavOptions): number | null => {
  const {
    count,
    orientation,
    loop,
    dir = TEXT_DIRECTION.ltr,
    pageStep = PAGE_STEP,
    textInput = false,
  } = options;
  const isSkipped = options.isSkipped ?? (() => false);
  const step = stepFor(key, orientation, dir);
  const isPage = key === NAV_KEY.pageUp || key === NAV_KEY.pageDown;
  const isEdge = !textInput && (key === NAV_KEY.home || key === NAV_KEY.end);
  if (step === 0 && !isPage && !isEdge) return null;
  if (count <= 0) return -1;

  if (key === NAV_KEY.home) return firstNavigable(count, isSkipped);
  if (key === NAV_KEY.end) return lastNavigable(count, isSkipped);

  if (isPage) {
    const direction = key === NAV_KEY.pageDown ? 1 : -1;
    const from = current < 0 ? 0 : current;
    const target = Math.min(count - 1, Math.max(0, from + direction * pageStep));
    const found = firstFrom(target, direction === 1 ? 1 : -1, count, isSkipped);
    if (found !== -1) return found;
    return firstFrom(target, direction === 1 ? -1 : 1, count, isSkipped);
  }

  if (current < 0) return step > 0 ? firstNavigable(count, isSkipped) : lastNavigable(count, isSkipped);

  let index = current;
  for (let visited = 0; visited < count; visited += 1) {
    let candidate = index + step;
    if (candidate < 0 || candidate >= count) {
      if (!loop) return isSkipped(current) ? -1 : current;
      candidate = candidate < 0 ? count - 1 : 0;
    }
    if (!isSkipped(candidate)) return candidate;
    index = candidate;
  }
  return isSkipped(current) ? -1 : current;
};

export interface ITypeaheadState {
  query: string;
  at: number;
}

export const EMPTY_TYPEAHEAD: ITypeaheadState = { query: '', at: 0 };

interface IKeyLike {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
}

/** A printable key that should feed typeahead. Space only continues a query already being typed. */
export const isTypeaheadKey = (event: IKeyLike, state: ITypeaheadState, now: number): boolean => {
  if (event.ctrlKey || event.metaKey || event.altKey) return false;
  if (event.key.length !== 1) return false;
  if (event.key === ' ') return state.query.length > 0 && now - state.at <= TYPEAHEAD_RESET_MS;
  return true;
};

export const typeaheadStep = (state: ITypeaheadState, char: string, now: number): ITypeaheadState => {
  const fresh = now - state.at > TYPEAHEAD_RESET_MS;
  return { query: fresh ? char : state.query + char, at: now };
};

const normalize = (value: string) => value.trim().toLocaleLowerCase('ru');

/**
 * Row matching the typed query, or -1. A repeated single letter ("aaa") cycles through rows
 * starting with that letter; a longer query keeps the current row while it still matches.
 */
export const typeaheadIndex = (
  labels: readonly string[],
  query: string,
  current: number,
  isSkipped: (index: number) => boolean = () => false,
): number => {
  const count = labels.length;
  const normalized = query.toLocaleLowerCase('ru');
  if (!count || !normalized.trim()) return -1;
  const repeated = [...normalized].every((char) => char === normalized[0]);
  const needle = repeated ? normalized[0] : normalized;
  const start = repeated || current < 0 ? current + 1 : current;
  for (let offset = 0; offset < count; offset += 1) {
    const index = (((start + offset) % count) + count) % count;
    if (!isSkipped(index) && normalize(labels[index]).startsWith(needle)) return index;
  }
  return -1;
};

export const MENU_INTENT = {
  move: 'move',
  activate: 'activate',
  openSubmenu: 'open-submenu',
  closeSubmenu: 'close-submenu',
  close: 'close',
  tab: 'tab',
  none: 'none',
} as const;

export interface IMenuKeyContext {
  isSubmenu: boolean;
  itemHasSubmenu: boolean;
  dir?: TValueOf<typeof TEXT_DIRECTION>;
}

/** What a key means inside a (sub)menu. Arrow movement itself comes from nextIndex(). */
export const menuKeyIntent = (key: string, context: IMenuKeyContext): TValueOf<typeof MENU_INTENT> => {
  const { isSubmenu, itemHasSubmenu, dir = TEXT_DIRECTION.ltr } = context;
  const inward = dir === TEXT_DIRECTION.rtl ? NAV_KEY.left : NAV_KEY.right;
  const outward = dir === TEXT_DIRECTION.rtl ? NAV_KEY.right : NAV_KEY.left;
  if (key === 'Enter' || key === ' ') return itemHasSubmenu ? MENU_INTENT.openSubmenu : MENU_INTENT.activate;
  if (key === inward) return itemHasSubmenu ? MENU_INTENT.openSubmenu : MENU_INTENT.none;
  if (key === outward) return isSubmenu ? MENU_INTENT.closeSubmenu : MENU_INTENT.none;
  if (key === 'Escape') return isSubmenu ? MENU_INTENT.closeSubmenu : MENU_INTENT.close;
  if (key === 'Tab') return MENU_INTENT.tab;
  if (key === NAV_KEY.up || key === NAV_KEY.down || key === NAV_KEY.home || key === NAV_KEY.end)
    return MENU_INTENT.move;
  if (key === NAV_KEY.pageUp || key === NAV_KEY.pageDown) return MENU_INTENT.move;
  return MENU_INTENT.none;
};

export const LISTBOX_ACTION = {
  move: 'move',
  select: 'select',
  dismiss: 'dismiss',
  none: 'none',
} as const;

export interface IListboxKeyOptions {
  count: number;
  active: number;
  loop: boolean;
  isSkipped?: (index: number) => boolean;
  /** Space picks the row (standalone listbox). In a text combobox Space is a character. */
  spaceSelects: boolean;
  textInput: boolean;
}

export type TListboxKeyResult =
  | { type: typeof LISTBOX_ACTION.move; index: number }
  | { type: typeof LISTBOX_ACTION.select; index: number }
  | { type: typeof LISTBOX_ACTION.dismiss }
  | { type: typeof LISTBOX_ACTION.none };

/** Keys of a listbox or of a text field that drives one through aria-activedescendant. */
export const listboxKeyAction = (key: string, options: IListboxKeyOptions): TListboxKeyResult => {
  const { count, active, loop, isSkipped, spaceSelects, textInput } = options;
  if (key === 'Escape') return { type: LISTBOX_ACTION.dismiss };
  const selectable = active >= 0 && active < count && !(isSkipped?.(active) ?? false);
  if (key === 'Enter' || (key === ' ' && spaceSelects)) {
    return selectable ? { type: LISTBOX_ACTION.select, index: active } : { type: LISTBOX_ACTION.none };
  }
  const index = nextIndex(active, key, {
    count,
    orientation: NAV_ORIENTATION.vertical,
    loop,
    isSkipped,
    textInput,
  });
  if (index === null || index === -1) return { type: LISTBOX_ACTION.none };
  return { type: LISTBOX_ACTION.move, index };
};
