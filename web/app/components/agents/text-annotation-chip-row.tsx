import { TextQuote, X } from 'lucide-react';
import { isTextAnnotation, removeAnnotation, useAnnotations } from '@/lib/hl-annotations-store';
import { INTENT_CARD_LABELS, textAnnotationLabel } from '@/lib/hl-text-annotation';
import { t } from '@/lib/i18n';

/** Fragments selected in the timeline, above the editor: a card each, the editor's text is their comment, × drops one. */
const TextAnnotationChipRow = ({ tabId }: { tabId: string }) => {
  const quotes = useAnnotations(tabId).filter(isTextAnnotation);
  if (!quotes.length) return null;
  return (
    <div className="tw-quote-cards" role="list" aria-label={t('agents.textAnnotationChipRow.listLabel')}>
      {quotes.map((annotation) => {
        const label = textAnnotationLabel(annotation);
        return (
          <div key={annotation.id} className="tw-quote-card" role="listitem" title={annotation.quote.quote}>
            <p className="tw-quote-card-text">{annotation.quote.quote}</p>
            <div className="tw-quote-card-foot">
              <TextQuote aria-hidden="true" />
              <span>{INTENT_CARD_LABELS[annotation.intent]}</span>
            </div>
            <button
              type="button"
              className="tw-quote-card-remove"
              onClick={() => removeAnnotation(tabId, annotation.id)}
              title={t('agents.textAnnotationChipRow.remove')}
              aria-label={t('agents.textAnnotationChipRow.removeLabel', { label })}
            >
              <X aria-hidden="true" />
            </button>
          </div>
        );
      })}
    </div>
  );
};

export default TextAnnotationChipRow;
