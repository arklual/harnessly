import { useCallback, useEffect, useState } from 'react';
import useTextSelectionAnnotation from '@/hooks/use-text-selection-annotation';
import TextAnnotationPopover from '@/components/agents/text-annotation-popover';
import { pickedRanges } from '@/lib/hl-annotation-composer';
import { addTextAnnotation, useAnnotations } from '@/lib/hl-annotations-store';
import { OPEN_PANE_TAB_EVENT, addSideChatExcerpt } from '@/lib/hl-side-chat-store';
import type { TTextIntent } from '@/lib/hl-text-annotation';

const isCoarsePointer = (): boolean => window.matchMedia('(pointer: coarse)').matches;
const PICKED_HIGHLIGHT = 'annotation-picked';

/**
 * Mounted inside a timeline: a fragment selected in one of its messages gets a panel that puts it into the
 * tab's composer as an annotation. Works within the timeline it sits in only, a pane next to it has its own.
 */
const TextSelectionAnnotator = ({ tabId }: { tabId: string }) => {
  const [sentinel, setSentinel] = useState<HTMLSpanElement | null>(null);
  const { target, dismiss } = useTextSelectionAnnotation(sentinel?.parentElement ?? null);
  const annotations = useAnnotations(tabId);

  useEffect(() => {
    if (typeof Highlight === 'undefined' || !CSS.highlights) return;
    const ranges = pickedRanges(annotations);
    if (ranges.length) CSS.highlights.set(PICKED_HIGHLIGHT, new Highlight(...ranges));
    return () => {
      CSS.highlights.delete(PICKED_HIGHLIGHT);
    };
  }, [annotations]);

  const close = useCallback(() => {
    if (
      document.activeElement instanceof HTMLElement &&
      document.activeElement.closest('[data-annotation-popover]')
    ) {
      sentinel?.parentElement?.querySelector<HTMLElement>('[role="log"]')?.focus();
    }
    dismiss();
  }, [dismiss, sentinel]);

  const pick = (intent: TTextIntent) => {
    if (!target) return;
    addTextAnnotation(tabId, {
      kind: 'text',
      intent,
      comment: '',
      quote: target.quote,
      source: target.source,
      range: target.range.cloneRange(),
    });
    window.getSelection()?.removeAllRanges();
    close();
  };

  const toSideChat = () => {
    if (!target) return;
    addSideChatExcerpt(tabId, { quote: target.quote.quote, source: target.source });
    window.dispatchEvent(new CustomEvent(OPEN_PANE_TAB_EVENT, { detail: { kind: 'side-chat' } }));
    window.getSelection()?.removeAllRanges();
    close();
  };

  return (
    <>
      <span ref={setSentinel} hidden />
      {target && (
        <TextAnnotationPopover
          key={`${target.source.entryId}:${target.quote.quote}`}
          target={target}
          compact={isCoarsePointer()}
          onPick={pick}
          onSideChat={toSideChat}
          onClose={close}
        />
      )}
    </>
  );
};

export default TextSelectionAnnotator;
