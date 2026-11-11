// The start of the «Ревью кода» app: the merge requests in three lists (waiting for me, mine, all), a field that
// searches them and opens a pasted link, and the row that opens a merge request in the review. GitLab's merge requests
// and the pull requests of the person's GitHub stand in the same lists, each row saying where it lives once both are
// there. While the machine is asked the rows are bars; neither set up, or GitLab not answering, says so, with the way to
// fix it.
import { useState, type KeyboardEvent } from 'react';
import { agoText } from '@/next/features/apps/model';
import { appNote, Note, PageSkeleton } from '@/next/features/apps/list';
import {
  HOME_TABS,
  homeCounts,
  homeRows,
  homeSourceRows,
  initialOf,
  openableLink,
  rowNote,
  sourceOf,
  type IGithubList,
  type THomeTab,
} from '@/next/features/review/app/home';
import type { AppFeed } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';

export type THomeFeed =
  | { k: 'loading' }
  | { k: 'failed'; text: string }
  /** `waiting`: the other source is still being read; its rows join when it answers. */
  | { k: 'ready'; gitlab: AppFeed | null; github: IGithubList | null; waiting: boolean };

export interface IReviewHomeProps {
  feed: THomeFeed;
  now: number;
  onOpen: (url: string) => void;
  onRetry: () => void;
  /** Opens the settings where GitLab and GitHub are connected. */
  onConnect: () => void;
  /** The sidebar on a phone. */
  onOpenDrawer: () => void;
}

const List = ({
  feed,
  tab,
  q,
  now,
  onOpen,
  onRetry,
  onConnect,
}: IReviewHomeProps & { tab: THomeTab; q: string }) => {
  if (feed.k === 'loading') return <PageSkeleton />;
  if (feed.k === 'failed')
    return <Note icon="wifi-off" title={t('next.reviewApp.offline')} text={feed.text} onRetry={onRetry} />;
  const { gitlab, github } = feed;
  const withGithub = github?.state === 'ready';
  const noGitlab = !gitlab || gitlab.reason === 'not-configured' || gitlab.reason === 'no-token';
  if (noGitlab && !withGithub)
    return (
      <Note
        icon="plug"
        title={t('next.reviewApp.noGitlab')}
        text={t('next.reviewApp.noGitlabText')}
        action={
          <Button size="sm" variant="primary" icon="plug" onClick={onConnect}>
            {t('next.reviewApp.connect')}
          </Button>
        }
      />
    );
  if (gitlab && !noGitlab && gitlab.state !== 'ready' && !withGithub) return appNote(gitlab, onRetry);
  const rows = homeRows(homeSourceRows(gitlab, github), tab, q);
  const githubDown = github?.state === 'failed' && (
    <p className="hx-rvhome__warn">
      {github.reason === 'denied' ? t('next.reviewApp.githubDenied') : t('next.reviewApp.githubDown')}
    </p>
  );
  if (rows.length === 0 && feed.waiting) return <PageSkeleton />;
  if (rows.length === 0)
    return (
      <>
        {githubDown}
        <p className="hx-rvhome__empty">
          {q ? t('next.reviewApp.nothingFound') : t(`next.reviewApp.empty.${tab}`)}
        </p>
      </>
    );
  return (
    <>
      {githubDown}
      <ul className="hx-rvhome__list">
        {rows.map((row) => {
          const note = rowNote(row);
          return (
            <li key={row.key}>
              <button type="button" className="hx-rvhome__row" onClick={() => onOpen(row.url)}>
                <span className="hx-rvhome__face" aria-hidden="true">
                  {initialOf(row.author)}
                </span>
                <span className="hx-rvhome__text">
                  <span className="hx-rvhome__title">{row.title}</span>
                  <span className="hx-rvhome__meta">
                    {[row.project, row.id, row.author, agoText(row.updatedAt, now)]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                </span>
                {withGithub && <span className="hx-rvhome__src">{sourceOf(row)}</span>}
                {note && (
                  <span className="hx-rvhome__note" data-tone={row.tone ?? undefined}>
                    {note}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </>
  );
};

const ReviewHome = (props: IReviewHomeProps) => {
  const [tab, setTab] = useState<THomeTab>('waiting');
  const [q, setQ] = useState('');
  const link = openableLink(q);
  const rows = props.feed.k === 'ready' ? homeSourceRows(props.feed.gitlab, props.feed.github) : [];
  const counts = homeCounts(rows);
  const options = HOME_TABS.map((id) => ({
    value: id,
    label:
      counts[id] > 0 && id !== 'all'
        ? `${t(`next.reviewApp.tab.${id}`)} ${counts[id]}`
        : t(`next.reviewApp.tab.${id}`),
  }));

  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' && link) {
      event.preventDefault();
      props.onOpen(link);
    }
  };

  return (
    <div className="hx-rvhome">
      <div className="hx-rvhome__inner">
        <header className="hx-rvhome__head">
          <button
            type="button"
            className="hx-head-btn hx-rv-menu"
            aria-label={t('next.header.openMenu')}
            onClick={props.onOpenDrawer}
          >
            <Icon name="menu" />
          </button>
          <span className="hx-rvhome__tile" aria-hidden="true">
            <Icon name="git-pull-request" />
          </span>
          <h1 className="hx-rvhome__h">{t('next.reviewApp.title')}</h1>
        </header>
        <div className="hx-rvhome__tools">
          <Segmented<THomeTab>
            label={t('next.reviewApp.lists')}
            value={tab}
            options={options}
            onChange={setTab}
          />
          <label className="hx-rvhome__search">
            <Icon name="search" size="sm" />
            <input
              type="search"
              value={q}
              placeholder={t('next.reviewApp.search')}
              aria-label={t('next.reviewApp.search')}
              onChange={(event) => setQ(event.target.value)}
              onKeyDown={onKey}
            />
          </label>
        </div>
        {link ? (
          <button
            type="button"
            className="hx-rvhome__row hx-rvhome__row--link"
            onClick={() => props.onOpen(link)}
          >
            <span className="hx-rvhome__face" aria-hidden="true">
              <Icon name="link" size="sm" />
            </span>
            <span className="hx-rvhome__text">
              <span className="hx-rvhome__title">{t('next.reviewApp.openLink')}</span>
              <span className="hx-rvhome__meta">{link}</span>
            </span>
            <kbd className="hx-rvhome__kbd">Enter</kbd>
          </button>
        ) : (
          <List {...props} tab={tab} q={q} />
        )}
      </div>
    </div>
  );
};

export default ReviewHome;
