// The layer over the picture where the person marks it themselves (markup.ts): a pen line, a frame, an arrow, a crop
// by dragging; the place of words by a tap. A gesture is kept here and given away when the finger lifts.
import { useEffect, useRef, type PointerEvent } from 'react';
import { clamp01, type IPoint } from '@/next/features/image-edit/marks';
import {
  MARKUP_LOOK,
  MARKUP_TOOL,
  addItem,
  cropOfDrag,
  itemOfDrag,
  paintMarkup,
  type IMarkup,
  type IPendingText,
  type TMarkupTool,
} from '@/next/features/image-edit/markup';

interface IMarkupLayerProps {
  /** Null: the marks are shown, a press goes to the viewer. */
  tool: TMarkupTool | null;
  markup: IMarkup;
  color: string;
  pending: IPendingText | null;
  onChange: (markup: IMarkup) => void;
  /** A tap with the words tool: where the words will stand. */
  onPlace: (at: IPoint) => void;
  label: string;
}

const MarkupLayer = ({ tool, markup, color, pending, onChange, onPlace, label }: IMarkupLayerProps) => {
  const canvas = useRef<HTMLCanvasElement>(null);
  // The marks while a finger is down; the given ones are the prop.
  const draft = useRef<IMarkup | null>(null);
  const start = useRef<IPoint | null>(null);
  const latest = useRef({ markup, pending });
  useEffect(() => {
    latest.current = { markup, pending };
  });

  const paint = () => {
    const el = canvas.current;
    const ctx = el?.getContext('2d');
    if (!el || !ctx) return;
    paintMarkup(
      ctx,
      el.width,
      el.height,
      draft.current ?? latest.current.markup,
      MARKUP_LOOK.show,
      latest.current.pending,
    );
  };

  useEffect(() => {
    const el = canvas.current;
    if (!el) return;
    const fit = () => {
      const ratio = window.devicePixelRatio || 1;
      const w = Math.max(1, Math.round(el.clientWidth * ratio));
      const h = Math.max(1, Math.round(el.clientHeight * ratio));
      if (el.width !== w || el.height !== h) {
        el.width = w;
        el.height = h;
      }
      paint();
    };
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  useEffect(paint);

  const at = (event: PointerEvent): IPoint => {
    const rect = event.currentTarget.getBoundingClientRect();
    return {
      x: clamp01((event.clientX - rect.left) / rect.width),
      y: clamp01((event.clientY - rect.top) / rect.height),
    };
  };
  /** What the drag from `start` to `p` leaves, drawn while the finger moves. */
  const dragged = (from: IPoint, p: IPoint): IMarkup => {
    const base = latest.current.markup;
    if (tool === null) return base;
    if (tool === MARKUP_TOOL.crop) {
      const crop = cropOfDrag(from, p);
      return crop === null ? base : { ...base, crop };
    }
    const item = itemOfDrag(tool, color, from, p);
    return item === null ? base : addItem(base, item);
  };
  const down = (event: PointerEvent<HTMLCanvasElement>) => {
    if (event.button !== 0 || tool === null) return;
    const p = at(event);
    if (tool === MARKUP_TOOL.text) {
      onPlace(p);
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    start.current = p;
    draft.current =
      tool === MARKUP_TOOL.pen ? addItem(latest.current.markup, { kind: 'pen', color, points: [p] }) : null;
    paint();
  };
  const move = (event: PointerEvent<HTMLCanvasElement>) => {
    const from = start.current;
    if (from === null) return;
    const p = at(event);
    if (tool === MARKUP_TOOL.pen && draft.current !== null) {
      const items = draft.current.items.slice();
      const last = items[items.length - 1];
      if (last.kind === 'pen') items[items.length - 1] = { ...last, points: [...last.points, p] };
      draft.current = { ...draft.current, items };
    } else {
      draft.current = dragged(from, p);
    }
    paint();
  };
  const up = (event: PointerEvent<HTMLCanvasElement>) => {
    const done = draft.current;
    draft.current = null;
    start.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
    if (done !== null && done !== latest.current.markup) onChange(done);
    else paint();
  };
  const cancel = () => {
    draft.current = null;
    start.current = null;
    paint();
  };

  return (
    <canvas
      ref={canvas}
      className="hx-ie__canvas hx-imgview__keep"
      data-tool={tool ?? undefined}
      role="img"
      aria-label={label}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={cancel}
    />
  );
};

export default MarkupLayer;
