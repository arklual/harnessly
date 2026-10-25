// The menu of a tab, opened at the pointer by a right click, a two-finger tap, a held finger or the menu key. It is the
// sidebar's menu on Base UI (the same look, keys and exit), placed on a point and not on a button.
import { Menu } from '@base-ui/react/menu';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';

/** Where the menu opens: a point of the window; a tab opened from the keys is its own box. */
export interface ITabMenuAt {
  tabId: string;
  x: number;
  y: number;
}

export interface ITabMenuProps {
  open: boolean;
  /** The tab the menu is of and the place it stands in (kept while the menu leaves). */
  at: ITabMenuAt | null;
  /** The strip's tabs by order, for what depends on where the tab stands. */
  ids: readonly string[];
  onOpenChange: (open: boolean) => void;
  onClose: (id: string) => void;
  onCloseOthers: (id: string) => void;
  onCloseRight: (id: string) => void;
  /** Where the focus goes when the menu has closed: the tab, when it is still there. */
  returnTo: (id: string) => HTMLElement | null;
}

const TabMenu = ({
  open,
  at,
  ids,
  onOpenChange,
  onClose,
  onCloseOthers,
  onCloseRight,
  returnTo,
}: ITabMenuProps) => {
  const menuActions = useMenuActions();
  if (!at) return null;
  const index = ids.indexOf(at.tabId);
  const anchor = {
    getBoundingClientRect: () => new DOMRect(at.x, at.y, 0, 0),
  };
  return (
    <Menu.Root open={open} onOpenChange={onOpenChange} actionsRef={menuActions}>
      <MenuPanel
        actions={menuActions}
        className="hx-cmenu"
        menu="tab"
        label={t('next.tabs.menu')}
        anchor={anchor}
        finalFocus={() => returnTo(at.tabId) ?? true}
      >
        <Menu.Item className="hx-cmenu__item" onClick={() => onClose(at.tabId)}>
          <Icon name="x" size="sm" />
          <span className="hx-cmenu__txt">{t('next.tabs.menuClose')}</span>
        </Menu.Item>
        <Menu.Item
          className="hx-cmenu__item"
          disabled={ids.length < 2}
          onClick={() => onCloseOthers(at.tabId)}
        >
          <Icon name="list-x" size="sm" />
          <span className="hx-cmenu__txt">{t('next.tabs.menuCloseOthers')}</span>
        </Menu.Item>
        <Menu.Item
          className="hx-cmenu__item"
          disabled={index < 0 || index === ids.length - 1}
          onClick={() => onCloseRight(at.tabId)}
        >
          <Icon name="arrow-right-to-line" size="sm" />
          <span className="hx-cmenu__txt">{t('next.tabs.menuCloseRight')}</span>
        </Menu.Item>
      </MenuPanel>
    </Menu.Root>
  );
};

export default TabMenu;
