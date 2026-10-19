import {
  type KeyboardEvent,
  type ReactElement,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';

import { cx } from './cx';
import { Popup } from './popup';
import type { Placement } from './position';

export interface MenuItem {
  id: string;
  label: ReactNode;
  icon?: ReactNode;
  hint?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

export type MenuEntry = MenuItem | 'separator';

export interface MenuProps {
  items: MenuEntry[];
  /** The element that opens the menu; it gets the ARIA wiring and the click handler. */
  trigger: (props: {
    ref: React.Ref<HTMLButtonElement>;
    onClick: () => void;
    'aria-haspopup': 'menu';
    'aria-expanded': boolean;
    'aria-controls': string | undefined;
  }) => ReactElement;
  placement?: Placement;
  label?: string;
}

/** A dropdown list of actions with arrow-key navigation, opened from a trigger. */
export function Menu({ items, trigger, placement = { side: 'bottom', align: 'start' }, label }: MenuProps) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const anchor = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const id = useId();
  const enabled = items.map((e, i) => (e !== 'separator' && !e.disabled ? i : -1)).filter((i) => i >= 0);

  const close = useCallback(() => {
    setOpen(false);
    setActive(-1);
  }, []);

  useEffect(() => {
    if (open) requestAnimationFrame(() => list.current?.focus());
  }, [open]);

  const choose = (i: number) => {
    const e = items[i];
    if (!e || e === 'separator' || e.disabled) return;
    close();
    anchor.current?.focus();
    e.onSelect();
  };

  const onKey = (ev: KeyboardEvent) => {
    const at = enabled.indexOf(active);
    const move = (to: number) => {
      ev.preventDefault();
      setActive(enabled[(to + enabled.length) % enabled.length] ?? -1);
    };
    if (ev.key === 'ArrowDown') move(at + 1);
    else if (ev.key === 'ArrowUp') move(at < 0 ? enabled.length - 1 : at - 1);
    else if (ev.key === 'Home') move(0);
    else if (ev.key === 'End') move(enabled.length - 1);
    else if (ev.key === 'Enter' || ev.key === ' ') {
      ev.preventDefault();
      choose(active);
    } else if (ev.key === 'Tab') close();
  };

  return (
    <>
      {trigger({
        ref: anchor,
        onClick: () => {
          if (open) return close();
          setActive(enabled[0] ?? -1);
          setOpen(true);
        },
        'aria-haspopup': 'menu',
        'aria-expanded': open,
        'aria-controls': open ? id : undefined,
      })}
      <Popup anchor={anchor} open={open} onClose={close} placement={placement} className="k-menu">
        <div
          ref={list}
          id={id}
          role="menu"
          aria-label={label}
          tabIndex={-1}
          aria-activedescendant={active >= 0 ? `${id}-${active}` : undefined}
          onKeyDown={onKey}
          className="k-menu__list"
        >
          {items.map((e, i) =>
            e === 'separator' ? (
              <div key={`sep-${i}`} role="separator" className="k-menu__separator" />
            ) : (
              <div
                key={e.id}
                id={`${id}-${i}`}
                role="menuitem"
                aria-disabled={e.disabled || undefined}
                data-active={i === active || undefined}
                data-danger={e.danger || undefined}
                className={cx('k-menu__item')}
                onPointerMove={() => !e.disabled && setActive(i)}
                onClick={() => choose(i)}
              >
                {e.icon != null && <span className="k-menu__icon">{e.icon}</span>}
                <span className="k-menu__label">{e.label}</span>
                {e.hint != null && <span className="k-menu__hint">{e.hint}</span>}
              </div>
            ),
          )}
        </div>
      </Popup>
    </>
  );
}
