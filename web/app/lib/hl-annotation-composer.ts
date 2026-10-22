import { isTextAnnotation, type TComposerAnnotation } from '@/lib/hl-annotations-store';
import { t } from '@/lib/i18n';
import { INTENT_PLACEHOLDERS, type ITextAnnotation } from '@/lib/hl-text-annotation';

/**
 * Each fragment has its own comment, typed in its card. The composer's text belongs to a fragment only when there is
 * exactly one and its card is empty (select, type, send); with several, or a commented one, it is the message itself
 * and goes before the fragments.
 */
const soleUncommented = (annotations: readonly TComposerAnnotation[]): ITextAnnotation | undefined => {
  const texts = annotations.filter(isTextAnnotation);
  return texts.length === 1 && !texts[0].comment.trim() ? texts[0] : undefined;
};

/** What the composer asks for while a fragment waits in it. */
export const commentPlaceholder = (annotations: readonly TComposerAnnotation[]): string | null => {
  const sole = soleUncommented(annotations);
  if (sole) return INTENT_PLACEHOLDERS[sole.intent];
  return annotations.some(isTextAnnotation) ? t('lib.hlAnnotationComposer.messagePlaceholder') : null;
};

/** The composer text of a message with one uncommented fragment becomes its comment; otherwise the text stays the message. */
export const composeAnnotationComment = (
  text: string,
  annotations: readonly TComposerAnnotation[],
): { text: string; annotations: readonly TComposerAnnotation[] } => {
  const target = soleUncommented(annotations);
  const typed = text.trim();
  if (!typed) return { text: annotations.some(isTextAnnotation) ? '' : text, annotations };
  if (!target) return { text, annotations };
  const comment = target.comment ? `${target.comment}\n${typed}` : typed;
  return {
    text: '',
    annotations: annotations.map((annotation) =>
      annotation === target ? { ...target, comment } : annotation,
    ),
  };
};

/** The fragments of the waiting annotations that are still in the page. */
export const pickedRanges = (annotations: readonly TComposerAnnotation[]): Range[] =>
  annotations
    .filter(isTextAnnotation)
    .flatMap(({ range }) => (range?.commonAncestorContainer.isConnected ? [range] : []));
