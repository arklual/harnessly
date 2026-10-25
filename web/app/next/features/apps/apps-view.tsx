// The Apps screen of «More» (after the design's settings frame, .hx-set): the connected apps in a list on the left, the
// chosen one open as a whole page on the right — its own search, filters and settings. On a phone the list is the screen
// and the page slides over it. A row is picked with its check; the bar under the list takes the picked ones into a new
// chat, and so does the row's own «To chat». An app the machine cannot read says why instead of an empty list.
import '@/next/styles/apps.css';

import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import GitlabPage from '@/next/features/apps/gitlab-page';
import { appNote, Note, PageSkeleton } from '@/next/features/apps/list';
import {
  appIcon,
  appsOf,
  barText,
  DEFAULT_MR_QUERY,
  DEFAULT_TRACKER_QUERY,
  rowsOf,
  type IMrQuery,
  type ITrackerQuery,
} from '@/next/features/apps/model';
import { type IPageHeadProps } from '@/next/features/apps/page-head';
import { SearchField } from '@/next/features/apps/controls';
import { usePrefs } from '@/next/features/apps/prefs';
import TrackerPage from '@/next/features/apps/tracker-page';
import { t } from '@/next/i18n';
import type { AppFeed, AppId, AppRow, AppsFeed } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { Skeleton } from '@/next/ui/skeleton';
import Swap from '@/next/ui/swap';

/** The bar's exit, as long as hx-hubbar-out in apps.css. */
const BAR_EXIT_MS = 220;
/** A list of more apps than this gets a field to narrow it. */
const NARROW_FROM = 7;

export interface IAppsViewProps {
  /** What the machine read; null while it is asked. */
  feed: AppsFeed | null;
  /** Why the machine could not be asked at all, in words. */
  error: string | null;
  /** True while the machine is asked again with a feed already on the screen. */
  refreshing: boolean;
  /** The open app. */
  app: AppId;
  onApp: (app: AppId) => void;
  /** On a phone: the page is over the list. */
  pageOpen: boolean;
  onBack: () => void;
  /** The keys of the picked rows. */
  picked: ReadonlySet<string>;
  onToggle: (key: string) => void;
  onClear: () => void;
  /** Takes rows of the open app into a new chat. */
  onChat: (rows: AppRow[]) => void;
  onRetry: () => void;
  onOpenDrawer: () => void;
  /** Opens the Settings page of the connected apps. */
  onSettings: () => void;
  now: number;
}

/** What an app says under its name in the list. */
const appSub = (app: AppFeed): string => {
  if (app.state === 'ready') return app.host ?? t('next.settings.apps.ready');
  return t(app.state === 'off' ? 'next.apps.list.off' : 'next.apps.list.failed');
};

interface INavProps {
  feed: AppsFeed | null;
  app: AppId;
  onApp: (app: AppId) => void;
  onOpenDrawer: () => void;
  onSettings: () => void;
}

const Nav = ({ feed, app, onApp, onOpenDrawer, onSettings }: INavProps) => {
  const [text, setText] = useState('');
  const apps = useMemo(() => (feed ? appsOf(feed) : []), [feed]);
  const words = text.trim().toLowerCase();
  const shown =
    words === '' ? apps : apps.filter((item) => t(`next.apps.tab.${item.id}`).toLowerCase().includes(words));
  return (
    <nav className="hx-hub__nav" aria-label={t('next.apps.nav')}>
      <div className="hx-hub__nav-head">
        <button
          type="button"
          className="hx-head-btn hx-hub__menu"
          aria-label={t('next.apps.menu')}
          onClick={onOpenDrawer}
        >
          <Icon name="menu" />
        </button>
        <h1 className="hx-hub__h1">{t('next.apps.title')}</h1>
      </div>
      {apps.length >= NARROW_FROM && (
        <SearchField value={text} onChange={setText} placeholder={t('next.apps.list.find')} />
      )}
      <Swap
        ready={feed !== null}
        skeleton={
          <div className="hx-hub__sk" aria-hidden="true">
            <Skeleton className="hx-hub__sk-item" />
            <Skeleton className="hx-hub__sk-item" />
          </div>
        }
      >
        <div className="hx-hub__list">
          {shown.map((item, index) => (
            <button
              key={item.id}
              type="button"
              className="hx-hub__item"
              aria-current={item.id === app ? true : undefined}
              data-state={item.state}
              ref={(element) => element?.style.setProperty('--i', String(index))}
              onClick={() => onApp(item.id)}
            >
              <span className="hx-hub__mark" aria-hidden="true">
                <Icon name={appIcon(item.id)} />
              </span>
              <span className="hx-hub__item-text">
                <span className="hx-hub__item-name">{t(`next.apps.tab.${item.id}`)}</span>
                <span className="hx-hub__item-sub">{appSub(item)}</span>
              </span>
              <Icon name="chevron-right" size="sm" className="hx-hub__chev" />
            </button>
          ))}
          {shown.length === 0 && apps.length > 0 && (
            <p className="hx-hub__none">{t('next.apps.menu.none')}</p>
          )}
        </div>
      </Swap>
      <button type="button" className="hx-hub__connect" onClick={onSettings}>
        <Icon name="settings-2" size="sm" />
        {t('next.apps.settings')}
      </button>
    </nav>
  );
};

