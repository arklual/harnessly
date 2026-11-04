// A reference in the agent's text: an icon and a label, a card on hover after 280 ms (the design's .hx-ref), a click
// opens it. What the page knows about it comes from the feed's refs; otherwise only the label shows.
//
// A merge request, a pull request or a commit is a chip only from its link (markdown.tsx): the page never guesses the
// project of a bare `!80`, so no chip opens the wrong place or nothing.
//
// A link (and a merge request's) is an <a>: a plain click opens our site in the pane and any other in a new tab of the
// browser (features/chat/pane-sites.ts); ⌘/Ctrl-click, the middle button and the menu's «Открыть в браузере» always
// open it outside; a right click or a held finger opens its menu.
import { createContext, useContext, type MouseEvent } from 'react';
import { useLinkMenu } from '@/next/features/chat/link-menu';
import { LINK_PLACE, linkPlace, openOutside, opensOutside } from '@/next/features/chat/pane-sites';
import { bareRef } from '@/next/features/chat/refs';
import { usePaneSites } from '@/next/stores/pane-sites';
import type { RefInfo, RefKind } from '@/next/features/chat/types';
import Icon, { type TIconName } from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface IRefContext {
  refs: Readonly<Record<string, RefInfo>>;
  onOpen: ((kind: RefKind, label: string, href: string | null) => void) | null;
  /** The Markdown files the conversation touched: a path to one in inline code is a chip. */
  docs?: ReadonlySet<string>;
}

export const RefContext = createContext<IRefContext>({ refs: {}, onOpen: null });

const ICONS: Record<RefKind, TIconName> = {
  task: 'list-checks',
  mr: 'git-pull-request',
  file: 'file-code',
  link: 'link',
};

const KIND_KEYS: Record<RefKind, string> = {
  task: 'next.ref.task',
  mr: 'next.ref.mr',
  file: 'next.ref.file',
  link: 'next.ref.link',
};

interface IRefChipProps {
  kind: RefKind;
  label: string;
  href?: string | null;
  /** What the chip says when it is not the label itself (a link's text). */
  text?: string;
}

interface IChipBodyProps {
  kind: RefKind;
  label: string;
  text?: string;
}

const ChipBody = ({ kind, label, text }: IChipBodyProps) => {
  const { refs } = useContext(RefContext);
  const info = refs[label] ?? bareRef(kind, label);
  return (
    <>
      <Icon name={ICONS[kind]} size="sm" />
      <span className="hx-ref__label">{text ?? info.label}</span>
      {info.title && (
        <span className="hx-ref__card" role="tooltip">
          <span className="hx-ref__kind">
            <Icon name={ICONS[kind]} size="sm" />
            {t(KIND_KEYS[kind])}
          </span>
          <span className="hx-ref__title">{info.title}</span>
          {(info.status || info.meta) && (
            <span className="hx-ref__row">
              {info.status && (
                <span className="hx-ref__status" data-tone={info.tone ?? undefined}>
                  {info.status}
                </span>
              )}
              {info.meta && <span className="hx-ref__meta">{info.meta}</span>}
            </span>
          )}
          <span className="hx-ref__go">{t('next.ref.open')}</span>
        </span>
      )}
    </>
  );
};

/** A click the browser itself opens in a new tab: ⌘/Ctrl, Shift or the middle button. */
export const isOutsideClick = (
  event: Pick<MouseEvent, 'metaKey' | 'ctrlKey' | 'shiftKey' | 'button'>,
): boolean => event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1;

interface ILinkChipProps extends IChipBodyProps {
  href: string;
}

// A link the person's browser can open: our site opens in the pane, any other outside, as the chip's rule says.
const LinkChip = ({ kind, label, href, text }: ILinkChipProps) => {
  const { onOpen } = useContext(RefContext);
  const sites = usePaneSites();
  const onPane = onOpen && (() => onOpen(kind, label, href));
  const { handlers, held, menu } = useLinkMenu({ href, onPane, outside: true });
  // A merge request opens with its diff in the pane wherever its GitLab is.
  const inPane = onPane !== null && (kind === 'mr' || linkPlace(href, sites) === LINK_PLACE.pane);
  const click = (event: MouseEvent<HTMLAnchorElement>) => {
    if (isOutsideClick(event)) return;
    event.preventDefault();
    if (held()) return;
    if (inPane) onPane();
    else openOutside(href);
  };
  return (
    <>
      <a
        className="hx-ref"
        data-type={kind}
        data-place={inPane ? LINK_PLACE.pane : LINK_PLACE.outside}
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        onClick={click}
        {...handlers}
      >
        <ChipBody kind={kind} label={label} text={text} />
      </a>
      {menu}
    </>
  );
};

const RefChip = ({ kind, label, href = null, text }: IRefChipProps) => {
  const { onOpen } = useContext(RefContext);
  if ((kind === 'link' || kind === 'mr') && opensOutside(href))
    return <LinkChip kind={kind} label={label} href={href} text={text} />;
  const open = () => onOpen?.(kind, label, href);
  return (
    <span
      className="hx-ref"
      data-type={kind}
      role="button"
      tabIndex={0}
      onClick={open}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          open();
        }
      }}
    >
      <ChipBody kind={kind} label={label} text={text} />
    </span>
  );
};

export default RefChip;
