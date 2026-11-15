// The layer over the picture where the person marks it themselves (markup.ts): a pen line, a frame, an arrow, a crop
// by dragging; the place of words by a tap. A gesture is kept here and given away when the finger lifts. The layer zooms
// with the picture (ui/use-image-zoom.ts): a second finger is a pinch and drops the stroke, a mark made zoomed in keeps
// the width it has on the screen (markScale).
import { useEffect, useRef, type PointerEvent } from 'react';
import { useZoomCanvas, zoomOf } from '@/next/features/image-edit/use-zoom-canvas';
import { clamp01, type IPoint } from '@/next/features/image-edit/marks';
import {
  MARKUP_LOOK,
  MARKUP_TOOL,
  addItem,
  cropOfDrag,
  itemOfDrag,
  markScale,
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
  /** A tap with the words tool: where the words will stand, and their size (markScale at the zoom of the tap). */
  onPlace: (at: IPoint, scale: number | undefined) => void;
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

  const painter = useRef(paint);
  useEffect(() => {
    painter.current = paint;
  });
  const pointers = useZoomCanvas(canvas, painter);
  // The share of the usual width for the gesture under way, taken when it starts.
  const share = useRef<number | undefined>(undefined);
  // A second finger came: the gesture is a pinch until every finger is up.
  const pinched = useRef(false);
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
      const crop = cropOfDrag(from, p, share.current);
      return crop === null ? base : { ...base, crop };
    }
    const item = itemOfDrag(tool, color, from, p, share.current);
    return item === null ? base : addItem(base, item);
  };
  const drop = () => {
    draft.current = null;
    start.current = null;
    paint();
  };
  const down = (event: PointerEvent<HTMLCanvasElement>) => {
    if (event.button !== 0 || tool === null) return;
    pointers.current.add(event.pointerId);
    if (pointers.current.size > 1) {
      pinched.current = true;
      drop();
      return;
    }
    pinched.current = false;
    const p = at(event);
    share.current = markScale(zoomOf(event.currentTarget));
    event.currentTarget.setPointerCapture(event.pointerId);
    start.current = p;
    // The words stand where the finger lifts (a pinch that starts there does not place them).
    if (tool === MARKUP_TOOL.text) return;
    const pen = { kind: 'pen' as const, color, points: [p] };
    draft.current =
      tool === MARKUP_TOOL.pen
        ? addItem(latest.current.markup, share.current === undefined ? pen : { ...pen, scale: share.current })
        : null;
    paint();
  };
  const move = (event: PointerEvent<HTMLCanvasElement>) => {
    const from = start.current;
    if (from === null || pinched.current || tool === MARKUP_TOOL.text) return;
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
    pointers.current.delete(event.pointerId);
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
    if (pinched.current) {
      if (pointers.current.size === 0) pinched.current = false;
      return;
    }
    const from = start.current;
    const done = draft.current;
    draft.current = null;
    start.current = null;
    if (tool === MARKUP_TOOL.text) {
      if (from !== null) onPlace(at(event), share.current);
      return;
    }
    if (done !== null && done !== latest.current.markup) onChange(done);
    else paint();
  };
  const cancel = (event: PointerEvent<HTMLCanvasElement>) => {
    pointers.current.delete(event.pointerId);
    if (pointers.current.size === 0) pinched.current = false;
    drop();
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
