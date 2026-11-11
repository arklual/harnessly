import { t, plural } from '@/lib/i18n';
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import dayjs from 'dayjs';
import {
  ArrowUpRight,
  ChevronRight,
  GitBranch,
  GitCommitHorizontal,
  GitCompareArrows,
  GitPullRequest,
  Loader2,
  RefreshCw,
} from 'lucide-react';
import dynamic from 'next/dynamic';
import { toast } from 'sonner';
import IssueKeyText from '@/components/timeline/issue-key-text';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import {
  fetchChatWork,
  fetchMachineChanges,
  fetchRemoteChanges,
  groupWork,
  mrLabel,
  type IChangeSet,
  type IChatWork,
  type IWorkEntry,
} from '@/lib/chat-work';
import { parseMultiFileDiff } from '@/lib/parse-git-diff';
import { goTo, ROUTE } from '@/next/routes/routes';
import { cn } from '@/lib/utils';
import type { IDiffCommenting } from '@/components/agents/diff-comment-layer';
import { addDiffComment } from '@/lib/hl-diff-comments-store';
import type { ICommitSummary, IRepoChanges } from '@/types/server/hl-changes';

const LocalReview = dynamic(() => import('@/next/legacy/local-review'), { ssr: false });

interface IChangesTabProps {
  paths: string[];
  sessionName: string;
  /** The conversation tab whose composer takes comments on diff lines; without it the diff is read-only. */
  tabId?: string | null;
  /** Session transcript; its first timestamp bounds which commits count as this session's. */
  jsonlPath?: string | null;
  /** Session commits plus uncommitted files across all repositories. */
  onCount?: (count: number) => void;
  /** Opens a commit as a tab of its own (`commitTarget`); without it a commit only unfolds in place. */
  onOpenCommit?: (target: string, title: string) => void;
  /** The chat: its registered work and its delegated chats' come first; the paths are the fallback. */
  conversationId?: string | null;
}

interface IDiffTotals {
  add: number;
  del: number;
  files: number;
}

const diffTotals = (diff: string): IDiffTotals =>
  (diff ? parseMultiFileDiff(diff) : []).reduce(
    (acc, f) => ({ add: acc.add + f.additions, del: acc.del + f.deletions, files: acc.files + 1 }),
    { add: 0, del: 0, files: 0 },
  );

const Stat = ({ add, del }: { add: number; del: number }) => (
  <span className="tw-repo-stat">
    <span className="tw-diff-add">+{add}</span>
    <span className="tw-diff-del">−{del}</span>
  </span>
);

/** Comments on diff lines go to the conversation's composer as chips. */
export const useDiffCommenting = (
  tabId: string | null | undefined,
  repo: string | null,
  commit: string | null,
): IDiffCommenting | undefined =>
  useMemo(
    () =>
      tabId
        ? {
            repo,
            commit,
            onSubmit: (comment) => {
              addDiffComment(tabId, comment);
              toast.success(t('inspector.changesTab.commentAdded'));
            },
          }
        : undefined,
    [tabId, repo, commit],
  );

interface ICommitDiff {
  diff: string;
  truncated: boolean;
}

const SHORT_SHA = 8;
const SHA = /^[0-9a-f]{7,40}$/;
const TARGET_SEPARATOR = '@';

/** A commit as the target of a «Changes» tab: its repository root and sha. */
export const commitTarget = (root: string, sha: string): string => `${root}${TARGET_SEPARATOR}${sha}`;

const parseCommitTarget = (target: string): { root: string; sha: string } | null => {
  const at = target.lastIndexOf(TARGET_SEPARATOR);
  const sha = target.slice(at + 1);
  return at > 0 && target.startsWith('/') && SHA.test(sha) ? { root: target.slice(0, at), sha } : null;
};

/** A path an agent asked the tab for is not a commit: the tab shows the list of changes then. */
export const isCommitTarget = (target: string): boolean => parseCommitTarget(target) !== null;

const fetchCommitDiff = async (root: string, sha: string, machineId?: string): Promise<ICommitDiff> => {
  const path = `/api/platform/changes?${new URLSearchParams({ root, sha })}`;
  const response = await (machineId
    ? fetch(scopedAppPath(path, machineId), { credentials: 'same-origin' })
    : appFetch(path));
  if (!response.ok) throw new Error(String(response.status));
  return (await response.json()) as ICommitDiff;
};

