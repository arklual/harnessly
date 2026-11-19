// The 3D scene's timeline, drawn like the video editor's: the play button and the clock, a ruler in seconds
// with the scene's markers, a lane of the cameras the timeline switches between, a lane of the pins and a lane of the
// draft frames. A press or a drag anywhere on the lanes seeks; the arrows step a frame (Shift: a second).
import { useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { clock, frameOf, markersOf, timeOf } from '@/next/features/designly/scene3d/scene-model';
import type { IScene3dFrame, IScene3dMeta, IScene3dPin } from '@/next/features/designly/scene3d/types';

export interface IScene3dTimelineProps {
  meta: IScene3dMeta;
  frame: number;
  playing: boolean;
  pins: readonly IScene3dPin[];
  activePin: string;
  frames: readonly IScene3dFrame[];
  onSeek: (frame: number) => void;
  onTogglePlay: () => void;
  onPin: (pin: IScene3dPin) => void;
  onFrame: (frame: IScene3dFrame) => void;
}

interface ISpan {
  cam: string;
  from: number;
  to: number;
}

/** The cameras of the timeline as spans: each marker that binds a camera starts one, the scene's own camera fills the start. */
export const cameraSpans = (meta: IScene3dMeta): ISpan[] => {
  const binds = markersOf(meta).filter((m) => m.cam);
  const first = meta.camera || meta.cameras[0]?.n || '';
  const out: ISpan[] = [];
  let cam = first;
  let from = meta.start;
  for (const m of binds) {
    const at = Math.min(meta.end, Math.max(meta.start, m.f));
    if (at > from) {
      if (cam) out.push({ cam, from, to: at });
      from = at;
    }
    cam = m.cam ?? cam;
  }
  if (cam && meta.end > from) out.push({ cam, from, to: meta.end });
  return out;
};

/** A ruler's label: whole seconds as m:ss, a half second with its tenth. */
const ruler = (s: number): string => {
  const m = Math.floor(s / 60);
  const rest = s - m * 60;
  const sec = Number.isInteger(rest) ? String(rest).padStart(2, '0') : rest.toFixed(1).padStart(4, '0');
  return `${m}:${sec}`;
};

const STEPS = [0.5, 1, 2, 5, 10, 15, 30, 60, 120];

const Scene3dTimeline = (props: IScene3dTimelineProps) => {
  const { meta, frame } = props;
  const span = Math.max(1, meta.end - meta.start);
  const at = (f: number) => `${((f - meta.start) / span) * 100}%`;
  const track = useRef<HTMLDivElement | null>(null);
  const [drag, setDrag] = useState(false);
  const total = timeOf(meta, meta.end);
  const spans = useMemo(() => cameraSpans(meta), [meta]);
  const markers = useMemo(() => markersOf(meta).filter((m) => !m.cam), [meta]);
  const ticks = useMemo(() => {
    const step = STEPS.find((s) => total / s <= 10) ?? 300;
    const out: { s: number; major: boolean; odd: boolean }[] = [];
    for (let s = 0; s <= total + 1e-6; s += step / 2) {
      const n = Math.round(s / step);
      out.push({ s, major: n * step === s, odd: n % 2 === 1 });
    }
    return out;
  }, [total]);

  const seekAt = (clientX: number) => {
    const el = track.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const k = Math.min(1, Math.max(0, (clientX - r.left) / Math.max(1, r.width)));
    props.onSeek(Math.round(meta.start + k * span));
  };
  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    if ((e.target as HTMLElement).closest('button')) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    setDrag(true);
    seekAt(e.clientX);
  };
  const onMove = (e: PointerEvent<HTMLDivElement>) => drag && seekAt(e.clientX);
  const onUp = () => setDrag(false);
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const by = e.shiftKey ? Math.round(meta.fps || 24) : 1;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      props.onSeek(Math.min(meta.end, Math.max(meta.start, frame + (e.key === 'ArrowLeft' ? -by : by))));
    } else if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault();
      props.onSeek(e.key === 'Home' ? meta.start : meta.end);
    } else if (e.key === ' ') {
      e.preventDefault();
      props.onTogglePlay();
    }
  };

  const onCam = spans.find((s) => frame >= s.from && frame < s.to)?.cam ?? spans.at(-1)?.cam;

  return (
    <footer className="hx-scene3d-tl" aria-label={t('next.scene3d.timeline')}>
      <div className="hx-scene3d-tl__head">
        <button
          type="button"
          className="hx-scene3d-tl__play"
          aria-label={props.playing ? t('next.scene3d.pause') : t('next.scene3d.play')}
          onClick={props.onTogglePlay}
        >
          <Icon name={props.playing ? 'pause' : 'play'} size="sm" />
        </button>
        <span className="hx-scene3d-tl__clock hx-scene3d__clock">
          <b>{clock(timeOf(meta, frame))}</b> / {clock(total)}
        </span>
        <span className="hx-scene3d-tl__frame">{t('next.scene3d.tlFrame', { n: frame })}</span>
      </div>
      <div className="hx-scene3d-tl__grid">
        <div className="hx-scene3d-tl__labels" aria-hidden="true">
          <span data-row="ruler" />
          <span data-row="cams">{t('next.scene3d.tlCameras')}</span>
          <span data-row="pins">{t('next.scene3d.tabPins')}</span>
          <span data-row="frames">{t('next.scene3d.tabFrames')}</span>
        </div>
        <div
          ref={track}
          className="hx-scene3d-tl__track"
          data-dragging={drag ? '1' : '0'}
          role="slider"
          tabIndex={0}
          aria-label={t('next.scene3d.timeline')}
          aria-valuemin={meta.start}
          aria-valuemax={meta.end}
          aria-valuenow={frame}
          aria-valuetext={clock(timeOf(meta, frame))}
          onPointerDown={onDown}
          onPointerMove={onMove}
          onPointerUp={onUp}
          onPointerCancel={onUp}
          onKeyDown={onKey}
        >
          <div className="hx-scene3d-tl__row" data-row="ruler">
            {ticks.map((x) => (
              <i
                key={x.s}
                className="hx-scene3d-tl__tick"
                data-major={x.major ? '1' : '0'}
                data-odd={x.odd ? '1' : '0'}
                style={{ left: at(frameOf(meta, x.s)) }}
              >
                {x.major && <span>{ruler(x.s)}</span>}
              </i>
            ))}
            {markers.map((m) => (
              <i
                key={`${m.f}:${m.n}`}
                className="hx-scene3d-tl__marker"
                title={m.n}
                style={{ left: at(m.f) }}
              />
            ))}
          </div>
          <div className="hx-scene3d-tl__row" data-row="cams">
            {spans.map((s) => (
              <span
                key={`${s.from}:${s.cam}`}
                className="hx-scene3d-tl__cam"
                data-on={s.cam === onCam && frame >= s.from && frame <= s.to ? '1' : '0'}
                title={`${s.cam} · ${clock(timeOf(meta, s.from))}–${clock(timeOf(meta, s.to))}`}
                style={{ left: at(s.from), width: `${((s.to - s.from) / span) * 100}%` }}
              >
                <Icon name="camera" size="sm" />
                <span>{s.cam}</span>
              </span>
            ))}
            {spans.length === 0 && (
              <span className="hx-scene3d-tl__none">{t('next.scene3d.tlNoCameras')}</span>
            )}
          </div>
          <div className="hx-scene3d-tl__row" data-row="pins">
            {props.pins.map((p, i) => (
              <button
                key={p.id}
                type="button"
                className="hx-scene3d-tl__pin"
                data-resolved={p.resolved ? '1' : '0'}
                data-active={p.id === props.activePin ? '1' : '0'}
                title={p.text}
                aria-label={`${i + 1}. ${p.text}`}
                style={{ left: at(p.scene.frame) }}
                onClick={() => props.onPin(p)}
              >
                {i + 1}
              </button>
            ))}
          </div>
          <div className="hx-scene3d-tl__row" data-row="frames">
            {props.frames.map((f) => (
              <button
                key={f.key}
                type="button"
                className="hx-scene3d-tl__shot"
                data-status={f.status}
                title={`${f.cam?.n ?? t('next.scene3d.camFree')} · ${clock(timeOf(meta, f.frame))}`}
                aria-label={`${f.cam?.n ?? t('next.scene3d.camFree')} · ${clock(timeOf(meta, f.frame))}`}
                style={{ left: at(f.frame) }}
                onClick={() => props.onFrame(f)}
              >
                <Icon name="image" size="sm" />
              </button>
            ))}
          </div>
          <div className="hx-scene3d-tl__head-line" style={{ left: at(frame) }} aria-hidden="true" />
        </div>
      </div>
    </footer>
  );
};

export default Scene3dTimeline;
