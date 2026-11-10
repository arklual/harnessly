// The page of the machine's live browser: its pictures drawn on a canvas as they come over the tab's socket,
// the person's pointer, keys, wheel and paste sent back. The page runs on the machine; here is only its picture.
import { useEffect, useRef, useState } from 'react';
import {
  buttonOf,
  decodeFrame,
  liveBrowserSocketUrl,
  modifiersOf,
  readLiveMessage,
  textOfKey,
  virtualKeyOf,
  type ILiveActed,
  type ILivePage,
} from '@/next/core/net/live-browser';
import type { IFrameAct } from '@/next/features/pane/use-frame-bridge';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import LoadFailed from '@/next/ui/load-failed';

/** What the tab does with the page: the machine's browser keeps the history, so the buttons ask it. */
export interface ILiveControl {
  go: (url: string) => void;
  back: () => void;
  forward: () => void;
  reload: () => void;
  /** The agent's action, run by the machine on the page; null when no answer came in time. */
  act: (message: IFrameAct, timeoutMs: number) => Promise<ILiveActed | null>;
}

export interface ILiveViewProps {
  machine: MachineId;
  /** The pane's tab: the machine keeps one page per tab, which survives a reload of the pane. */
  tab: string;
  /** The address the tab opened; the page's own moves come back through onPage. */
  url: string;
  /** The page's CSS size on a device; null fills the stage. */
  size: { w: number; h: number } | null;
  theme: 'light' | 'dark';
  onPage: (page: ILivePage) => void;
  /** The tab's handle on the page, null while there is none. */
  onControl: (control: ILiveControl | null) => void;
  /** The box the picture is in: the agent's cursor is placed over it. */
  onBox?: (el: HTMLDivElement | null) => void;
}

const SIZE_DEBOUNCE_MS = 160;
const MAX_DPR = 2;
const WHEEL_LINE_PX = 16;
const PASTE_WAIT_MS = 250;

type TState = 'connecting' | 'live' | 'ended';

