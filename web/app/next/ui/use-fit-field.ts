// A text field as tall as its text, the same in the chat and on Designly's start: it follows its text and eases to the
// new height; CSS caps it and the text scrolls inside past that. A text that is set from outside (dictation, a message
// taken back from the queue) shows its end, so the caret stays in sight. It shrinks the same way when the text leaves.
// Measuring lays the page out, so a keystroke that only adds text measures once: the field needs a new height only when
// the text no longer fits it, and never once it has reached its cap.
import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { canAnimate, FLIGHT_EASE } from '@/next/ui/flight';

/** The field eases to its new height as the text grows or goes (sent, cleared): ease-out, this long. */
const GROW_MS = 180;

/** Whether `next` is `prev` with text put in at one place and nothing taken out: typing, a paste without a selection. */
const onlyAdded = (prev: string, next: string) => {
  if (next.length <= prev.length) return false;
  let head = 0;
  while (head < prev.length && prev.charCodeAt(head) === next.charCodeAt(head)) head++;
  let tail = 0;
  while (
    tail < prev.length - head &&
    prev.charCodeAt(prev.length - 1 - tail) === next.charCodeAt(next.length - 1 - tail)
  )
    tail++;
  return head + tail === prev.length;
};

/** The field's height after a fit, and whether CSS capped it below its text (it then scrolls). */
interface IFit {
  grow: Animation | null;
  capped: boolean;
}

/**
 * Sets the field's height from its text and eases it there from the height it shows now. A height animation still
 * running (the text grew a moment ago: dictation that ends with «send», a paste sent at once) is cancelled before the
 * field is measured: while it runs it holds the field at its in-between height, scrollHeight reads that height and an
 * empty field would keep it. Returns the new animation, or null where the field jumps (no change, no motion, `animate`).
 */
const fitHeight = (field: HTMLTextAreaElement, running: Animation | null, animate: boolean): IFit => {
  const was = field.offsetHeight;
  running?.cancel();
  field.style.height = 'auto';
  const content = field.scrollHeight;
  field.style.height = `${content}px`;
  const target = field.offsetHeight;
  // Held lower than its text (the max height): more text scrolls inside and the height stays.
  const capped = target + 1 < content;
  if (!animate || was <= 0 || Math.abs(target - was) <= 1 || !canAnimate(field))
    return { grow: null, capped };
  const grow = field.animate([{ height: `${was}px` }, { height: `${target}px` }], {
    duration: GROW_MS,
    easing: FLIGHT_EASE,
  });
  return { grow, capped };
};

export const useFitField = (fieldRef: RefObject<HTMLTextAreaElement | null>, value: string) => {
  const growRef = useRef<Animation | null>(null);
  const shownRef = useRef({ value, capped: false });
  useLayoutEffect(() => {
    const field = fieldRef.current;
    if (!field) return;
    const shown = shownRef.current;
    const added = onlyAdded(shown.value, value) && growRef.current?.playState !== 'running';
    shown.value = value;
    if (!added || (!shown.capped && field.scrollHeight > field.clientHeight)) {
      const fit = fitHeight(field, growRef.current, true);
      growRef.current = fit.grow;
      shown.capped = fit.capped;
    }
    if (field.selectionStart === field.value.length) field.scrollTop = field.scrollHeight;
  }, [fieldRef, value]);

  // The field can be laid out narrow first (a column that is still opening) and wide later: the text then wraps
  // otherwise, and the height computed for the old width is wrong. Recalculate on a width change too, once per frame.
  useEffect(() => {
    const field = fieldRef.current;
    if (!field || typeof ResizeObserver === 'undefined') return;
    let width = field.clientWidth;
    let frame = 0;
    // A width change mid-ease goes on easing to the height of the new width; otherwise the field just takes it.
    const fit = () => {
      frame = 0;
      const easing = growRef.current?.playState === 'running';
      const next = fitHeight(field, growRef.current, easing);
      growRef.current = next.grow;
      shownRef.current.capped = next.capped;
    };
    const watcher = new ResizeObserver(() => {
      if (field.clientWidth === width) return;
      width = field.clientWidth;
      if (frame === 0) frame = requestAnimationFrame(fit);
    });
    watcher.observe(field);
    return () => {
      watcher.disconnect();
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, [fieldRef]);
};

/** Whether the field's capped text goes on past its top and its bottom edge: the stylesheet fades those edges. */
const markEdges = (field: HTMLTextAreaElement) => {
  const cap = Number.parseFloat(getComputedStyle(field).maxHeight);
  const capped = field.scrollHeight > cap + 1;
  field.dataset.fadeTop = String(capped && field.scrollTop > 1);
  field.dataset.fadeBottom = String(capped && field.scrollTop + field.clientHeight < field.scrollHeight - 1);
};

/** Keeps the field's edge marks true as its text changes and as it scrolls. */
export const useFieldEdges = (fieldRef: RefObject<HTMLTextAreaElement | null>, value: string) => {
  useLayoutEffect(() => {
    if (fieldRef.current) markEdges(fieldRef.current);
  }, [fieldRef, value]);
  useEffect(() => {
    const field = fieldRef.current;
    if (!field) return;
    const onScroll = () => markEdges(field);
    field.addEventListener('scroll', onScroll, { passive: true });
    return () => field.removeEventListener('scroll', onScroll);
  }, [fieldRef]);
};
