// What the pages of the Apps screen have in common: the list that shows a first run of rows and more on demand, the
// empty answers, the loading skeleton and the note an app that cannot be read gives in place of its page.
import { useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import type { AppFeed } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { Skeleton } from '@/next/ui/skeleton';

/** Rows drawn at first, and added by each «Show more»: a long list stays light and its entrance short. */
export const PAGE_ROWS = 60;

interface IMoreListProps<T> {
  items: readonly T[];
  /** Changes when the query does: the list starts from its first run again. */
  sig: string;
  render: (item: T, index: number) => ReactNode;
}

export const MoreList = <T,>({ items, sig, render }: IMoreListProps<T>) => {
  const [grown, setGrown] = useState({ sig, rows: PAGE_ROWS });
  const rows = grown.sig === sig ? grown.rows : PAGE_ROWS;
  return (
    <>
      {items.slice(0, rows).map((item, index) => render(item, index))}
      {items.length > rows && (
        <div className="hx-hubmore">
          <Button size="sm" onClick={() => setGrown({ sig, rows: rows + PAGE_ROWS })}>
            {t('next.apps.more', { n: Math.min(PAGE_ROWS, items.length - rows) })}
          </Button>
        </div>
      )}
    </>
  );
};

interface INoteProps {
  icon: string;
  title: string;
  text: string;
  onRetry?: () => void;
  action?: ReactNode;
}

/** In place of the list: the app is not connected, the answer did not come, or the search found nothing. */
export const Note = ({ icon, title, text, onRetry, action }: INoteProps) => (
  <div className="hx-hubnote" role="status">
    <span className="hx-hubnote__icon" aria-hidden="true">
      <Icon name={icon} />
    </span>
    <span className="hx-hubnote__title">{title}</span>
    <p className="hx-hubnote__text">{text}</p>
    {onRetry && (
      <Button size="sm" icon="rotate-ccw" onClick={onRetry}>
        {t('next.apps.retry')}
      </Button>
    )}
    {action}
  </div>
);

/** What an app that is not ready says: no address, no token, a refused token, or no answer. */
export const appNote = (app: AppFeed, onRetry: () => void) => {
  const host = app.host ?? t(`next.apps.tab.${app.id}`);
  switch (app.reason) {
    case 'not-configured':
      return (
        <Note
          icon="app-window"
          title={t('next.apps.off.title')}
          text={t(`next.apps.off.notConfigured.${app.id}`)}
        />
      );
    case 'no-token':
      return (
        <Note
          icon="key-round"
          title={t('next.apps.off.title')}
          text={t(`next.apps.off.noToken.${app.id}`, { host })}
          onRetry={onRetry}
        />
      );
    case 'denied':
    case 'unreachable':
      return (
        <Note
          icon="triangle-alert"
          title={t('next.apps.failed.title')}
          text={t(`next.apps.failed.${app.reason}`, { host })}
          onRetry={onRetry}
        />
      );
    default:
      return null;
  }
};

/** The page while the machine is asked: bars where the toolbar and the first rows will be. */
export const PageSkeleton = () => (
  <div className="hx-hubsk" aria-hidden="true">
    <Skeleton className="hx-hubsk__bar" />
    <Skeleton className="hx-hubsk__row" />
    <Skeleton className="hx-hubsk__row" />
    <Skeleton className="hx-hubsk__row" />
    <Skeleton className="hx-hubsk__row" />
  </div>
);
