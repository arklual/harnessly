import { isTextAnnotation, type TComposerAnnotation } from '@/lib/hl-annotations-store';
import { INTENT_PLACEHOLDERS, type ITextAnnotation } from '@/lib/hl-text-annotation';

const lastText = (annotations: readonly TComposerAnnotation[]): ITextAnnotation | undefined =>
  annotations.filter(isTextAnnotation).at(-1);

/** What the composer asks for while a fragment waits in it: its text is the comment of the last one. */
export const commentPlaceholder = (annotations: readonly TComposerAnnotation[]): string | null => {
  const last = lastText(annotations);
  return last ? INTENT_PLACEHOLDERS[last.intent] : null;
};

/** The composer text of a message with fragments goes into the last fragment as its comment, the message itself stays empty. */
export const composeAnnotationComment = (
  text: string,
  annotations: readonly TComposerAnnotation[],
): { text: string; annotations: readonly TComposerAnnotation[] } => {
  const target = lastText(annotations);
  const typed = text.trim();
  if (!target || !typed) return { text: target ? '' : text, annotations };
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
