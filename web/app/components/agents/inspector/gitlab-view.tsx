import { t } from '@/lib/i18n';
import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import dayjs from 'dayjs';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ExternalLink, GitBranch, GitCommitHorizontal, Loader2 } from 'lucide-react';
import IssueKeyText from '@/components/timeline/issue-key-text';
import { MrState } from '@/components/agents/link-meta-parts';
import { PipelineBadge } from '@/components/agents/inspector/gitlab-tab';
import { useDiffCommenting } from '@/components/agents/inspector/changes-tab';
import { appFetch } from '@/lib/app-path';
import type { TMrState } from '@/lib/link-meta';
import type { IGitLabView } from '@/lib/hl-gitlab-ref';

const DiffFileList = dynamic(() => import('@/components/workspace/diff-file-list'), { ssr: false });

const MR_STATES: readonly TMrState[] = ['opened', 'merged', 'closed', 'locked'];

type TLoad = { url: string; view: IGitLabView | null; error: string | null };

const load = async (url: string): Promise<TLoad> => {
  try {
    const response = await appFetch(`/api/platform/gitlab-view?${new URLSearchParams({ url })}`);
    if (response.ok) return { url, view: (await response.json()) as IGitLabView, error: null };
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

interface IGitLabViewProps {
  /** A merge request or a commit of GitLab. */
  url: string;
  sessionName: string | null;
  /** The conversation whose composer takes comments on diff lines. */
  tabId: string | null;
}

/**
 * A merge request or a commit as a tab of the right panel: GitLab refuses frames, so the title, the state,
 * the pipeline and the diff come through the API, and a comment on a line goes to the agent with the next message.
 */
const GitLabView = ({ url, sessionName, tabId }: IGitLabViewProps) => {
  const [loaded, setLoaded] = useState<TLoad | null>(null);
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
  const commenting = useDiffCommenting(
    tabId,
    repo,
    view ? (view.kind === 'commit' ? view.ref : `MR ${view.ref}`) : null,
  );
  const outside = (
    <a
      className="tw-icon-button"
      href={view?.url ?? url}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={t('inspector.gitlabView.openInGitlab')}
      title={t('inspector.gitlabView.openInGitlab')}
    >
      <ExternalLink />
    </a>
  );

  if (!current)
    return (
      <p className="tw-inspector-muted">
        <Loader2 className="tw-inline-spin" /> {t('inspector.gitlabView.loading')}
      </p>
    );
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

  const state = MR_STATES.find((known) => known === view.state);
  return (
    <div className="tw-rpane-commit" data-kind={view.kind}>
      <header className="tw-rpane-commit-head">
        <h3 className="tw-rpane-commit-title">
          <IssueKeyText text={view.title} />
        </h3>
        {outside}
      </header>
      <p className="tw-rpane-commit-meta">
        {view.kind === 'mr' && state && (
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
        <PipelineBadge pipeline={view.pipeline} />
      </p>
      {view.description && (
        <details className="tw-rpane-commit-description">
          <summary>{t('inspector.gitlabView.description')}</summary>
          <article className="tw-viewer-md">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{view.description}</ReactMarkdown>
          </article>
        </details>
      )}
      {view.diff ? (
        <DiffFileList
          diff={view.diff}
          viewMode="unified"
          sessionName={sessionName ?? ''}
          loadContent={false}
          commenting={commenting}
        />
      ) : (
        <p className="tw-rpane-muted">{t('inspector.gitlabView.noChanges')}</p>
      )}
      {view.truncated && (
        <p className="tw-inspector-muted tw-repo-note">{t('inspector.gitlabView.truncated')}</p>
      )}
    </div>
  );
};

export default GitLabView;
