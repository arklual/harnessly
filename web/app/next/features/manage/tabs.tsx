// The three parts of «Управление»: the personal history, and the two screens it grew from.
import { ROUTE, type TRoute } from '@/next/routes/routes';
import { hubHref } from '@/next/features/manage/api';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

const TABS: readonly { id: string; route: TRoute; label: () => string }[] = [
  { id: ROUTE.manage, route: { kind: ROUTE.manage }, label: () => t('next.manage.tab.history') },
  { id: ROUTE.stats, route: { kind: ROUTE.stats }, label: () => t('next.sidebar.stats') },
  { id: ROUTE.efficiency, route: { kind: ROUTE.efficiency }, label: () => t('next.sidebar.efficiency') },
];

const ManageTabs = ({ current, onOpen }: { current: string; onOpen: (route: TRoute) => void }) => {
  const hub = hubHref();
  return (
    <nav className="hx-mg-tabs" aria-label={t('next.manage.title')}>
      {TABS.map((tab) => (
        <button
          key={tab.id}
          type="button"
          className="hx-mg-tab"
          aria-current={tab.id === current ? 'page' : undefined}
          onClick={() => onOpen(tab.route)}
        >
          {tab.label()}
        </button>
      ))}
      {hub && (
        <a className="hx-mg-tab hx-mg-tab--hub" href={hub}>
          {t('next.sidebar.hub')}
          <Icon name="arrow-up-right" size="sm" />
        </a>
      )}
    </nav>
  );
};

export default ManageTabs;
