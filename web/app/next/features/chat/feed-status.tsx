// What the feed says about how much of the conversation it holds: the conversation is on its way (a ring and a line
// in the middle, never a skeleton that looks like turns), older turns are on their way (a small ring at the top),
// there is nothing older (a quiet «Начало диалога»), and the feed on screen is a copy the runtime is still bringing
// up to date (a thin bar over the feed, so a cached feed never passes for the whole conversation).
import { usePresentFlag } from '@/next/ui/presence';
import { useDelayed } from '@/next/ui/skeleton';
import Spinner from '@/next/ui/spinner';
import { t } from '@/next/i18n';

/** How long the bar of a feed that catches up takes to leave. */
export const CATCHING_UP_EXIT_MS = 240;
/** The bar comes only after this: a catch-up that ends at once does not flash it. */
export const CATCHING_UP_DELAY_MS = 300;

/** The conversation is not read yet; shown after a moment, so a fast answer does not flash. */
export const FeedLoading = () =>
  useDelayed(true) ? (
    <div className="hx-feed__loading" role="status" data-busy="true">
      <Spinner />
      <span>{t('next.feed.loading')}</span>
    </div>
  ) : null;

/** Older turns are on their way. */
export const OlderLoading = () => (
  <span
    className="hx-feed__olderspin"
    role="status"
    aria-label={t('next.feed.olderLoading')}
    data-busy="true"
  >
    <Spinner />
  </span>
);

/** Nothing is older than the first turn on screen. */
export const FeedStart = () => <div className="hx-feed__start">{t('next.feed.start')}</div>;

/** A thin bar at the top of the feed while it catches up; it fades out when the runtime has said everything. */
export const CatchingUp = ({ on }: { on: boolean }) => {
  const shown = useDelayed(on, CATCHING_UP_DELAY_MS);
  const { mounted, state } = usePresentFlag(shown, CATCHING_UP_EXIT_MS);
  if (!mounted) return null;
  return (
    <div
      className="hx-feed__progress"
      data-state={state}
      role="progressbar"
      aria-label={t('next.feed.catchingUp')}
    />
  );
};