interface ICommitViewProps {
  /** `commitTarget(root, sha)`. */
  target: string;
  title: string | null;
  sessionName: string;
  tabId?: string | null;
}

/** One commit as a tab of the right panel: its diff with comments on lines. */
export const CommitView = ({ target, title }: ICommitViewProps) => {
  const parsed = useMemo(() => parseCommitTarget(target), [target]);
  const repoName = parsed?.root.replace(/\/+$/, '').split('/').at(-1) ?? null;
  const short = parsed?.sha.slice(0, SHORT_SHA) ?? null;
  const [loaded, setLoaded] = useState<{ target: string; diff: ICommitDiff | null } | null>(null);

  useEffect(() => {
    if (!parsed) return;
    let cancelled = false;
    fetchCommitDiff(parsed.root, parsed.sha)
      .then((diff) => {
        if (!cancelled) setLoaded({ target, diff });
      })
      .catch(() => {
        if (!cancelled) setLoaded({ target, diff: null });
      });
    return () => {
      cancelled = true;
    };
  }, [parsed, target]);

  if (!parsed) return <p className="tw-rpane-muted">{t('inspector.changesTab.badCommitAddress')}</p>;
  const current = loaded?.target === target ? loaded : null;
  return (
    <div className="tw-rpane-commit">
      {!current?.diff && (
        <header className="tw-rpane-commit-head">
          <h3 className="tw-rpane-commit-title">{title ? <IssueKeyText text={title} /> : short}</h3>
          <p className="tw-rpane-commit-meta">
            <GitCommitHorizontal />
            <code>{short}</code>
            <span title={parsed.root}>{repoName}</span>
          </p>
        </header>
      )}
      {!current && (
        <p className="tw-inspector-muted tw-repo-note">
          <Loader2 className="tw-inline-spin" /> {t('inspector.changesTab.diffLoading')}
        </p>
      )}
      {current && !current.diff && (
        <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.commitNotInRepo')}</p>
      )}
      {current?.diff && (
        <>
          <LocalReview
            root={parsed.root}
            name={repoName ?? parsed.root}
            branch={null}
            diff={current.diff.diff}
            truncated={current.diff.truncated}
            sha={parsed.sha}
            title={title ?? undefined}
          />
          {current.diff.truncated && (
            <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.commitTruncated')}</p>
          )}
        </>
      )}
    </div>
  );
};

interface ICommitRowProps {
  commit: ICommitSummary;
  repo: IRepoChanges;
  sessionName: string;
  tabId?: string | null;
  onOpenCommit?: (target: string, title: string) => void;
  /** The machine the repository is on; absent for the page's own. */
  machineId?: string;
}

const CommitRow = ({ commit, repo, onOpenCommit, machineId }: ICommitRowProps) => {
  const root = repo.root;
  const [open, setOpen] = useState(false);
  const [diff, setDiff] = useState<ICommitDiff | null>(null);
  const [failed, setFailed] = useState(false);

  const toggle = () => {
    setOpen((value) => !value);
    if (diff || failed) return;
    fetchCommitDiff(root, commit.sha, machineId)
      .then(setDiff)
      .catch(() => setFailed(true));
  };

  return (
    <li className={cn('tw-commit', open && 'tw-commit-open')}>
      <div className="tw-commit-line">
        <div
          role="button"
          tabIndex={0}
          className="tw-commit-row"
          onClick={toggle}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              toggle();
            }
          }}
          aria-expanded={open}
          title={`${commit.subject}\n${commit.author} · ${dayjs(commit.date).format('DD.MM HH:mm')}`}
        >
          <ChevronRight className="tw-commit-chevron" />
          <span className="tw-commit-text">
            <strong
              onClick={(e) => {
                if ((e.target as HTMLElement).closest('a')) e.stopPropagation();
              }}
            >
              <IssueKeyText text={commit.subject} />
            </strong>
            <small>
              <code>{commit.short}</code>
              {dayjs(commit.date).format('HH:mm')} · {plural(commit.files, 'inspector.changesTab.files')}
            </small>
          </span>
          <Stat add={commit.additions} del={commit.deletions} />
        </div>
        {onOpenCommit && !machineId && (
          <button
            type="button"
            className="tw-icon-button tw-commit-open"
            aria-label={t('inspector.changesTab.openCommitAsTab', { sha: commit.short })}
            title={t('inspector.changesTab.openAsTab')}
            onClick={() => onOpenCommit(commitTarget(root, commit.sha), commit.subject)}
          >
            <ArrowUpRight />
          </button>
        )}
      </div>
      {open && (
        <div className="tw-commit-diff">
          {diff ? (
            <>
              <LocalReview
                root={root}
                name={repo.name}
                branch={repo.branch}
                diff={diff.diff}
                truncated={diff.truncated}
                sha={commit.sha}
                title={commit.subject}
              />
              {diff.truncated && (
                <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.commitTruncated')}</p>
              )}
            </>
          ) : failed ? (
            <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.diffFailed')}</p>
          ) : (
            <p className="tw-inspector-muted tw-repo-note">
              <Loader2 className="tw-inline-spin" /> {t('inspector.changesTab.diffLoading')}
            </p>
          )}
        </div>
      )}
    </li>
  );
};

