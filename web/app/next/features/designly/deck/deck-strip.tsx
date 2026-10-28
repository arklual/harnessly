// The slides down the left of the editor (the board's .pr-strip): a numbered picture of each board, the one on screen
// ringed, and «＋ слайд», which asks the agent for a slide after the current one. In the chat's sidebar the same strip
// runs along the bottom without numbers (deck.css).
import { useEffect, useRef } from 'react';
import { t } from '@/next/i18n';
import type { IDeckBoard } from '@/next/features/designly/deck/deck-view';

export interface IDeckStripProps {
  boards: readonly IDeckBoard[];
  index: number;
  onGo: (index: number) => void;
  /** The picture of a board; null while it cannot be had (the deck not read yet, nothing drawn on it). */
  thumb: (path: string) => string | null;
  /** The boards the agent is drawing now: a shimmer in place of their picture. */
  making: (path: string) => boolean;
  /** Placeholders while the agent makes the first slides (a deck with none yet); 0 for none. */
  pending: number;
  /** Absent where the agent cannot be asked. */
  onAdd?: () => void;
}

/** How many placeholders stand for the first slides being made. */
export const DECK_PENDING_THUMBS = 3;

const DeckStrip = ({ boards, index, onGo, thumb, making, pending, onAdd }: IDeckStripProps) => {
  const ref = useRef<HTMLElement>(null);
  // The slide on screen stays in view as the arrows turn the slides.
  useEffect(() => {
    ref.current
      ?.querySelector<HTMLElement>('[data-on="1"]')
      ?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [index]);
  return (
    <nav ref={ref} className="hx-deck__strip" aria-label={t('next.deck.strip.label')}>
      {boards.map((board, i) => {
        const src = thumb(board.path);
        const busy = making(board.path);
        return (
          <button
            key={board.path}
            type="button"
            className="hx-deck-th"
            data-on={i === index ? '1' : '0'}
            aria-current={i === index ? 'true' : undefined}
            aria-label={t('next.deck.strip.slide', { n: i + 1 })}
            title={board.title}
            onClick={() => onGo(i)}
          >
            <span>{i + 1}</span>
            <div className="hx-deck-mini" data-making={busy ? '1' : undefined}>
              {busy && <span className="hx-sk hx-deck-mini__sk" aria-hidden="true" />}
              {!busy && src && (
                <img
                  src={src}
                  alt=""
                  loading="lazy"
                  draggable={false}
                  // A picture that could not be drawn leaves the slide's own colour, not a broken image.
                  onError={(event) => event.currentTarget.setAttribute('data-failed', '1')}
                  onLoad={(event) => event.currentTarget.removeAttribute('data-failed')}
                />
              )}
            </div>
          </button>
        );
      })}
      {Array.from({ length: pending }, (_, i) => (
        <div key={`pending-${i}`} className="hx-deck-th" data-pending="1" aria-hidden="true">
          <span>{boards.length + i + 1}</span>
          <div className="hx-deck-mini" data-making="1">
            <span className="hx-sk hx-deck-mini__sk" />
          </div>
        </div>
      ))}
      {onAdd && (
        <button type="button" className="hx-deck-th__add" onClick={onAdd}>
          {t('next.deck.strip.add')}
        </button>
      )}
    </nav>
  );
};

export default DeckStrip;
