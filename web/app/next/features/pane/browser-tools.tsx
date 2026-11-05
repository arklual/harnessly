// The tools of a browser tab's bar, as the design's page has them: pick an element and comment on it in the chat, look at
// the page on a phone or a tablet, open the page in the person's own browser. And the card a picked element gets.
import { Menu } from '@base-ui/react/menu';
import { useEffect, useRef, useState, type RefObject } from 'react';
import {
  BROWSER_DEVICE,
  BROWSER_DEVICES,
  type IBrowserScreen,
  type TBrowserDevice,
} from '@/next/features/pane/browser-device';
import type { IFramePicked } from '@/next/features/pane/frame-bridge';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const DEVICE_ICONS: Record<TBrowserDevice, string> = {
  fill: 'monitor',
  phone: 'smartphone',
  tablet: 'tablet',
};

interface IDeviceMenuProps {
  screen: IBrowserScreen;
  onScreen: (screen: IBrowserScreen) => void;
}

/** «Устройство»: the pane, a phone or a tablet, and «Повернуть» for the last two. */
export const DeviceMenu = ({ screen, onScreen }: IDeviceMenuProps) => {
  const actions = useMenuActions();
  const label = t('next.pane.device');
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        render={
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            data-active={screen.device !== BROWSER_DEVICE.fill || undefined}
            aria-label={label}
          >
            <Icon name={DEVICE_ICONS[screen.device]} size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {label}
            </span>
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" menu="device">
        {BROWSER_DEVICES.map((device) => (
          <Menu.Item
            key={device}
            className="hx-cmenu__item"
            role="menuitemradio"
            aria-checked={screen.device === device}
            onClick={() => onScreen({ device, turned: device === screen.device ? screen.turned : false })}
          >
            <Icon name={DEVICE_ICONS[device]} size="sm" />
            <span className="hx-cmenu__txt">{t(`next.pane.device.${device}`)}</span>
            <Icon name="check" size="sm" className="hx-cmenu__check" />
          </Menu.Item>
        ))}
        {screen.device !== BROWSER_DEVICE.fill && (
          <Menu.Item
            className="hx-cmenu__item"
            onClick={() => onScreen({ ...screen, turned: !screen.turned })}
          >
            <Icon name="rotate-cw" size="sm" />
            <span className="hx-cmenu__txt">{t('next.pane.device.turn')}</span>
          </Menu.Item>
        )}
      </MenuPanel>
    </Menu.Root>
  );
};

export interface IVaultView {
  entries: { id: string; login: string; label: string | null }[];
  /** A login is running: the menu waits for it. */
  busy: boolean;
  onLogin: (id: string) => void;
}