interface IRepoChangesProps {
  repo: IRepoChanges;
  sessionName: string;
  tabId?: string | null;
  onOpenCommit?: (target: string, title: string) => void;
  machineId?: string;
  /** Chips after the branch in the header (the merge request, a delegated chat's mark). */
  extra?: ReactNode;
}

const RepoChanges = ({ repo, sessionName, tabId, onOpenCommit, machineId, extra }: IRepoChangesProps) => {
  const commits = useMemo(
    () => repo.commits.map((c) => ({ sha: c.sha, title: c.subject, author: c.author, at: null })),
    [repo.commits],
  );
  const pending = useMemo(() => diffTotals(repo.diff), [repo.diff]);
  const add = pending.add + repo.commits.reduce((sum, c) => sum + c.additions, 0);
  const del = pending.del + repo.commits.reduce((sum, c) => sum + c.deletions, 0);

  return (
    <section className="tw-repo">
      <header className="tw-repo-head" title={repo.root}>
        <span className="tw-repo-name">{repo.name}</span>
        {repo.branch && (
          <span className="tw-repo-branch">
            <GitBranch />
            {repo.branch}
          </span>
        )}
        {extra}
        <Stat add={add} del={del} />
      </header>
      {repo.commits.length > 0 && (
        <div className="tw-repo-block">
          <h4 className="tw-repo-subhead">
            <GitCommitHorizontal />
            {t('inspector.changesTab.sessionCommits')}{' '}
            <span>
              {repo.commits.length}
              {repo.commitsTruncated ? '+' : ''}
            </span>
          </h4>
          <ul className="tw-commit-list">
            {repo.commits.map((commit) => (
              <CommitRow
                key={commit.sha}
                commit={commit}
                repo={repo}
                sessionName={sessionName}
                tabId={tabId}
                onOpenCommit={onOpenCommit}
                machineId={machineId}
              />
            ))}
          </ul>
        </div>
      )}
      {repo.diff && (
        <div className="tw-repo-block">
          <h4 className="tw-repo-subhead">
            <GitCompareArrows />
            {t('inspector.changesTab.uncommitted')}{' '}
            <span>{plural(pending.files, 'inspector.changesTab.files')}</span>
          </h4>
          <div className="tw-repo-diff">
            <LocalReview
              root={repo.root}
              name={repo.name}
              branch={repo.branch}
              diff={repo.diff}
              truncated={repo.truncated}
              commits={commits}
            />
          </div>
          {repo.truncated && (
            <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.repoTruncated')}</p>
          )}
        </div>
      )}
    </section>
  );
};

type TWorktreeStatus = 'loading' | 'ready' | 'none' | 'failed';

const lastSegment = (p: string) => p.replace(/\/+$/, '').split('/').at(-1) ?? p;

