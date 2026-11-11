// The head of the review: what the change is. A merge request: its title, author, branches, state, pipeline (with the
// failed job), approvals and reviewers, labels, tracker tasks as chips, size, commits, how far it is behind its target
// and whether it conflicts. Local work: the repository, branch, base, commits and what is uncommitted. A commit: its
// title, author and time. `actions` is where the header's buttons go.
import { useState, type ReactNode } from 'react';
import { issueKeysOf, issueLink } from '@/next/features/review/issue-keys';
import { appConfig } from '@/platform/config';
import type { IMrMeta, IReviewCommit, TReviewMeta } from '@/next/features/review/model';
import { Counts } from '@/next/features/review/review-tree';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { plural, t } from '@/next/i18n';

const LIST_MS = 160;
const SHORT_SHA = 8;

interface ISize {
  additions: number;
  deletions: number;
  files: number;
  truncated: boolean;
}

const Size = ({ size }: { size: ISize }) => (
  <span className="hx-review__chip" data-tone="plain">
    <Counts add={size.additions} del={size.deletions} />
    <span>
      {plural(size.files, 'next.review.filesCount')}
      {size.truncated ? '+' : ''}
    </span>
  </span>
);

const Commits = ({ commits }: { commits: readonly IReviewCommit[] }) => {
  const [open, setOpen] = useState(false);
  const { mounted, state } = usePresentFlag(open, LIST_MS);
  if (!commits.length) return null;
  return (
    <div className="hx-review__commits">
      <button
        type="button"
        className="hx-review__chip"
        data-tone="plain"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon name="git-branch" size="sm" />
        {plural(commits.length, 'next.review.commitsCount')}
        <Icon name="chevron-down" size="sm" className="hx-review__chev" />
      </button>
      {mounted && (
        <ol className="hx-review__commit-list" data-state={state}>
          {commits.map((commit) => (
            <li key={commit.sha}>
              <code>{commit.sha.slice(0, SHORT_SHA)}</code>
              <span className="hx-review__commit-title">{commit.title}</span>
              {commit.author && <span className="hx-review__muted">{commit.author}</span>}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
};

const Issues = ({ texts }: { texts: (string | null)[] }) => {
  const config = appConfig();
  const keys = issueKeysOf(texts, config.taskKeyPattern);
  if (!keys.length) return null;
  return (
    <>
      {keys.map((key) => {
        const href = issueLink(key, config.trackerIssueUrl);
        return href ? (
          <a
            key={key}
            className="hx-review__chip"
            data-tone="issue"
            href={href}
            target="_blank"
            rel="noreferrer"
          >
            {key}
          </a>
        ) : (
          <span key={key} className="hx-review__chip" data-tone="issue">
            {key}
          </span>
        );
      })}
    </>
  );
};

const stateLabel = (meta: IMrMeta): string => {
  if (meta.draft && meta.state === 'opened') return t('next.review.stateDraft');
  if (meta.state === 'merged') return t('next.review.stateMerged');
  if (meta.state === 'closed') return t('next.review.stateClosed');
  return t('next.review.stateOpen');
};

const MrHead = ({ meta, size }: { meta: IMrMeta; size: ISize }) => (
  <>
    <div className="hx-review__chips">
      <span
        className="hx-review__chip"
        data-tone="state"
        data-state={meta.draft ? 'draft' : (meta.state ?? 'opened')}
      >
        {stateLabel(meta)}
      </span>
      <code className="hx-review__muted">
        {meta.project} {meta.ref}
      </code>
      {meta.author && <span className="hx-review__muted">{meta.author}</span>}
      {meta.sourceBranch && (
        <span
          className="hx-review__chip"
          data-tone="plain"
          title={`${meta.sourceBranch} → ${meta.targetBranch ?? ''}`}
        >
          <Icon name="git-branch" size="sm" />
          <span className="hx-review__branch">{meta.sourceBranch}</span>
          {meta.targetBranch && (
            <>
              <Icon name="arrow-right" size="sm" />
              <span className="hx-review__branch">{meta.targetBranch}</span>
            </>
          )}
        </span>
      )}
      {meta.pipeline && (
        <a
          className="hx-review__chip"
          data-tone="pipeline"
          data-status={meta.pipeline.status}
          href={meta.pipeline.failedJobUrl ?? meta.pipeline.url ?? undefined}
          target="_blank"
          rel="noreferrer"
          title={meta.pipeline.failedJobUrl ? t('next.review.failedJob') : t('next.review.pipeline')}
        >
          <Icon name="circle-dot" size="sm" />
          {t(`next.review.pipeline.${meta.pipeline.status}`)}
        </a>
      )}
      {meta.conflicts && (
        <span className="hx-review__chip" data-tone="warn">
          <Icon name="triangle-alert" size="sm" />
          {t('next.review.conflicts')}
        </span>
      )}
      {meta.behind !== null && meta.behind > 0 && (
        <span className="hx-review__chip" data-tone="warn">
          {t('next.review.behind', { n: meta.behind, target: meta.targetBranch ?? 'main' })}
        </span>
      )}
    </div>
    <div className="hx-review__chips">
      <Size size={size} />
      {(meta.approvedBy.length > 0 || meta.approvalsLeft !== null) && (
        <span className="hx-review__chip" data-tone={meta.approvalsLeft ? 'plain' : 'ok'}>
          <Icon name="circle-check" size="sm" />
          {meta.approvedBy.length ? meta.approvedBy.join(', ') : t('next.review.noApprovals')}
          {meta.approvalsLeft ? ` · ${t('next.review.approvalsLeft', { n: meta.approvalsLeft })}` : ''}
        </span>
      )}
      {meta.reviewers.length > 0 && (
        <span className="hx-review__chip" data-tone="plain" title={t('next.review.reviewers')}>
          <Icon name="users" size="sm" />
          {meta.reviewers.join(', ')}
        </span>
      )}
      {meta.labels.map((label) => (
        <span key={label} className="hx-review__chip" data-tone="label">
          {label}
        </span>
      ))}
      <Issues texts={[meta.title, ...meta.commits.map((c) => c.title)]} />
    </div>
    <Commits commits={meta.commits} />
  </>
);

interface IReviewHeaderProps {
  meta: TReviewMeta;
  size: ISize;
  actions?: ReactNode;
}

const titleOf = (meta: TReviewMeta): string => {
  if (meta.kind === 'local') return meta.repo;
  return meta.title;
};

const ReviewHeader = ({ meta, size, actions }: IReviewHeaderProps) => (
  <header className="hx-review__head">
    <div className="hx-review__title-row">
      <h3 className="hx-review__title">{titleOf(meta)}</h3>
      <div className="hx-review__actions">{actions}</div>
    </div>
    {meta.kind === 'mr' && <MrHead meta={meta} size={size} />}
    {meta.kind === 'commit' && (
      <div className="hx-review__chips">
        <code className="hx-review__muted">
          {meta.project} {meta.ref}
        </code>
        {meta.author && <span className="hx-review__muted">{meta.author}</span>}
        <Size size={size} />
        <Issues texts={[meta.title]} />
      </div>
    )}
    {meta.kind === 'local' && (
      <>
        <div className="hx-review__chips">
          {meta.branch && (
            <span className="hx-review__chip" data-tone="plain">
              <Icon name="git-branch" size="sm" />
              <span className="hx-review__branch">{meta.branch}</span>
            </span>
          )}
          {meta.base && (
            <span className="hx-review__muted">{t('next.review.base', { base: meta.base })}</span>
          )}
          {meta.uncommitted && (
            <span className="hx-review__chip" data-tone="warn">
              {t('next.review.uncommitted')}
            </span>
          )}
          <Size size={size} />
          <Issues texts={meta.commits.map((c) => c.title)} />
        </div>
        <Commits commits={meta.commits} />
      </>
    )}
  </header>
);

export default ReviewHeader;
