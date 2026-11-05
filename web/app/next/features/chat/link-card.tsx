// A link to one of the installation's own documents or boards, alone in its paragraph: a compact card (what it is,
// its name, where it lives) instead of a bare link; a tap opens it in the pane like the chip of any other link.
// Which hosts are ours comes from the page's config (the artifacts, the boards and the reader).
// ⌘/Ctrl-click opens it in a new tab of the browser, and a right click or a held finger opens the link's menu.
import { useContext } from 'react';
import { useLinkMenu } from '@/next/features/chat/link-menu';
import { openOutside } from '@/next/features/chat/pane-sites';
import { isOutsideClick, RefContext } from '@/next/features/chat/ref-chip';
import { appConfig } from '@/platform/config';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export type TCardKind = 'doc' | 'board';

const originOf = (url: string | null): string | null => {
  if (!url) return null;
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
};

/** Which of our pages a link is, or null for any other link. */
export const cardKindOf = (href: string): TCardKind | null => {
  const origin = originOf(href);
  if (!origin) return null;
  const { artifactsUrl, boardsUrl, readerUrl } = appConfig();
  if (origin === originOf(boardsUrl)) return 'board';
  if (origin === originOf(artifactsUrl) || origin === originOf(readerUrl)) return 'doc';
  return null;
};

const ICONS = { doc: 'file-text', board: 'workflow' } as const satisfies Record<TCardKind, string>;

/** The name the card shows: the link's text, else the last part of its path. */
export const cardTitle = (href: string, text: string): string => {
  if (text && text !== href) return text;
  try {
    const last = new URL(href).pathname.split('/').filter(Boolean).pop() ?? '';
    return decodeURIComponent(last) || new URL(href).host;
  } catch {
    return href;
  }
};

interface ILinkCardProps {
  kind: TCardKind;
  href: string;
  text: string;
}

const LinkCard = ({ kind, href, text }: ILinkCardProps) => {
  const { onOpen } = useContext(RefContext);
  const host = new URL(href).host;
  const body = (
    <>
      <span className="hx-file__ico" data-tone="accent">
        <Icon name={ICONS[kind]} />
      </span>
      <span className="hx-file__text">
        <span className="hx-file__name">{cardTitle(href, text)}</span>
        <span className="hx-file__meta">{`${t(`next.rich.link.${kind}`)} · ${host}`}</span>
      </span>
    </>
  );
  const onPane = onOpen && (() => onOpen('link', href, href));
  const { handlers, held, menu } = useLinkMenu({ href, onPane, outside: true });
  return (
    <>
      <a
        className="hx-file hx-linkcard"
        data-open="true"
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        onClick={(event) => {
          if (isOutsideClick(event)) return;
          event.preventDefault();
          if (held()) return;
          if (onPane) onPane();
          else openOutside(href);
        }}
        {...handlers}
      >
        {body}
      </a>
      {menu}
    </>
  );
};

export default LinkCard;