/** «Войти из сейфа»: the safe's entries for this site. Always a menu: the pick is the person's consent. */
export const VaultMenu = ({ entries, busy, onLogin }: IVaultView) => {
  const actions = useMenuActions();
  const label = t('next.pane.vault');
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        render={
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={label}
            aria-disabled={busy || undefined}
            data-active={busy || undefined}
          >
            <Icon name="key-round" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {label}
            </span>
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" menu="vault">
        {entries.map((entry) => (
          <Menu.Item
            key={entry.id}
            className="hx-cmenu__item"
            aria-disabled={busy || undefined}
            onClick={() => !busy && onLogin(entry.id)}
          >
            <Icon name="key-round" size="sm" />
            <span className="hx-cmenu__txt">
              {t('next.pane.vault.as', { login: entry.login })}
              {entry.label ? ` · ${entry.label}` : ''}
            </span>
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};

/** The card's exit, as long as hx-pick-out in pane.css. */
export const PICK_CARD_OUT_MS = 160;
const CARD_W = 300;
const GAP = 8;

interface IPickCardProps {
  /** The element the card is of; null closes it (with its exit). */
  picked: IFramePicked | null;
  /** The page's frame and how much its screen is scaled: the card stands under the element. */
  frame: RefObject<HTMLIFrameElement | null>;
  stage: RefObject<HTMLDivElement | null>;
  fit: number;
  onSend: (comment: string) => void;
  onCancel: () => void;
}

/** The card of a picked element: what it is, a comment, «Отправить в чат». */
export const PickCard = ({ picked, frame, stage, fit, onSend, onCancel }: IPickCardProps) => {
  const shown = usePresence(picked, PICK_CARD_OUT_MS);
  const [text, setText] = useState('');
  const card = useRef<HTMLFormElement>(null);
  const value = shown.value;

  // A new element starts with an empty comment.
  const [of, setOf] = useState<IFramePicked | null>(null);
  if (picked !== null && picked !== of) {
    setOf(picked);
    setText('');
  }

  useEffect(() => {
    const el = card.current;
    const box = stage.current?.getBoundingClientRect();
    const page = frame.current?.getBoundingClientRect();
    if (!el || !value || !box || !page) return;
    const r = value.rect;
    const below = page.top - box.top + (r.y + r.height) * fit + GAP;
    const above = page.top - box.top + r.y * fit - GAP - el.offsetHeight;
    const top =
      below + el.offsetHeight <= box.height || above < 0
        ? Math.min(below, box.height - el.offsetHeight - GAP)
        : above;
    const left = Math.max(GAP, Math.min(page.left - box.left + r.x * fit, box.width - CARD_W - GAP));
    el.style.setProperty('--x', `${Math.round(left)}px`);
    el.style.setProperty('--y', `${Math.round(Math.max(GAP, top))}px`);
  }, [value, fit, frame, stage]);

  if (!value) return null;
  const name = value.text ? `<${value.tag}> «${value.text}»` : `<${value.tag}> ${value.selector}`;
  return (
    <form
      ref={card}
      className="hx-pickcard"
      data-state={shown.state === PRESENCE.closing ? 'closing' : 'open'}
      aria-label={t('next.pane.pick.card')}
      onSubmit={(event) => {
        event.preventDefault();
        onSend(text);
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          onCancel();
        }
        if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
          event.preventDefault();
          onSend(text);
        }
      }}
    >
      <span className="hx-pickcard__what">
        <Icon name="square-mouse-pointer" size="sm" />
        <span className="hx-pickcard__name">{name}</span>
      </span>
      <textarea
        className="hx-pickcard__text"
        value={text}
        rows={3}
        autoFocus
        placeholder={t('next.pane.pick.placeholder')}
        onChange={(event) => setText(event.target.value)}
      />
      <span className="hx-pickcard__row">
        <button type="button" className="hx-pickcard__btn" onClick={onCancel}>
          {t('next.pane.pick.cancel')}
        </button>
        <button type="submit" className="hx-pickcard__btn" data-primary>
          {t('next.pane.pick.send')}
        </button>
      </span>
    </form>
  );
};

interface IMoreMenuProps {
  canForward: boolean;
  onForward: () => void;
  screen: IBrowserScreen;
  onScreen: (screen: IBrowserScreen) => void;
  outsideUrl: string | null;
  onOutside: () => void;
  /** «Закрепить» or «Открепить»; null when the page cannot be pinned. */
  pin: { pinned: boolean; onToggle: () => void } | null;
  onNewTab: () => void;
  /** «Инструменты разработчика»; null when the tab has no page. */
  devtools: { open: boolean; onToggle: () => void } | null;
}

/** The narrow bar's «…»: what the wide bar has as buttons, so the address keeps its room. */
export const MoreMenu = ({
  canForward,
  onForward,
  screen,
  onScreen,
  outsideUrl,
  onOutside,
  pin,
  onNewTab,
  devtools,
}: IMoreMenuProps) => {
  const actions = useMenuActions();
  const label = t('next.pane.more');
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-head-btn hx-tbrowser__narrow" aria-label={label}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" menu="browser-more">
        <Menu.Item className="hx-cmenu__item" disabled={!canForward} onClick={onForward}>
          <Icon name="arrow-right" size="sm" />
          <span className="hx-cmenu__txt">{t('next.site.forward')}</span>
        </Menu.Item>
        {BROWSER_DEVICES.map((device) => (
          <Menu.Item
            key={device}
            className="hx-cmenu__item"
            role="menuitemradio"
            aria-checked={screen.device === device}
            onClick={() => onScreen({ device, turned: false })}
          >
            <Icon name={DEVICE_ICONS[device]} size="sm" />
            <span className="hx-cmenu__txt">{t(`next.pane.device.${device}`)}</span>
            <Icon name="check" size="sm" className="hx-cmenu__check" />
          </Menu.Item>
        ))}
        {devtools && (
          <Menu.Item
            className="hx-cmenu__item"
            role="menuitemcheckbox"
            aria-checked={devtools.open}
            onClick={devtools.onToggle}
          >
            <Icon name="panel-bottom" size="sm" />
            <span className="hx-cmenu__txt">{t('next.site.devtools')}</span>
            <Icon name="check" size="sm" className="hx-cmenu__check" />
          </Menu.Item>
        )}
        {outsideUrl !== null && (
          <Menu.Item className="hx-cmenu__item" onClick={onOutside}>
            <Icon name="external-link" size="sm" />
            <span className="hx-cmenu__txt">{t('next.link.openOutside')}</span>
          </Menu.Item>
        )}
        {pin && (
          <Menu.Item className="hx-cmenu__item" onClick={pin.onToggle}>
            <Icon name={pin.pinned ? 'pin-off' : 'pin'} size="sm" />
            <span className="hx-cmenu__txt">{pin.pinned ? t('next.pane.unpin') : t('next.pane.pin')}</span>
          </Menu.Item>
        )}
        <Menu.Item className="hx-cmenu__item" onClick={onNewTab}>
          <Icon name="plus" size="sm" />
          <span className="hx-cmenu__txt">{t('next.pane.newBrowserTab')}</span>
        </Menu.Item>
      </MenuPanel>
    </Menu.Root>
  );
};
