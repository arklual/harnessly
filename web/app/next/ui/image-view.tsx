// A picture opened from its square: full size over a scrim, it grows in and shrinks out; Esc, the scrim and the cross close it.
// With its neighbours (the other pictures of the same message) ← and →, the side buttons and a swipe sideways step
// through them (a swipe is a touch or a pen moving mostly sideways by at least SWIPE_PX, and only while the picture
// is fitted). The picture zooms: Ctrl/⌘ + wheel or a trackpad pinch, two fingers, a double click or tap, the «− NN% +»
// buttons and the keys + − 0; a drag (or two fingers on a trackpad) pans it (ui/use-image-zoom.ts). In the editor the
// stage zooms whole, the picture with its marks, and a drawing tool keeps one finger for drawing.
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeys } from '@/next/features/keys/use-keys';
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import useImageZoom, { type IZoomControls, type IZoomState } from '@/next/ui/use-image-zoom';
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

/** What the picture editor (features/image-edit) adds to the viewer: a dock under the picture and a layer over it. */
export interface IImageViewEdit {
  /** Under the picture: the tools and the field. Beside it on a phone held sideways. */
  dock: ReactNode;
  /** The versions as thumbnails: a column on the left, a strip above the dock on a phone; absent for one version. */
  versions?: ReactNode;
  /** Over the picture, the size of the stage: the drawing layer. */
  layer?: ReactNode;
  /** The shape the stage takes (width / height); absent until the picture is known. */
  ratio?: number;
  /** The picture has real transparency: shown on a checkerboard. */
  clear?: boolean;
  /** A change is running: the picture is blurred and shimmers. */
  busy?: boolean;
  /** A new version has just arrived: it comes in from blur. */
  fresh?: boolean;
  /** A tool that draws is on: one finger draws, two zoom; swiping to the next picture is off. */
  locked?: boolean;
  /** The arrows and «2 / 3» stay while a tool is on (the marks keep as a draft). */
  steps?: boolean;
  download?: { href: string; name: string; label: string };
  /** Esc: true when it was taken (a tool was left), false lets the viewer close. */
  onBack?: () => boolean;
  /** The picture's own size, once it has loaded. */
  onLoad?: (size: { w: number; h: number }) => void;
}

interface IImageViewProps {
  item: IImageViewItem | null;
  onClose: () => void;
  /** «2 / 3» and the arrows; absent for a picture alone. */
  position?: { at: number; of: number };
  onStep?: (delta: 1 | -1) => void;
  /** The editor's slots; absent: a plain viewer. */
  edit?: IImageViewEdit;
}

/** What a press on must neither close the viewer nor pan or swipe the picture: the editor's own controls. */
const KEEP = '.hx-imgview__keep';
/** Below fitted the picture may get this small (a share of fitted). */
const ZOOM_OUT_MIN = 0.5;

/** «−  NN%  +» and «Вписать» at the top: the percent is of the picture's own pixels, a press on it goes to 100% and back. */
const ZoomBar = ({ state, controls }: { state: IZoomState; controls: RefObject<IZoomControls | null> }) => (
  <div className="hx-imgview__zoom hx-imgview__keep" role="group" aria-label={t('next.imageView.zoom')}>
    <button
      type="button"
      className="hx-imgview__zb"
      aria-label={t('next.imageView.zoomOut')}
      disabled={state.atMin}
      onClick={() => controls.current?.zoomOut()}
    >
      <Icon name="minus" size="sm" />
    </button>
    <button
      type="button"
      className="hx-imgview__zb hx-imgview__zpct"
      aria-label={state.fitted ? t('next.imageView.zoomReal') : t('next.imageView.zoomFit')}
      onClick={() => controls.current?.toggleReal()}
    >
      {`${Math.round(state.real * 100)}%`}
    </button>
    <button
      type="button"
      className="hx-imgview__zb"
      aria-label={t('next.imageView.zoomIn')}
      disabled={state.atMax}
      onClick={() => controls.current?.zoomIn()}
    >
      <Icon name="plus" size="sm" />
    </button>
    <button
      type="button"
      className="hx-imgview__zb hx-imgview__zfit"
      aria-label={t('next.imageView.zoomFit')}
      title={t('next.imageView.zoomFit')}
      disabled={state.fitted}
      onClick={() => controls.current?.fit()}
    >
      <Icon name="minimize-2" size="sm" />
    </button>
  </div>
);

