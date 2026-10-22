import { useEffect, useRef } from 'react';
import { TextQuote, X } from 'lucide-react';
import {
  isTextAnnotation,
  removeAnnotation,
  updateAnnotationComment,
  useAnnotations,
} from '@/lib/hl-annotations-store';
import { INTENT_CARD_LABELS, INTENT_PLACEHOLDERS, textAnnotationLabel } from '@/lib/hl-text-annotation';
import { t } from '@/lib/i18n';

const FOCUS_EVENT = 'hl:focus-annotation-comment';

/** Puts the caret into a fragment's comment field once its card is in the composer. */
export const focusAnnotationComment = (tabId: string, id: string): void => {
  window.dispatchEvent(new CustomEvent(FOCUS_EVENT, { detail: { tabId, id } }));
};

/**
 * Fragments selected in the timeline, above the editor: a card each with its own comment, so with several of them every
 * comment stays next to the text it is about; × drops one. Enter goes back to the editor, from where the message is
 * sent.
 */
const TextAnnotationChipRow = ({ tabId }: { tabId: string }) => {
  const quotes = useAnnotations(tabId).filter(isTextAnnotation);
  const rowRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onFocus = (event: Event) => {
      const { detail } = event as CustomEvent<{ tabId: string; id: string }>;
      if (detail.tabId !== tabId) return;
      // The card mounts on the next frame after the store changes.
      requestAnimationFrame(() =>
        rowRef.current
          ?.querySelector<HTMLTextAreaElement>(`[data-annotation-comment="${CSS.escape(detail.id)}"]`)
          ?.focus(),
      );
    };
    window.addEventListener(FOCUS_EVENT, onFocus);
    return () => window.removeEventListener(FOCUS_EVENT, onFocus);
  }, [tabId]);

  if (!quotes.length) return null;
  return (
    <div
      ref={rowRef}
      className="tw-quote-cards"
      role="list"
      aria-label={t('agents.textAnnotationChipRow.listLabel')}
    >
      {quotes.map((annotation) => {
        const label = textAnnotationLabel(annotation);
        return (
          <div key={annotation.id} className="tw-quote-card" role="listitem">
            <p className="tw-quote-card-text" title={annotation.quote.quote}>
              {annotation.quote.quote}
            </p>
            <textarea
              className="tw-quote-card-comment"
              data-annotation-comment={annotation.id}
              rows={2}
              value={annotation.comment}
              placeholder={INTENT_PLACEHOLDERS[annotation.intent]}
              aria-label={t('agents.textAnnotationChipRow.commentLabel', { label })}
              onChange={(event) => updateAnnotationComment(tabId, annotation.id, event.target.value)}
              onKeyDown={(event) => {
                if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
                event.preventDefault();
                // The editor is the composer's contenteditable textbox, a sibling of this row.
                event.currentTarget
                  .closest('.tw-quote-cards')
                  ?.parentElement?.querySelector<HTMLElement>('[role="textbox"][contenteditable]')
                  ?.focus();
              }}
            />
            <div className="tw-quote-card-foot">
              <TextQuote aria-hidden="true" />
              <span>{`#${annotation.n} ${INTENT_CARD_LABELS[annotation.intent]}`}</span>
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