const LiveView = ({ machine, tab, url, size, theme, onPage, onControl, onBox }: ILiveViewProps) => {
  const box = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const socket = useRef<WebSocket | null>(null);
  // The agent's actions waiting for the machine's answer, by id.
  const acting = useRef(new Map<string, (acted: ILiveActed | null) => void>());
  const [state, setState] = useState<TState>('connecting');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // The latest props, for the socket's callbacks and the handlers: they outlive one render.
  const sizeRef = useRef(size);
  const page = useRef(onPage);
  const control = useRef(onControl);
  useEffect(() => {
    sizeRef.current = size;
    page.current = onPage;
    control.current = onControl;
  });

  /** The page's CSS size now: the device's, or the box's. */
  const cssSize = () => {
    const fixed = sizeRef.current;
    if (fixed) return fixed;
    const el = box.current;
    return {
      w: Math.max(200, Math.round(el?.clientWidth ?? 0) || 1280),
      h: Math.max(200, Math.round(el?.clientHeight ?? 0) || 800),
    };
  };
  const dpr = () => Math.min(MAX_DPR, Math.max(1, window.devicePixelRatio || 1));

  const send = (message: Record<string, unknown>) => {
    const ws = socket.current;
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(message));
  };

  useEffect(() => {
    const ws = new WebSocket(liveBrowserSocketUrl(machine, tab));
    ws.binaryType = 'arraybuffer';
    socket.current = ws;
    let drawing = false;
    let pending: ArrayBuffer | null = null;
    // Every picture is answered once it is drawn or dropped: the machine keeps at most two on the way.
    const ack = () => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'ack' }));
    };
    const draw = async (bytes: ArrayBuffer) => {
      if (drawing) {
        if (pending) ack();
        pending = bytes;
        return;
      }
      drawing = true;
      try {
        const bitmap = await decodeFrame(bytes);
        const el = canvas.current;
        if (el) {
          if (el.width !== bitmap.width || el.height !== bitmap.height) {
            el.width = bitmap.width;
            el.height = bitmap.height;
          }
          el.getContext('2d')?.drawImage(bitmap, 0, 0);
          bitmap.close();
          setState((was) => (was === 'connecting' ? 'live' : was));
        }
      } catch {
        // A frame that does not decode is skipped: the next one shows the page.
      }
      ack();
      drawing = false;
      if (pending) {
        const next = pending;
        pending = null;
        void draw(next);
      }
    };
    const act = (message: IFrameAct, timeoutMs: number) =>
      new Promise<ILiveActed | null>((resolve) => {
        if (ws.readyState !== WebSocket.OPEN) {
          resolve(null);
          return;
        }
        const timer = window.setTimeout(() => {
          acting.current.delete(message.id);
          resolve(null);
        }, timeoutMs);
        acting.current.set(message.id, (acted) => {
          window.clearTimeout(timer);
          acting.current.delete(message.id);
          resolve(acted);
        });
        ws.send(JSON.stringify({ t: 'act', ...message }));
      });
    ws.onopen = () => {
      const { w, h } = cssSize();
      ws.send(JSON.stringify({ t: 'open', url, w, h, dpr: dpr(), ack: true }));
      control.current({
        go: (next) => send({ t: 'nav', url: next }),
        back: () => send({ t: 'back' }),
        forward: () => send({ t: 'fwd' }),
        reload: () => send({ t: 'reload' }),
        act,
      });
    };
    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        void draw(event.data);
        return;
      }
      let data: unknown;
      try {
        data = JSON.parse(String(event.data));
      } catch {
        return;
      }
      const m = readLiveMessage(data);
      if (!m) return;
      switch (m.t) {
        case 'page':
          setLoading(m.loading);
          page.current(m);
          return;
        case 'clip':
          if (m.text) void navigator.clipboard?.writeText(m.text).catch(() => undefined);
          return;
        case 'acted':
          acting.current.get(m.id)?.(m);
          return;
        case 'error':
          setError(m.text);
          return;
        case 'end':
          setState('ended');
          return;
        default:
          return;
      }
    };
    ws.onclose = () => {
      // An action still waiting hears nothing more from this socket.
      acting.current.forEach((answer) => answer(null));
      acting.current.clear();
      if (socket.current === ws) {
        socket.current = null;
        control.current(null);
        setState((was) => (was === 'live' ? 'ended' : was));
      }
    };
    const visibility = () => send({ t: 'cast', on: document.visibilityState === 'visible' });
    document.addEventListener('visibilitychange', visibility);
    return () => {
      document.removeEventListener('visibilitychange', visibility);
      if (socket.current === ws) {
        socket.current = null;
        control.current(null);
      }
      ws.close();
    };
    // The address is where the tab started; later ones go through the control, not a new socket.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [machine, tab, attempt]);

  // The page follows the box's size (or the device's) with a short delay, so a drag of the pane's edge sends one size.
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    let timer = 0;
    const tell = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        const { w, h } = cssSize();
        send({ t: 'size', w, h, dpr: dpr() });
      }, SIZE_DEBOUNCE_MS);
    };
    const observer = new ResizeObserver(tell);
    observer.observe(el);
    tell();
    return () => {
      observer.disconnect();
      window.clearTimeout(timer);
    };
  }, [size?.w, size?.h]);

  /** Where a pointer is on the page, in its CSS pixels (the box may be scaled to fit the pane). */
  const at = (e: { clientX: number; clientY: number }) => {
    const el = box.current;
    if (!el) return { x: 0, y: 0 };
    const rect = el.getBoundingClientRect();
    const { w, h } = cssSize();
    return {
      x: Math.round(((e.clientX - rect.left) * w) / Math.max(1, rect.width)),
      y: Math.round(((e.clientY - rect.top) * h) / Math.max(1, rect.height)),
    };
  };

  const moveFrame = useRef(0);
  const lastMove = useRef<Record<string, unknown> | null>(null);
  const mouse = (k: 'down' | 'up' | 'move', e: React.PointerEvent) => {
    const { x, y } = at(e);
    const message = {
      t: 'mouse',
      k,
      x,
      y,
      b: buttonOf(e.button),
      bs: e.buttons,
      m: modifiersOf(e),
      c: k === 'move' ? 0 : 1,
    };
    if (k !== 'move') {
      window.cancelAnimationFrame(moveFrame.current);
      lastMove.current = null;
      send(message);
      return;
    }
    // Moves come faster than the socket should carry them: one per frame.
    lastMove.current = message;
    if (moveFrame.current) return;
    moveFrame.current = window.requestAnimationFrame(() => {
      moveFrame.current = 0;
      if (lastMove.current) send(lastMove.current);
      lastMove.current = null;
    });
  };

  const pasteAt = useRef(0);
  const key = (k: 'down' | 'up', e: React.KeyboardEvent) => {
    const ctrlOrMeta = e.ctrlKey || e.metaKey;
    if (ctrlOrMeta && k === 'down' && (e.key === 'v' || e.key === 'V')) {
      // The person's clipboard goes to the page as text; the paste event brings it, or the clipboard is asked.
      pasteAt.current = Date.now();
      window.setTimeout(() => {
        if (pasteAt.current === 0) return;
        pasteAt.current = 0;
        void navigator.clipboard
          ?.readText()
          .then((text) => text && send({ t: 'text', text }))
          .catch(() => undefined);
      }, PASTE_WAIT_MS);
      return;
    }
    if (ctrlOrMeta && k === 'down' && (e.key === 'c' || e.key === 'C')) send({ t: 'copy' });
    // The page's keys are the page's: the pane's own shortcuts do not fire while the page has the focus.
    e.preventDefault();
    send({
      t: 'key',
      k,
      key: e.key,
      code: e.code,
      text: k === 'down' ? textOfKey(e.key, ctrlOrMeta) : '',
      vk: virtualKeyOf(e.key, e.code),
      m: modifiersOf(e),
    });
  };

  const paste = (e: React.ClipboardEvent) => {
    e.preventDefault();
    pasteAt.current = 0;
    const text = e.clipboardData.getData('text/plain');
    if (text) send({ t: 'text', text });
  };

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    // A trackpad gives many small turns a frame: they add up and go once per frame.
    let turn: { x: number; y: number; dx: number; dy: number; m: number } | null = null;
    let frame = 0;
    const flush = () => {
      frame = 0;
      if (turn) send({ t: 'wheel', ...turn });
      turn = null;
    };
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      const scale = e.deltaMode === 1 ? WHEEL_LINE_PX : e.deltaMode === 2 ? cssSize().h : 1;
      const { x, y } = at(e);
      const m = modifiersOf(e);
      const dx = Math.round(e.deltaX * scale);
      const dy = Math.round(e.deltaY * scale);
      if (turn && turn.m === m) {
        turn = { x, y, dx: turn.dx + dx, dy: turn.dy + dy, m };
      } else {
        if (turn) flush();
        turn = { x, y, dx, dy, m };
      }
      if (!frame) frame = window.requestAnimationFrame(flush);
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => {
      el.removeEventListener('wheel', wheel);
      window.cancelAnimationFrame(frame);
    };
    // The handler reads the box and the size through refs: it needs no new listener per render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      ref={(el) => {
        box.current = el;
        onBox?.(el);
      }}
      className="hx-tbrowser__frame hx-tbrowser__live"
      data-theme={theme}
      data-state={state}
      data-loading={loading || undefined}
      tabIndex={0}
      role="application"
      aria-label={t('next.pane.live.label')}
      onPointerDown={(e) => {
        box.current?.focus({ preventScroll: true });
        mouse('down', e);
      }}
      onPointerUp={(e) => mouse('up', e)}
      onPointerMove={(e) => mouse('move', e)}
      onContextMenu={(e) => e.preventDefault()}
      onKeyDown={(e) => key('down', e)}
      onKeyUp={(e) => key('up', e)}
      onPaste={paste}
    >
      <canvas ref={canvas} className="hx-tbrowser__live-canvas" />
      {state === 'connecting' && error === null && (
        <div className="hx-tbrowser__live-note">{t('next.pane.live.opening')}</div>
      )}
      {error !== null && state !== 'ended' && (
        <div className="hx-tbrowser__live-note" data-tone="error">
          {error}
        </div>
      )}
      {state === 'ended' && (
        <div className="hx-tbrowser__live-failed">
          <LoadFailed
            title={t('next.pane.live.ended')}
            reason={error ?? t('next.pane.live.endedText')}
            onRetry={() => {
              setError(null);
              setState('connecting');
              setAttempt((n) => n + 1);
            }}
          />
        </div>
      )}
    </div>
  );
};

export default LiveView;
