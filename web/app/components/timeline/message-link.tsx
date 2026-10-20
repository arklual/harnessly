import { Children, useState, type AnchorHTMLAttributes, type MouseEvent, type ReactNode } from 'react';
import { ArrowUpRight } from 'lucide-react';
import { hasLinkMeta, presentLink } from '@/lib/presented-link';
import useLinkMeta from '@/hooks/use-link-meta';
import SiteIcon from '@/components/agents/site-icon';
import { AssigneeAvatar, IssueState, LinkHover, MrState } from '@/components/agents/link-meta-parts';
import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import { isImageHref, isPlainClick, linkImageName } from '@/lib/image-link';
import { openLinkInPane } from '@/lib/hl-pane-link';
import { cn } from '@/lib/utils';
import type { TLinkMeta } from '@/lib/link-meta';

const isRawText = (href: string, children: ReactNode) => {
  const parts = Children.toArray(children);
  if (parts.length === 0) return true;
  if (!parts.every((child) => typeof child === 'string')) return false;
  const text = parts.join('').trim();
  const bare = (value: string) => value.replace(/^https?:\/\//, '').replace(/\/$/, '');
  return text === href || bare(text) === bare(href);
};

const RichLabel = ({ meta, custom }: { meta: TLinkMeta; custom: ReactNode | null }) => {
  if (meta.kind === 'issue') {
    return (
      <>
        <span className="message-link-text">
          <b>{meta.key}</b>
        </span>
        <IssueState meta={meta} />
        <AssigneeAvatar assignee={meta.assignee} />
      </>
    );
  }
  if (meta.kind === 'mr') {
    return (
      <>
        <span className="message-link-text">
          <b>
            {meta.project.split('/').at(-1)} !{meta.iid}
          </b>
        </span>
        <MrState meta={meta} />
      </>
    );
  }
  return <span className="message-link-text">{custom ?? meta.title}</span>;
};

const MessageLink = ({ href = '', children }: AnchorHTMLAttributes<HTMLAnchorElement>) => {
  const link = presentLink(href);
  const meta = useLinkMeta(hasLinkMeta(link.kind) ? href : null);
  const raw = isRawText(href, children);
  const [viewing, setViewing] = useState<IAttachmentView | null>(null);

  if (isImageHref(href)) {
    const open = (event: MouseEvent<HTMLAnchorElement>) => {
      if (!isPlainClick(event.nativeEvent)) return;
      event.preventDefault();
      setViewing({
        kind: 'image',
        url: href,
        name: linkImageName(href, Children.toArray(children).join('')),
      });
    };
    return (
      <>
        <a
          href={href}
          className={cn('message-link', `message-link-${link.kind}`)}
          target="_blank"
          rel="noopener noreferrer"
          title={link.detail}
          onClick={open}
        >
          <SiteIcon href={href} className="message-link-icon" />
          <span>{raw ? link.label : children}</span>
        </a>
        <AttachmentViewer item={viewing} onClose={() => setViewing(null)} />
      </>
    );
  }

  const label = raw
    ? null
    : Children.toArray(children)
        .filter((child) => typeof child === 'string')
        .join('')
        .trim() || null;
  const openInPane = (event: MouseEvent<HTMLAnchorElement>) => openLinkInPane(event, href, label);

  if (meta) {
    return (
      <LinkHover
        href={href}
        meta={meta}
        link={link}
        className={cn('message-link', 'message-link-rich', `message-link-${meta.kind}`)}
        onClick={openInPane}
      >
        <SiteIcon href={href} className="message-link-icon" />
        <RichLabel meta={meta} custom={raw ? null : children} />
      </LinkHover>
    );
  }

  return (
    <a
      href={href}
      className={cn('message-link', `message-link-${link.kind}`)}
      data-testid={link.kind === 'artifact' ? (link.board ? 'board-chip' : 'document-chip') : undefined}
      target="_blank"
      rel="noopener noreferrer"
      title={link.detail}
      onClick={openInPane}
      onAuxClick={openInPane}
    >
      <SiteIcon href={href} className="message-link-icon" />
      <span>{raw ? link.label : children}</span>
      <ArrowUpRight className="message-link-arrow" aria-hidden="true" />
    </a>
  );
};

export default MessageLink;
