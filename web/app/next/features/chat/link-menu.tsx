// The menu of a link of the chat: «Открыть в панели», «Открыть в браузере», «Копировать ссылку». It opens at the
// pointer by a right click, a held finger or the menu key, as a tab's menu does (the same look, keys and exit; a bottom
// sheet on the phone).
import { Menu } from '@base-ui/react/menu';
import { useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent } from 'react';
import { openOutside } from '@/next/features/chat/pane-sites';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { toast } from '@/next/ui/toast';
import { afterTouchClick } from '@/next/ui/use-drag';
import { t } from '@/next/i18n';

/** A finger held this long on a link opens its menu. */
export const LINK_HOLD_MS = 450;
/** A finger that moves this far scrolls; it is no hold. */
const HOLD_SLOP_PX = 8;

interface IPoint {
  x: number;
  y: number;
}

export interface ILinkMenuActions {
  href: string;
  /** Opens it in the pane; null where there is no pane (the menu has no such item). */
  onPane: (() => void) | null;
  /** The person's browser can open it (not a server of the machine). */
  outside: boolean;
}

export const copyLink = (href: string): void => {
  void navigator.clipboard.writeText(href).then(
    () => toast(t('next.link.copied')),
    () => toast(t('next.shellData.noClipboard'), { tone: 'error' }),
  );
};

/**
 * What a link needs for its menu: the handlers to spread on it and the menu to draw beside it. `held()` says the click
 * that follows is the one a held finger ends with, which opens nothing.
 */
export const useLinkMenu = ({ href, onPane, outside }: ILinkMenuActions) => {
  const [at, setAt] = useState<IPoint | null>(null);
  const [open, setOpen] = useState(false);
  const hold = useRef<{ timer: number; x: number; y: number } | null>(null);
  const heldClick = useRef(false);
  const actions = useMenuActions();
  const anchorEl = useRef<HTMLElement | null>(null);

  const show = (point: IPoint) => {
    setAt(point);
    setOpen(true);
  };
  const cancelHold = () => {
    if (hold.current) window.clearTimeout(hold.current.timer);
    hold.current = null;
  };

  const handlers = {
    onContextMenu: (event: MouseEvent<HTMLElement>) => {
      event.preventDefault();
      cancelHold();
      anchorEl.current = event.currentTarget;
      const rect = event.currentTarget.getBoundingClientRect();
      // The menu key has no pointer: the menu opens under the link.
      const fromKeys = event.clientX === 0 && event.clientY === 0;
      show(fromKeys ? { x: rect.left + 8, y: rect.bottom } : { x: event.clientX, y: event.clientY });
    },
    onPointerDown: (event: PointerEvent<HTMLElement>) => {
      if (event.pointerType !== 'touch') return;
      cancelHold();
      const el = event.currentTarget;
      const { clientX: x, clientY: y } = event;
      const timer = window.setTimeout(() => {
        hold.current = null;
        heldClick.current = true;
        anchorEl.current = el;
        afterTouchClick(() => {
          heldClick.current = false;
          show({ x, y });
        });
      }, LINK_HOLD_MS);
      hold.current = { timer, x, y };
    },
    onPointerMove: (event: PointerEvent<HTMLElement>) => {
      const h = hold.current;
      if (h && Math.hypot(event.clientX - h.x, event.clientY - h.y) > HOLD_SLOP_PX) cancelHold();
    },
    onPointerUp: cancelHold,
    onPointerCancel: cancelHold,
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
      event.preventDefault();
      anchorEl.current = event.currentTarget;
      const rect = event.currentTarget.getBoundingClientRect();
      show({ x: rect.left + 8, y: rect.bottom });
    },
  };

  /** The click is the end of a held finger: it opens nothing (the menu opens instead). */
  const held = (): boolean => heldClick.current;

  const menu = at && (
    <Menu.Root open={open} onOpenChange={setOpen} actionsRef={actions}>
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        menu="link"
        label={t('next.link.menu')}
        anchor={{ getBoundingClientRect: () => new DOMRect(at.x, at.y, 0, 0) }}
        finalFocus={() => anchorEl.current ?? true}
      >
        {onPane && (
          <Menu.Item className="hx-cmenu__item" onClick={onPane}>
            <Icon name="panel-right" size="sm" />
            <span className="hx-cmenu__txt">{t('next.link.openPane')}</span>
          </Menu.Item>
        )}
        {outside && (
          <Menu.Item className="hx-cmenu__item" onClick={() => openOutside(href)}>
            <Icon name="external-link" size="sm" />
            <span className="hx-cmenu__txt">{t('next.link.openOutside')}</span>
          </Menu.Item>
        )}
        <Menu.Item className="hx-cmenu__item" onClick={() => copyLink(href)}>
          <Icon name="copy" size="sm" />
          <span className="hx-cmenu__txt">{t('next.link.copy')}</span>
        </Menu.Item>
      </MenuPanel>
    </Menu.Root>
  );

  return { handlers, held, menu };
};
