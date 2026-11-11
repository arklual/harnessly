// «Ревью» of the pane: a project of the deployment's GitLab (found by name, the ones opened last on top), then one of
// its merge requests (open, waiting for my review, my own, or found by number or title) or a branch and one of its
// commits; the pick opens as a change tab. A pasted link of a merge request or a commit opens straight away.
import { useEffect, useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { Button } from '@/next/ui/button';
import Segmented from '@/next/ui/segmented';
import LoadFailed from '@/next/ui/load-failed';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import { formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import type { PageOutcome } from '@/next/core/net/http';
import type { MachineId } from '@/next/protocol/v2';
import {
  CHANGE_KIND,
  MR_FILTER,
  PICK_LIST,
  PICK_STATE,
  changeLinkOf,
  changeTitle,
  loadPick,
  type IChangeLink,
  type IPickAnswer,
  type IPickBranch,
  type IPickCommit,
  type IPickMr,
  type IPickProject,
  type TMrFilter,
} from '@/next/features/review/gitlab-pick';
import { pushRecent, readRecent, withRecentFirst, writeRecent } from '@/next/features/review/recent-projects';
import '@/next/styles/review-picker.css';

/** How long typing waits before the list is asked again. */
export const SEARCH_WAIT_MS = 250;
const SKELETON_ROWS = 5;

/** What a pick opens: the change and what the list knew of it. */
export interface IPicked {
  link: IChangeLink;
  title: string;
  name: string | null;
  source: string | null;
  target: string | null;
}

/** The modes of a project's list: its merge requests by filter, or its branches. */
const MODE = { ...MR_FILTER, branches: 'branches' } as const;
type TMode = (typeof MODE)[keyof typeof MODE];

type TLoad<T> = { key: string; out: PageOutcome<IPickAnswer<T>> } | null;

/** The newest answer of `ask` for `key`; null while it is asked. An older answer that comes late is dropped. */
const useAsked = <T,>(
  key: string | null,
  ask: () => Promise<PageOutcome<IPickAnswer<T>>>,
  attempt: number,
) => {
  const [loaded, setLoaded] = useState<TLoad<T>>(null);
  useEffect(() => {
    if (key === null) return;
    let gone = false;
    void ask().then((out) => {
      if (!gone) setLoaded({ key, out });
    });
    return () => {
      gone = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the key names everything the question depends on
  }, [key, attempt]);
  return loaded?.key === key ? loaded.out : null;
};

const useDebounced = (value: string, ms: number) => {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return settled;
};

const failedReason = (out: Exclude<PageOutcome<unknown>, { ok: true }>): string => {
  if (out.r === 'failed' && out.code === 'http') return t('next.review.failedHttp', { status: out.status });
  return t('next.review.unreachable');
};

const offText = (reason: string | null): string => {
  if (reason === 'not-configured') return t('next.review.offNotConfigured');
  if (reason === 'no-token') return t('next.review.offNoToken');
  if (reason === 'denied') return t('next.review.offDenied');
  return t('next.review.unreachable');
};

const ago = (at: string | null, now: number): string | null => {
  const ms = at ? Date.parse(at) : NaN;
  return Number.isFinite(ms) ? formatAgo(ms, now) : null;
};

interface IListProps<T> {
  out: PageOutcome<IPickAnswer<T>> | null;
  empty: string;
  onRetry: () => void;
  onSettings: () => void;
  row: (item: T, i: number) => ReactNode;
  order?: (items: readonly T[]) => readonly T[];
}

/** A list as it stands: rows, a skeleton while it loads, the reason when GitLab gives none. */
const PickList = <T,>({ out, empty, onRetry, onSettings, row, order }: IListProps<T>) => {
  if (out === null)
    return (
      <Delayed>
        <ul className="hx-rp__list" aria-busy="true">
          {Array.from({ length: SKELETON_ROWS }, (_, i) => (
            <li key={i}>
              <Skeleton className="hx-rp__skel" />
            </li>
          ))}
        </ul>
      </Delayed>
    );
  if (!('ok' in out)) return <LoadFailed reason={failedReason(out)} onRetry={onRetry} />;
  const answer = out.value;
  if (answer.state !== PICK_STATE.ready)
    return (
      <div className="hx-rp__note" role="status">
        <p>{offText(answer.reason)}</p>
        {answer.state === PICK_STATE.off ? (
          <Button size="sm" onClick={onSettings}>
            {t('next.review.toConnections')}
          </Button>
        ) : (
          <Button size="sm" icon="rotate-ccw" onClick={onRetry}>
            {t('next.load.retry')}
          </Button>
        )}
      </div>
    );
  const items = order ? order(answer.items) : answer.items;
  if (items.length === 0) return <p className="hx-rp__empty">{empty}</p>;
  return <ul className="hx-rp__list">{items.map(row)}</ul>;
};

/** A merge request of a list: opens as a change; the project is named when the list is of every project. */
const MrRow = ({ mr, now, onOpen }: { mr: IPickMr; now: number; onOpen: (picked: IPicked) => void }) => {
  const link = changeLinkOf(mr.url);
  return (
    <li>
      <button
        type="button"
        className="hx-rp__row"
        disabled={!link}
        onClick={() =>
          link &&
          onOpen({ link, title: changeTitle(link), name: mr.title, source: mr.source, target: mr.target })
        }
      >
        <Icon name="git-pull-request" size="sm" />
        <span className="hx-rp__main">
          <span className="hx-rp__title">
            {mr.draft && <span className="hx-rp__draft">{t('next.review.draft')}</span>}
            {mr.title}
          </span>
          <span className="hx-rp__sub">
            {mr.project ? `${mr.project}!${mr.iid}` : `!${mr.iid}`} · {mr.source} → {mr.target}
            {mr.author && ` · ${mr.author}`}
          </span>
        </span>
        <span className="hx-rp__meta">{ago(mr.updated, now)}</span>
      </button>
    </li>
  );
};

export interface IReviewPickerProps {
  machine: MachineId;
  onOpen: (picked: IPicked) => void;
  /** Settings → Connections, for a GitLab that is not connected. */
  onSettings: () => void;
  /** «Now» for the ages of the rows (tests pin it). */
  now?: number;
}

const ReviewPicker = ({ machine, onOpen, onSettings, now: pinned }: IReviewPickerProps) => {
  // The ages are counted from when the picker came: a list read later is not a reason to draw it again.
  const [opened] = useState(() => Date.now());
  const now = pinned ?? opened;
  const [project, setProject] = useState<IPickProject | null>(null);
  const [branch, setBranch] = useState<IPickBranch | null>(null);
  const [mode, setMode] = useState<TMode>(MODE.open);
  const [query, setQuery] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [recent, setRecent] = useState(readRecent);
  const q = useDebounced(query, SEARCH_WAIT_MS);
  const pasted = changeLinkOf(query);

  const listKey = (() => {
    if (pasted) return null;
    if (!project) return `p|${q}`;
    if (branch) return `c|${project.path}|${branch.name}`;
    return `${mode}|${project.path}|${q}`;
  })();
  const out = useAsked<IPickProject | IPickMr | IPickBranch | IPickCommit>(
    listKey,
    () => {
      if (!project) return loadPick(machine, { list: PICK_LIST.projects, q });
      if (branch)
        return loadPick(machine, { list: PICK_LIST.commits, project: project.path, ref: branch.name });
      if (mode === MODE.branches)
        return loadPick(machine, { list: PICK_LIST.branches, project: project.path, q });
      return loadPick(machine, { list: PICK_LIST.mrs, project: project.path, filter: mode as TMrFilter, q });
    },
    attempt,
  );
  const retry = () => setAttempt((n) => n + 1);

  const pickProject = (p: IPickProject) => {
    const next = pushRecent(recent, { path: p.path, name: p.name });
    setRecent(next);
    writeRecent(next);
    setProject(p);
    setQuery('');
  };
  const back = () => {
    if (branch) setBranch(null);
    else {
      setProject(null);
      setMode(MODE.open);
    }
    setQuery('');
  };
  const openLink = (link: IChangeLink, extra: Omit<IPicked, 'link' | 'title'>) =>
    onOpen({ link, title: changeTitle(link), ...extra });
  const openUrl = (url: string, extra: Omit<IPicked, 'link' | 'title'>) => {
    const link = changeLinkOf(url);
    if (link) openLink(link, extra);
  };

  const placeholder = (() => {
    if (!project) return t('next.review.findProject');
    if (branch) return t('next.review.pasteLink');
    if (mode === MODE.branches) return t('next.review.findBranch');
    return t('next.review.findMr');
  })();

  const rows = (): ReactNode => {
    if (pasted)
      return (
        <div className="hx-rp__paste">
          <Button
            variant="primary"
            icon={pasted.kind === CHANGE_KIND.mr ? 'git-pull-request' : 'git-compare'}
            onClick={() => {
              openLink(pasted, { name: null, source: null, target: null });
              setQuery('');
            }}
          >
            {t('next.review.openLink', { title: changeTitle(pasted) })}
          </Button>
        </div>
      );
    const common = { onRetry: retry, onSettings };
    if (!project)
      return (
        <PickList<IPickProject>
          {...common}
          out={out as PageOutcome<IPickAnswer<IPickProject>> | null}
          empty={t('next.review.noProjects')}
          order={(items) => withRecentFirst(items, recent)}
          row={(p) => (
            <li key={p.path}>
              <button type="button" className="hx-rp__row" onClick={() => pickProject(p)}>
                <Icon name={recent.some((x) => x.path === p.path) ? 'history' : 'folder-git-2'} size="sm" />
                <span className="hx-rp__main">
                  <span className="hx-rp__title">{p.name}</span>
                  <span className="hx-rp__sub">{p.path}</span>
                </span>
                <span className="hx-rp__meta">{ago(p.activity, now)}</span>
              </button>
            </li>
          )}
        />
      );
    if (branch)
      return (
        <PickList<IPickCommit>
          {...common}
          out={out as PageOutcome<IPickAnswer<IPickCommit>> | null}
          empty={t('next.review.noCommits')}
          row={(c) => (
            <li key={c.sha}>
              <button
                type="button"
                className="hx-rp__row"
                onClick={() => openUrl(c.url, { name: c.title, source: branch.name, target: null })}
              >
                <Icon name="git-compare" size="sm" />
                <span className="hx-rp__main">
                  <span className="hx-rp__title">{c.title}</span>
                  <span className="hx-rp__sub">
                    <code>{c.short}</code> {c.author}
                  </span>
                </span>
                <span className="hx-rp__meta">{ago(c.at, now)}</span>
              </button>
            </li>
          )}
        />
      );
    if (mode === MODE.branches)
      return (
        <PickList<IPickBranch>
          {...common}
          out={out as PageOutcome<IPickAnswer<IPickBranch>> | null}
          empty={t('next.review.noBranches')}
          row={(b) => (
            <li key={b.name}>
              <button
                type="button"
                className="hx-rp__row"
                onClick={() => {
                  setBranch(b);
                  setQuery('');
                }}
              >
                <Icon name="git-branch" size="sm" />
                <span className="hx-rp__main">
                  <span className="hx-rp__title">{b.name}</span>
                  <span className="hx-rp__sub">{b.title}</span>
                </span>
                <span className="hx-rp__meta">{ago(b.at, now)}</span>
              </button>
            </li>
          )}
        />
      );
    return (
      <PickList<IPickMr>
        {...common}
        out={out as PageOutcome<IPickAnswer<IPickMr>> | null}
        empty={t('next.review.noMrs')}
        row={(mr) => <MrRow key={mr.iid} mr={mr} now={now} onOpen={onOpen} />}
      />
    );
  };

  return (
    <div className="hx-rp">
      {project && (
        <div className="hx-rp__head">
          <button type="button" className="hx-head-btn" aria-label={t('next.review.back')} onClick={back}>
            <Icon name="arrow-left" size="sm" />
          </button>
          <span className="hx-rp__crumb">
            <span className="hx-rp__title">{project.name}</span>
            {branch && <span className="hx-rp__sub">{branch.name}</span>}
          </span>
        </div>
      )}
      {project && !branch && (
        <Segmented
          label={t('next.review.modes')}
          value={mode}
          onChange={(next) => {
            setMode(next);
            setQuery('');
          }}
          options={[
            { value: MODE.open, label: t('next.review.modeOpen') },
            { value: MODE.review, label: t('next.review.modeReview') },
            { value: MODE.mine, label: t('next.review.modeMine') },
            { value: MODE.branches, label: t('next.review.modeBranches') },
          ]}
        />
      )}
      <label className="hx-rp__search">
        <Icon name="search" size="sm" />
        <input
          type="search"
          value={query}
          placeholder={placeholder}
          aria-label={placeholder}
          onChange={(event) => setQuery(event.target.value)}
        />
      </label>
      {rows()}
    </div>
  );
};

export default ReviewPicker;

export interface IMyChangesProps {
  machine: MachineId;
  onOpen: (picked: IPicked) => void;
  onSettings: () => void;
  now?: number;
}

const MINE = { review: MR_FILTER.review, mine: MR_FILTER.mine } as const;
type TMine = (typeof MINE)[keyof typeof MINE];

/** «GitLab» of the start page: the merge requests waiting for my review and my own, across every project. */
export const MyChanges = ({ machine, onOpen, onSettings, now: pinned }: IMyChangesProps) => {
  const [opened] = useState(() => Date.now());
  const now = pinned ?? opened;
  const [mode, setMode] = useState<TMine>(MINE.review);
  const [attempt, setAttempt] = useState(0);
  const out = useAsked<IPickMr>(
    mode,
    () => loadPick(machine, { list: PICK_LIST.mrs, filter: mode }),
    attempt,
  );
  return (
    <div className="hx-rp">
      <Segmented
        label={t('next.review.modes')}
        value={mode}
        onChange={setMode}
        options={[
          { value: MINE.review, label: t('next.review.modeReview') },
          { value: MINE.mine, label: t('next.review.modeMine') },
        ]}
      />
      <PickList<IPickMr>
        out={out}
        empty={t('next.review.noMrs')}
        onRetry={() => setAttempt((n) => n + 1)}
        onSettings={onSettings}
        row={(mr) => <MrRow key={mr.url} mr={mr} now={now} onOpen={onOpen} />}
      />
    </div>
  );
};
