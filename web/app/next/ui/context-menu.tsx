// Our menu in place of the browser's (HRN-962): a right click, a two-finger tap, the menu key or a held finger opens it
// at the point it was asked from, in the one look of every menu (MenuPanel: black, from the point, a sheet on the phone).
// The browser's own menu stays where it is the person's tool: a field, selected text, a link, a picture, a frame.
import { Menu } from '@base-ui/react/menu';
import {
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from 'react';
import Icon, { type TIconName } from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { afterTouchClick } from '@/next/ui/use-drag';

export interface IMenuPoint {
  x: number;
  y: number;
}

/** A finger held this long opens the menu; one that moves farther than the slop scrolls instead. */
export const MENU_HOLD_MS = 450;
const HOLD_SLOP_PX = 8;

/** What the browser's own menu is for: text being written, links, pictures and media, frames of other pages. */
const SYSTEM_MENU = [
  'input',
  'textarea',
  'select',
  '[contenteditable=""]',
  '[contenteditable="true"]',
  '[contenteditable="plaintext-only"]',
  'a[href]',
  'img',
  'video',
  'audio',
  'canvas',
  'iframe',
  'webview',
  '[data-system-menu]',
].join(',');

const hasSelection = (): boolean => {
  const selection = window.getSelection();
  return selection !== null && !selection.isCollapsed;
};

/**
 * The person asks the browser's own menu here: on a field, a link, a picture or a frame, or on text they selected.
 * `selected` says the selection was there before the press: Safari selects the word under a right click itself.
 */
export const wantsSystemMenu = (target: EventTarget | null, selected = true): boolean => {
  if (!(target instanceof Element)) return true;
  if (target.closest(SYSTEM_MENU)) return true;
  return selected && hasSelection() && window.getSelection()?.containsNode(target, true) === true;
};

/** The point a menu asked by `event` opens at: the pointer, or under the element when the menu key asked. */
export const menuPointOf = (event: MouseEvent<Element> | KeyboardEvent<Element>): IMenuPoint => {
  const rect = event.currentTarget.getBoundingClientRect();
  if ('clientX' in event && (event.clientX !== 0 || event.clientY !== 0))
    return { x: event.clientX, y: event.clientY };
  return { x: rect.left + 12, y: rect.bottom };
};

/** A point of the window as a menu's anchor. */
export const pointAnchor = (at: IMenuPoint) => ({
  getBoundingClientRect: () => new DOMRect(at.x, at.y, 0, 0),
});

const isMenuKey = (event: KeyboardEvent<Element>) =>
  event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10');

export interface IContextMenuOptions {
  /** A held finger opens the menu too (off where a held finger selects text, as in the feed). */
  hold?: boolean;
  /** Asked before opening, with the element under the pointer: false leaves the browser its menu. */
  accept?: (target: Element) => boolean;
}

/**
 * Opens a menu at the point the person asked it from. Spread `handlers` on the element the menu is of and draw
 * `<PointMenu menu={…}>` beside it; `target` is the element it was asked on (to tell which row, which turn).
 * `held()` says the click that follows is the end of a held finger, which opens nothing.
 */
export const useContextMenu = ({ hold = true, accept }: IContextMenuOptions = {}) => {
  const [at, setAt] = useState<IMenuPoint | null>(null);
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<Element | null>(null);
  const holding = useRef<{ timer: number; x: number; y: number } | null>(null);
  const heldClick = useRef(false);
  const from = useRef<HTMLElement | null>(null);
  /** Text was selected before the right button went down (and not by the press itself). */
  const selectedAtPress = useRef<boolean | null>(null);

  const show = (point: IMenuPoint, el: Element) => {
    setTarget(el);
    setAt(point);
    setOpen(true);
  };
  const cancelHold = () => {
    if (holding.current) window.clearTimeout(holding.current.timer);
    holding.current = null;
  };
  const wanted = (el: EventTarget | null, selected = true): el is Element =>
    !wantsSystemMenu(el, selected) && el instanceof Element && (accept === undefined || accept(el));

  const handlers = {
    onContextMenu: (event: MouseEvent<HTMLElement>) => {
      // Another menu (a link's, a tab's) took it, or a held finger has opened ours already.
      if (event.defaultPrevented) return;
      if (heldClick.current || holding.current) {
        event.preventDefault();
        return;
      }
      const selected = selectedAtPress.current ?? true;
      selectedAtPress.current = null;
      if (!wanted(event.target, selected)) return;
      event.preventDefault();
      // The word the right click selected on its own is not the person's selection.
      if (!selected) window.getSelection()?.removeAllRanges();
      from.current = event.currentTarget;
      show(menuPointOf(event), event.target);
    },
    onPointerDown: (event: PointerEvent<HTMLElement>) => {
      if (event.pointerType === 'mouse' && (event.button === 2 || event.ctrlKey)) {
        selectedAtPress.current = hasSelection();
        return;
      }
      if (!hold || event.pointerType !== 'touch') return;
      cancelHold();
      const el = event.target;
      if (!wanted(el)) return;
      const box = event.currentTarget;
      const { clientX: x, clientY: y } = event;
      const timer = window.setTimeout(() => {
        holding.current = null;
        heldClick.current = true;
        from.current = box;
        navigator.vibrate?.(8);
        afterTouchClick(() => {
          heldClick.current = false;
          show({ x, y }, el);
        });
      }, MENU_HOLD_MS);
      holding.current = { timer, x, y };
    },
    onPointerMove: (event: PointerEvent<HTMLElement>) => {
      const h = holding.current;
      if (h && Math.hypot(event.clientX - h.x, event.clientY - h.y) > HOLD_SLOP_PX) cancelHold();
    },
    onPointerUp: cancelHold,
    onPointerCancel: cancelHold,
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (!isMenuKey(event) || !wanted(event.target)) return;
      event.preventDefault();
      from.current = event.currentTarget;
      show(menuPointOf(event), event.target);
    },
  };

  return {
    handlers,
    at,
    open,
    target,
    setOpen,
    /** The click is the end of a held finger: it opens nothing (the menu opens instead). */
    held: (): boolean => heldClick.current,
    /** Where the focus goes back when the menu closes. */
    returnFocus: (): HTMLElement | boolean => from.current ?? true,
  };
};

