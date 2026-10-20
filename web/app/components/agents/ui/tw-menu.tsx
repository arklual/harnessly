import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import { cx, focusElement, scrollIntoViewIfNeeded } from './dom';
import { TwCheckIcon, TwChevronLeftIcon, TwChevronRightIcon } from './icons';
import { LAYER_ALIGN, LAYER_SIDE, type TLayerAlign, type TLayerSide } from './placement';
import {
  EMPTY_TYPEAHEAD,
  firstNavigable,
  isTypeaheadKey,
  lastNavigable,
  MENU_INTENT,
  menuKeyIntent,
  NAV_ORIENTATION,
  nextIndex,
  typeaheadIndex,
  typeaheadStep,
} from './roving-focus';
import { LAYER_KIND, TwLayer } from './tw-layer';
import type { ITwPoint, TTwAnchor } from './use-anchored-position';
import { DISMISS_REASON } from './use-dismissable';

export const TW_MENU_KIND = {
  action: 'action',
  radio: 'radio',
  checkbox: 'checkbox',
  submenu: 'submenu',
  separator: 'separator',
  label: 'label',
} as const;

export const MENU_CLOSE_REASON = {
  ...DISMISS_REASON,
  select: 'select',
  tab: 'tab',
} as const;

export type TMenuCloseReason = (typeof MENU_CLOSE_REASON)[keyof typeof MENU_CLOSE_REASON];

export const MENU_INITIAL_FOCUS = {
  first: 'first',
  last: 'last',
  /** Focus the menu itself, no row highlighted (opened with a pointer). */
  menu: 'menu',
  /** Leave focus where it is (a submenu opened by hover keeps focus on its parent row). */
  none: 'none',
} as const;

type TMenuInitialFocus = (typeof MENU_INITIAL_FOCUS)[keyof typeof MENU_INITIAL_FOCUS];

interface ITwMenuItemCommon {
  id: string;
  label: string;
  icon?: ReactNode;
  /** Secondary line under the label. */
  hint?: string;
  disabled?: boolean;
  /** Why the item is unavailable. Shown under the label (touch has no hover) and announced. */
  disabledReason?: string;
}

export interface ITwMenuAction extends ITwMenuItemCommon {
  kind: typeof TW_MENU_KIND.action;
  onSelect: () => void;
  /** Display only, e.g. "⌘1". */
  shortcut?: string;
  danger?: boolean;
  keepOpen?: boolean;
}

export interface ITwMenuToggle extends ITwMenuItemCommon {
  kind: typeof TW_MENU_KIND.radio | typeof TW_MENU_KIND.checkbox;
  checked: boolean;
  onSelect: () => void;
  keepOpen?: boolean;
}

export interface ITwMenuSubmenu extends ITwMenuItemCommon {
  kind: typeof TW_MENU_KIND.submenu;
  items: readonly TTwMenuEntry[];
}

export interface ITwMenuSeparator {
  kind: typeof TW_MENU_KIND.separator;
  id: string;
}

export interface ITwMenuLabel {
  kind: typeof TW_MENU_KIND.label;
  id: string;
  label: string;
}

export type TTwMenuEntry = ITwMenuAction | ITwMenuToggle | ITwMenuSubmenu | ITwMenuSeparator | ITwMenuLabel;

type TTwMenuItem = ITwMenuAction | ITwMenuToggle | ITwMenuSubmenu;

const isItem = (entry: TTwMenuEntry): entry is TTwMenuItem =>
  entry.kind !== TW_MENU_KIND.separator && entry.kind !== TW_MENU_KIND.label;

const ROLE_BY_KIND = {
  [TW_MENU_KIND.action]: 'menuitem',
  [TW_MENU_KIND.submenu]: 'menuitem',
  [TW_MENU_KIND.radio]: 'menuitemradio',
  [TW_MENU_KIND.checkbox]: 'menuitemcheckbox',
} as const;

/** Hover intent before a submenu opens or closes, so crossing a row diagonally does not flicker. */
const SUBMENU_HOVER_MS = 140;

interface IOpenSubmenu {
  id: string;
  focus: TMenuInitialFocus;
}

