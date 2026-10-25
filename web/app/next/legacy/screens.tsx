// The screens of «More» the design names and Legacy already has: the statistics and the efficiency of the agents. They
// come over whole, in a page of the new interface; each loads only when it is opened, and a chunk that does not come
// says so with a retry.
import LazyView from '@/next/ui/lazy-view';
import { t } from '@/next/i18n';
import { Delayed, Skeleton } from '@/next/ui/skeleton';

export const LEGACY_SCREEN = { stats: 'stats', efficiency: 'efficiency' } as const;
export type TLegacyScreen = (typeof LEGACY_SCREEN)[keyof typeof LEGACY_SCREEN];

const LOADERS = {
  stats: () => import('@/components/agents/stats/stats-view'),
  efficiency: () => import('@/components/agents/efficiency/efficiency-view'),
} as const;

const NO_PROPS = {};

const LegacyScreen = ({ screen }: { screen: TLegacyScreen }) => (
  <section className="hx-lscreen" data-screen={screen}>
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
