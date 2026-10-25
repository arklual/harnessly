// A reference in the agent's text: an icon and a label, a card on hover after 280 ms (the design's .hx-ref), a click
// opens it. What the page knows about it comes from the feed's refs; otherwise only the label shows.
//
// A merge request, a pull request or a commit is a chip only from its link (markdown.tsx): the page never guesses the
// project of a bare `!80`, so no chip opens the wrong place or nothing.
import { createContext, useContext } from 'react';
import { bareRef } from '@/next/features/chat/refs';
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

const RefChip = ({ kind, label, href = null, text }: IRefChipProps) => {
  const { refs, onOpen } = useContext(RefContext);
  const info = refs[label] ?? bareRef(kind, label);
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
      <Icon name={ICONS[kind]} size="sm" />
      {text ?? info.label}
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
    </span>
  );
};

export default RefChip;
