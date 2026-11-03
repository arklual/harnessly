// The three parts of «Управление»: the personal history, and the two screens it grew from.
import { ROUTE, type TRoute } from '@/next/routes/routes';
import { t } from '@/next/i18n';

const TABS: readonly { id: string; route: TRoute; label: () => string }[] = [
  { id: ROUTE.manage, route: { kind: ROUTE.manage }, label: () => t('next.manage.tab.history') },
  { id: ROUTE.stats, route: { kind: ROUTE.stats }, label: () => t('next.sidebar.stats') },
  { id: ROUTE.efficiency, route: { kind: ROUTE.efficiency }, label: () => t('next.sidebar.efficiency') },
];

const ManageTabs = ({ current, onOpen }: { current: string; onOpen: (route: TRoute) => void }) => {
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
    </nav>
  );
};

export default ManageTabs;
