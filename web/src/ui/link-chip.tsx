import { FileText, PenLine } from 'lucide-react';
import { type ReactNode, isValidElement } from 'react';

import { linkTitle, recognize, type ServiceLink } from '../lib/links';
import { integrationsStore, openInPane } from '../stores/side-pane';
import { useStore } from '../stores/store';

/** A board or document link as a chip that opens it in the side pane. */
export function LinkChip({ link, label }: { link: ServiceLink; label?: string }) {
  return (
    <button
      type="button"
      className="link-chip"
      data-kind={link.kind}
      data-testid={`${link.kind}-chip`}
      title={link.url}
      onClick={() => openInPane(link)}
    >
      {link.kind === 'board' ? <PenLine size={12} /> : <FileText size={12} />}
      <span className="link-chip__label">{label || linkTitle(link)}</span>
    </button>
  );
}

function textOf(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(textOf).join('');
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return '';
}

/** A link in an agent's markdown: a chip for a board or a document, else a link that opens in a new tab. */
export function MarkdownLink({ href, children }: { href?: string; children?: ReactNode }) {
  const services = useStore(integrationsStore);
  const link = href ? recognize(href, services) : null;
  if (link) {
    const text = textOf(children).trim();
    return <LinkChip link={link} label={text && text !== href ? text : undefined} />;
  }
  return (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

const URL_RE = /(https?:\/\/[^\s<>"'`]+[^\s<>"'`.,;:!?)\]])/g;

/** Plain text (a person's message) with its board and document links as chips. */
export function TextWithChips({ text }: { text: string }) {
  const services = useStore(integrationsStore);
  if (!services.boards && !services.artifacts) return <>{text}</>;
  const parts = text.split(URL_RE);
  return (
    <>
      {parts.map((part, i) => {
        const link = i % 2 === 1 ? recognize(part, services) : null;
        return link ? <LinkChip key={i} link={link} /> : part;
      })}
    </>
  );
}
