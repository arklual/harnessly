// The background of a look behind the chat (styles/looks.css .hx-backdrop): a colour, the hues' gradient, a picture or
// an animated scene, with the grain and the dim over it. A scene is drawn in a worker on an OffscreenCanvas where the
// browser has one, so the page's own thread does no work per frame: no layout, no style, no React. It runs at the
// screen's rate and stops only while the page is hidden; with less motion asked for it is one still frame.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { clusterApi } from '@/next/core/net/machine';
import { useActiveLook } from '@/next/features/looks/look-layer';
import { SceneRunner, type ISceneSize, type ISceneSpec } from '@/next/features/looks/runner';
import type { TSceneMessage, TSceneReply } from '@/next/features/looks/scene-worker';
import { colorOf, gradientOf, sceneColors, type TMode } from '@/next/features/looks/tokens';
import type { ILook } from '@/next/features/looks/types';

/** The picture of a look, kept by the person's settings (/api/v2/me/looks/<hash>). */
export const lookImageUrl = (hash: string) => clusterApi(`/me/looks/${hash}` as `/${string}`);

interface IDriver {
  set: (spec: ISceneSpec | null, size: ISceneSize) => void;
  play: (on: boolean, still: boolean) => void;
  stop: () => void;
  /** It draws on the page's own thread: the dim and the grain stay layers of their own there (runner.ts). */
  onPage: boolean;
}

/** The scene's loop: in a worker when the canvas can go there, else here. */
const driverFor = (canvas: HTMLCanvasElement): IDriver => {
  // The detail a weak device fell to, on the canvas for diagnostics (runner.ts).
  const onDetail = (detail: number) => {
    canvas.dataset.detail = String(detail);
  };
  if (typeof canvas.transferControlToOffscreen === 'function' && typeof Worker !== 'undefined') {
    try {
      const worker = new Worker(new URL('./scene-worker.ts', import.meta.url), {
        type: 'module',
        name: 'hx-look-scene',
      });
      const offscreen = canvas.transferControlToOffscreen();
      const post = (msg: TSceneMessage, transfer: Transferable[] = []) => worker.postMessage(msg, transfer);
      worker.onmessage = (event: MessageEvent<TSceneReply>) => {
        if (event.data.k === 'detail') onDetail(event.data.detail);
      };
      post({ k: 'init', canvas: offscreen }, [offscreen]);
      return {
        set: (spec, size) => post({ k: 'set', spec, size }),
        play: (on, still) => post({ k: 'play', on, still }),
        stop: () => {
          post({ k: 'stop' });
          worker.terminate();
        },
        onPage: false,
      };
    } catch {
      // A canvas already handed over, or no module workers: draw here.
    }
  }
  const runner = new SceneRunner(canvas, 'page', onDetail);
  // On the page's thread a full-size grain pass each frame costs more than the compositor blending a still layer.
  return {
    set: (spec, size) => runner.set(spec && { ...spec, dim: 0, grain: 0 }, size),
    play: (on, still) => runner.play(on, still),
    stop: () => runner.stop(),
    onPage: true,
  };
};

const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)');