const ImageView = ({ item, onClose, position, onStep, edit }: IImageViewProps) => {
  const { value, state } = usePresence(item, IMAGE_VIEW_EXIT_MS);
  const container = usePortalContainer();
  const ref = useRef<HTMLDivElement>(null);
  const imgRef = useRef<HTMLImageElement>(null);
  const open = item !== null;
  const stageRef = useRef<HTMLDivElement>(null);
  const locked = edit?.locked === true;
  const [zoomState, setZoomState] = useState<IZoomState | null>(null);
  // In the editor the stage zooms whole (the picture and its marks together), in the plain viewer the picture.
  const zoom = useImageZoom(ref, edit ? stageRef : imgRef, value !== null, value?.src, {
    drawing: locked,
    min: ZOOM_OUT_MIN,
    wheelPans: true,
    keys: true,
    // Only what the buttons show: a pinch moves many frames within one percent.
    onChange: (next) =>
      setZoomState((now) =>
        now !== null &&
        Math.round(now.real * 100) === Math.round(next.real * 100) &&
        now.fitted === next.fitted &&
        now.atMin === next.atMin &&
        now.atMax === next.atMax
          ? now
          : next,
      ),
  });
  const start = useRef<{ x: number; y: number } | null>(null);
  // The click that ends a swipe is not a tap on the scrim.
  const swiped = useRef(false);

  // The picture's keys (features/keys): Esc closes, the arrows (and ⌥← ⌥→, even from the field) step through the pictures.
  useKeys(
    KEY_SCOPE.pictures,
    {
      'pictures.close': () => {
        if (edit?.onBack?.() === true) return;
        onClose();
      },
      'pictures.next': () => onStep?.(1),
      'pictures.prev': () => onStep?.(-1),
      'global.stepOn': () => onStep?.(1),
      'global.stepBack': () => onStep?.(-1),
    },
    open,
  );
  useEffect(() => {
    if (open) ref.current?.focus();
  }, [open]);
  const many = position !== undefined && position.of > 1 && onStep !== undefined;
  const ratio = edit?.ratio;
  // The stage's shape is a CSS variable (registered, so a new shape grows over 240 ms).
  useLayoutEffect(() => {
    const stage = stageRef.current;
    if (ratio === undefined || !stage) return;
    stage.style.setProperty('--hx-r', String(ratio));
    // The first shape is taken at once; after that a new one grows.
    const frame = requestAnimationFrame(() => stage.setAttribute('data-animate', ''));
    return () => cancelAnimationFrame(frame);
  }, [ratio, value]);

  if (!value) return null;
  const onDown = (event: PointerEvent) => {
    if (event.target instanceof Element && event.target.closest(KEEP)) return;
    start.current = { x: event.clientX, y: event.clientY };
    swiped.current = false;
  };
  const onUp = (event: PointerEvent) => {
    const from = start.current;
    start.current = null;
    if (!from || !many || locked || !zoom.flags.current.swipeOk) return;
    const dx = event.clientX - from.x;
    const dy = event.clientY - from.y;
    if (Math.abs(dx) < SWIPE_PX || Math.abs(dx) < Math.abs(dy) * 1.5) return;
    swiped.current = true;
    onStep(dx < 0 ? 1 : -1);
  };
  const onTap = (event: MouseEvent) => {
    // The picture itself is for zooming (a double click is two clicks), the scrim around it closes.
    const onPicture =
      event.target instanceof Element && event.target.closest(`.hx-imgview__img, ${KEEP}`) !== null;
    if (swiped.current || zoom.flags.current.dragged || onPicture) {
      swiped.current = false;
      return;
    }
    onClose();
  };
  const picture = (
    <img
      ref={imgRef}
      key={value.src}
      draggable={false}
      className="hx-imgview__img"
      src={value.src}
      alt={value.name}
      onLoad={
        edit?.onLoad
          ? (event) =>
              edit.onLoad?.({ w: event.currentTarget.naturalWidth, h: event.currentTarget.naturalHeight })
          : undefined
      }
    />
  );
  const body = (
    <div
      ref={ref}
      className="hx-imgview"
      data-state={state}
      data-edit={edit ? '' : undefined}
      data-locked={locked || undefined}
      data-versions={edit?.versions ? '' : undefined}
      role="dialog"
      aria-modal="true"
      aria-label={value.name}
      tabIndex={-1}
      onClick={onTap}
      onPointerDown={onDown}
      onPointerUp={onUp}
      onPointerCancel={() => (start.current = null)}
    >
      {edit ? (
        <div className="hx-imgview__wrap">
          <div
            ref={stageRef}
            className="hx-imgview__stage"
            data-pending={ratio === undefined || undefined}
            data-clear={edit.clear || undefined}
            data-busy={edit.busy || undefined}
            data-fresh={edit.fresh || undefined}
          >
            {picture}
            {edit.layer}
          </div>
        </div>
      ) : (
        picture
      )}
      {!edit && value.caption && <span className="hx-imgview__cap">{value.caption}</span>}
      {!edit && (
        <span className="hx-imgview__name">
          {value.name}
          {many && <span className="hx-imgview__pos">{` · ${position.at} / ${position.of}`}</span>}
        </span>
      )}
      {edit?.versions}
      {edit && <div className="hx-imgview__dock hx-imgview__keep">{edit.dock}</div>}
      {many && (!locked || edit?.steps === true) && (
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
      {zoomState && <ZoomBar state={zoomState} controls={zoom.controls} />}
      {edit?.download && (
        <a
          className="hx-imgview__x hx-imgview__dl"
          href={edit.download.href}
          download={edit.download.name}
          aria-label={edit.download.label}
          onClick={(event) => event.stopPropagation()}
        >
          <Icon name="download" />
        </a>
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