interface IWorktreeChangesProps {
  entry: IWorkEntry;
  /** The chat the tab belongs to: work of another chat is a delegated chat's. */
  conversationId: string;
  machineName: string | null;
  online: boolean;
  sessionName: string;
  tabId?: string | null;
  onOpenCommit?: (target: string, title: string) => void;
  onCount: (key: string, count: number) => void;
}

/** One worktree of the chat's work: read on its machine while it is on; a pushed branch from the code host otherwise. */
const WorktreeChanges = ({
  entry,
  conversationId,
  machineName,
  online,
  sessionName,
  tabId,
  onOpenCommit,
  onCount,
}: IWorktreeChangesProps) => {
  const key = `${entry.machineId ?? ''}\u0000${entry.conv}\u0000${entry.worktree}`;
  const [loaded, setLoaded] = useState<{
    key: string;
    set: IChangeSet | null;
    status: TWorktreeStatus;
  } | null>(null);
  const [remoteError, setRemoteError] = useState<string | null>(null);
  const current = loaded?.key === key ? loaded : null;
  let status: TWorktreeStatus = current?.status ?? 'none';
  if (!current && online) status = 'loading';
  const set = current?.set ?? null;

  useEffect(() => {
    if (!online) return;
    let cancelled = false;
    fetchMachineChanges(entry)
      .then((got) => {
        if (!cancelled) setLoaded({ key, set: got, status: got ? 'ready' : 'none' });
      })
      .catch(() => {
        if (!cancelled) setLoaded({ key, set: null, status: 'none' });
      });
    return () => {
      cancelled = true;
    };
  }, [key, online, entry]);

  useEffect(() => {
    const files = set ? set.files.length : 0;
    onCount(key, (set ? set.commits.length : (entry.commits?.length ?? 0)) + files);
  }, [key, set, entry.commits, onCount]);

  const loadRemote = () => {
    setLoaded({ key, set: null, status: 'loading' });
    setRemoteError(null);
    fetchRemoteChanges(entry)
      .then((got) => setLoaded({ key, set: got, status: 'ready' }))
      .catch((err: unknown) => {
        setRemoteError(err instanceof Error ? err.message : String(err));
        setLoaded({ key, set: null, status: 'failed' });
      });
  };

  const chips = (
    <>
      {entry.conv !== conversationId && (
        <span className="tw-work-tag">{t('inspector.changesTab.delegated')}</span>
      )}
      {entry.mr && (
        <a
          className="tw-work-tag tw-work-mr"
          href={entry.mr}
          target="_blank"
          rel="noreferrer"
          title={entry.mr}
        >
          {mrLabel(set?.mr ?? entry.mr)}
        </a>
      )}
      {!entry.mr && set?.mr && (
        <a className="tw-work-tag tw-work-mr" href={set.mr} target="_blank" rel="noreferrer" title={set.mr}>
          {mrLabel(set.mr)}
        </a>
      )}
    </>
  );
  const name = lastSegment(entry.worktree);

  if (status === 'ready' && set?.source === 'machine')
    return (
      <RepoChanges
        repo={{
          root: entry.worktree,
          name,
          branch: set.branch,
          diff: set.diff,
          truncated: set.truncated,
          commits: set.commits,
          commitsTruncated: false,
        }}
        sessionName={sessionName}
        tabId={tabId}
        onOpenCommit={onOpenCommit}
        machineId={entry.machineId}
        extra={chips}
      />
    );

  return (
    <section className="tw-repo">
      <header className="tw-repo-head" title={entry.worktree}>
        <span className="tw-repo-name">{name}</span>
        {entry.branch && (
          <span className="tw-repo-branch">
            <GitBranch />
            {entry.branch}
          </span>
        )}
        {chips}
      </header>
      {status === 'loading' && (
        <p className="tw-inspector-muted tw-repo-note">
          <Loader2 className="tw-inline-spin" /> {t('inspector.changesTab.diffLoading')}
        </p>
      )}
      {status === 'ready' && set && (
        <>
          <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.fromCodeHost')}</p>
          {set.commits.length > 0 && (
            <ul className="tw-work-commits">
              {set.commits.map((c) => (
                <li key={c.sha}>
                  <code>{c.short}</code> <IssueKeyText text={c.subject} />
                </li>
              ))}
            </ul>
          )}
          {set.diff && (
            <div className="tw-repo-diff">
              <LocalReview
                root={set.repo}
                name={set.repo}
                branch={set.branch}
                diff={set.diff}
                truncated={set.truncated}
              />
            </div>
          )}
          {set.truncated && (
            <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.repoTruncated')}</p>
          )}
        </>
      )}
      {(status === 'none' || status === 'failed') && (
        <div className="tw-repo-block">
          <p className="tw-inspector-muted tw-repo-note">
            {online
              ? t('inspector.changesTab.worktreeGone')
              : t('inspector.changesTab.machineOffline', { name: machineName ?? entry.machineId ?? '' })}
          </p>
          {(entry.commits?.length ?? 0) > 0 && (
            <p className="tw-inspector-muted tw-repo-note">
              {plural(entry.commits?.length ?? 0, 'inspector.changesTab.commits')}:{' '}
              {entry.commits?.slice(0, 10).map((c) => (
                <code key={c}>{c.slice(0, SHORT_SHA)} </code>
              ))}
            </p>
          )}
          {entry.remote && entry.branch ? (
            <button type="button" className="tw-work-remote" onClick={loadRemote}>
              {t('inspector.changesTab.showBranchDiff')}
            </button>
          ) : (
            <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.notPushed')}</p>
          )}
          {status === 'failed' && remoteError && (
            <p className="tw-inspector-muted tw-repo-note">
              {t('inspector.changesTab.branchFailed', { reason: remoteError })}
            </p>
          )}
        </div>
      )}
    </section>
  );
};

