// One card at the bottom of an answer, as an attachment of Linear: the thing's icon, its name, a small line of what it
// is, and one quiet icon button at its end. A tap on the card opens the thing in the right pane; ⌘/Ctrl-click opens
// its address in a new tab of the browser, when it has one the browser can show. The links of a turn (outputs.tsx)
// and the files the agent handed over (delivery-card.tsx) are both this card.
import type { MouseEvent, ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { isOutsideClick } from '@/next/features/chat/ref-chip';

interface IOutputRowProps {
  index: number;
  icon: string;
  title: ReactNode;
  meta: ReactNode;
  /** The line under the name is still being read: it rests softly. */
  pending?: boolean;
  /** The thing's address: ⌘/Ctrl-click and the middle button open it in a new tab when `newTab`. */
  href?: string;
  newTab?: boolean;
  onOpen?: (() => void) | null;
  /** The one icon button at the card's end. */
  end?: ReactNode;
}

const OutputRow = ({ index, icon, title, meta, pending, href, newTab, onOpen, end }: IOutputRowProps) => {
  const body = (
    <>
      <span className="hx-file__ico" data-tone="accent">
        <Icon name={icon} />
      </span>
      <span className="hx-file__text">
        <span className="hx-file__name">{title}</span>
        <span className="hx-file__meta" data-pending={pending || undefined}>
          {meta}
        </span>
      </span>
    </>
  );
  const open = (event: MouseEvent<HTMLElement>) => {
    if (isOutsideClick(event) && newTab && href) return;
    event.preventDefault();
    onOpen?.();
  };
  return (
    <div className="hx-file hx-file--row hx-file--link" data-j={index} data-open="true">
      {href ? (
        <a className="hx-file__open" href={href} target="_blank" rel="noopener noreferrer" onClick={open}>
          {body}
        </a>
      ) : (
        <button type="button" className="hx-file__open" onClick={open}>
          {body}
        </button>
      )}
      {end}
    </div>
  );
};

export default OutputRow;
