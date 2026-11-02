// The screens of «More» the design names and Legacy already has: the statistics and the efficiency of the agents. They
// come over whole, in a page of the new interface; each loads only when it is opened, and a chunk that does not come
// says so with a retry.
import LazyView from '@/next/ui/lazy-view';
import { t } from '@/next/i18n';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import type { TRoute } from '@/next/routes/routes';
import ManageTabs from '@/next/features/manage/tabs';

export const LEGACY_SCREEN = { stats: 'stats', efficiency: 'efficiency' } as const;

// Kept outside the component: LazyView keeps its lazy component per this function, so a new one on each render would
// remount the screen every time the shell draws, with the fallback and every request over again.
const loadManage = () => import('@/next/legacy/manage/manage-view');

/** «Управление»: the personal page of the hub era; Statistics and Efficiency sit beside it as its tabs. */
export const ManageScreen = ({ onOpen }: { onOpen: (route: TRoute) => void }) => (
  <section className="hx-lscreen" data-screen="manage">
    <LazyView
      load={loadManage}
      props={{ onOpen }}
      title={t('next.load.screen')}
      size="page"
      fallback={
        <Delayed>
          <Skeleton className="hx-lscreen__sk" />
        </Delayed>
      }
    />
  </section>
);
export type TLegacyScreen = (typeof LEGACY_SCREEN)[keyof typeof LEGACY_SCREEN];

// Each screen brings the old stylesheets along (styles/legacy.ts), the page does not load them.
const LOADERS = {
  stats: () => import('@/styles/legacy').then(() => import('@/components/agents/stats/stats-view')),
  efficiency: () =>
    import('@/styles/legacy').then(() => import('@/components/agents/efficiency/efficiency-view')),
} as const;

const NO_PROPS = {};

const LegacyScreen = ({ screen, onOpen }: { screen: TLegacyScreen; onOpen: (route: TRoute) => void }) => (
  <section className="hx-lscreen" data-screen={screen}>
    <ManageTabs current={screen} onOpen={onOpen} />
    <LazyView
      key={screen}
      load={LOADERS[screen]}
      props={NO_PROPS}
      title={t('next.load.screen')}
      size="page"
      fallback={
        <Delayed>
          <Skeleton className="hx-lscreen__sk" />
        </Delayed>
      }
    />
  </section>
);

export default LegacyScreen;
