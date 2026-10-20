import { t, plural } from '@/lib/i18n';
import { useCallback, useEffect, useMemo, useState } from 'react';
import dayjs from 'dayjs';
import {
  ArrowUpRight,
  ChevronRight,
  GitBranch,
  GitCommitHorizontal,
  GitCompareArrows,
  Loader2,
  RefreshCw,
} from 'lucide-react';
import dynamic from 'next/dynamic';
import { toast } from 'sonner';
import IssueKeyText from '@/components/timeline/issue-key-text';
import { appFetch } from '@/lib/app-path';
import { parseMultiFileDiff } from '@/lib/parse-git-diff';
import { cn } from '@/lib/utils';
import type { IDiffCommenting } from '@/components/agents/diff-comment-layer';
import { addDiffComment } from '@/lib/hl-diff-comments-store';
import type { ICommitSummary, IRepoChanges } from '@/types/server/hl-changes';

const DiffFileList = dynamic(() => import('@/components/workspace/diff-file-list'), { ssr: false });

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

const fetchCommitDiff = async (root: string, sha: string): Promise<ICommitDiff> => {
  const response = await appFetch(`/api/platform/changes?${new URLSearchParams({ root, sha })}`);
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
export const CommitView = ({ target, title, sessionName, tabId }: ICommitViewProps) => {
  const parsed = useMemo(() => parseCommitTarget(target), [target]);
  const repoName = parsed?.root.replace(/\/+$/, '').split('/').at(-1) ?? null;
  const short = parsed?.sha.slice(0, SHORT_SHA) ?? null;
  const commenting = useDiffCommenting(tabId, repoName, short);
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
      <header className="tw-rpane-commit-head">
        <h3 className="tw-rpane-commit-title">{title ? <IssueKeyText text={title} /> : short}</h3>
        <p className="tw-rpane-commit-meta">
          <GitCommitHorizontal />
          <code>{short}</code>
          <span title={parsed.root}>{repoName}</span>
        </p>
      </header>
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
          <DiffFileList
            diff={current.diff.diff}
            viewMode="unified"
            sessionName={sessionName}
            loadContent={false}
            commenting={commenting}
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
}

const CommitRow = ({ commit, repo, sessionName, tabId, onOpenCommit }: ICommitRowProps) => {
  const root = repo.root;
  const commenting = useDiffCommenting(tabId, repo.name, commit.short);
  const [open, setOpen] = useState(false);
  const [diff, setDiff] = useState<ICommitDiff | null>(null);
  const [failed, setFailed] = useState(false);

  const toggle = () => {
    setOpen((value) => !value);
    if (diff || failed) return;
    fetchCommitDiff(root, commit.sha)
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
        {onOpenCommit && (
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
              <DiffFileList
                diff={diff.diff}
                viewMode="unified"
                sessionName={sessionName}
                loadContent={false}
                commenting={commenting}
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
}

const RepoChanges = ({ repo, sessionName, tabId, onOpenCommit }: IRepoChangesProps) => {
  const commenting = useDiffCommenting(tabId, repo.name, null);
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
            <DiffFileList
              diff={repo.diff}
              viewMode="unified"
              sessionName={sessionName}
              loadContent={false}
              commenting={commenting}
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

const EMPTY_TEXT = t('inspector.changesTab.empty');

const ChangesTab = ({ paths, sessionName, tabId, jsonlPath, onCount, onOpenCommit }: IChangesTabProps) => {
  const [repos, setRepos] = useState<IRepoChanges[] | null>(null);
  const [loading, setLoading] = useState(false);
  const key = paths.join('\n');

  const load = useCallback(async () => {
    if (!key) return;
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
  }, [key, jsonlPath]);

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
    onCount?.(count);
  }, [count, onCount]);

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