interface IWorkViewProps {
  work: IChatWork;
  conversationId: string;
  sessionName: string;
  tabId?: string | null;
  onOpenCommit?: (target: string, title: string) => void;
  onCount?: (count: number) => void;
  onRefresh: () => void;
  refreshing: boolean;
}

/** The chat's registered work and its delegated chats': repositories, then a worktree (a branch) each. */
const WorkView = ({
  work,
  conversationId,
  sessionName,
  tabId,
  onOpenCommit,
  onCount,
  onRefresh,
  refreshing,
}: IWorkViewProps) => {
  const repos = useMemo(() => groupWork(work.entries), [work.entries]);
  const machines = useMemo(() => new Map(work.machines.map((m) => [m.id, m])), [work.machines]);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const report = useCallback(
    (key: string, n: number) => setCounts((c) => (c[key] === n ? c : { ...c, [key]: n })),
    [],
  );
  const total = Object.values(counts).reduce((sum, n) => sum + n, 0);
  useEffect(() => {
    onCount?.(total);
  }, [total, onCount]);

  return (
    <div className="tw-changes">
      <div className="tw-inspector-summary">
        <span>{t('inspector.changesTab.workHead')}</span>
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('inspector.changesTab.openReview')}
          title={t('inspector.changesTab.openReview')}
          onClick={() => goTo({ kind: ROUTE.review, chat: conversationId })}
        >
          <GitPullRequest />
        </button>
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('inspector.changesTab.refresh')}
          title={t('inspector.changesTab.refresh')}
          onClick={onRefresh}
          disabled={refreshing}
        >
          <RefreshCw className={refreshing ? 'tw-inline-spin' : undefined} />
        </button>
      </div>
      {!work.complete && (
        <p className="tw-inspector-muted tw-repo-note">{t('inspector.changesTab.scanning')}</p>
      )}
      {repos.map((repo) => {
        const machine = repo.machineId ? machines.get(repo.machineId) : undefined;
        const online = !repo.machineId || machine?.state !== 'offline';
        return (
          <div key={repo.key} className="tw-work-group">
            <h4 className="tw-work-group-head" title={repo.repo}>
              {repo.name}
              {machine && <span>{machine.name}</span>}
            </h4>
            {repo.worktrees.map((entry) => (
              <WorktreeChanges
                key={`${entry.conv}\u0000${entry.worktree}`}
                entry={entry}
                conversationId={conversationId}
                machineName={machine?.name ?? null}
                online={online}
                sessionName={sessionName}
                tabId={tabId}
                onOpenCommit={onOpenCommit}
                onCount={report}
              />
            ))}
          </div>
        );
      })}
    </div>
  );
};

