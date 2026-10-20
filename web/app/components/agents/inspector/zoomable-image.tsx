import { t } from '@/lib/i18n';
import {
  useCallback,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type WheelEvent as ReactWheelEvent,
} from 'react';
import { Maximize, Minus, Plus } from 'lucide-react';
import { swipeStep, type TPageStep } from '@/lib/viewer-paging';

const MIN = 1;
const MAX = 12;

interface IView {
  scale: number;
  x: number;
  y: number;
}
const FIT: IView = { scale: 1, x: 0, y: 0 };

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/** A picture that zooms towards the cursor or the pinch, pans by dragging, and fits back on double click. */
const ZoomableImage = ({
  src,
  alt,
  onSwipe,
}: {
  src: string;
  alt: string;
  onSwipe?: (step: TPageStep) => void;
}) => {
  const [view, setView] = useState<IView>(FIT);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const imgRef = useRef<HTMLImageElement | null>(null);
  // Active pointers for dragging and pinching; refs because they change on every move event.
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<{ distance: number; view: IView } | null>(null);
  const swipe = useRef<{ x: number; y: number } | null>(null);

  const zoomAt = useCallback((next: number, clientX?: number, clientY?: number) => {
    setView((current) => {
      const scale = clamp(next, MIN, MAX);
      if (scale === MIN) return FIT;
      const rect = stageRef.current?.getBoundingClientRect();
      if (!rect || clientX === undefined || clientY === undefined) return { ...current, scale };
      const px = clientX - rect.left - rect.width / 2;
      const py = clientY - rect.top - rect.height / 2;
      const ratio = scale / current.scale;
      return { scale, x: px - (px - current.x) * ratio, y: py - (py - current.y) * ratio };
    });
  }, []);

  const onWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    event.preventDefault();
    zoomAt(view.scale * Math.exp(-event.deltaY * 0.0022), event.clientX, event.clientY);
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    swipe.current = pointers.current.size === 1 ? { x: event.clientX, y: event.clientY } : null;
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()];
      pinch.current = { distance: Math.hypot(a.x - b.x, a.y - b.y), view };
    }
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const previous = pointers.current.get(event.pointerId);
    if (!previous) return;
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.current.size === 2 && pinch.current) {
      const [a, b] = [...pointers.current.values()];
      const start = pinch.current;
      zoomAt(
        start.view.scale * (Math.hypot(a.x - b.x, a.y - b.y) / start.distance),
        (a.x + b.x) / 2,
        (a.y + b.y) / 2,
      );
      return;
    }
    if (view.scale > MIN)
      setView((current) => ({
        ...current,
        x: current.x + event.clientX - previous.x,
        y: current.y + event.clientY - previous.y,
      }));
  };

  const onPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    const start = swipe.current;
    swipe.current = null;
    if (start && onSwipe && event.type === 'pointerup') {
      const step = swipeStep(event.clientX - start.x, event.clientY - start.y, view.scale);
      if (step !== 0) onSwipe(step);
    }
    pointers.current.delete(event.pointerId);
    if (pointers.current.size < 2) pinch.current = null;
  };

  const naturalScale = () => {
    const img = imgRef.current;
    return img && img.clientWidth ? img.naturalWidth / img.clientWidth : 2;
  };

  return (
    <div
      ref={stageRef}
      className={view.scale > MIN ? 'tw-zoom tw-zoom-active' : 'tw-zoom'}
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onDoubleClick={(event) =>
        view.scale > MIN ? setView(FIT) : zoomAt(Math.max(2, naturalScale()), event.clientX, event.clientY)
      }
    >
      <img
        ref={imgRef}
        src={src}
        alt={alt}
        className="tw-viewer-img"
        draggable={false}
        style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
      />
      <div
        className="tw-zoom-bar"
        onPointerDown={(e) => e.stopPropagation()}
        onDoubleClick={(e) => e.stopPropagation()}
      >
        <button
          type="button"
          aria-label={t('inspector.zoomableImage.zoomOut')}
          title={t('inspector.zoomableImage.zoomOut')}
          onClick={() => zoomAt(view.scale / 1.5)}
          disabled={view.scale <= MIN}
        >
          <Minus />
        </button>
        <span>{Math.round(view.scale * 100)}%</span>
        <button
          type="button"
          aria-label={t('inspector.zoomableImage.zoomIn')}
          title={t('inspector.zoomableImage.zoomIn')}
          onClick={() => zoomAt(view.scale * 1.5)}
          disabled={view.scale >= MAX}
        >
          <Plus />
        </button>
        <button
          type="button"
          aria-label={t('inspector.zoomableImage.fit')}
          title={t('inspector.zoomableImage.fitHint')}
          onClick={() => setView(FIT)}
          disabled={view.scale <= MIN}
        >
          <Maximize />
        </button>
      </div>
    </div>
  );
};

export default ZoomableImage;
