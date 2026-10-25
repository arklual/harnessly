// The right pane's width, dragged by its left edge (the design's .hx-pane__split): the stage's --pane-w follows the
// pointer once a frame and data-pdrag holds the feed still meanwhile; the width is kept per device, and the pane's owner
// may keep its own (a chat's) over it.
import {
  useEffect,
  useLayoutEffect,
  useRef,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from 'react';

const KEY = 'hx-pane-width';
const MIN_W = 340;
/** The chat keeps at least this much of the stage. */
const CHAT_MIN_W = 360;

const read = (): number | null => {
  try {
    const value = Number(localStorage.getItem(KEY));
    return value >= MIN_W ? value : null;
  } catch {
    return null;
  }
};

const write = (width: number) => {
  try {
    localStorage.setItem(KEY, String(width));
  } catch {
    // Private mode or full storage: the width holds for this page.
  }
};

/** The window's body holds the pane and the stage side by side: the width is theirs both. */
const stageOf = (pane: HTMLElement | null): HTMLElement | null =>
  pane?.closest<HTMLElement>('.hx-main__body') ?? null;

/** Answers the split's pointerdown; the stage gets the kept width when the pane mounts and when `own` changes. */
const usePaneWidth = (
  paneRef: RefObject<HTMLElement | null>,
  mounted: boolean,
  own: number | null = null,
  onWidth?: (width: number) => void,
) => {
  const frame = useRef(0);

  // Before the pane's motion measures it (use-pane-motion.ts): an opening pane slides in at its own width.
  useLayoutEffect(() => {
    const stage = stageOf(paneRef.current);
    if (!stage) return;
    const kept = own ?? read();
    if (kept) stage.style.setProperty('--pane-w', `${Math.min(kept, stage.clientWidth - CHAT_MIN_W)}px`);
    else stage.style.removeProperty('--pane-w');
  }, [paneRef, mounted, own]);

  useEffect(() => () => cancelAnimationFrame(frame.current), []);

  return (event: ReactPointerEvent<HTMLElement>) => {
    const pane = paneRef.current;
    const stage = stageOf(pane);
    if (!pane || !stage || event.button !== 0) return;
    event.preventDefault();
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const from = { x: event.clientX, w: pane.offsetWidth, max: stage.clientWidth - CHAT_MIN_W };
    let width = 0;
    stage.dataset.pdrag = 'true';
    const move = (e: PointerEvent) => {
      width = Math.round(Math.max(MIN_W, Math.min(from.max, from.w + from.x - e.clientX)));
      frame.current ||= requestAnimationFrame(() => {
        frame.current = 0;
        stage.style.setProperty('--pane-w', `${width}px`);
      });
    };
    const up = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      handle.removeEventListener('pointercancel', up);
      delete stage.dataset.pdrag;
      if (width) {
        stage.style.setProperty('--pane-w', `${width}px`);
        write(width);
        onWidth?.(width);
      }
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', up);
  };
};

export default usePaneWidth;