interface IMenuSurfaceProps {
  items: readonly TTwMenuEntry[];
  anchor: TTwAnchor;
  side: TLayerSide;
  align: TLayerAlign;
  label: string;
  initialFocus: TMenuInitialFocus;
  rootTriggerRef?: RefObject<HTMLElement | null>;
  /** Presses on it are inside this level: the root trigger, or the parent row of a submenu. */
  triggerRef?: RefObject<HTMLElement | null>;
  onCloseAll: (reason: TMenuCloseReason) => void;
  /** Present on submenus: close only this level and return focus to its parent row. */
  onCloseSelf?: () => void;
  /** Present on submenus: an outside press closes only this level, focus stays put. */
  onDismissSelf?: () => void;
  parentLabel?: string;
  id?: string;
  className?: string;
}

const MenuSurface = ({
  items,
  anchor,
  side,
  align,
  label,
  initialFocus,
  rootTriggerRef,
  triggerRef,
  onCloseAll,
  onCloseSelf,
  onDismissSelf,
  parentLabel,
  id,
  className,
}: IMenuSurfaceProps) => {
  const layerRef = useRef<HTMLDivElement>(null);
  const itemRefs = useRef(new Map<string, HTMLElement>());
  const submenuAnchorRef = useRef<HTMLElement | null>(null);
  const hoverTimer = useRef(0);
  const typeahead = useRef(EMPTY_TYPEAHEAD);
  const itemIndexes = items.flatMap((entry, index) => (isItem(entry) ? [index] : []));
  const rows = itemIndexes.map((index) => items[index] as TTwMenuItem);
  const [active, setActive] = useState(() => {
    if (initialFocus === MENU_INITIAL_FOCUS.first) return firstNavigable(rows.length);
    if (initialFocus === MENU_INITIAL_FOCUS.last) return lastNavigable(rows.length);
    return -1;
  });
  const [submenu, setSubmenu] = useState<IOpenSubmenu | null>(null);
  const isSubmenu = Boolean(onCloseSelf);
  const submenuId = useId();
  const keepFocusOnMount = useRef(initialFocus === MENU_INITIAL_FOCUS.none);
  const activeId = rows[active]?.id ?? null;

  useLayoutEffect(() => {
    if (activeId === null) {
      if (keepFocusOnMount.current) keepFocusOnMount.current = false;
      else focusElement(layerRef.current);
      return;
    }
    keepFocusOnMount.current = false;
    const element = itemRefs.current.get(activeId) ?? null;
    focusElement(element);
    scrollIntoViewIfNeeded(element);
  }, [activeId]);

  useEffect(() => () => window.clearTimeout(hoverTimer.current), []);

  const openSubmenu = (row: ITwMenuSubmenu, focus: TMenuInitialFocus) => {
    window.clearTimeout(hoverTimer.current);
    submenuAnchorRef.current = itemRefs.current.get(row.id) ?? null;
    setSubmenu({ id: row.id, focus });
  };

  const closeSubmenu = () => {
    window.clearTimeout(hoverTimer.current);
    setSubmenu(null);
  };

  const activate = (row: TTwMenuItem) => {
    if (row.disabled) return;
    if (row.kind === TW_MENU_KIND.submenu) {
      openSubmenu(row, MENU_INITIAL_FOCUS.first);
      return;
    }
    row.onSelect();
    if (!row.keepOpen) onCloseAll(MENU_CLOSE_REASON.select);
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.nativeEvent.isComposing) return;
    const row = rows[active];
    const intent = menuKeyIntent(event.key, {
      isSubmenu,
      itemHasSubmenu: row?.kind === TW_MENU_KIND.submenu && !row.disabled,
    });
    if (intent === MENU_INTENT.move) {
      event.preventDefault();
      const index = nextIndex(active, event.key, {
        count: rows.length,
        orientation: NAV_ORIENTATION.vertical,
        loop: true,
      });
      if (index !== null && index !== -1) setActive(index);
      return;
    }
    if (intent === MENU_INTENT.activate) {
      event.preventDefault();
      if (row) activate(row);
      return;
    }
    if (intent === MENU_INTENT.openSubmenu && row?.kind === TW_MENU_KIND.submenu) {
      event.preventDefault();
      openSubmenu(row, MENU_INITIAL_FOCUS.first);
      return;
    }
    if (intent === MENU_INTENT.closeSubmenu) {
      event.preventDefault();
      onCloseSelf?.();
      return;
    }
    if (intent === MENU_INTENT.close) {
      event.preventDefault();
      onCloseAll(MENU_CLOSE_REASON.escape);
      return;
    }
    if (intent === MENU_INTENT.tab) {
      const trigger = rootTriggerRef?.current;
      if (trigger) trigger.focus();
      else event.preventDefault();
      onCloseAll(MENU_CLOSE_REASON.tab);
      return;
    }
    const now = event.timeStamp;
    if (!isTypeaheadKey(event, typeahead.current, now)) return;
    event.preventDefault();
    typeahead.current = typeaheadStep(typeahead.current, event.key, now);
    const match = typeaheadIndex(
      rows.map((item) => item.label),
      typeahead.current.query,
      active,
    );
    if (match !== -1) setActive(match);
  };

  const onRowPointerMove = (event: ReactPointerEvent<HTMLElement>, row: TTwMenuItem, index: number) => {
    if (event.pointerType === 'touch') return;
    if (index !== active) setActive(index);
    window.clearTimeout(hoverTimer.current);
    if (row.kind === TW_MENU_KIND.submenu && !row.disabled && submenu?.id !== row.id) {
      hoverTimer.current = window.setTimeout(
        () => openSubmenu(row, MENU_INITIAL_FOCUS.none),
        SUBMENU_HOVER_MS,
      );
    } else if (submenu && submenu.id !== row.id) {
      hoverTimer.current = window.setTimeout(closeSubmenu, SUBMENU_HOVER_MS);
    }
  };

  const onRowPointerLeave = (event: ReactPointerEvent<HTMLElement>, row: TTwMenuItem) => {
    if (event.pointerType === 'touch' || submenu?.id === row.id) return;
    window.clearTimeout(hoverTimer.current);
    setActive(-1);
  };

  const onRowClick = (row: TTwMenuItem, index: number) => {
    setActive(index);
    if (row.kind === TW_MENU_KIND.submenu && submenu?.id === row.id) {
      closeSubmenu();
      return;
    }
    activate(row);
  };

  const openRow = submenu ? rows.find((row) => row.id === submenu.id) : undefined;

  return (
    <TwLayer
      open
      anchor={anchor}
      triggerRef={triggerRef}
      onDismiss={(reason) => (onDismissSelf ? onDismissSelf() : onCloseAll(reason))}
      kind={LAYER_KIND.menu}
      side={side}
      align={align}
      offset={isSubmenu ? -2 : 6}
      layerRef={layerRef}
      escape={false}
      focusOutside={!isSubmenu}
      restoreFocus={!isSubmenu}
      id={id}
      role="menu"
      aria-label={label}
      aria-orientation="vertical"
      tabIndex={-1}
      className={cx('tw-ui-menu', isSubmenu && 'tw-ui-submenu', className)}
      onKeyDown={onKeyDown}
      onContextMenu={(event) => event.preventDefault()}
    >
      {parentLabel && (
        <button type="button" tabIndex={-1} className="tw-ui-menu-back" onClick={() => onCloseSelf?.()}>
          <TwChevronLeftIcon />
          <span>{parentLabel}</span>
        </button>
      )}
      {items.map((entry) => {
        if (entry.kind === TW_MENU_KIND.separator)
          return <div key={entry.id} role="separator" className="tw-ui-menu-separator" />;
        if (entry.kind === TW_MENU_KIND.label) {
          return (
            <div key={entry.id} role="presentation" className="tw-ui-menu-label">
              {entry.label}
            </div>
          );
        }
        const index = rows.indexOf(entry);
        const isOpen = submenu?.id === entry.id;
        const reasonId =
          entry.disabled && entry.disabledReason ? `${submenuId}-${entry.id}-reason` : undefined;
        const checked =
          entry.kind === TW_MENU_KIND.radio || entry.kind === TW_MENU_KIND.checkbox
            ? entry.checked
            : undefined;
        return (
          <div
            key={entry.id}
            ref={(element) => {
              if (element) itemRefs.current.set(entry.id, element);
              else itemRefs.current.delete(entry.id);
            }}
            role={ROLE_BY_KIND[entry.kind]}
            tabIndex={index === active ? 0 : -1}
            aria-disabled={entry.disabled || undefined}
            aria-checked={checked}
            aria-haspopup={entry.kind === TW_MENU_KIND.submenu ? 'menu' : undefined}
            aria-expanded={entry.kind === TW_MENU_KIND.submenu ? isOpen : undefined}
            aria-controls={isOpen ? submenuId : undefined}
            aria-describedby={reasonId}
            data-active={index === active ? '' : undefined}
            data-danger={entry.kind === TW_MENU_KIND.action && entry.danger ? '' : undefined}
            className="tw-ui-menu-item"
            onPointerMove={(event) => onRowPointerMove(event, entry, index)}
            onPointerLeave={(event) => onRowPointerLeave(event, entry)}
            onClick={() => onRowClick(entry, index)}
          >
            <span className="tw-ui-menu-icon" aria-hidden="true">
              {checked === undefined ? entry.icon : checked && <TwCheckIcon />}
            </span>
            <span className="tw-ui-menu-text">
              <span className="tw-ui-menu-title">{entry.label}</span>
              {entry.hint && <span className="tw-ui-menu-hint">{entry.hint}</span>}
              {reasonId && (
                <span id={reasonId} className="tw-ui-menu-reason">
                  {entry.disabledReason}
                </span>
              )}
            </span>
            {entry.kind === TW_MENU_KIND.action && entry.shortcut && (
              <kbd className="tw-ui-kbd">{entry.shortcut}</kbd>
            )}
            {entry.kind === TW_MENU_KIND.submenu && (
              <span className="tw-ui-menu-chevron" aria-hidden="true">
                <TwChevronRightIcon />
              </span>
            )}
          </div>
        );
      })}
      {openRow?.kind === TW_MENU_KIND.submenu && submenu && (
        <MenuSurface
          key={openRow.id}
          id={submenuId}
          items={openRow.items}
          anchor={submenuAnchorRef}
          side={LAYER_SIDE.right}
          align={LAYER_ALIGN.start}
          label={openRow.label}
          parentLabel={openRow.label}
          initialFocus={submenu.focus}
          rootTriggerRef={rootTriggerRef}
          triggerRef={submenuAnchorRef}
          onCloseAll={onCloseAll}
          onCloseSelf={() => {
            closeSubmenu();
            focusElement(itemRefs.current.get(openRow.id));
          }}
          onDismissSelf={closeSubmenu}
        />
      )}
    </TwLayer>
  );
};

