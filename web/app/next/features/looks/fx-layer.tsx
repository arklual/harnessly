// The live effect of a look (docs/looks.md, «Живые эффекты»): a transparent sandboxed frame over the chat's stage (or
// under it, as a background) that runs a built-in effect or the person's own script against the hx SDK
// (fx/runtime.js). The frame never takes the pointer (pointer-events: none); this side tells it, in the stage's
// coordinates, where the pointer is, the boxes of the field and the messages on screen, the agent's state and a few
// events: a prompt sent, an answer being written, its end. Never what a message says.
import { useEffect, useRef, useState } from 'react';
import { clusterApi } from '@/next/core/net/machine';
import { FX_BACK, FX_LOAD, FX_SANDBOX, fxCodeMessage, fxFrameUrl } from '@/next/features/looks/fx-frame';
import { lastWeather, watchWeather } from '@/next/features/looks/weather-feed';
import { useActiveLook } from '@/next/features/looks/look-layer';
import type { TMode } from '@/next/features/looks/tokens';
import type { ILookFx } from '@/next/features/looks/types';
import { entryStore } from '@/next/stores/conversation';
import { conversationsStore } from '@/next/stores/conversations';
import { agentStateOf } from '@/next/stores/chat-state';
import type { ConvId } from '@/next/protocol/v2';

type TLayer = 'front' | 'back';

/** The blocks an effect knows of, by their selector in the stage. */
const BLOCKS: readonly (readonly [string, string])[] = [
  ['composer', '.hx-composer'],
  ['user', '.hx-msg-user'],
  ['answer', '.hx-msg[data-kind="turn"]'],
];
const MAX_BLOCKS = 48;