const Scene = ({ look, mode }: { look: ILook; mode: TMode }) => {
  const ref = useRef<HTMLCanvasElement>(null);
  const driver = useRef<IDriver | null>(null);
  const [onPage, setOnPage] = useState(false);
  const size = useRef<ISceneSize>({ w: 0, h: 0, dpr: 1 });
  const specKey = JSON.stringify(
    look.bg.scene
      ? {
          scene: look.bg.scene,
          colors: sceneColors(look, mode),
          speed: (look.bg.speed ?? 50) / 100,
          dim: look.dim / 100,
          grain: look.grain / 100,
        }
      : null,
  );
  const spec = useRef<ISceneSpec | null>(null);
  useLayoutEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const d = driverFor(canvas);
    driver.current = d;
    setOnPage(d.onPage);
    const motion = reducedMotion();
    const play = () => d.play(document.visibilityState === 'visible', motion.matches);
    const measure = () => {
      const box = canvas.getBoundingClientRect();
      size.current = {
        w: Math.round(box.width),
        h: Math.round(box.height),
        dpr: window.devicePixelRatio || 1,
      };
      d.set(spec.current, size.current);
      play();
    };
    // A resize is measured on the observer's own beat, not in a frame of the feed.
    const ro = new ResizeObserver(measure);
    ro.observe(canvas);
    document.addEventListener('visibilitychange', play);
    motion.addEventListener('change', play);
    return () => {
      ro.disconnect();
      document.removeEventListener('visibilitychange', play);
      motion.removeEventListener('change', play);
      d.stop();
      driver.current = null;
    };
  }, []);
  useEffect(() => {
    spec.current = JSON.parse(specKey) as ISceneSpec | null;
    const d = driver.current;
    if (!d || size.current.w === 0) return;
    d.set(spec.current, size.current);
    d.play(document.visibilityState === 'visible', reducedMotion().matches);
  }, [specKey]);
  return (
    <>
      <canvas ref={ref} className="hx-backdrop__canvas" aria-hidden="true" />
      {onPage && look.dim > 0 && <div className="hx-backdrop__dim" />}
      {onPage && look.grain > 0 && <div className="hx-backdrop__grain" />}
    </>
  );
};

/** Mounted inside a stage (chat-stage.tsx); nothing without a look that has a background. */
const LookBackdrop = () => {
  const active = useActiveLook();
  const ref = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);
  const look = active?.look ?? null;
  const has = look !== null && (look.bg.kind !== 'none' || look.grain > 0);
  const live = look?.bg.kind === 'scene';
  useLayoutEffect(() => {
    const stage = ref.current?.parentElement;
    if (!stage || !has) return;
    // «scene»: the parts over it drop their backdrop blur (styles/looks.css), which would be computed anew each frame.
    stage.dataset.backdrop = live ? 'scene' : '';
    return () => {
      delete stage.dataset.backdrop;
    };
  }, [has, live]);
  useEffect(() => {
    if (!has) return;
    const id = requestAnimationFrame(() => setShown(true));
    return () => {
      cancelAnimationFrame(id);
      setShown(false);
    };
  }, [has]);
  const mode = active?.mode ?? 'light';
  let fill = 'none';
  if (look?.bg.kind === 'gradient') fill = gradientOf(look, mode);
  if (look?.bg.kind === 'color') fill = `linear-gradient(${colorOf(look, mode)}, ${colorOf(look, mode)})`;
  if (look?.bg.kind === 'image' && look.bg.image) fill = `url("${lookImageUrl(look.bg.image)}")`;
  const dim = look ? String(look.dim / 100) : '0';
  const grain = look ? String((look.grain / 100) * 0.55) : '0';
  const blur = look && look.blur > 0 ? `blur(${look.blur}px)` : 'none';
  // The values go on the element as custom properties (styles/looks.css reads them), not as inline styles.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.setProperty('--hx-look-fill', fill);
    el.style.setProperty('--hx-look-dim', dim);
    el.style.setProperty('--hx-look-grain', grain);
    el.style.setProperty('--hx-look-blur', blur);
  }, [fill, dim, grain, blur, has]);
  if (!active || !look || !has) return null;
  return (
    <div ref={ref} className="hx-backdrop" data-shown={shown || undefined} aria-hidden="true">
      {fill !== 'none' && <div className="hx-backdrop__fill" data-fit={look.bg.fit} />}
      {look.bg.kind === 'scene' && <Scene look={look} mode={mode} />}
      {/* A scene draws its own dim and grain (runner.ts): nothing above it is blended anew each frame. */}
      {!live && look.dim > 0 && <div className="hx-backdrop__dim" />}
      {!live && look.grain > 0 && <div className="hx-backdrop__grain" />}
    </div>
  );
};

export default LookBackdrop;