const AppsView = ({
  feed,
  error,
  refreshing,
  app,
  onApp,
  pageOpen,
  onBack,
  picked,
  onToggle,
  onClear,
  onChat,
  onRetry,
  onOpenDrawer,
  onSettings,
  now,
}: IAppsViewProps) => {
  const prefs = usePrefs();
  const page = useRef<HTMLDivElement>(null);
  const [tracker, setTracker] = useState<ITrackerQuery>(DEFAULT_TRACKER_QUERY);
  const [mrs, setMrs] = useState<IMrQuery>({ ...DEFAULT_MR_QUERY, scope: prefs.scope });
  const open = feed?.apps.find((item) => item.id === app) ?? null;
  const rows = open ? rowsOf(open) : [];
  const chosen = rows.filter((row) => picked.has(row.key));
  const bar = usePresence(chosen.length > 0 ? barText(app, chosen) : null, BAR_EXIT_MS);
  const staleDays = prefs.staleDays ?? feed?.staleDays ?? 5;

  // A page opens from its top.
  useEffect(() => {
    page.current?.scrollTo({ top: 0 });
  }, [app]);

  const head: Omit<IPageHeadProps, 'children'> = {
    app,
    host: open?.host ?? null,
    refreshing,
    onBack,
    onRefresh: onRetry,
  };

  // null is loading: the skeleton stands where the page will be and gives way to it.
  let body: ReactNode = null;
  if (error)
    body = <Note icon="triangle-alert" title={t('next.apps.loadFailed')} text={error} onRetry={onRetry} />;
  else if (open && feed) {
    const common = { app: open, head, now, staleDays, prefs, picked, onToggle, onChat };
    if (open.state !== 'ready')
      body = (
        <>
          <div className="hx-hubhead">
            <button
              type="button"
              className="hx-head-btn hx-hubhead__back"
              aria-label={t('next.apps.back')}
              onClick={onBack}
            >
              <Icon name="chevron-left" />
            </button>
            <span className="hx-hubhead__mark" aria-hidden="true">
              <Icon name={appIcon(app)} />
            </span>
            <h2 className="hx-hubhead__title">{t(`next.apps.tab.${app}`)}</h2>
          </div>
          {appNote(open, onRetry)}
        </>
      );
    else if (app === 'tracker') body = <TrackerPage {...common} query={tracker} onQuery={setTracker} />;
    else body = <GitlabPage {...common} query={mrs} onQuery={setMrs} />;
  }

  return (
    <section className="hx-hub" data-page={pageOpen}>
      <Nav feed={feed} app={app} onApp={onApp} onOpenDrawer={onOpenDrawer} onSettings={onSettings} />
      <div className="hx-hub__page" ref={page}>
        <div className="hx-hub__page-in" key={app}>
          <Swap ready={body !== null} skeleton={<PageSkeleton />}>
            {body}
          </Swap>
        </div>
        {bar.value !== null && (
          <div
            className="hx-hubbar"
            data-state={bar.state}
            role="region"
            aria-label={t('next.apps.selected')}
            inert={bar.state === 'closing' || undefined}
          >
            <span className="hx-hubbar__txt">{bar.value}</span>
            <button type="button" className="hx-hubbar__x" onClick={onClear}>
              {t('next.apps.clear')}
            </button>
            <Button variant="primary" icon="message-square-plus" onClick={() => onChat(chosen)}>
              {t('next.apps.discuss')}
            </Button>
          </div>
        )}
      </div>
    </section>
  );
};

export default AppsView;
