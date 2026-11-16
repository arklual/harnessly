// A menu that is a bottom sheet of our own on the phone (ui/sheet.tsx) and a Base UI menu on a wide window. Opening a
// Base UI menu on a phone is one long task of its machinery (the positioner, the focus manager, the list, the
// animations it waits for), whatever the items are: the «⋯» of a chat froze for 100–190 ms at CPU ×6. The
// sheet keeps what a menu gives: the roles menu and menuitem, the arrows and Home/End between the items, Esc, a tap
// outside and a swipe down to close, the focus inside while it is open and back on the trigger when it closes.
//
// The items are written once: MenuItem and MenuSeparator draw Base UI's parts inside a Base UI menu and plain buttons
// inside a SheetMenu.
import { Menu } from '@base-ui/react/menu';
import {
  cloneElement,
  createContext,
  useContext,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
  type MouseEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import '@/next/styles/menu.css';
import Sheet, { SHEET_EXIT_MS } from '@/next/ui/sheet';

interface ISheetMenuContext {
  close: () => void;
}

const SheetMenuContext = createContext<ISheetMenuContext | null>(null);

interface IMenuItemProps {
  className?: string;
  onClick?: (event: MouseEvent<HTMLElement>) => void;
  /** false: the menu stays open after the click (a second view, a question asked in the menu). */
  closeOnClick?: boolean;
  disabled?: boolean;
  'data-danger'?: boolean | 'true';
  /** A choice among several (a project, a group of the favorites): menuitemradio with aria-checked. */
  role?: 'menuitem' | 'menuitemradio';
  'aria-checked'?: boolean;
  children: ReactNode;
}

export const MenuItem = ({
  className,
  onClick,
  closeOnClick = true,
  disabled,
  role = 'menuitem',
  children,
  ...rest
}: IMenuItemProps) => {
  const sheet = useContext(SheetMenuContext);
  if (!sheet)
    return (
      <Menu.Item
        className={className}
        onClick={onClick}
        closeOnClick={closeOnClick}
        disabled={disabled}
        role={role}
        {...rest}
      >
        {children}
      </Menu.Item>
    );
  return (
    <button
      type="button"
      role={role}
      className={className}
      disabled={disabled}
      data-disabled={disabled || undefined}
      onClick={(event) => {
        onClick?.(event);
        if (closeOnClick) sheet.close();
      }}
      {...rest}
    >
      {children}
    </button>
  );
};

export const MenuSeparator = ({ className }: { className?: string }) => {
  const sheet = useContext(SheetMenuContext);
  if (!sheet) return <Menu.Separator className={className} />;
  return <div role="separator" className={className} />;
};

const ITEMS = ':is([role="menuitem"], [role="menuitemradio"]):not([disabled])';

/** Where a key moves the focus from the item `at` (-1: none) among `count`: round at the ends, as in Base UI's menu. */
const STEP: Record<string, (at: number, count: number) => number> = {
  ArrowDown: (at, count) => (at + 1) % count,
  ArrowUp: (at, count) => (at <= 0 ? count : at) - 1,
  Home: () => 0,
  End: (_at, count) => count - 1,
};

/** A key pressed in the sheet (on an item, or on the sheet itself before an item has the focus) moves it. */
const onMenuKey = (event: KeyboardEvent, list: HTMLElement) => {
  const step = STEP[event.key];
  const sheet = list.closest('.hx-sheet');
  if (!step || !sheet || !(event.target instanceof Node) || !sheet.contains(event.target)) return;
  const items = [...list.querySelectorAll<HTMLElement>(ITEMS)];
  if (items.length === 0) return;
  event.preventDefault();
  items[step(items.indexOf(document.activeElement as HTMLElement), items.length)].focus();
};

interface ISheetMenuProps {
  /** The button that opens the menu; it gets the menu's state on it like a Base UI trigger. */
  trigger: ReactElement;
  label: string;
  /** The classes of the sheet, after .hx-sheet: the menu's look (.hx-cmenu .hx-sheet-phone). */
  className: string;
  /** data-view of the list, for its look. */
  view?: string;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** The sheet has gone (its exit played). */
  onClosed?: () => void;
  children: ReactNode;
}

export const SheetMenu = ({
  trigger,
  label,
  className,
  view,
  defaultOpen = false,
  onOpenChange,
  onClosed,
  children,
}: ISheetMenuProps) => {
  const [open, setOpen] = useState(defaultOpen);
  const [list, setList] = useState<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!open || !list) return;
    const onKey = (event: KeyboardEvent) => onMenuKey(event, list);
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, list]);
  const gone = useEffectEvent(() => onClosed?.());
  const wasOpen = useRef(defaultOpen);
  useEffect(() => {
    if (open) {
      wasOpen.current = true;
      return;
    }
    if (!wasOpen.current) return;
    wasOpen.current = false;
    const timer = window.setTimeout(gone, SHEET_EXIT_MS);
    return () => window.clearTimeout(timer);
  }, [open]);
  const set = (next: boolean) => {
    setOpen(next);
    onOpenChange?.(next);
  };
  const context: ISheetMenuContext = { close: () => set(false) };
  const button = cloneElement(trigger, {
    onClick: (event: MouseEvent<HTMLElement>) => {
      (trigger.props as { onClick?: (event: MouseEvent<HTMLElement>) => void }).onClick?.(event);
      set(true);
    },
    'aria-haspopup': 'menu',
    'aria-expanded': open,
    'data-popup-open': open ? '' : undefined,
  } as Record<string, unknown>);
  return (
    <>
      {button}
      <Sheet open={open} onClose={context.close} label={label} className={className}>
        <SheetMenuContext.Provider value={context}>
          <div role="menu" aria-label={label} className="hx-cmenu__list" data-view={view} ref={setList}>
            {children}
          </div>
        </SheetMenuContext.Provider>
      </Sheet>
    </>
  );
};
