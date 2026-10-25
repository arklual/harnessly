// A picture opened from its square: full size over a scrim, it grows in and shrinks out; Esc, the scrim and the cross close it.
// With its neighbours (the other pictures of the same message) ← and →, the side buttons and a swipe sideways step
// through them (a swipe is a touch or a pen moving mostly sideways by at least SWIPE_PX, and only while the picture
// is fitted). The picture zooms: wheel, pinch, double click or tap, and a drag pans it (ui/use-image-zoom.ts).
import { useEffect, useRef, type MouseEvent, type PointerEvent } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import useImageZoom from '@/next/ui/use-image-zoom';
import { createPortal } from 'react-dom';

export const IMAGE_VIEW_EXIT_MS = 200;

/** How far a finger has to travel sideways to step to the next picture. */
export const SWIPE_PX = 48;

export interface IImageViewItem {
  src: string;
  name: string;
  /** A line under the name: where the picture comes from, when. */
  caption?: string;
}

interface IImageViewProps {
  item: IImageViewItem | null;
  onClose: () => void;
  /** «2 / 3» and the arrows; absent for a picture alone. */
  position?: { at: number; of: number };
  onStep?: (delta: 1 | -1) => void;
}

const ImageView = ({ item, onClose, position, onStep }: IImageViewProps) => {
  const { value, state } = usePresence(item, IMAGE_VIEW_EXIT_MS);
  const container = usePortalContainer();
  const ref = useRef<HTMLDivElement>(null);
  const imgRef = useRef<HTMLImageElement>(null);
  const open = item !== null;
  const zoom = useImageZoom(ref, imgRef, value !== null, value?.src);
  const start = useRef<{ x: number; y: number } | null>(null);
  // The click that ends a swipe is not a tap on the scrim.
  const swiped = useRef(false);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
      if (event.key === 'ArrowRight') onStep?.(1);
      if (event.key === 'ArrowLeft') onStep?.(-1);
    };
    document.addEventListener('keydown', onKey);
    ref.current?.focus();
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose, onStep]);
  const many = position !== undefined && position.of > 1 && onStep !== undefined;

  if (!value) return null;
  const onDown = (event: PointerEvent) => {
    start.current = { x: event.clientX, y: event.clientY };
    swiped.current = false;
  };
  const onUp = (event: PointerEvent) => {
    const from = start.current;
    start.current = null;
    if (!from || !many || !zoom.current.swipeOk) return;
    const dx = event.clientX - from.x;
    const dy = event.clientY - from.y;
    if (Math.abs(dx) < SWIPE_PX || Math.abs(dx) < Math.abs(dy) * 1.5) return;
    swiped.current = true;
    onStep(dx < 0 ? 1 : -1);
  };
  const onTap = (event: MouseEvent) => {
    // The picture itself is for zooming (a double click is two clicks), the scrim around it closes.
    const onPicture = event.target instanceof Element && event.target.closest('.hx-imgview__img') !== null;
    if (swiped.current || zoom.current.dragged || onPicture) {
      swiped.current = false;
      return;
    }
    onClose();
  };
  const body = (
    <div
      ref={ref}
      className="hx-imgview"
      data-state={state}
      role="dialog"
      aria-modal="true"
      aria-label={value.name}
      tabIndex={-1}
      onClick={onTap}
      onPointerDown={onDown}
      onPointerUp={onUp}
      onPointerCancel={() => (start.current = null)}
    >
      <img
        ref={imgRef}
        key={value.src}
        draggable={false}
        className="hx-imgview__img"
        src={value.src}
        alt={value.name}
      />
      {value.caption && <span className="hx-imgview__cap">{value.caption}</span>}
      <span className="hx-imgview__name">
        {value.name}
        {many && <span className="hx-imgview__pos">{` · ${position.at} / ${position.of}`}</span>}
      </span>
      {many && (
        <>
          <button
            type="button"
            className="hx-imgview__step"
            data-dir="prev"
            aria-label={t('next.imageView.prev')}
            onClick={(event) => {
              event.stopPropagation();
              onStep(-1);
            }}
          >
            <Icon name="chevron-left" />
          </button>
          <button
            type="button"
            className="hx-imgview__step"
            data-dir="next"
            aria-label={t('next.imageView.next')}
            onClick={(event) => {
              event.stopPropagation();
              onStep(1);
            }}
          >
            <Icon name="chevron-right" />
          </button>
        </>
      )}
      <button
        type="button"
        className="hx-imgview__x"
        aria-label={t('next.imageView.close')}
        onClick={onClose}
      >
        <Icon name="x" />
      </button>
    </div>
  );
  return container?.current ? createPortal(body, container.current) : body;
};

export default ImageView;
