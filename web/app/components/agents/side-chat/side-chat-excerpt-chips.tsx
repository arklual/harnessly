import { TextQuote, X } from 'lucide-react';
import type { ISideChatExcerptChip } from '@/lib/hl-side-chat-store';
import { t } from '@/lib/i18n';

interface ISideChatExcerptChipsProps {
  excerpts: readonly ISideChatExcerptChip[];
  onRemove?: (id: string) => void;
}

/** Fragments of the timeline that go along with the question: «Excerpt» with the quote, removable until the question is sent. */
const SideChatExcerptChips = ({ excerpts, onRemove }: ISideChatExcerptChipsProps) => {
  if (!excerpts.length) return null;
  return (
    <ul className="tw-sidechat-chips" aria-label={t('sideChat.sideChatExcerptChips.list')}>
      {excerpts.map((excerpt) => (
        <li key={excerpt.id} className="tw-sidechat-chip">
          <p className="tw-sidechat-chip-quote" title={excerpt.quote}>
            {excerpt.quote}
          </p>
          <span className="tw-sidechat-chip-label">
            <TextQuote aria-hidden="true" />
            {t('sideChat.sideChatExcerptChips.excerpt')}
          </span>
          {onRemove && (
            <button
              type="button"
              className="tw-sidechat-chip-remove"
              onClick={() => onRemove(excerpt.id)}
              aria-label={t('sideChat.sideChatExcerptChips.removeLabel')}
              title={t('sideChat.sideChatExcerptChips.remove')}
            >
              <X aria-hidden="true" />
            </button>
          )}
        </li>
      ))}
    </ul>
  );
};

export default SideChatExcerptChips;