export interface ITwMenuProps {
  open: boolean;
  onClose: (reason: TMenuCloseReason) => void;
  /** The trigger element, or a pointer position for a context menu. */
  anchor: TTwAnchor;
  items: readonly TTwMenuEntry[];
  'aria-label': string;
  /** Receives focus back on close; presses on it are not "outside". */
  triggerRef?: RefObject<HTMLElement | null>;
  side?: TLayerSide;
  align?: TLayerAlign;
  initialFocus?: TMenuInitialFocus;
  id?: string;
  className?: string;
}

/**
 * Menu with submenus: roving tabindex, arrows, Home/End, PageUp/PageDown, typeahead, Enter/Space,
 * ArrowRight/ArrowLeft for submenus, Escape one level at a time, Tab closes. Disabled rows stay
 * reachable and show why they are disabled. On touch phones it is a bottom sheet (CSS).
 */
export const TwMenu = ({
  open,
  onClose,
  anchor,
  items,
  'aria-label': ariaLabel,
  triggerRef,
  side = LAYER_SIDE.bottom,
  align = LAYER_ALIGN.start,
  initialFocus = MENU_INITIAL_FOCUS.first,
  id,
  className,
}: ITwMenuProps) => {
  if (!open) return null;
  return (
    <MenuSurface
      items={items}
      anchor={anchor}
      side={side}
      align={align}
      label={ariaLabel}
      initialFocus={initialFocus}
      rootTriggerRef={triggerRef}
      triggerRef={triggerRef}
      onCloseAll={onClose}
      id={id}
      className={className}
    />
  );
};

