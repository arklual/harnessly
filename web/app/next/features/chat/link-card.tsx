// A link to one of the installation's own documents or boards, alone in its paragraph: a compact card (what it is,
// its name, where it lives) instead of a bare link; a tap opens it in the pane like the chip of any other link.
// Which hosts are ours comes from the page's config (the artifacts, the boards and the reader).
// ⌘/Ctrl-click opens it in a new tab of the browser, and a right click or a held finger opens the link's menu.
import { createContext, lazy, Suspense, useContext, useState, type ReactNode } from 'react';
import { useLinkMenu } from '@/next/features/chat/link-menu';
import { previewSrc, useLinkCardMeta, whenText } from '@/next/features/chat/link-card-meta';
import { openOutside } from '@/next/features/chat/pane-sites';
import { isOutsideClick, RefContext } from '@/next/features/chat/ref-chip';
import { appConfig, type IAppConfig } from '@/platform/config';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

// The share window (Sinopia's access dialog) is loaded on «Share» only: a static import put the whole dialog into the
// feed's Markdown chunk, which every message with a link loads.
const ObjectShare = lazy(() => import('@/next/features/app-main/object-share'));

export type TCardKind = 'doc' | 'board';

/** The answer's links stay links: their cards are in the block «Сделано за ход» at its end (outputs.tsx). */
export const LinkCardsInline = createContext(false);

const parse = (url: string | null): URL | null => {
  if (!url) return null;
  try {
    return new URL(url.replace('{slug}', ''));
  } catch {
    return null;
  }
};

const SLUG = String.raw`[\w-]+`;
const DOC_PATH = new RegExp(String.raw`^/a/${SLUG}/?$`);
const BOARD_PATH = new RegExp(String.raw`^/b/(${SLUG})/?$`);
const escape = (text: string): string => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/**
 * Which of our pages a link is, or null for any other link, by the exact patterns the runtime's
 * `open` reads (internal/panel Classify): a document is `/a/<slug>` of the documents' server or `<reader path><slug>` of
 * the platform's reader, a board `/b/<slug>` of the boards' server. The host alone tells nothing: the portal serves
 * documents and boards on one host, and the platform's other pages are no document.
 */
export const cardKindOf = (
  href: string,
  config: Pick<IAppConfig, 'artifactsUrl' | 'boardsUrl' | 'readerUrl'> = appConfig(),
): TCardKind | null => {
  const url = parse(href);
  if (!url || (url.protocol !== 'https:' && url.protocol !== 'http:')) return null;
  const path = url.pathname;
  const boards = parse(config.boardsUrl);
  const board = BOARD_PATH.exec(path);
  if (boards && url.origin === boards.origin && board && board[1] !== 'api') return 'board';
  const docs = parse(config.artifactsUrl);
  if (docs && url.origin === docs.origin && DOC_PATH.test(path)) return 'doc';
  const reader = parse(config.readerUrl);
  if (reader && url.origin === reader.origin) {
    const prefix = reader.pathname.endsWith('/') ? reader.pathname : `${reader.pathname}/`;
    if (new RegExp(String.raw`^${escape(prefix)}${SLUG}/?$`).test(path)) return 'doc';
  }
  return null;
};

/** The slug of a link to a board of the installation's boards service; null for any other link. */
export const boardSlugOf = (
  href: string,
  config: Pick<IAppConfig, 'boardsUrl'> = appConfig(),
): string | null => {
  const url = parse(href);
  const boards = parse(config.boardsUrl);
  if (!url || !boards || url.origin !== boards.origin) return null;
  const m = BOARD_PATH.exec(url.pathname);
  return m && m[1] !== 'api' && m[1] !== 'app' ? m[1] : null;
};

/** The slug of a link to one of our documents or boards: what the hub's card is asked by; null for any other. */
export const cardSlugOf = (
  href: string,
  config: Pick<IAppConfig, 'artifactsUrl' | 'boardsUrl' | 'readerUrl'> = appConfig(),
): string | null => {
  const kind = cardKindOf(href, config);
  const url = parse(href);
  if (kind === null || url === null) return null;
  if (kind === 'board') return boardSlugOf(href, config);
  const last = url.pathname.split('/').filter(Boolean).pop() ?? '';
  return new RegExp(String.raw`^${SLUG}$`).test(last) ? last : null;
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
  // What the hub says of it, for a person who may read it: the name, who changed it last and when.
  const slug = cardSlugOf(href);
  const meta = useLinkCardMeta(slug);
  const line = meta
    ? [t(`next.rich.link.${kind}`), meta.editor, whenText(meta.editedAt)].filter(Boolean).join(' · ')
    : `${t(`next.rich.link.${kind}`)} · ${host}`;
  const body = (
    <>
      {meta?.preview && slug ? (
        <span className="hx-file__ico hx-linkcard__thumb">
          <img src={previewSrc(slug)} alt="" loading="lazy" decoding="async" />
        </span>
      ) : meta?.page && slug ? (
        <span className="hx-file__ico hx-linkcard__thumb hx-linkcard__thumb--page" aria-hidden="true">
          <iframe src={previewSrc(slug)} title="" sandbox="allow-scripts" loading="lazy" tabIndex={-1} />
        </span>
      ) : (
        <span className="hx-file__ico" data-tone="accent">
          <Icon name={ICONS[kind]} />
        </span>
      )}
      <span className="hx-file__text">
        <span className="hx-file__name">{meta?.title ?? cardTitle(href, text)}</span>
        <span className="hx-file__meta">{line}</span>
        {meta?.excerpt ? <span className="hx-linkcard__excerpt">{meta.excerpt}</span> : null}
      </span>
    </>
  );
  const onPane = onOpen && (() => onOpen('link', href, href));
  // A board or a document the hub tells about (the person may read it) is shared in the one dialog.
  const [sharing, setSharing] = useState(false);
  const onShare = meta && slug ? () => setSharing(true) : null;
  const { handlers, held, menu } = useLinkMenu({ href, onPane, outside: true, onShare });
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
      {sharing && slug && (
        <Suspense fallback={null}>
          <ObjectShare
            kind={kind === 'board' ? 'board' : 'artifact'}
            slug={slug}
            open
            onClose={() => setSharing(false)}
            fallbackLink={href}
          />
        </Suspense>
      )}
    </>
  );
};

/** A link alone in its paragraph: its card, or the paragraph as it is where the answer keeps its links inline. */
export const LoneLink = ({ children, ...card }: ILinkCardProps & { children: ReactNode }) =>
  useContext(LinkCardsInline) ? <p>{children}</p> : <LinkCard {...card} />;

export default LinkCard;
