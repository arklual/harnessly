// Text picked in the agent's answers gets one button over it, «Annotate» (ui/annotate): a press opens a card for the
// remark (optional), and the words with the remark go to the field as a chip. A finger has no mouseup, so on touch the
// pill waits until the handles stop moving. A scroll of the feed carries the pill along; Esc or a press elsewhere folds it.
import { useCallback, useEffect, useState } from 'react';
import Annotate, { type IAnnotateTarget } from '@/next/ui/annotate/annotate';

const TOUCH_SETTLE_MS = 380;
const HIGHLIGHT = 'hx-annotate';

interface IPicked extends IAnnotateTarget {
  /** The picked words in the page: the card marks them while the field has the selection. */
  range: Range;
}

interface ISelBarProps {
  /** A quote, with the person's remark when they wrote one. */
  onAdd: (text: string, note: string | null) => void;
}

const coarse = () => window.matchMedia('(pointer: coarse)').matches;

const spotOf = (range: Range) => {
  const r = range.getBoundingClientRect();
  return { x: Math.round(r.left + r.width / 2), top: Math.round(r.top), bottom: Math.round(r.bottom) };
};

const pickedNow = (): Pick<IPicked, 'quote' | 'x' | 'top' | 'bottom' | 'range'> | null => {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const text = sel.toString().replace(/\s+/g, ' ').trim();
  const range = sel.getRangeAt(0).cloneRange();
  const node = range.commonAncestorContainer;
  const el = node.nodeType === 1 ? (node as Element) : node.parentElement;
  if (text.length < 2 || !el?.closest('.hx-thread') || el.closest('.hx-msg-user, textarea, input, .hx-annot'))
    return null;
  return { quote: text, range, ...spotOf(range) };
};

/** Keeps the picked words marked in the page while the selection itself has moved into the field. */
const mark = (range: Range | null) => {
  if (typeof CSS === 'undefined' || !('highlights' in CSS)) return;
  if (range) CSS.highlights.set(HIGHLIGHT, new Highlight(range));
  else CSS.highlights.delete(HIGHLIGHT);
};

const SelBar = ({ onAdd }: ISelBarProps) => {
  const [picked, setPicked] = useState<IPicked | null>(null);

  useEffect(() => {
    let settle = 0;
    const look = () => {
      const now = pickedNow();
      setPicked((have) => {
        // A remark being written keeps its card; it follows its words when the feed scrolls.
        if (have?.expanded) return { ...have, ...spotOf(have.range) };
        return now ? { ...now, expanded: false } : null;
      });
    };
    const up = (event: Event) => {
      if (event instanceof globalThis.KeyboardEvent && !event.shiftKey) return;
      if ((event.target as Element | null)?.closest?.('.hx-annot')) return;
      window.setTimeout(look, 0);
    };
    const changed = () => {
      if (!coarse()) return;
      window.clearTimeout(settle);
      settle = window.setTimeout(look, TOUCH_SETTLE_MS);
    };
    // The feed scrolls under the pill: it follows the picked words (a scroll the page makes itself must not close it).
    const scrolled = (event: Event) => {
      if ((event.target as Element).closest?.('.hx-feed')) look();
    };
    document.addEventListener('mouseup', up);
    document.addEventListener('keyup', up);
    document.addEventListener('selectionchange', changed);
    document.addEventListener('scroll', scrolled, true);
    return () => {
      window.clearTimeout(settle);
      document.removeEventListener('mouseup', up);
      document.removeEventListener('keyup', up);
      document.removeEventListener('selectionchange', changed);
      document.removeEventListener('scroll', scrolled, true);
    };
  }, []);

  const open = picked?.expanded === true ? picked.range : null;
  useEffect(() => {
    mark(open);
    return () => mark(null);
  }, [open]);

  const expand = useCallback(() => setPicked((have) => have && { ...have, expanded: true }), []);
  const dismiss = useCallback(() => setPicked(null), []);
  const submit = useCallback(
    (note: string | null) => {
      if (!picked) return;
      onAdd(picked.quote, note);
      window.getSelection()?.removeAllRanges();
      setPicked(null);
    },
    [picked, onAdd],
  );

  return (
    <Annotate
      target={picked}
      preferBelow={coarse()}
      onExpand={expand}
      onSubmit={submit}
      onDismiss={dismiss}
    />
  );
};

export default SelBar;