export interface ITwMenuButtonProps extends Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  'onClick' | 'type'
> {
  items: readonly TTwMenuEntry[];
  menuLabel: string;
  children: ReactNode;
  side?: TLayerSide;
  align?: TLayerAlign;
  menuClassName?: string;
}

/** A button that opens a TwMenu. ArrowDown/Enter/Space focus the first row, ArrowUp the last. */
export const TwMenuButton = ({
  items,
  menuLabel,
  children,
  side,
  align,
  menuClassName,
  className,
  disabled,
  onKeyDown,
  ...rest
}: ITwMenuButtonProps) => {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [focus, setFocus] = useState<TMenuInitialFocus | null>(null);
  const menuId = useId();
  const open = focus !== null;

  return (
    <>
      <button
        {...rest}
        ref={triggerRef}
        type="button"
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        className={cx('tw-ui-menu-trigger', className)}
        onClick={(event) => {
          event.stopPropagation();
          setFocus((current) => {
            if (current) return null;
            return event.detail === 0 ? MENU_INITIAL_FOCUS.first : MENU_INITIAL_FOCUS.menu;
          });
        }}
        onKeyDown={(event) => {
          onKeyDown?.(event);
          if (event.defaultPrevented) return;
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault();
            setFocus(event.key === 'ArrowDown' ? MENU_INITIAL_FOCUS.first : MENU_INITIAL_FOCUS.last);
          }
        }}
      >
        {children}
      </button>
      {open && !disabled && (
        <TwMenu
          open
          onClose={() => setFocus(null)}
          anchor={triggerRef}
          triggerRef={triggerRef}
          items={items}
          aria-label={menuLabel}
          side={side}
          align={align}
          initialFocus={focus ?? MENU_INITIAL_FOCUS.first}
          id={menuId}
          className={menuClassName}
        />
      )}
    </>
  );
};

