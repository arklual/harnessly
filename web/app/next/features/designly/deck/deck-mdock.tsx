// The phone's dock under the slide (the board's .pr-mdock): the chat folded into one line, «Попросить агента…» with a
// mic glyph, which opens the chat sheet with its field in focus, and the comments' round button with the count of the
// open ones. Shown only on a phone in the editor while no panel or sheet covers the slide (deck.css, deck-view.tsx).
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

export interface IDeckMdockProps {
  onAsk: () => void;
  onComments: () => void;
  /** Open comments of the deck. */
  count: number;
}

const DeckMdock = ({ onAsk, onComments, count }: IDeckMdockProps) => (
  <div className="hx-deck-mdock">
    <button
      type="button"
      className="hx-deck-mdock__in"
      aria-label={t('next.deck.mdock.askLabel')}
      onClick={onAsk}
    >
      <span>{t('next.deck.mdock.ask')}</span>
      <span className="hx-deck-mdock__mic" aria-hidden="true">
        <Icon name="mic" size="sm" />
      </span>
    </button>
    <button
      type="button"
      className="hx-deck-mdock__cm"
      aria-label={t('next.deck.mdock.comments')}
      onClick={onComments}
    >
      <Icon name="message-square" />
      {count > 0 && <span className="hx-deck-badge">{count}</span>}
    </button>
  </div>
);

export default DeckMdock;
