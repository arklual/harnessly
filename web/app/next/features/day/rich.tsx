// A line of the feed's text with links in it: to a thing to do, to a slot of the calendar, down the page to a block.
// And the inline marks of a thing's text: **bold** and [label|icon], a material named in passing.
import { Fragment, type ReactNode } from 'react';
import type { TItemId, TRichPart } from '@/next/features/day/feed';
import { feedIcon } from '@/next/features/day/icons';
import Icon from '@/next/ui/icon';

export interface IRichActions {
  onItem: (id: TItemId, from: HTMLElement) => void;
  onSlot: (key: string) => void;
  onJump: (to: 'pri' | 'cal') => void;
}

interface IRichLineProps extends IRichActions {
  parts: TRichPart[];
  /** The look of a link: in the greeting's line or in the digest. */
  tone: 'hello' | 'digest';
}

export const RichLine = ({ parts, tone, onItem, onSlot, onJump }: IRichLineProps) => (
  <>
    {parts.map((part, i) => {
      if (typeof part === 'string') return <Fragment key={i}>{part}</Fragment>;
      const { item, slot, jump } = part;
      if (item === undefined && slot === undefined && jump === undefined)
        return <Fragment key={i}>{part.t}</Fragment>;
      const go = (from: HTMLElement) => {
        if (item !== undefined) onItem(item, from);
        else if (slot !== undefined) onSlot(slot);
        else if (jump !== undefined) onJump(jump);
      };
      // A span, not a button: the link wraps with the words around it, and the comma after it stays on its line.
      return (
        <span
          key={i}
          role="button"
          tabIndex={0}
          className="hx-day-link"
          data-tone={tone}
          onClick={(event) => go(event.currentTarget)}
          onKeyDown={(event) => {
            if (event.key !== 'Enter' && event.key !== ' ') return;
            event.preventDefault();
            go(event.currentTarget);
          }}
        >
          {part.t}
        </span>
      );
    })}
  </>
);

const MARK = /\*\*([^*]+)\*\*|\[([^|\]]+)\|([a-z0-9-]+)\]/g;

/** Text with **bold** and [label|icon] in it. */
export const inline = (text: string): ReactNode[] => {
  const out: ReactNode[] = [];
  let at = 0;
  for (const match of text.matchAll(MARK)) {
    const [whole, bold, label, icon] = match;
    if (match.index > at) out.push(text.slice(at, match.index));
    if (bold !== undefined) out.push(<b key={match.index}>{bold}</b>);
    else if (label !== undefined && icon !== undefined)
      out.push(
        <span key={match.index} className="hx-day-ln">
          <Icon name={feedIcon(icon, 'file-text')} size="sm" />
          {label}
        </span>,
      );
    at = match.index + whole.length;
  }
  if (at < text.length) out.push(text.slice(at));
  return out;
};

/** The same text for a place that takes words only: the marks go, their labels stay. */
export const plain = (text: string): string => text.replace(MARK, (_, bold, label) => bold ?? label ?? '');
