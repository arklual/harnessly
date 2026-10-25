import { plural, t } from '@/lib/i18n';
import dayjs from 'dayjs';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ExternalLink, GitPullRequest, KeyRound } from 'lucide-react';
import { PipelineIcon, pipelineLabel } from '@/components/agents/inspector/gitlab-tab';
import { cn } from '@/lib/utils';
import type {
  IChangeJob,
  IChangeStage,
  IChangeThread,
  IChangeView,
  TChangeLimit,
  TChangeProvider,
} from '@/lib/hl-gitlab-ref';

const MS = 1000;
const MINUTE = 60;

const SERVICE: Record<TChangeProvider, string> = { gitlab: 'GitLab', github: 'GitHub' };

/** «Open on GitHub» / «Open in GitLab». */
export const openInLabel = (provider: TChangeProvider): string =>
  provider === 'github' ? t('inspector.gitlabView.openInGithub') : t('inspector.gitlabView.openInGitlab');

const FENCE = /(```[\s\S]*?```)/;
const COMMENT = /<!--[\s\S]*?-->/g;
const SUMMARY = /<summary[^>]*>([\s\S]*?)<\/summary>/gi;
const TAG = /<\/?[A-Za-z][^>\n]*>/g;

/**
 * Bots and templates write HTML into Markdown (`<details>`, `<br />`, comments). The page does not run it, and shown as
 * text it is noise: a summary becomes a bold line, the other tags go and their text stays. Code is left as written.
 */
export const withoutHtml = (text: string): string =>
  text
    .split(FENCE)
    .map((part, index) =>
      index % 2 === 1 ? part : part.replace(COMMENT, '').replace(SUMMARY, '**$1**\n').replace(TAG, ''),
    )
    .join('');

/** The text of a description or a comment: Markdown without raw HTML, so nothing from the service runs on the page. */
export const Markdown = ({ children }: { children: string }) => (
  <article className="tw-viewer-md">
    <ReactMarkdown remarkPlugins={[remarkGfm]}>{withoutHtml(children)}</ReactMarkdown>
  </article>
);

export const ChangeSkeleton = () => (
  <div
    className="tw-rpane-commit tw-change-skeleton"
    role="status"
    aria-label={t('inspector.gitlabView.loading')}
  >
    <span className="tw-skeleton tw-change-sk-title" />
    <span className="tw-skeleton tw-change-sk-meta" />
    <span className="tw-skeleton tw-change-sk-line" />
    <span className="tw-skeleton tw-change-sk-line" />
    <span className="tw-skeleton tw-change-sk-line tw-change-sk-short" />
    <span className="tw-skeleton tw-change-sk-block" />
  </div>
);

const CARD_KIND: Record<IChangeView['kind'], Record<TChangeProvider, string>> = {
  mr: { gitlab: t('inspector.changeView.kindMr'), github: t('inspector.changeView.kindPull') },
  commit: { gitlab: t('inspector.changeView.kindCommit'), github: t('inspector.changeView.kindCommit') },
  issue: { gitlab: t('inspector.changeView.kindIssue'), github: t('inspector.changeView.kindIssue') },
};

const LIMIT_KEY: Record<TChangeLimit, string> = {
  'no-token': 'inspector.changeView.limitNoToken',
  'bad-token': 'inspector.changeView.limitBadToken',
  'not-found': 'inspector.changeView.limitNotFound',
  'rate-limit': 'inspector.changeView.limitRate',
  unavailable: 'inspector.changeView.limitUnavailable',
};

/** What is known from the link alone, when the service gave nothing more, and the way out: never an empty frame. */
export const ChangeCard = ({ view, limit }: { view: IChangeView; limit: TChangeLimit }) => (
  <div className="tw-rpane-commit tw-change-card" data-limit={limit}>
    <header className="tw-rpane-commit-head">
      <GitPullRequest className="tw-change-card-icon" aria-hidden="true" />
      <h3 className="tw-rpane-commit-title">
        {CARD_KIND[view.kind][view.provider]} <code>{view.project}</code> <code>{view.ref}</code>
      </h3>
    </header>
    <p className="tw-rpane-muted">{t(LIMIT_KEY[limit], { service: SERVICE[view.provider] })}</p>
    <a className="tw-change-card-link" href={view.url} target="_blank" rel="noopener noreferrer">
      <ExternalLink aria-hidden="true" />
      {openInLabel(view.provider)}
    </a>
    {limit === 'no-token' && (
      <p className="tw-rpane-muted tw-change-card-hint">
        <KeyRound aria-hidden="true" />{' '}
        {t('inspector.changeView.tokenHint', { service: SERVICE[view.provider] })}
      </p>
    )}
  </div>
);

