// The kit screen while its canvas loads (HRN-897): the same head, tree, canvas and panel as bars, so a switch of theme
// or the first open never leaves the window empty. Bars appear after the shared delay, a fast answer does not flash.
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import { t } from '@/next/i18n';

const TREE_ROWS = 12;
const CARDS = 12;

const KitSkeleton = () => (
  <div className="hx-kit hx-kit--sk" role="status" aria-label={t('next.kit.loading')}>
    <Delayed>
      <div className="hx-kit__head">
        <Skeleton className="hx-kit-sk__btn" />
        <Skeleton className="hx-kit-sk__crumbs" />
        <Skeleton className="hx-kit-sk__themes" />
        <Skeleton className="hx-kit-sk__search" />
      </div>
      <div className="hx-kit__body">
        <div className="hx-kit__side hx-kit-sk__side">
          {Array.from({ length: TREE_ROWS }, (_, i) => (
            <Skeleton key={i} className="hx-kit-sk__row" />
          ))}
        </div>
        <div className="hx-kit__main hx-kit-sk__main">
          <Skeleton className="hx-kit-sk__title" />
          <div className="hx-kit-sk__grid">
            {Array.from({ length: CARDS }, (_, i) => (
              <Skeleton key={i} className="hx-kit-sk__card" />
            ))}
          </div>
        </div>
        <div className="hx-kit-sk__panel">
          <Skeleton className="hx-kit-sk__shot" />
          <Skeleton className="hx-kit-sk__line" />
          <Skeleton className="hx-kit-sk__line" />
        </div>
      </div>
    </Delayed>
  </div>
);

export default KitSkeleton;
