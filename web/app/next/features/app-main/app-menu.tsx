// «…» of an app's screen: about the app, its settings, the debug section, turning the app off or on in this chat.
import { Menu } from '@base-ui/react/menu';
import Icon from '@/next/ui/icon';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { t } from '@/next/i18n';

export interface IAppMenuItem {
  id: string;
  icon: string;
  label: string;
  onSelect: () => void;
  danger?: boolean;
}

export const AppMenu = ({ items }: { items: readonly IAppMenuItem[] }) => {
  const actions = useMenuActions();
  if (items.length === 0) return null;
  return (
    <MenuRoot actionsRef={actions}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-head-btn" aria-label={t('next.appmain.menu')}>
            <Icon name="ellipsis" />
          </button>
        }
      />
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={t('next.appmain.menu')}
        side="bottom"
        align="end"
      >
        {items.map((item) => (
          <Menu.Item
            key={item.id}
            className="hx-cmenu__item"
            data-danger={item.danger || undefined}
            onClick={item.onSelect}
          >
            <Icon name={item.icon} size="sm" />
            <span className="hx-cmenu__txt">{item.label}</span>
          </Menu.Item>
        ))}
      </MenuPanel>
    </MenuRoot>
  );
};
