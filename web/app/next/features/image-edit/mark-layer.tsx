// The drawing layer over the picture: a canvas the size of the stage that shows the marks the person draws (a pen, a
// brush, a frame). The marks are data (marks.ts); a gesture is kept here and committed when the finger lifts.
import { useEffect, useRef, type PointerEvent } from 'react';
import {
  BRUSH_WIDTH,
  LOOK,
  MARK_KIND,
  PEN_WIDTH,
  clamp01,
  paintMarks,
  type IMarks,
  type IPoint,
  type TBrushId,
  type TMarkKind,
} from '@/next/features/image-edit/marks';

interface IMarkLayerProps {
  kind: TMarkKind;
  marks: IMarks;
  color: string;
  brush: TBrushId;
  onChange: (marks: IMarks) => void;
  /** A frame was dropped: the field is next. */
  onFramed?: () => void;
  label: string;
}

const MarkLayer = ({ kind, marks, color, brush, onChange, onFramed, label }: IMarkLayerProps) => {
  const canvas = useRef<HTMLCanvasElement>(null);
  // The marks while a finger is down; the committed ones are the prop.
  const draft = useRef<IMarks | null>(null);
  const latest = useRef(marks);
  useEffect(() => {
    latest.current = marks;
  });

  const paint = () => {
    const el = canvas.current;
    const ctx = el?.getContext('2d');
    if (!el || !ctx) return;
    paintMarks(ctx, el.width, el.height, kind, draft.current ?? latest.current, LOOK.show);
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
    // paint reads refs and `kind`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind]);
  useEffect(paint);

  const at = (event: PointerEvent): IPoint => {
    const rect = event.currentTarget.getBoundingClientRect();
    return {
      x: clamp01((event.clientX - rect.left) / rect.width),
      y: clamp01((event.clientY - rect.top) / rect.height),
    };
  };
  const down = (event: PointerEvent<HTMLCanvasElement>) => {
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    const p = at(event);
    if (kind === MARK_KIND.note) {
      draft.current = { strokes: [], box: { x0: p.x, y0: p.y, x1: p.x, y1: p.y } };
    } else {
      const width = kind === MARK_KIND.mark ? PEN_WIDTH : BRUSH_WIDTH[brush];
      draft.current = {
        ...latest.current,
        strokes: [...latest.current.strokes, { color, width, points: [p] }],
      };
    }
    paint();
  };
  const move = (event: PointerEvent<HTMLCanvasElement>) => {
    const current = draft.current;
    if (!current) return;
    const p = at(event);
    if (current.box) {
      draft.current = { ...current, box: { ...current.box, x1: p.x, y1: p.y } };
    } else {
      const strokes = current.strokes.slice();
      const last = strokes[strokes.length - 1];
      strokes[strokes.length - 1] = { ...last, points: [...last.points, p] };
      draft.current = { ...current, strokes };
    }
    paint();
  };
  const up = (event: PointerEvent<HTMLCanvasElement>) => {
    const done = draft.current;
    draft.current = null;
    if (!done) return;
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
    onChange(done);
    if (done.box && onFramed) onFramed();
  };
  const cancel = () => {
    draft.current = null;
    paint();
  };

  return (
    <canvas
      ref={canvas}
      className="hx-ie__canvas hx-imgview__keep"
      data-kind={kind}
      role="img"
      aria-label={label}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={cancel}
    />
  );
};

export default MarkLayer;