const EMPTY_TEXT = t('inspector.changesTab.empty');

const ChangesTab = ({
  paths,
  sessionName,
  tabId,
  jsonlPath,
  onCount,
  onOpenCommit,
  conversationId,
}: IChangesTabProps) => {
  const [repos, setRepos] = useState<IRepoChanges[] | null>(null);
  const [loading, setLoading] = useState(false);
  // The chat's registered work: undefined while it is read, null where there is none to read (no route, no chat).
  const [work, setWork] = useState<{ conv: string; work: IChatWork | null } | undefined>(undefined);
  const [workLoading, setWorkLoading] = useState(false);
  const key = paths.join('\n');
  const currentWork = conversationId && work?.conv === conversationId ? work.work : undefined;
  const byWork = !!currentWork && currentWork.entries.length > 0;

  const loadWork = useCallback(async () => {
    if (!conversationId) return;
    setWorkLoading(true);
    const got = await fetchChatWork(conversationId);
    setWork({ conv: conversationId, work: got });
    setWorkLoading(false);
  }, [conversationId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadWork();
  }, [loadWork]);

  const load = useCallback(async () => {
    if (!key || byWork) return;
    setLoading(true);
    try {
      const response = await appFetch('/api/platform/changes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ paths: key.split('\n'), jsonlPath }),
      });
      if (response.ok) setRepos((await response.json()).repos);
    } catch {}
    setLoading(false);
  }, [key, jsonlPath, byWork]);

  // Fetches when the set of work paths changes; state lands after the request.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  const { active, idle, count } = useMemo(() => {
    const list = repos ?? [];
    const busy = list.filter((repo) => repo.diff || repo.commits.length);
    return {
      active: busy,
      idle: list.filter((repo) => !busy.includes(repo)),
      count: busy.reduce((sum, repo) => sum + repo.commits.length + diffTotals(repo.diff).files, 0),
    };
  }, [repos]);

  useEffect(() => {
    if (!byWork) onCount?.(count);
  }, [count, onCount, byWork]);

  if (conversationId && currentWork === undefined)
    return (
      <div className="tw-inspector-empty">
        <Loader2 className="tw-inline-spin" />
        <p>{t('inspector.changesTab.loading')}</p>
      </div>
    );
  if (conversationId && currentWork && byWork)
    return (
      <WorkView
        work={currentWork}
        conversationId={conversationId}
        sessionName={sessionName}
        tabId={tabId}
        onOpenCommit={onOpenCommit}
        onCount={onCount}
        onRefresh={() => void loadWork()}
        refreshing={workLoading}
      />
    );
  if (!key)
    return (
      <div className="tw-inspector-empty">
        <GitCompareArrows />
        <p>{EMPTY_TEXT}</p>
      </div>
    );
  if (!repos)
    return (
      <div className="tw-inspector-empty">
        <Loader2 className="tw-inline-spin" />
        <p>{t('inspector.changesTab.loading')}</p>
      </div>
    );
  if (!repos.length)
    return (
      <div className="tw-inspector-empty">
        <GitCompareArrows />
        <p>{EMPTY_TEXT}</p>
      </div>
    );

  return (
    <div className="tw-changes">
      <div className="tw-inspector-summary">
        <span>
          {active.length ? t('inspector.changesTab.headActive') : t('inspector.changesTab.headIdle')}
        </span>
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('inspector.changesTab.refresh')}
          title={t('inspector.changesTab.refresh')}
          onClick={() => void load()}
          disabled={loading}
        >
          <RefreshCw className={loading ? 'tw-inline-spin' : undefined} />
        </button>
      </div>
      {active.map((repo) => (
        <RepoChanges
          key={repo.root}
          repo={repo}
          sessionName={sessionName}
          tabId={tabId}
          onOpenCommit={onOpenCommit}
        />
      ))}
      {idle.length > 0 && (
        <p className="tw-changes-idle" title={idle.map((repo) => repo.root).join('\n')}>
          <span>{t('inspector.changesTab.noChanges')}</span>
          {idle.map((repo) => (
            <code key={repo.root}>{repo.name}</code>
          ))}
        </p>
      )}
    </div>
  );
};

export default ChangesTab;
