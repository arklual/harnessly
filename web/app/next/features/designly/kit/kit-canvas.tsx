// The kit's canvas on screen: ⌘/Ctrl + wheel or a pinch zooms around the cursor, the wheel alone pans, a drag of the
// canvas (or anything with Space held, or the middle button) pans, two fingers pinch on a touch screen. ⇧1 fits all,
// ⇧2 the selection, ⌘0 too fits all. A click selects, a double click focuses a section, opens a template's stack, or a
// layout's single view. The camera lives in a ref and the canvas redraws once a frame; React renders only what the
// controls read. A few cells shown near get a live page over the canvas (a pool of frames), the rest stay pictures.
import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type Ref,
} from 'react';
import {
  fitRect,
  lerpCamera,
  panCamera,
  toWorld,
  zoomAround,
  type ICamera,
} from '@/next/features/designly/kit/kit-camera';
import {
  drawWorld,
  PictureCache,
  readPalette,
  type INearCell,
  type IPalette,
} from '@/next/features/designly/kit/kit-draw';
import {
  frameAt,
  itemAt,
  rectOf,
  type IRect,
  type IWorld,
  type IWorldItem,
} from '@/next/features/designly/kit/kit-world';
import type { IAtlas } from '@/next/features/designly/kit/kit-api';
import { wheelFactor } from '@/next/features/designly/site-zoom';

export interface IKitCanvasHandle {
  /** Flies to a node of the world (smoothly unless asked not to); false when the world does not hold it. */
  flyTo: (node: string, smooth?: boolean) => boolean;
  fitAll: (smooth?: boolean) => void;
  zoomBy: (factor: number) => void;
}

interface IKitCanvasProps {
  world: IWorld;
  /** Changes when another world comes (a view or a theme): the camera fits it unless a node is asked for. */
  worldKey: string;
  selected: string | null;
  fileUrl: (file: string) => string;
  atlases: ReadonlyMap<string, { atlas: IAtlas; url: string }>;
  onSelect: (node: string | null) => void;
  onOpen: (item: IWorldItem) => void;
  /** The live page of a node near enough, null for none. */
  liveSrc: (node: string) => string | null;
  /** How wide a node's live page is drawn before it is scaled to its cell. */
  liveWidth: (node: string) => number;
  onZoom: (pct: number) => void;
  ref?: Ref<IKitCanvasHandle>;
}

const MAX_LIVE = 3;
const FLY_MS = 420;
const DRAG_PX = 4;

