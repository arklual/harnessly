// The tools of a browser tab's bar, as the design's page has them: look at the page on a phone or a tablet, open the
// page in the person's own browser, sign in from the safe.
import { Menu } from '@base-ui/react/menu';
import {
  BROWSER_DEVICE,
  BROWSER_DEVICES,
  type IBrowserScreen,
  type TBrowserDevice,
} from '@/next/features/pane/browser-device';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
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
