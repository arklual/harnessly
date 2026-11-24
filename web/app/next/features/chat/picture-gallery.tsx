// Several pictures of a turn as a gallery card (outputs.tsx, «Скриншоты · N»), the same on a computer and on a phone:
// one picture large with the arrows on its sides and a swipe, «2 / 6» in the head, a strip of all of them under it that
// scrolls sideways, and «Все» — every picture in a grid over the screen. A press on the large one opens the viewer,
// which steps through the same pictures; the card follows it. However many pictures there are, the card keeps its height.
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { t } from '@/next/i18n';
import type { Picture } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { SWIPE_PX } from '@/next/ui/image-view';
import { usePortalContainer } from '@/next/ui/portal';
import { useHScroll } from '@/next/ui/use-h-scroll';
import '@/next/styles/picture-gallery.css';

const nameOf = (picture: Picture): string => picture.text.slice(picture.text.lastIndexOf('/') + 1);

/** The index a step lands on: round the ends, so the last one leads to the first. */
export const stepIndex = (at: number, delta: number, of: number): number =>
  of <= 0 ? 0 : (at + delta + of) % of;

/** What a gesture on the large picture was: a swipe one way, or a press (null). */
export const swipeOf = (dx: number, dy: number): 1 | -1 | null => {
  if (Math.abs(dx) < SWIPE_PX || Math.abs(dx) <= Math.abs(dy)) return null;
  return dx < 0 ? 1 : -1;
};

interface IPictureGalleryProps {
  pictures: readonly Picture[];
  srcOf: (picture: Picture) => string;
  title: string;
  /** The picture shown large. */
  at: number;
  onAt: (at: number) => void;
  /** Opens the viewer on a picture. */
  onOpen: (at: number) => void;
  /** The viewer is open over the page: the grid leaves Escape to it. */
  viewing: boolean;
}

export const PictureGallery = ({
  pictures,
  srcOf,
  title,
  at,
  onAt,
  onOpen,
  viewing,
}: IPictureGalleryProps) => {
  const [grid, setGrid] = useState(false);
  const strip = useRef<HTMLDivElement>(null);
  const gesture = useRef<{ x: number; y: number; id: number } | null>(null);
  const swiped = useRef(false);
  useHScroll(strip);
  const n = pictures.length;
  const i = Math.min(Math.max(at, 0), n - 1);
  const current = pictures[i];
  if (!current) return null;
  const step = (delta: 1 | -1) => onAt(stepIndex(i, delta, n));

  const down = (event: ReactPointerEvent) => {
    gesture.current = { x: event.clientX, y: event.clientY, id: event.pointerId };
    swiped.current = false;
  };
  const up = (event: ReactPointerEvent) => {
    const g = gesture.current;
    gesture.current = null;
    if (!g || g.id !== event.pointerId) return;
    const dir = swipeOf(event.clientX - g.x, event.clientY - g.y);
    if (dir === null) return;
    swiped.current = true;
    step(dir);
  };

  return (
    <section className="hx-made hx-made--shots hx-gal" aria-label={title}>
      <div className="hx-made__head hx-gal__head">
        <span className="hx-gal__title">{title}</span>
        <span className="hx-gal__count" aria-live="polite">
          {t('next.pictures.position', { at: i + 1, of: n })}
        </span>
        <button type="button" className="hx-gal__all" onClick={() => setGrid(true)}>
          <Icon name="layout-grid" size="sm" />
          {t('next.pictures.all')}
        </button>
      </div>
      <div
        className="hx-gal__stage"
        role="group"
        aria-roledescription={t('next.pictures.carousel')}
        tabIndex={0}
        onKeyDown={(event) => {
          if (event.key === 'ArrowLeft') step(-1);
          else if (event.key === 'ArrowRight') step(1);
          else return;
          event.preventDefault();
        }}
        onPointerDown={down}
        onPointerUp={up}
        onPointerCancel={() => (gesture.current = null)}
      >
        <button
          type="button"
          className="hx-gal__hero"
          aria-label={t('next.pictures.open', { name: nameOf(current) })}
          title={current.text}
          onClick={() => {
            // A swipe ends in a click on the picture: it stepped, it does not open.
            if (swiped.current) {
              swiped.current = false;
              return;
            }
            onOpen(i);
          }}
        >
          <img
            key={current.id}
            className="hx-gal__img"
            src={srcOf(current)}
            alt=""
            decoding="async"
            draggable={false}
          />
        </button>
        {n > 1 && (
          <>
            <button
              type="button"
              className="hx-gal__arrow"
              data-dir="prev"
              aria-label={t('next.pictures.prev')}
              onClick={() => step(-1)}
            >
              <Icon name="chevron-left" />
            </button>
            <button
              type="button"
              className="hx-gal__arrow"
              data-dir="next"
              aria-label={t('next.pictures.next')}
              onClick={() => step(1)}
            >
              <Icon name="chevron-right" />
            </button>
          </>
        )}
      </div>
      {n > 1 && (
        <div ref={strip} className="hx-gal__strip" role="listbox" aria-label={t('next.pictures.strip')}>
          {pictures.map((picture, k) => (
            <button
              key={picture.id}
              type="button"
              role="option"
              aria-selected={k === i}
              className="hx-gal__thumb"
              aria-label={t('next.pictures.open', { name: nameOf(picture) })}
              onClick={() => onAt(k)}
            >
              <img src={srcOf(picture)} alt="" loading="lazy" decoding="async" draggable={false} />
            </button>
          ))}
        </div>
      )}
      {grid && (
        <GalleryGrid
          pictures={pictures}
          srcOf={srcOf}
          title={title}
          at={i}
          viewing={viewing}
          onPick={(k) => {
            onAt(k);
            onOpen(k);
          }}
          onClose={() => setGrid(false)}
        />
      )}
    </section>
  );
};

interface IGalleryGridProps {
  pictures: readonly Picture[];
  srcOf: (picture: Picture) => string;
  title: string;
  at: number;
  viewing: boolean;
  onPick: (at: number) => void;
  onClose: () => void;
}

/** Every picture of the card in a grid over the screen; the viewer opens over it and comes back to it. */
const GalleryGrid = ({ pictures, srcOf, title, at, viewing, onPick, onClose }: IGalleryGridProps) => {
  const container = usePortalContainer();
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    close.current?.focus();
  }, []);
  useEffect(() => {
    if (viewing) return;
    const key = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      onClose();
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [viewing, onClose]);
  const body = (
    <div className="hx-galgrid" role="dialog" aria-modal="true" aria-label={title}>
      <div className="hx-galgrid__head">
        <span className="hx-galgrid__title">{title}</span>
        <button
          ref={close}
          type="button"
          className="hx-galgrid__close"
          aria-label={t('next.pictures.close')}
          onClick={onClose}
        >
          <Icon name="x" />
        </button>
      </div>
      <div className="hx-galgrid__grid">
        {pictures.map((picture, k) => (
          <button
            key={picture.id}
            type="button"
            className="hx-galgrid__cell"
            aria-current={k === at ? 'true' : undefined}
            aria-label={t('next.pictures.open', { name: nameOf(picture) })}
            onClick={() => onPick(k)}
          >
            <img src={srcOf(picture)} alt="" loading="lazy" decoding="async" draggable={false} />
            <span className="hx-galgrid__n">{k + 1}</span>
          </button>
        ))}
      </div>
    </div>
  );
  return createPortal(body, container?.current ?? document.body);
};