const KitCanvas = ({
  world,
  worldKey,
  selected,
  fileUrl,
  atlases,
  onSelect,
  onOpen,
  liveSrc,
  liveWidth,
  onZoom,
  ref,
}: IKitCanvasProps) => {
  const areaRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const cam = useRef<ICamera>({ s: 0.1, x: 0, y: 0 });
  const size = useRef({ w: 0, h: 0 });
  const palette = useRef<IPalette | null>(null);
  const frame = useRef(0);
  const flight = useRef(0);
  const hovered = useRef<string | null>(null);
  const space = useRef(false);
  const lastPct = useRef(-1);
  const liveEls = useRef(new Map<string, HTMLIFrameElement>());
  const [live, setLive] = useState<string[]>([]);
  const [loaded, setLoaded] = useState<ReadonlySet<string>>(() => new Set());
  const [panning, setPanning] = useState(false);
  const pictures = useRef<PictureCache | null>(null);

  // What the frame reads, kept current without restarting anything.
  const now = useRef({ world, selected, fileUrl, atlases, liveSrc, liveWidth, onZoom, loaded, live });
  useLayoutEffect(() => {
    now.current = { world, selected, fileUrl, atlases, liveSrc, liveWidth, onZoom, loaded, live };
  });
  // The frame is drawn by the latest draw; the picture cache and the input ask for one through schedule.
  const drawRef = useRef<() => void>(() => undefined);
  const schedule = useCallback(() => {
    if (frame.current === 0) frame.current = requestAnimationFrame(() => drawRef.current());
  }, []);

  const placeLive = useCallback((near: INearCell[]) => {
    for (const [node, el] of liveEls.current) {
      const cell = near.find((c) => c.node === node);
      if (!cell) {
        el.dataset.off = '1';
        continue;
      }
      const w = now.current.liveWidth(node);
      delete el.dataset.off;
      el.style.setProperty('--lx', `${cell.x}px`);
      el.style.setProperty('--ly', `${cell.y}px`);
      el.style.setProperty('--lw', `${w}px`);
      el.style.setProperty('--lh', `${(w * cell.h) / cell.w}px`);
      el.style.setProperty('--ls', String(cell.w / w));
    }
  }, []);

  const draw = useCallback(() => {
    frame.current = 0;
    const canvas = canvasRef.current;
    const area = areaRef.current;
    const ctx = canvas?.getContext('2d');
    if (!canvas || !area || !ctx || size.current.w === 0) return;
    palette.current ??= readPalette(area);
    pictures.current ??= new PictureCache(() => schedule());
    const n = now.current;
    const near = drawWorld(ctx, {
      world: n.world,
      camera: cam.current,
      area: size.current,
      dpr: window.devicePixelRatio || 1,
      palette: palette.current,
      pictures: pictures.current,
      fileUrl: n.fileUrl,
      atlases: n.atlases,
      selected: n.selected,
      hovered: hovered.current,
      live: n.loaded,
    });
    // The live pages: the selected cell first, then the largest on screen.
    const want = near
      .filter((c) => n.liveSrc(c.node) !== null)
      .sort((a, b) => Number(b.node === n.selected) - Number(a.node === n.selected) || b.w - a.w)
      .slice(0, MAX_LIVE)
      .map((c) => c.node);
    if (want.join('|') !== n.live.join('|')) {
      setLive(want);
      // A live page that left the pool is no longer counted as drawn.
      setLoaded((prev) => {
        const next = new Set([...prev].filter((node) => want.includes(node)));
        return next.size === prev.size ? prev : next;
      });
    }
    placeLive(near);
    const pct = Math.round(cam.current.s * 100);
    if (pct !== lastPct.current) {
      lastPct.current = pct;
      n.onZoom(pct);
    }
  }, [placeLive, schedule]);
  useLayoutEffect(() => {
    drawRef.current = draw;
  }, [draw]);

  const setCamera = useCallback(
    (c: ICamera) => {
      cancelAnimationFrame(flight.current);
      flight.current = 0;
      cam.current = c;
      schedule();
    },
    [schedule],
  );

  const flyCamera = useCallback(
    (to: ICamera, smooth: boolean) => {
      cancelAnimationFrame(flight.current);
      if (!smooth || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
        setCamera(to);
        return;
      }
      const from = cam.current;
      const start = performance.now();
      const step = (t: number) => {
        const k = Math.min(1, (t - start) / FLY_MS);
        cam.current = lerpCamera(from, to, k, size.current);
        schedule();
        flight.current = k < 1 ? requestAnimationFrame(step) : 0;
      };
      flight.current = requestAnimationFrame(step);
    },
    [schedule, setCamera],
  );

  const fitAll = useCallback(
    (smooth = true) => {
      const w = now.current.world;
      flyCamera(fitRect({ x: -40, y: -80, w: w.w + 80, h: w.h + 120 }, size.current, 32, 1), smooth);
    },
    [flyCamera],
  );

  const flyToRect = useCallback(
    (r: IRect, smooth: boolean) =>
      flyCamera(fitRect({ x: r.x, y: r.y - 48, w: r.w, h: r.h + 56 }, size.current, 56, 1.2), smooth),
    [flyCamera],
  );

  useImperativeHandle(
    ref,
    () => ({
      flyTo: (node, smooth = true) => {
        const r = rectOf(now.current.world, node);
        if (!r) return false;
        flyToRect(r, smooth);
        return true;
      },
      fitAll,
      zoomBy: (factor) =>
        setCamera(zoomAround(cam.current, { x: size.current.w / 2, y: size.current.h / 2 }, factor)),
    }),
    [fitAll, flyToRect, setCamera],
  );

  // The canvas follows the area's size and the screen's density.
  useLayoutEffect(() => {
    const area = areaRef.current;
    const canvas = canvasRef.current;
    if (!area || !canvas) return;
    const resize = () => {
      const r = area.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      size.current = { w: r.width, h: r.height };
      canvas.width = Math.round(r.width * dpr);
      canvas.height = Math.round(r.height * dpr);
      schedule();
    };
    const ro = new ResizeObserver(resize);
    ro.observe(area);
    resize();
    // Another theme of the app: the colours are read again.
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const theme = () => {
      palette.current = null;
      schedule();
    };
    mq.addEventListener('change', theme);
    const mo = new MutationObserver(theme);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'class'] });
    return () => {
      ro.disconnect();
      mq.removeEventListener('change', theme);
      mo.disconnect();
      cancelAnimationFrame(frame.current);
      cancelAnimationFrame(flight.current);
      frame.current = 0;
    };
  }, [schedule]);

  // A new world starts fitted (a node asked for flies there itself, after this).
  const fittedKey = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (fittedKey.current === worldKey) return;
    fittedKey.current = worldKey;
    pictures.current?.forgetQueued();
    if (size.current.w > 0) fitAll(false);
  }, [worldKey, fitAll]);

  useEffect(() => {
    schedule();
  }, [world, selected, atlases, loaded, live, schedule]);

  // The pointer: a drag pans, a click selects, a double click opens; wheel and pinch zoom.
  useEffect(() => {
    const area = areaRef.current;
    if (!area) return;
    const pointers = new Map<number, { x: number; y: number }>();
    let down: { x: number; y: number; moved: boolean; pan: boolean } | null = null;
    let pinch: { d: number } | null = null;
    const local = (e: { clientX: number; clientY: number }) => {
      const r = area.getBoundingClientRect();
      return { x: e.clientX - r.left, y: e.clientY - r.top };
    };
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) {
        setCamera(zoomAround(cam.current, local(e), wheelFactor(e.deltaY)));
        return;
      }
      setCamera(panCamera(cam.current, -e.deltaX, -e.deltaY));
    };
    const onDown = (e: PointerEvent) => {
      if ((e.target as HTMLElement).closest('button, a, input, textarea, [role="button"]')) return;
      area.setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, local(e));
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()];
        if (a && b) pinch = { d: Math.hypot(a.x - b.x, a.y - b.y) };
        down = null;
        return;
      }
      down = { ...local(e), moved: false, pan: e.button === 1 || space.current };
    };
    const onMove = (e: PointerEvent) => {
      const p = local(e);
      if (pointers.has(e.pointerId)) {
        const prev = pointers.get(e.pointerId) as { x: number; y: number };
        pointers.set(e.pointerId, p);
        if (pinch && pointers.size === 2) {
          const [a, b] = [...pointers.values()];
          if (a && b) {
            const d = Math.hypot(a.x - b.x, a.y - b.y);
            setCamera(
              zoomAround(cam.current, { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }, d / Math.max(1, pinch.d)),
            );
            pinch = { d };
          }
          return;
        }
        if (down) {
          if (!down.moved && Math.hypot(p.x - down.x, p.y - down.y) > DRAG_PX) {
            down.moved = true;
            setPanning(true);
          }
          if (down.moved) setCamera(panCamera(cam.current, p.x - prev.x, p.y - prev.y));
        }
        return;
      }
      const w = toWorld(cam.current, p);
      const it = itemAt(now.current.world, w.x, w.y);
      const next = it?.node ?? null;
      if (next !== hovered.current) {
        hovered.current = next;
        area.dataset.pointing = next ? '1' : '';
        schedule();
      }
    };
    const onUp = (e: PointerEvent) => {
      pointers.delete(e.pointerId);
      if (pointers.size < 2) pinch = null;
      const was = down;
      down = null;
      setPanning(false);
      if (!was || was.moved || was.pan) return;
      const w = toWorld(cam.current, local(e));
      const it = itemAt(now.current.world, w.x, w.y);
      if (it) {
        onSelect(it.node);
        return;
      }
      const f = frameAt(now.current.world, w.x, w.y);
      onSelect(f ? f.node : null);
    };
    const onDouble = (e: MouseEvent) => {
      const w = toWorld(cam.current, local(e));
      const it = itemAt(now.current.world, w.x, w.y);
      if (it) {
        onOpen(it);
        return;
      }
      const f = frameAt(now.current.world, w.x, w.y);
      if (f) flyToRect(f, true);
    };
    const typing = (t: EventTarget | null) =>
      t instanceof HTMLElement && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
    const onKey = (e: KeyboardEvent) => {
      if (typing(e.target)) return;
      if (e.code === 'Space') {
        space.current = e.type === 'keydown';
        area.dataset.space = space.current ? '1' : '';
        if (e.type === 'keydown') e.preventDefault();
        return;
      }
      if (e.type !== 'keydown') return;
      if (e.shiftKey && e.code === 'Digit1') {
        e.preventDefault();
        fitAll();
      } else if (e.shiftKey && e.code === 'Digit2') {
        e.preventDefault();
        const sel = now.current.selected;
        const r = sel ? rectOf(now.current.world, sel) : null;
        if (r) flyToRect(r, true);
      } else if ((e.metaKey || e.ctrlKey) && (e.key === '=' || e.key === '+')) {
        e.preventDefault();
        setCamera(zoomAround(cam.current, { x: size.current.w / 2, y: size.current.h / 2 }, 1.25));
      } else if ((e.metaKey || e.ctrlKey) && e.key === '-') {
        e.preventDefault();
        setCamera(zoomAround(cam.current, { x: size.current.w / 2, y: size.current.h / 2 }, 0.8));
      } else if ((e.metaKey || e.ctrlKey) && e.key === '0') {
        e.preventDefault();
        fitAll();
      }
    };
    area.addEventListener('wheel', onWheel, { passive: false });
    area.addEventListener('pointerdown', onDown);
    area.addEventListener('pointermove', onMove);
    area.addEventListener('pointerup', onUp);
    area.addEventListener('pointercancel', onUp);
    area.addEventListener('dblclick', onDouble);
    window.addEventListener('keydown', onKey);
    window.addEventListener('keyup', onKey);
    return () => {
      area.removeEventListener('wheel', onWheel);
      area.removeEventListener('pointerdown', onDown);
      area.removeEventListener('pointermove', onMove);
      area.removeEventListener('pointerup', onUp);
      area.removeEventListener('pointercancel', onUp);
      area.removeEventListener('dblclick', onDouble);
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('keyup', onKey);
    };
  }, [fitAll, flyToRect, onOpen, onSelect, schedule, setCamera]);

  return (
    <div ref={areaRef} className="hx-kit-canvas" data-panning={panning || undefined} tabIndex={-1}>
      <canvas ref={canvasRef} className="hx-kit-canvas__paint" aria-hidden="true" />
      {live.map((node) => {
        const src = liveSrc(node);
        if (!src) return null;
        return (
          <iframe
            key={`${node}|${src}`}
            ref={(el) => {
              if (el) liveEls.current.set(node, el);
              else liveEls.current.delete(node);
            }}
            className="hx-kit-live"
            data-off="1"
            data-ready={loaded.has(node) || undefined}
            src={src}
            title={node}
            tabIndex={-1}
            onLoad={() => {
              // The engine draws after load: the picture under it stays a moment longer.
              window.setTimeout(() => setLoaded((prev) => new Set(prev).add(node)), 600);
            }}
          />
        );
      })}
    </div>
  );
};

export default KitCanvas;
