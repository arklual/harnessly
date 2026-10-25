import { t } from '@/lib/i18n';
import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import dayjs from 'dayjs';
import { ExternalLink, GitBranch, GitCommitHorizontal } from 'lucide-react';
import IssueKeyText from '@/components/timeline/issue-key-text';
import { MrState } from '@/components/agents/link-meta-parts';
import { PipelineBadge } from '@/components/agents/inspector/gitlab-tab';
import {
  ChangeCard,
  changesLabel,
  ChangeSkeleton,
  ChecksSection,
  DiscussionSection,
  Markdown,
  openInLabel,
} from '@/components/agents/inspector/change-parts';
import { useDiffCommenting } from '@/components/agents/inspector/changes-tab';
import { appFetch } from '@/lib/app-path';
import type { TMrState } from '@/lib/link-meta';
import { gitlabRefOf, type IChangeView } from '@/lib/hl-gitlab-ref';

const DiffFileList = dynamic(() => import('@/components/workspace/diff-file-list'), { ssr: false });

const MR_STATES: readonly TMrState[] = ['opened', 'merged', 'closed', 'locked'];

type TLoad = { url: string; view: IChangeView | null; error: string | null };

const load = async (url: string): Promise<TLoad> => {
  try {
    const response = await appFetch(`/api/platform/change-view?${new URLSearchParams({ url })}`);
    if (response.ok) return { url, view: (await response.json()) as IChangeView, error: null };
    const said = await response
      .json()
      .then((body: { error?: unknown }) => (typeof body?.error === 'string' ? body.error : null))
      .catch(() => null);
    return {
      url,
      view: null,
      error: said ?? t('inspector.gitlabView.httpError', { status: response.status }),
    };
  } catch {
    return { url, view: null, error: t('inspector.gitlabView.unreachable') };
  }
};

const filesOf = (diff: string): number => diff.match(/^diff --git /gm)?.length ?? 0;

/** What the change is called in a comment on a line: `MR !42`, `PR #42`, the short sha. */
const commentRef = (view: IChangeView): string => {
  if (view.kind === 'commit') return view.ref;
  return `${view.provider === 'github' ? 'PR' : 'MR'} ${view.ref}`;
};

interface IGitLabViewProps {
  /** A merge request, a pull request, a commit or an issue, of GitLab or of GitHub. */
  url: string;
  sessionName: string | null;
  /** The conversation whose composer takes comments on diff lines. */
  tabId: string | null;
}

/**
 * A merge request, a pull request, a commit or an issue as a tab of the right panel: GitLab and GitHub refuse frames,
 * so what is on the page comes through the service's API. Top to bottom: the title and state, the description, the
 * checks, the discussion, and the diff folded away until it is asked for; a comment on a line goes to the agent with
 * the next message. When the service gives nothing (no token, a private repository) it is a card with a link out.
 */
const GitLabView = ({ url, sessionName, tabId }: IGitLabViewProps) => {
  const [loaded, setLoaded] = useState<TLoad | null>(null);
  const [diffSeen, setDiffSeen] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void load(url).then((next) => {
      if (!cancelled) setLoaded(next);
    });
    return () => {
      cancelled = true;
    };
  }, [url]);

  const current = loaded?.url === url ? loaded : null;
  const view = current?.view ?? null;
  const repo = view?.project.split('/').at(-1) ?? null;
  const commenting = useDiffCommenting(tabId, repo, view ? commentRef(view) : null);
  const provider = view?.provider ?? gitlabRefOf(url)?.provider ?? 'gitlab';
  const outside = (
    <a
      className="tw-icon-button"
      href={view?.url ?? url}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={openInLabel(provider)}
      title={openInLabel(provider)}
    >
      <ExternalLink />
    </a>
  );

  if (!current) return <ChangeSkeleton />;
  if (!view) {
    return (
      <div className="tw-rpane-commit">
        <header className="tw-rpane-commit-head">
          <h3 className="tw-rpane-commit-title">{current.error}</h3>
          {outside}
        </header>
        <p className="tw-rpane-muted">{t('inspector.gitlabView.opensInGitlab')}</p>
      </div>
    );
  }
  if (view.limited) return <ChangeCard view={view} limit={view.limited} />;

  const state = MR_STATES.find((known) => known === view.state);
  const files = filesOf(view.diff);
  const hasDiff = view.kind !== 'issue';
  return (
    <div className="tw-rpane-commit" data-kind={view.kind} data-provider={view.provider}>
      <header className="tw-rpane-commit-head">
        <h3 className="tw-rpane-commit-title">
          <IssueKeyText text={view.title} />
        </h3>
        {outside}
      </header>
      <p className="tw-rpane-commit-meta">
        {view.kind !== 'commit' && state && (
          <MrState
            meta={{ kind: 'mr', iid: 0, title: view.title, state, project: view.project, draft: view.draft }}
          />
        )}
        {view.kind === 'commit' && <GitCommitHorizontal />}
        <code>
          {repo} {view.ref}
        </code>
        {view.sourceBranch && (
          <span
            className="tw-rpane-commit-branch"
            title={`${view.sourceBranch} → ${view.targetBranch ?? ''}`}
          >
            <GitBranch />
            {view.sourceBranch}
          </span>
        )}
        {view.author && <span>{view.author}</span>}
        {view.at && <span title={dayjs(view.at).format('DD.MM.YYYY HH:mm')}>{dayjs(view.at).fromNow()}</span>}
        {view.kind !== 'issue' && <PipelineBadge pipeline={view.pipeline} />}
      </p>
      {view.labels.length > 0 && (
        <ul className="tw-change-labels">
          {view.labels.map((label) => (
            <li key={label} className="tw-change-label">
              {label}
            </li>
          ))}
        </ul>
      )}
      {view.description && (
        <div className="tw-change-description">
          <Markdown>{view.description}</Markdown>
        </div>
      )}
      {view.stages && view.stages.length > 0 && <ChecksSection stages={view.stages} />}
      {view.comments && view.comments.length > 0 && <DiscussionSection threads={view.comments} />}
      {hasDiff && view.diff && (
        <details
          className="tw-change-diff"
          onToggle={(event) => {
            if (event.currentTarget.open) setDiffSeen(true);
          }}
        >
          <summary>{changesLabel(files)}</summary>
          {diffSeen && (
            <DiffFileList
              diff={view.diff}
              viewMode="unified"
              sessionName={sessionName ?? ''}
              loadContent={false}
              commenting={commenting}
            />
          )}
        </details>
      )}
      {hasDiff && !view.diff && <p className="tw-rpane-muted">{t('inspector.gitlabView.noChanges')}</p>}
      {hasDiff && view.truncated && (
        <p className="tw-inspector-muted tw-repo-note">{t('inspector.gitlabView.truncated')}</p>
      )}
    </div>
  );
};

export default GitLabView;
