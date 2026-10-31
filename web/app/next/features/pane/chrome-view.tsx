// The machine's Chrome that the agents drive (a chrome MCP), live: the tab in front comes as frames over the runtime's
// socket, and what the person does on it (clicks, wheel, keys, a pasted text) goes back to it, so a sign-in the agent
// waits for is done right here.
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type WheelEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import {
  pagePoint,
  parseChromeFrame,
  parseChromeMessage,
  tabLabel,
  type IChromeTab,
} from '@/next/features/pane/chrome-frame';

/** A socket that went is opened again after this. */
const RETRY_MS = 2000;
/** Moves of the mouse go at most this often (hover menus need them, a stream of them does not). */
const MOVE_MS = 50;

type TState = 'connecting' | 'up' | 'down';

export interface IChromeViewProps {
  /** The runtime's /api/chrome socket of the machine. */
  url: string;
}

const BUTTONS = ['left', 'middle', 'right'] as const;

const ChromeView = ({ url }: IChromeViewProps) => {
  const canvas = useRef<HTMLCanvasElement>(null);
  const socket = useRef<WebSocket | null>(null);
  const page = useRef({ w: 0, h: 0 });
  const lastMove = useRef(0);
  const [state, setState] = useState<TState>('connecting');
  const [error, setError] = useState('');
  const [tabs, setTabs] = useState<IChromeTab[]>([]);
  const [framed, setFramed] = useState(false);
  const [draft, setDraft] = useState<string | null>(null);

  useEffect(() => {
    let gone = false;
    let retry: ReturnType<typeof setTimeout> | null = null;
    // Frames decode one at a time; one that comes meanwhile replaces the one waiting.
    let drawing = false;
    let waiting: ArrayBuffer | null = null;
    const draw = (data: ArrayBuffer) => {
      if (drawing) {
        waiting = data;
        return;
      }
      const frame = parseChromeFrame(data);
      const el = canvas.current;
      if (!frame || !el || typeof createImageBitmap !== 'function') return;
      drawing = true;
      void createImageBitmap(frame.jpeg)
        .then((bitmap) => {
          if (gone) return;
          page.current = { w: frame.w, h: frame.h };
          if (el.width !== bitmap.width) el.width = bitmap.width;
          if (el.height !== bitmap.height) el.height = bitmap.height;
          el.getContext('2d')?.drawImage(bitmap, 0, 0);
          bitmap.close();
          setFramed(true);
        })
        .catch(() => undefined)
        .finally(() => {
          drawing = false;
          const next = waiting;
          waiting = null;
          if (next && !gone) draw(next);
        });
    };
    const open = () => {
      const ws = new WebSocket(url);
      ws.binaryType = 'arraybuffer';
      socket.current = ws;
      ws.onmessage = (event: MessageEvent<ArrayBuffer | string>) => {
        if (typeof event.data !== 'string') {
          draw(event.data);
          return;
        }
        const m = parseChromeMessage(event.data);
        if (m?.t === 'status') {
          setState(m.up ? 'up' : 'down');
          setError(m.error);
          if (!m.up) setFramed(false);
        } else if (m?.t === 'tabs') setTabs(m.tabs);
      };
      ws.onclose = () => {
        if (gone) return;
        setState('down');
        retry = setTimeout(open, RETRY_MS);
      };
    };
    open();
    return () => {
      gone = true;
      if (retry) clearTimeout(retry);
      socket.current?.close();
      socket.current = null;
    };
  }, [url]);

  const send = (input: Record<string, unknown>) => {
    const ws = socket.current;
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(input));
  };

  const pointOf = (event: MouseEvent | WheelEvent) => {
    const el = canvas.current;
    if (!el || page.current.w === 0) return null;
    const p = pagePoint(el.getBoundingClientRect(), page.current, { x: event.clientX, y: event.clientY });
    return p && { ...p, alt: event.altKey, ctrl: event.ctrlKey, meta: event.metaKey, shift: event.shiftKey };
  };

  const mouse = (type: string, event: MouseEvent) => {
    const p = pointOf(event);
    if (!p) return;
    send({
      t: 'mouse',
      type,
      button:
        type === 'mouseMoved' ? (event.buttons & 1 ? 'left' : 'none') : (BUTTONS[event.button] ?? 'none'),
      buttons: event.buttons,
      clickCount: type === 'mouseMoved' ? 0 : Math.max(1, event.detail),
      ...p,
    });
  };

  const active = tabs.find((x) => x.active);
  const typed = draft ?? active?.url ?? '';

  return (
    <div className="hx-tv" data-tool="chrome">
      <div className="hx-tbrowser hx-chrome">
        <div className="hx-tbrowser__bar">
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.site.back')}
            onClick={() => send({ t: 'nav', action: 'back' })}
          >
            <Icon name="arrow-left" size="sm" />
          </button>
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.site.forward')}
            onClick={() => send({ t: 'nav', action: 'forward' })}
          >
            <Icon name="arrow-right" size="sm" />
          </button>
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.site.reload')}
            onClick={() => send({ t: 'nav', action: 'reload' })}
          >
            <Icon name="rotate-cw" size="sm" />
          </button>
          <input
            className="hx-tbrowser__url"
            type="text"
            autoComplete="off"
            spellCheck={false}
            placeholder={t('next.pane.address')}
            aria-label={t('next.pane.addressLabel')}
            value={typed}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={() => setDraft(null)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') setDraft(null);
              if (event.key !== 'Enter' || typed.trim() === '') return;
              send({ t: 'nav', action: 'go', url: typed });
              setDraft(null);
              canvas.current?.focus();
            }}
          />
          <span className="hx-chrome__dot" data-state={state} title={t(`next.chrome.state.${state}`)} />
        </div>
        {tabs.length > 1 && (
          <div className="hx-chrome__tabs" role="tablist" aria-label={t('next.chrome.tabs')}>
            {tabs.map((x) => (
              <button
                key={x.id}
                type="button"
                role="tab"
                aria-selected={x.active}
                className="hx-chrome__tab"
                title={x.url}
                onClick={() => send({ t: 'tab', id: x.id })}
              >
                {tabLabel(x)}
              </button>
            ))}
          </div>
        )}
        <div className="hx-chrome__view">
          <canvas
            ref={canvas}
            className="hx-chrome__screen"
            data-shown={framed}
            tabIndex={0}
            aria-label={t('next.chrome.screen')}
            onMouseDown={(event) => {
              event.currentTarget.focus();
              event.preventDefault();
              mouse('mousePressed', event);
            }}
            onMouseUp={(event) => mouse('mouseReleased', event)}
            onMouseMove={(event) => {
              const now = performance.now();
              if (now - lastMove.current < MOVE_MS) return;
              lastMove.current = now;
              mouse('mouseMoved', event);
            }}
            onContextMenu={(event) => event.preventDefault()}
            onWheel={(event) => {
              const p = pointOf(event);
              if (p) send({ t: 'wheel', dx: event.deltaX, dy: event.deltaY, ...p });
            }}
            onKeyDown={(event: KeyboardEvent) => {
              // A paste comes as its text (onPaste): the page's clipboard is not this machine's.
              if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v') return;
              event.preventDefault();
              event.stopPropagation();
              send({
                t: 'key',
                key: event.key,
                code: event.code,
                keyCode: event.keyCode,
                alt: event.altKey,
                ctrl: event.ctrlKey,
                meta: event.metaKey,
                shift: event.shiftKey,
              });
            }}
            onPaste={(event) => {
              const text = event.clipboardData.getData('text');
              if (text) send({ t: 'text', text });
              event.preventDefault();
            }}
          />
          {!framed && (
            <div className="hx-tbrowser__note hx-chrome__note">
              {state === 'down' ? (
                <>
                  <div>{t('next.chrome.down')}</div>
                  {error && <div className="hx-chrome__error">{error}</div>}
                </>
              ) : (
                t('next.chrome.waiting')
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default ChromeView;
