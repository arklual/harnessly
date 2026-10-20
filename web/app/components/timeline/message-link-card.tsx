import { Children, isValidElement, type HTMLAttributes, type ReactElement, type ReactNode } from 'react';
import { ArrowUpRight } from 'lucide-react';
import SiteIcon from '@/components/agents/site-icon';
import { AssigneeAvatar, IssueState, MrState } from '@/components/agents/link-meta-parts';
import useLinkMeta from '@/hooks/use-link-meta';
import { hasLinkMeta, presentLink } from '@/lib/presented-link';
import { openLinkInPane } from '@/lib/hl-pane-link';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import { appConfig } from '@/platform/config';

const KIND_CAPTION: Record<string, string> = {
  artifact: t('timeline.messageLinkCard.artifact', { product: appConfig().productName }),
  issue: t('timeline.messageLinkCard.issue'),
  gitlab: 'GitLab',
  github: 'GitHub',
  web: t('timeline.messageLinkCard.link'),
  file: t('timeline.messageLinkCard.file'),
  mail: t('timeline.messageLinkCard.mail'),
};

const textOf = (node: ReactNode): string =>
  Children.toArray(node)
    .map((child) =>
      typeof child === 'string'
        ? child
        : isValidElement(child)
          ? textOf((child.props as { children?: ReactNode }).children)
          : '',
    )
    .join('');

/** A link that stands alone in a paragraph or list item becomes a card, like link unfurls in chat apps. */
export const LinkCard = ({ href, label }: { href: string; label: string }) => {
  const link = presentLink(href);
  const meta = useLinkMeta(hasLinkMeta(link.kind) ? href : null);
  const labelIsUrl =
    !label ||
    label.replace(/^https?:\/\//, '').replace(/\/$/, '') ===
      href.replace(/^https?:\/\//, '').replace(/\/$/, '');
  const title =
    meta?.kind === 'issue'
      ? meta.title
      : meta?.kind === 'mr'
        ? meta.title
        : meta?.kind === 'artifact'
          ? meta.title
          : labelIsUrl
            ? link.label
            : label;
  const caption =
    meta?.kind === 'issue'
      ? meta.key
      : meta?.kind === 'mr'
        ? `${meta.project} · !${meta.iid}`
        : (KIND_CAPTION[link.kind] ?? t('timeline.messageLinkCard.link'));
  return (
    <a
      className={cn('tw-link-block', `tw-link-block-${meta?.kind ?? link.kind}`)}
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={href.replace(/([?&])k=[^&]+/, '$1k=…')}
      onClick={(event) => openLinkInPane(event, href, labelIsUrl ? null : label)}
      onAuxClick={(event) => openLinkInPane(event, href, labelIsUrl ? null : label)}
    >
      <span className="tw-link-block-icon">
        <SiteIcon href={href} className="tw-link-block-glyph" />
      </span>
      <span className="tw-link-block-text">
        <span className="tw-link-block-caption">
          {meta?.kind === 'issue' && meta.parent ? (
            <>
              <span className="tw-link-block-epic" title={meta.parent.title}>
                {meta.parent.key} · {meta.parent.title}
              </span>
              <span aria-hidden="true"> › </span>
            </>
          ) : null}
          {caption}
        </span>
        <strong>{title}</strong>
        <small>{meta?.kind === 'artifact' && meta.description ? meta.description : link.detail}</small>
      </span>
      <span className="tw-link-block-side">
        {meta?.kind === 'issue' && (
          <>
            <IssueState meta={meta} withLabel />
            <AssigneeAvatar assignee={meta.assignee} />
          </>
        )}
        {meta?.kind === 'mr' && <MrState meta={meta} />}
        <ArrowUpRight className="tw-link-block-go" />
      </span>
    </a>
  );
};

const soleLink = (children: ReactNode): ReactElement<{ href?: string; children?: ReactNode }> | null => {
  const parts = Children.toArray(children).filter((child) => !(typeof child === 'string' && !child.trim()));
  if (parts.length !== 1 || !isValidElement(parts[0])) return null;
  const el = parts[0] as ReactElement<{ href?: string; children?: ReactNode }>;
  // Keys the agent wrote as plain text stay inline chips even when alone on a line.
  if ((el.props as Record<string, unknown>)['data-issue-key']) return null;
  return typeof el.props.href === 'string' && /^https?:\/\//.test(el.props.href) ? el : null;
};

type TMdProps<T> = HTMLAttributes<T> & { node?: unknown };

export const MessageParagraph = ({ children, node: _node, ...props }: TMdProps<HTMLParagraphElement>) => {
  const link = soleLink(children);
  if (link)
    return (
      <div className="tw-link-block-wrap">
        <LinkCard href={link.props.href!} label={textOf(link.props.children)} />
      </div>
    );
  return <p {...props}>{children}</p>;
};

export const MessageListItem = ({ children, node: _node, ...props }: TMdProps<HTMLLIElement>) => {
  const link = soleLink(children);
  if (link)
    return (
      <li {...props} className="tw-link-block-li">
        <LinkCard href={link.props.href!} label={textOf(link.props.children)} />
      </li>
    );
  return <li {...props}>{children}</li>;
};
