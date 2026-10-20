import { useState, type MouseEvent, type ReactNode } from 'react';
import { PreviewCard } from '@base-ui/react/preview-card';
import { GitMerge, GitPullRequest, GitPullRequestClosed, GitPullRequestDraft } from 'lucide-react';
import SiteIcon from '@/components/agents/site-icon';
import { scopedAppPath } from '@/lib/app-path';
import {
  ISSUE_TONE_LABEL,
  issueTone,
  MR_STATE_LABEL,
  type IIssueMeta,
  type IMrMeta,
  type TLinkMeta,
} from '@/lib/link-meta';
import type { IPresentedLink } from '@/lib/presented-link';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join('') || '?';

export const AssigneeAvatar = ({
  assignee,
  className,
}: {
  assignee: IIssueMeta['assignee'];
  className?: string;
}) => {
  const [failed, setFailed] = useState(false);
  if (!assignee) return null;
  if (!assignee.avatar || failed) {
    return (
      <span className={cn('tw-link-avatar tw-link-avatar-initials', className)} title={assignee.name}>
        {initials(assignee.name)}
      </span>
    );
  }
  return (
    <img
      className={cn('tw-link-avatar', className)}
      src={scopedAppPath(assignee.avatar)}
      alt={assignee.name}
      title={assignee.name}
      loading="lazy"
      onError={() => setFailed(true)}
    />
  );
};

export const IssueState = ({ meta, withLabel = false }: { meta: IIssueMeta; withLabel?: boolean }) => {
  const tone = issueTone(meta.state, meta.resolved);
  const label = meta.state ?? ISSUE_TONE_LABEL[tone];
  return (
    <span
      className={cn('tw-link-state', `tw-issue-${tone}`, withLabel && 'tw-link-state-labelled')}
      title={label}
    >
      <i aria-hidden="true" />
      {withLabel ? label : <span className="sr-only">{label}</span>}
    </span>
  );
};

const MR_ICONS = {
  opened: GitPullRequest,
  merged: GitMerge,
  closed: GitPullRequestClosed,
  locked: GitPullRequestClosed,
};

export const MrState = ({ meta }: { meta: IMrMeta }) => {
  const Icon = meta.draft && meta.state === 'opened' ? GitPullRequestDraft : MR_ICONS[meta.state];
  return (
    <span className={cn('tw-link-pill', `tw-mr-${meta.state}`)}>
      <Icon aria-hidden="true" />
      {meta.draft && meta.state === 'opened' ? t('agents.linkMetaParts.draft') : MR_STATE_LABEL[meta.state]}
    </span>
  );
};

const PreviewBody = ({ href, meta, link }: { href: string; meta: TLinkMeta; link: IPresentedLink }) => (
  <>
    <div className="tw-link-card-head">
      <SiteIcon href={href} className="tw-link-card-icon" />
      <span>
        {meta.kind === 'issue' && meta.key}
        {meta.kind === 'mr' && `${meta.project} !${meta.iid}`}
        {meta.kind === 'artifact' && t('agents.linkMetaParts.artifact')}
      </span>
    </div>
    <strong className="tw-link-card-title">{meta.title}</strong>
    {meta.kind === 'artifact' && meta.description && <p className="tw-link-card-text">{meta.description}</p>}
    <div className="tw-link-card-meta">
      {meta.kind === 'issue' && <IssueState meta={meta} withLabel />}
      {meta.kind === 'issue' && meta.assignee && (
        <span className="tw-link-card-person">
          <AssigneeAvatar assignee={meta.assignee} />
          {meta.assignee.name}
        </span>
      )}
      {meta.kind === 'issue' && !meta.assignee && (
        <span className="tw-link-card-muted">{t('agents.linkMetaParts.unassigned')}</span>
      )}
      {meta.kind === 'mr' && <MrState meta={meta} />}
      {meta.kind === 'artifact' && <span className="tw-link-card-muted">{link.detail}</span>}
    </div>
  </>
);

interface ILinkHoverProps {
  href: string;
  meta: TLinkMeta;
  link: IPresentedLink;
  className?: string;
  onClick?: (event: MouseEvent<HTMLAnchorElement>) => void;
  children: ReactNode;
}

/** Anchor that shows a small card with the full title, state and assignee on hover. */
export const LinkHover = ({ href, meta, link, className, onClick, children }: ILinkHoverProps) => (
  <PreviewCard.Root>
    <PreviewCard.Trigger
      href={href}
      className={className}
      target="_blank"
      rel="noopener noreferrer"
      delay={350}
      closeDelay={150}
      onClick={onClick}
    >
      {children}
    </PreviewCard.Trigger>
    <PreviewCard.Portal>
      <PreviewCard.Positioner side="top" sideOffset={8} className="tw-link-card-positioner">
        <PreviewCard.Popup className="tw-link-card">
          <PreviewBody href={href} meta={meta} link={link} />
        </PreviewCard.Popup>
      </PreviewCard.Positioner>
    </PreviewCard.Portal>
  </PreviewCard.Root>
);
