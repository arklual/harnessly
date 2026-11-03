// The product name at the top of the sidebar as a switcher between the two products of the platform: the hub (team,
// spend, machines) and this workspace. The hub row is a real link, so it opens in a new tab like any other.
import { Menu } from '@base-ui/react/menu';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';

interface IProductSwitcherProps {
  brand: string;
  /** Where «Хаб» leads. */
  hubHref: string;
}

const ProductSwitcher = ({ brand, hubHref }: IProductSwitcherProps) => {
  const menuActions = useMenuActions();
  return (
    <Menu.Root actionsRef={menuActions}>
      <Menu.Trigger className="hx-brand__name hx-brand__switch" aria-label={t('next.sidebar.switcher')}>
        <span>{brand}</span>
        <Icon name="chevrons-up-down" size="sm" className="hx-brand__chev" />
      </Menu.Trigger>
      <MenuPanel
        actions={menuActions}
        className="hx-cmenu hx-cmenu--switch"
        label={t('next.sidebar.switcher')}
        side="bottom"
        align="start"
        sideOffset={6}
      >
        <Menu.LinkItem className="hx-cmenu__item hx-cmenu__item--two" href={hubHref}>
          <Icon name="layout-dashboard" size="sm" />
          <span className="hx-cmenu__txt">
            {t('next.sidebar.hub')}
            <span className="hx-cmenu__hint">{t('next.sidebar.hubHint')}</span>
          </span>
        </Menu.LinkItem>
        <Menu.Item className="hx-cmenu__item hx-cmenu__item--two" aria-current="page">
          <Icon name="messages-square" size="sm" />
          <span className="hx-cmenu__txt">
            {t('next.sidebar.workspace')}
            <span className="hx-cmenu__hint">{t('next.sidebar.workspaceCurrent')}</span>
          </span>
          <Icon name="check" size="sm" className="hx-cmenu__check" />
        </Menu.Item>
      </MenuPanel>
    </Menu.Root>
  );
};

export default ProductSwitcher;