interface IBlock {
  id: number;
  kind: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

const ids = new WeakMap<Element, number>();
let nextId = 1;
const idOf = (el: Element): number => {
  let id = ids.get(el);
  if (id === undefined) ids.set(el, (id = nextId++));
  return id;
};

/** The person's scripts by hash, fetched once per page. */
const scripts = new Map<string, Promise<string | null>>();
const scriptOf = (hash: string): Promise<string | null> => {
  let p = scripts.get(hash);
  if (!p) {
    p = fetch(clusterApi(`/me/looks/${hash}` as `/${string}`), { credentials: 'include' })
      .then((r) => (r.ok ? r.text() : null))
      .catch(() => null);
    scripts.set(hash, p);
  }
  return p;
};

/** A colour of the page as rgb(), whatever space the token is written in. */
const probe = typeof document !== 'undefined' ? document.createElement('canvas').getContext('2d') : null;
const rgbOf = (css: string, fallback: string): string => {
  if (!probe || !css) return fallback;
  probe.clearRect(0, 0, 1, 1);
  probe.fillStyle = fallback;
  probe.fillStyle = css;
  probe.fillRect(0, 0, 1, 1);
  const [r, g, b] = probe.getImageData(0, 0, 1, 1).data;
  return `rgb(${r},${g},${b})`;
};
/** The colour the chat's feed is drawn on: the first painted background from the feed up (a laser masks text with it). */
const stageColor = (from: Element | null): string => {
  for (let el = from; el; el = el.parentElement) {
    const c = getComputedStyle(el).backgroundColor;
    if (c && c !== 'transparent' && !/rgba\(.*,\s*0\)$/.test(c)) return rgbOf(c, '#111111');
  }
  return rgbOf(getComputedStyle(document.body).backgroundColor, '#111111');
};

const colorsNow = (stage: Element | null = null) => {
  const style = getComputedStyle(document.documentElement);
  const v = (name: string) => style.getPropertyValue(name).trim();
  return {
    accent: rgbOf(v('--hx-accent'), '#8b7cf6'),
    fg: rgbOf(v('--hx-fg-1'), '#ffffff'),
    bg: rgbOf(v('--hx-bg-pane') || v('--hx-bg'), '#111111'),
    stage: stageColor(stage?.querySelector('.hx-feed') ?? stage),
    hues: [] as string[],
  };
};

const agentOf = (chat: string | null): { state: 'idle' | 'working' | 'waiting'; since: number } => {
  const s = chat ? agentStateOf(chat as ConvId) : null;
  if (!s) return { state: 'idle', since: 0 };
  if (s.k === 'working') return { state: 'working', since: s.since };
  if (s.k === 'waiting-question' || s.k === 'waiting-permission') return { state: 'waiting', since: s.since };
  return { state: 'idle', since: s.since };
};

const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

interface IFxFrameProps {
  fx: ILookFx;
  mode: TMode;
  chat: string | null;
  layer: TLayer;
}

const FxFrame = ({ fx, mode, chat, layer }: IFxFrameProps) => {
  const ref = useRef<HTMLIFrameElement>(null);
  // The effect's code: a built-in one loaded with its pictures, or the person's script by its hash.
  const source = fx.preset ?? fx.script ?? '';
  const [fetched, setFetched] = useState<{ source: string; text: string | null } | null>(null);
  useEffect(() => {
    if (!source) return;
    let off = false;
    const load = fx.preset ? FX_LOAD[fx.preset]().catch(() => null) : scriptOf(source);
    void load.then((text) => !off && setFetched({ source, text }));
    return () => {
      off = true;
    };
  }, [source, fx.preset]);
  const code = fetched?.source === source ? fetched.text : null;
  const doc = code;
  const power = (fx.power ?? 60) / 100;
  const theme = useRef({ mode, power });
  useEffect(() => {
    theme.current = { mode, power };
  }, [mode, power]);

  useEffect(() => {
    const frame = ref.current;
    const stage = frame?.parentElement?.closest('.hx-stage') ?? frame?.parentElement ?? null;
    if (!frame || !doc || !stage) return;
    const post = (m: Record<string, unknown>) => frame.contentWindow?.postMessage(m, '*');
    let ready = false;
    let box = frame.getBoundingClientRect();
    let blocks: IBlock[] = [];
    let lastAnswer: { id: number; h: number } | null = null;
    let agent = agentOf(chat);
    let warned = false;

    const sizeMsg = () => ({
      w: Math.round(box.width),
      h: Math.round(box.height),
      dpr: window.devicePixelRatio || 1,
    });
    const measure = () => {
      box = frame.getBoundingClientRect();
      const list: IBlock[] = [];
      for (const [kind, sel] of BLOCKS)
        for (const el of stage.querySelectorAll(sel)) {
          const r = el.getBoundingClientRect();
          if (r.width < 4 || r.bottom < box.top - 40 || r.top > box.bottom + 40) continue;
          list.push({
            id: idOf(el),
            kind,
            x: r.left - box.left,
            y: r.top - box.top,
            w: r.width,
            h: r.height,
          });
          if (list.length >= MAX_BLOCKS) break;
        }
      blocks = list;
      if (!ready) return;
      post({ hx: 'blocks', list });
      // The newest answer grows while the agent works: it is being written.
      const answers = list.filter((b) => b.kind === 'answer');
      const newest = answers.length ? answers.reduce((a, b) => (b.y > a.y ? b : a)) : null;
      if (newest && agent.state === 'working') {
        if (!lastAnswer || lastAnswer.id !== newest.id || newest.h > lastAnswer.h + 0.5)
          post({ hx: 'ev', name: 'answer', data: { block: newest.id, done: false } });
        lastAnswer = { id: newest.id, h: newest.h };
      }
    };

    let measureQueued = false;
    const queueMeasure = () => {
      if (measureQueued) return;
      measureQueued = true;
      requestAnimationFrame(() => {
        measureQueued = false;
        measure();
      });
    };

    let ptr: { x: number; y: number; down: boolean } | null = null;
    let ptrQueued = false;
    const sendPtr = () => {
      ptrQueued = false;
      if (!ptr || !ready) return;
      const x = ptr.x - box.left;
      const y = ptr.y - box.top;
      post({
        hx: 'ptr',
        x,
        y,
        down: ptr.down,
        inside: x >= 0 && y >= 0 && x <= box.width && y <= box.height,
      });
    };
    const onMove = (e: PointerEvent) => {
      ptr = { x: e.clientX, y: e.clientY, down: e.buttons !== 0 };
      if (!ptrQueued) {
        ptrQueued = true;
        requestAnimationFrame(sendPtr);
      }
    };
    const onLeave = () => {
      ptr = { x: -9999, y: -9999, down: false };
      sendPtr();
    };
    const blockAt = (x: number, y: number) =>
      [...blocks].reverse().find((b) => x >= b.x && x <= b.x + b.w && y >= b.y && y <= b.y + b.h)?.id ?? null;
    const onButton = (name: 'down' | 'up' | 'dblclick') => (e: MouseEvent) => {
      if (!ready) return;
      const x = e.clientX - box.left;
      const y = e.clientY - box.top;
      if (x < 0 || y < 0 || x > box.width || y > box.height) return;
      post({ hx: 'ev', name, data: { x, y, block: blockAt(x, y) } });
    };
    const onDown = onButton('down');
    const onUp = onButton('up');
    const onDbl = onButton('dblclick');

    const onMessage = (event: MessageEvent) => {
      if (event.source !== frame.contentWindow) return;
      const data = event.data as { hxFx?: string; msg?: string } | null;
      if (data?.hxFx === 'boot') {
        if (doc) post(fxCodeMessage(doc, layer));
      } else if (data?.hxFx === 'ready') {
        ready = true;
        box = frame.getBoundingClientRect();
        post({
          hx: 'init',
          ...sizeMsg(),
          layer,
          power: theme.current.power,
          mode: theme.current.mode,
          colors: colorsNow(stage),
          reduced: reduced(),
        });
        post({ hx: 'agent', ...agent });
        const w = fx.preset === 'weather' ? lastWeather() : null;
        if (w) post({ hx: 'weather', ...w });
        measure();
      } else if (data?.hxFx === 'err' && !warned) {
        warned = true;
        console.warn(`[живая тема] ${String(data.msg ?? '').slice(0, 400)}`);
      }
    };

    const ro = new ResizeObserver(() => {
      box = frame.getBoundingClientRect();
      if (ready) post({ hx: 'size', ...sizeMsg() });
      queueMeasure();
    });
    ro.observe(frame);
    const timer = setInterval(measure, 300);
    const mountedAt = Date.now();
    const prefix = chat ? `${chat} ` : null;
    const offEntries = prefix
      ? entryStore.subscribeAll((changed) => {
          for (const k of changed) {
            if (!k.startsWith(prefix)) continue;
            const e = entryStore.get(k);
            if (!e || e.at < mountedAt || e.t !== 'user') continue;
            // The person's message is drawn in a frame or two: then it has a box.
            setTimeout(() => {
              measure();
              const users = blocks.filter((b) => b.kind === 'user');
              const last = users.length ? users.reduce((a, b) => (b.y > a.y ? b : a)) : null;
              if (ready) post({ hx: 'ev', name: 'prompt', data: { block: last?.id ?? null } });
            }, 120);
          }
        })
      : () => {};
    const offState = chat
      ? conversationsStore.subscribeKey(chat as ConvId, () => {
          const next = agentOf(chat);
          if (next.state === agent.state) return;
          const was = agent.state;
          agent = next;
          if (!ready) return;
          post({ hx: 'agent', ...agent });
          if (was === 'working' && next.state !== 'working') {
            post({ hx: 'ev', name: 'answer-end', data: { block: lastAnswer?.id ?? null } });
            lastAnswer = null;
          }
          if (next.state === 'working') post({ hx: 'ev', name: 'answer-start', data: {} });
        })
      : () => {};

    // «Погода»: the weather where the person is, from the page (the frame has no network).
    const offWeather =
      fx.preset === 'weather' ? watchWeather((w) => ready && post({ hx: 'weather', ...w })) : () => {};
    window.addEventListener('message', onMessage);
    window.addEventListener('pointermove', onMove, { passive: true, capture: true });
    window.addEventListener('pointerdown', onDown, { passive: true, capture: true });
    window.addEventListener('pointerup', onUp, { passive: true, capture: true });
    window.addEventListener('dblclick', onDbl, { passive: true, capture: true });
    document.documentElement.addEventListener('pointerleave', onLeave);
    window.addEventListener('scroll', queueMeasure, { passive: true, capture: true });
    return () => {
      window.removeEventListener('message', onMessage);
      window.removeEventListener('pointermove', onMove, { capture: true });
      window.removeEventListener('pointerdown', onDown, { capture: true });
      window.removeEventListener('pointerup', onUp, { capture: true });
      window.removeEventListener('dblclick', onDbl, { capture: true });
      document.documentElement.removeEventListener('pointerleave', onLeave);
      window.removeEventListener('scroll', queueMeasure, { capture: true });
      ro.disconnect();
      clearInterval(timer);
      offEntries();
      offState();
      offWeather();
    };
  }, [doc, chat, layer, fx.preset]);

  // A new colour or power reaches the running effect without starting it again.
  useEffect(() => {
    const frame = ref.current;
    frame?.contentWindow?.postMessage(
      { hx: 'theme', mode, power, colors: colorsNow(frame.parentElement?.closest('.hx-stage') ?? null) },
      '*',
    );
  }, [mode, power]);

  if (!doc) return null;
  return (
    <iframe
      ref={ref}
      className="hx-fx"
      data-layer={layer}
      sandbox={FX_SANDBOX}
      src={fxFrameUrl()}
      key={`${layer}:${doc.length}:${doc.slice(-64)}`}
      title=""
      aria-hidden="true"
      tabIndex={-1}
    />
  );
};

/** The live effect of the look on the page now, in one layer; nothing without one or with less motion asked for. */
const LookFx = ({ layer }: { layer: TLayer }) => {
  const active = useActiveLook();
  const [still, setStill] = useState(() => typeof window !== 'undefined' && reduced());
  useEffect(() => {
    const m = window.matchMedia('(prefers-reduced-motion: reduce)');
    const on = () => setStill(m.matches);
    m.addEventListener('change', on);
    return () => m.removeEventListener('change', on);
  }, []);
  const fx = active?.look.fx;
  if (!active || !fx || still) return null;
  if (layer === 'back' && !fx.back && !(fx.preset && FX_BACK.has(fx.preset))) return null;
  return <FxFrame fx={fx} mode={active.mode} chat={active.chat} layer={layer} />;
};

export default LookFx;