/** Long press on touch that opens the context menu (iOS Safari has no contextmenu event). */
const LONG_PRESS_MS = 500;
const LONG_PRESS_SLOP_PX = 10;

interface IContextMenuState {
  point: ITwPoint;
  focus: TMenuInitialFocus;
}

export interface IUseTwContextMenuOptions {
  items: readonly TTwMenuEntry[];
  'aria-label': string;
  disabled?: boolean;
  className?: string;
}

/**
 * Context menu for any element: right click, the context-menu key / Shift+F10, and a long press
 * on touch. Spread `bind` on the target element and render `menu` anywhere next to it.
 */
export const useTwContextMenu = ({
  items,
  'aria-label': ariaLabel,
  disabled = false,
  className,
}: IUseTwContextMenuOptions) => {
  const [state, setState] = useState<IContextMenuState | null>(null);
  const press = useRef<{ x: number; y: number; timer: number; fired: boolean } | null>(null);

  const cancelPress = () => {
    if (press.current) window.clearTimeout(press.current.timer);
  };

  useEffect(() => {
    const pending = press;
    return () => {
      if (pending.current) window.clearTimeout(pending.current.timer);
    };
  }, []);

  const bind = {
    'data-tw-context-menu': '',
    onContextMenu: (event: ReactMouseEvent<HTMLElement>) => {
      if (disabled) return;
      event.preventDefault();
      event.stopPropagation();
      if (press.current?.fired) return;
      const fromKeyboard = event.clientX === 0 && event.clientY === 0;
      if (fromKeyboard) {
        const rect = event.currentTarget.getBoundingClientRect();
        setState({ point: { x: rect.left + 12, y: rect.bottom }, focus: MENU_INITIAL_FOCUS.first });
        return;
      }
      setState({ point: { x: event.clientX, y: event.clientY }, focus: MENU_INITIAL_FOCUS.menu });
    },
    onPointerDown: (event: ReactPointerEvent<HTMLElement>) => {
      cancelPress();
      if (disabled || event.pointerType !== 'touch') {
        press.current = null;
        return;
      }
      const { clientX: x, clientY: y } = event;
      const timer = window.setTimeout(() => {
        if (!press.current) return;
        press.current.fired = true;
        setState({ point: { x, y }, focus: MENU_INITIAL_FOCUS.menu });
      }, LONG_PRESS_MS);
      press.current = { x, y, timer, fired: false };
    },
    onPointerMove: (event: ReactPointerEvent<HTMLElement>) => {
      const current = press.current;
      if (!current || current.fired) return;
      if (Math.hypot(event.clientX - current.x, event.clientY - current.y) > LONG_PRESS_SLOP_PX)
        cancelPress();
    },
    onPointerUp: cancelPress,
    onPointerCancel: cancelPress,
    onClickCapture: (event: ReactMouseEvent<HTMLElement>) => {
      if (!press.current?.fired) return;
      press.current = null;
      event.preventDefault();
      event.stopPropagation();
    },
  } as const;

  const menu = state && (
    <TwMenu
      open
      onClose={() => {
        press.current = null;
        setState(null);
      }}
      anchor={state.point}
      items={items}
      aria-label={ariaLabel}
      initialFocus={state.focus}
      className={className}
    />
  );

  return { bind, menu, open: state !== null } as const;
};