export type TContextMenu = ReturnType<typeof useContextMenu>;

interface IPointMenuProps {
  menu: Pick<TContextMenu, 'at' | 'open' | 'setOpen' | 'returnFocus'>;
  label: string;
  /** data-menu of the popup, for its look and for tests. */
  kind?: string;
  children: ReactNode;
}

/** The menu at a point: the one look and keys of every menu (arrows, Enter, Escape, the first letters of an item). */
export const PointMenu = ({ menu, label, kind, children }: IPointMenuProps) => {
  const actions = useMenuActions();
  if (menu.at === null) return null;
  return (
    <Menu.Root open={menu.open} onOpenChange={menu.setOpen} actionsRef={actions}>
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        menu={kind}
        label={label}
        anchor={pointAnchor(menu.at)}
        finalFocus={menu.returnFocus}
      >
        {children}
      </MenuPanel>
    </Menu.Root>
  );
};

interface IMenuItemProps {
  icon: TIconName;
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
  /** Said quietly at the end: a shortcut, a count. */
  hint?: string | null;
  /** Keeps the menu open after the pick (a second view, a question). */
  stay?: boolean;
}

/** An item of a menu: a line icon, the words, a quiet hint at the end. */
export const MenuItem = ({ icon, children, onClick, disabled, danger, hint, stay }: IMenuItemProps) => (
  <Menu.Item
    className="hx-cmenu__item"
    data-danger={danger || undefined}
    disabled={disabled}
    closeOnClick={!stay}
    onClick={onClick}
  >
    <Icon name={icon} size="sm" />
    <span className="hx-cmenu__txt">{children}</span>
    {hint && <span className="hx-cmenu__hint">{hint}</span>}
  </Menu.Item>
);

/** Air between groups of items: no line. */
export const MenuGap = () => <Menu.Separator className="hx-cmenu__sep" />;

const typing = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));

/**
 * The keys of a menu made of plain buttons (role="menu", not on Base UI): the arrows and Home/End move along its items,
 * a letter goes to the next item that begins with it. Enter and Escape are the buttons' and the popover's own.
 */
export const onMenuKeys = (event: KeyboardEvent<HTMLElement>) => {
  const items = [
    ...event.currentTarget.querySelectorAll<HTMLElement>(
      '[role^="menuitem"]:not(:disabled):not([aria-disabled="true"])',
    ),
  ];
  if (items.length === 0) return;
  const at = items.indexOf(document.activeElement as HTMLElement);
  let to: number | null = null;
  if (event.key === 'ArrowDown') to = at < 0 ? 0 : (at + 1) % items.length;
  else if (event.key === 'ArrowUp') to = at <= 0 ? items.length - 1 : at - 1;
  else if (event.key === 'Home' && !typing(event.target)) to = 0;
  else if (event.key === 'End' && !typing(event.target)) to = items.length - 1;
  else if (
    event.key.length === 1 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey &&
    !typing(event.target)
  ) {
    const letter = event.key.toLocaleLowerCase();
    const next = [...items.slice(at + 1), ...items.slice(0, at + 1)].find((el) =>
      el.textContent?.trim().toLocaleLowerCase().startsWith(letter),
    );
    if (next) to = items.indexOf(next);
  }
  if (to === null) return;
  event.preventDefault();
  items[to].focus();
};