const duration = (ms: number | null): string | null => {
  if (ms === null) return null;
  const seconds = Math.round(ms / MS);
  if (seconds < MINUTE) return t('inspector.changeView.seconds', { n: seconds });
  return t('inspector.changeView.minutes', { m: Math.floor(seconds / MINUTE), s: seconds % MINUTE });
};

const Job = ({ job }: { job: IChangeJob }) => {
  const body = (
    <>
      <span className={cn('tw-change-status', `tw-ci-${job.status}`)} title={pipelineLabel(job.status)}>
        <PipelineIcon status={job.status} />
      </span>
      <span className="tw-change-job-name">{job.name}</span>
      {duration(job.durationMs) && <span className="tw-change-job-time">{duration(job.durationMs)}</span>}
      {job.url && <ExternalLink className="tw-change-job-out" aria-hidden="true" />}
    </>
  );
  return (
    <li>
      {job.url ? (
        <a
          className="tw-change-job"
          href={job.url}
          target="_blank"
          rel="noopener noreferrer"
          title={t('inspector.changeView.openJob')}
        >
          {body}
        </a>
      ) : (
        <span className="tw-change-job">{body}</span>
      )}
    </li>
  );
};

const Stage = ({ stage }: { stage: IChangeStage }) => (
  // What works stays folded; what failed, runs or waits is open.
  <details className="tw-change-stage" open={stage.status !== 'success' && stage.status !== 'skipped'}>
    <summary>
      <span className={cn('tw-change-status', `tw-ci-${stage.status}`)} title={pipelineLabel(stage.status)}>
        <PipelineIcon status={stage.status} />
      </span>
      <span className="tw-change-stage-name">{stage.name}</span>
      <span className="tw-change-stage-count">{stage.jobs.length}</span>
    </summary>
    <ul className="tw-change-jobs">
      {stage.jobs.map((job, index) => (
        <Job key={`${job.name}-${index}`} job={job} />
      ))}
    </ul>
  </details>
);

/** The pipeline or the checks: stages (workflows) with their jobs, a job opens its page. */
export const ChecksSection = ({ stages }: { stages: IChangeStage[] }) => (
  <section className="tw-change-section" aria-label={t('inspector.changeView.checks')}>
    <h4 className="tw-change-heading">{t('inspector.changeView.checks')}</h4>
    <div className="tw-change-stages">
      {stages.map((stage, index) => (
        <Stage key={`${stage.name}-${index}`} stage={stage} />
      ))}
    </div>
  </section>
);

const Thread = ({ thread }: { thread: IChangeThread }) => {
  const where = thread.path ? `${thread.path}${thread.line ? `:${thread.line}` : ''}` : null;
  const notes = thread.notes.map((note, index) => (
    <div className="tw-change-note" key={index}>
      <p className="tw-change-note-head">
        <b>{note.author ?? '—'}</b>
        {note.at && <span title={dayjs(note.at).format('DD.MM.YYYY HH:mm')}>{dayjs(note.at).fromNow()}</span>}
      </p>
      <Markdown>{note.body}</Markdown>
    </div>
  ));
  const head = (
    <>
      {where && <code className="tw-change-thread-where">{where}</code>}
      {thread.outdated && <span className="tw-change-tag">{t('inspector.changeView.outdated')}</span>}
      {thread.resolved === true && (
        <span className="tw-change-tag" data-resolved>
          {t('inspector.changeView.resolved')}
        </span>
      )}
      {thread.resolved === false && (
        <span className="tw-change-tag" data-unresolved>
          {t('inspector.changeView.unresolved')}
        </span>
      )}
    </>
  );
  if (thread.resolved === true)
    return (
      <li className="tw-change-thread" data-resolved>
        <details>
          <summary className="tw-change-thread-head">
            {head}
            <span className="tw-change-thread-by">{thread.notes[0]?.author ?? ''}</span>
          </summary>
          {notes}
        </details>
      </li>
    );
  return (
    <li className="tw-change-thread">
      {(where || thread.resolved !== null || thread.outdated) && (
        <div className="tw-change-thread-head">{head}</div>
      )}
      {notes}
    </li>
  );
};

/** Comments and discussions with their authors, time and whether a thread is resolved. */
export const DiscussionSection = ({ threads }: { threads: IChangeThread[] }) => (
  <section className="tw-change-section" aria-label={t('inspector.changeView.discussion')}>
    <h4 className="tw-change-heading">
      {t('inspector.changeView.discussion')} <span className="tw-change-count">{threads.length}</span>
    </h4>
    <ul className="tw-change-threads">
      {threads.map((thread) => (
        <Thread key={thread.id} thread={thread} />
      ))}
    </ul>
  </section>
);

/** «Changes · 3 files» */
export const changesLabel = (files: number): string => plural(files, 'inspector.changeView.changes');
