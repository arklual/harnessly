import { useEffect, useRef, useState, type PointerEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { pagePoint, type IBrowserTab, type IViewport } from './agent-browser-frame';
import { useBrowserSocket } from './agent-browser-socket';

const presets: IViewport[] = [
  { mode: 'fixed', w: 1280, h: 800, dpr: 1 },
  { mode: 'fixed', w: 1440, h: 900, dpr: 1 },
  { mode: 'fixed', w: 1920, h: 1080, dpr: 1 },
  { mode: 'mobile', w: 390, h: 844, dpr: 2 },
  { mode: 'mobile', w: 820, h: 1180, dpr: 2 },
];
const label = (key: string) => t(`next.agentBrowser.${key}`);

/** A conversation's managed browser, with person input independent of the agent's working tab. */
export default function AgentBrowserView({ url }: { url: string }) {
  const { canvas, page, status, tabs, menu, setMenu, notice, framed, imageSize, send } =
    useBrowserSocket(url);
  const active = tabs.tabs.find((tab) => tab.id === tabs.current);
  const area = useRef<HTMLDivElement>(null),
    address = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const [sizeOpen, setSizeOpen] = useState(false),
    [custom, setCustom] = useState(false);
  const [width, setWidth] = useState(1280),
    [height, setHeight] = useState(800);
  const [bounds, setBounds] = useState({ w: 320, h: 240 });
  const [now, setNow] = useState(() => Date.now());
  const focusNew = useRef(false);
  const touch = useRef<{ x: number; y: number; startX: number; startY: number; dragged: boolean } | null>(
    null,
  );
  const moveAt = useRef(0);
  const resize = useRef<{ x: number; y: number; w: number; h: number } | null>(null);
  // Recovered and newly discovered tabs can arrive before their first explicit viewport.
  const mode = active?.viewport.mode || 'fit';
  const activeId = active?.id;
  const dialog = active?.dialog;
  const viewportWidth = active?.viewport.w,
    viewportHeight = active?.viewport.h;
  useEffect(() => {
    const el = area.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setBounds({ w: entry.contentRect.width, h: entry.contentRect.height });
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!activeId || mode !== 'fit' || dialog || status?.state !== 'up') return;
    const w = Math.max(320, Math.floor(bounds.w)),
      h = Math.max(240, Math.floor(bounds.h));
    // Avoid clearing device metrics again after dialogs or metadata-only updates.
    if (viewportWidth === w && viewportHeight === h) return;
    const timer = setTimeout(
      () =>
        send({
          t: 'viewport',
          mode: 'fit',
          w,
          h,
          dpr: 1,
        }),
      300,
    );
    return () => clearTimeout(timer);
  }, [bounds, mode, activeId, dialog, viewportWidth, viewportHeight, status?.state, send]);
  useEffect(() => {
    if (focusNew.current && active?.url === 'about:blank') {
      focusNew.current = false;
      address.current?.focus();
      address.current?.select();
    }
  }, [active?.id, active?.url]);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const newTab = () => {
    focusNew.current = true;
    setDraft('');
    send({ t: 'tab.new', url: 'about:blank' });
  };
  const viewport = (v: IViewport) => {
    send({ t: 'viewport', ...v });
    setSizeOpen(false);
  };
  const dismiss = () => {
    setMenu(null);
    send({ t: 'select.dismiss' });
  };
  const point = (event: { clientX: number; clientY: number }) =>
    canvas.current &&
    pagePoint(canvas.current.getBoundingClientRect(), page.current, { x: event.clientX, y: event.clientY });
  const mouse = (type: string, event: PointerEvent<HTMLCanvasElement>) => {
    const p = point(event);
    if (!p) return;
    send({
      t: 'mouse',
      type,
      ...p,
      button: ['left', 'middle', 'right'][event.button] ?? 'none',
      buttons: event.buttons,
      clickCount: 1,
      alt: event.altKey,
      ctrl: event.ctrlKey,
      meta: event.metaKey,
      shift: event.shiftKey,
    });
  };
  const tabStrip = (items: IBrowserTab[]) => (
    <div className="hx-agent-browser__tabs" role="tablist" aria-label={label('tabs')}>
      {items.map((tab) => (
        <div
          className="hx-agent-browser__tab"
          key={tab.id}
          data-active={tab.id === tabs.current}
          onAuxClick={(event) => {
            if (event.button === 1) {
              event.preventDefault();
              send({ t: 'tab.close', id: tab.id });
            }
          }}
        >
          <button
            type="button"
            role="tab"
            aria-selected={tab.id === tabs.current}
            title={tab.url}
            onClick={() => {
              dismiss();
              setDraft(null);
              send({ t: 'tab.select', id: tab.id });
            }}
          >
            <span title={label(tab.by)}>
              <Icon name={tab.by === 'agent' ? 'sparkles' : 'user'} size="sm" />
            </span>
            <span>{tab.title || tab.url || label('newTab')}</span>
          </button>
          <button
            type="button"
            aria-label={`${label('closeTab')}: ${tab.title || tab.url}`}
            onClick={() => send({ t: 'tab.close', id: tab.id })}
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
  const scale = Math.min(bounds.w / imageSize.w, bounds.h / imageSize.h, 1);
  const screen = { width: Math.max(1, imageSize.w * scale), height: Math.max(1, imageSize.h * scale) };
  const menuWidth = Math.min(bounds.w, Math.max(160, (menu?.w ?? 0) * scale));
  const menuTop = Math.max(
    0,
    Math.min(bounds.h - 120, (bounds.h - screen.height) / 2 + ((menu?.y ?? 0) + (menu?.h ?? 0)) * scale),
  );
  const firstOption = menu?.options.findIndex((option, index) => !option.disabled && index === menu.selected);
  const focusOption =
    firstOption !== undefined && firstOption >= 0
      ? firstOption
      : menu?.options.findIndex((option) => !option.disabled);
  const others = tabs.tabs.filter((tab) => tab.owner === 'other');
  return (
    <div className="hx-tv" data-tool="agent-browser">
      <div className="hx-tbrowser hx-agent-browser">
        <div className="hx-tbrowser__bar">
          <span className="hx-agent-browser__profile" title={status?.profile.name}>
            {status?.profile.name}
          </span>
          {(['back', 'forward', 'reload'] as const).map((action, index) => (
            <button
              key={action}
              type="button"
              className="hx-head-btn"
              aria-label={t(`next.site.${action}`)}
              disabled={!active}
              onClick={() => send({ t: 'nav', action })}
            >
              <Icon name={['arrow-left', 'arrow-right', 'rotate-cw'][index]!} size="sm" />
            </button>
          ))}
          <input
            ref={address}
            className="hx-tbrowser__url"
            aria-label={t('next.pane.addressLabel')}
            placeholder={t('next.pane.address')}
            value={draft ?? active?.url ?? ''}
            disabled={!active}
            spellCheck={false}
            autoComplete="off"
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') setDraft(null);
              if (event.key === 'Enter' && (draft ?? active?.url)?.trim()) {
                send({ t: 'nav', action: 'go', url: draft ?? active?.url });
                setDraft(null);
                canvas.current?.focus();
              }
            }}
          />
          <button
            type="button"
            className="hx-head-btn hx-agent-browser__size-button"
            disabled={!active}
            aria-label={label('size')}
            aria-expanded={sizeOpen}
            onClick={() => setSizeOpen(!sizeOpen)}
          >
            {mode === 'fit' ? label('fit') : `${active?.viewport.w ?? 0}×${active?.viewport.h ?? 0}`} ▾
          </button>
        </div>
        <div className="hx-agent-browser__tabrow">
          {tabStrip(tabs.tabs.filter((tab) => tab.owner === 'this'))}
          <button
            type="button"
            className="hx-head-btn"
            aria-label={label('newTab')}
            disabled={status?.state !== 'up'}
            onClick={newTab}
          >
            +
          </button>
        </div>
        {others.length > 0 && (
          <details className="hx-agent-browser__others">
            <summary>
              {label('otherChats')} ({others.length})
            </summary>
            {tabStrip(others)}
          </details>
        )}
        {sizeOpen && (
          <div className="hx-agent-browser__sizes" role="dialog" aria-label={label('size')}>
            <button
              type="button"
              onClick={() => {
                setCustom(false);
                viewport({
                  mode: 'fit',
                  w: Math.max(320, Math.floor(bounds.w)),
                  h: Math.max(240, Math.floor(bounds.h)),
                  dpr: 1,
                });
              }}
            >
              {label('fit')}
            </button>
            {presets.map((v, index) => (
              <button
                type="button"
                key={index}
                onClick={() => {
                  setCustom(false);
                  viewport(v);
                }}
              >
                {index === 3 ? `${label('phone')} · ` : index === 4 ? `${label('tablet')} · ` : ''}
                {v.w}×{v.h}
              </button>
            ))}
            <form
              onSubmit={(event) => {
                event.preventDefault();
                setCustom(true);
                viewport({ mode: 'fixed', w: width, h: height, dpr: 1 });
              }}
            >
              <label>
                {label('custom')}
                <input
                  aria-label={label('width')}
                  type="number"
                  min={320}
                  max={7680}
                  value={width}
                  onChange={(event) => setWidth(+event.target.value)}
                />
                ×
                <input
                  aria-label={label('height')}
                  type="number"
                  min={240}
                  max={4320}
                  value={height}
                  onChange={(event) => setHeight(+event.target.value)}
                />
              </label>
              <button type="submit">{label('apply')}</button>
            </form>
          </div>
        )}
        <div className="hx-agent-browser__view" ref={area}>
          <div className="hx-agent-browser__image" style={screen}>
            <canvas
              ref={canvas}
              data-page-width={imageSize.w}
              data-page-height={imageSize.h}
              style={screen}
              data-shown={framed && !active?.crashed && status?.state === 'up'}
              tabIndex={0}
              aria-label={label('screen')}
              onPointerDown={(event) => {
                event.preventDefault();
                event.currentTarget.focus();
                event.currentTarget.setPointerCapture(event.pointerId);
                if (event.pointerType === 'touch') {
                  touch.current = {
                    x: event.clientX,
                    y: event.clientY,
                    startX: event.clientX,
                    startY: event.clientY,
                    dragged: false,
                  };
                  return;
                }
                mouse('mousePressed', event);
              }}
              onPointerMove={(event) => {
                const p = point(event),
                  start = touch.current;
                if (start && p) {
                  if (Math.hypot(event.clientX - start.startX, event.clientY - start.startY) > 6)
                    start.dragged = true;
                  if (start.dragged)
                    send({
                      t: 'wheel',
                      ...p,
                      dx: (start.x - event.clientX) / scale,
                      dy: (start.y - event.clientY) / scale,
                    });
                  start.x = event.clientX;
                  start.y = event.clientY;
                  return;
                }
                if (performance.now() - moveAt.current < 50) return;
                moveAt.current = performance.now();
                mouse('mouseMoved', event);
              }}
              onPointerUp={(event) => {
                if (event.pointerType === 'touch') {
                  if (touch.current && !touch.current.dragged) {
                    mouse('mousePressed', event);
                    mouse('mouseReleased', event);
                  }
                  touch.current = null;
                } else mouse('mouseReleased', event);
              }}
              onPointerCancel={() => {
                touch.current = null;
              }}
              onContextMenu={(event) => event.preventDefault()}
              onWheel={(event) => {
                const p = point(event);
                if (p) send({ t: 'wheel', ...p, dx: event.deltaX, dy: event.deltaY });
              }}
              onKeyDown={(event) => {
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
                event.preventDefault();
                send({ t: 'text', text: event.clipboardData.getData('text') });
              }}
            />
            {custom && active && mode === 'fixed' && (
              <button
                type="button"
                className="hx-agent-browser__resize"
                aria-label={label('resize')}
                onPointerDown={(event) => {
                  event.currentTarget.setPointerCapture(event.pointerId);
                  resize.current = {
                    x: event.clientX,
                    y: event.clientY,
                    w: active.viewport.w,
                    h: active.viewport.h,
                  };
                }}
                onPointerUp={(event) => {
                  const start = resize.current;
                  resize.current = null;
                  if (!start) return;
                  viewport({
                    mode: 'fixed',
                    w: Math.min(7680, Math.max(320, Math.round(start.w + (event.clientX - start.x) / scale))),
                    h: Math.min(4320, Math.max(240, Math.round(start.h + (event.clientY - start.y) / scale))),
                    dpr: 1,
                  });
                }}
              >
                ↘
              </button>
            )}
          </div>
          {(!active || active.crashed || status?.state !== 'up' || !framed) && (
            <div className="hx-agent-browser__note" role="status">
              <strong>
                {active?.crashed
                  ? label('crashed')
                  : status?.state === 'up'
                    ? !active
                      ? label('empty')
                      : label('waiting')
                    : label(status?.state ?? 'connecting')}
              </strong>
              {status?.error && <p>{status.error}</p>}
              {status?.hint && <p>{status.hint}</p>}
              {status?.state === 'preparing' && <progress max={100} value={status.progress} />}
              {status?.state === 'up' && !active && (
                <button type="button" onClick={newTab}>
                  {label('newTab')}
                </button>
              )}
              {active?.crashed && (
                <button type="button" onClick={() => send({ t: 'nav', action: 'reload' })}>
                  {t('next.site.reload')}
                </button>
              )}
            </div>
          )}
          {menu && (
            <div
              className="hx-agent-browser__overlay"
              onPointerDown={(event) => {
                if (event.target === event.currentTarget) dismiss();
              }}
              onKeyDown={(event) => {
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                  event.preventDefault();
                  const options = Array.from(
                    event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'),
                  );
                  const at = options.indexOf(document.activeElement as HTMLButtonElement);
                  const delta = event.key === 'ArrowDown' ? 1 : -1;
                  options[(at + delta + options.length) % options.length]?.focus();
                }
                if (event.key === 'Escape') {
                  event.preventDefault();
                  event.stopPropagation();
                  dismiss();
                  canvas.current?.focus();
                }
              }}
            >
              <div
                className="hx-agent-browser__select"
                role="listbox"
                aria-label={label('select')}
                style={{
                  left: Math.max(
                    0,
                    Math.min(bounds.w - menuWidth, (bounds.w - screen.width) / 2 + menu.x * scale),
                  ),
                  top: menuTop,
                  minWidth: menuWidth,
                  maxHeight: Math.min(240, bounds.h - menuTop - 4),
                }}
              >
                {menu.options.map((option, index) => (
                  <button
                    type="button"
                    role="option"
                    aria-selected={index === menu.selected}
                    autoFocus={index === focusOption}
                    disabled={option.disabled}
                    key={index}
                    onClick={() => {
                      send({ t: 'select.choose', index });
                      setMenu(null);
                      canvas.current?.focus();
                    }}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            </div>
          )}
          {active?.dialog && (
            <div className="hx-agent-browser__overlay hx-agent-browser__modal">
              <form
                role="dialog"
                aria-modal="true"
                aria-label={label('dialog')}
                key={`${active.id}:${active.dialog.message}`}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') {
                    event.preventDefault();
                    event.stopPropagation();
                    if (active.dialog?.type !== 'alert') send({ t: 'dialog', accept: false });
                  }
                  if (event.key === 'Tab') {
                    const fields = Array.from(
                      event.currentTarget.querySelectorAll<HTMLElement>('input, button'),
                    );
                    const at = fields.indexOf(document.activeElement as HTMLElement);
                    event.preventDefault();
                    fields[(at + (event.shiftKey ? -1 : 1) + fields.length) % fields.length]?.focus();
                  }
                }}
                onSubmit={(event) => {
                  event.preventDefault();
                  send({
                    t: 'dialog',
                    accept: true,
                    text: String(new FormData(event.currentTarget).get('answer') ?? ''),
                  });
                }}
              >
                <strong>{label('dialog')}</strong>
                <p>{active.dialog.message}</p>
                {active.dialog.type === 'prompt' && (
                  <input
                    aria-label={label('answer')}
                    defaultValue={active.dialog.defaultPrompt}
                    name="answer"
                    autoFocus
                  />
                )}
                <div>
                  {active.dialog.type !== 'alert' && (
                    <button type="button" onClick={() => send({ t: 'dialog', accept: false })}>
                      {label('cancel')}
                    </button>
                  )}
                  <button type="submit" autoFocus={active.dialog.type !== 'prompt'}>
                    {label('ok')}
                  </button>
                </div>
              </form>
            </div>
          )}
          {notice && (
            <div className="hx-agent-browser__toast" role="alert">
              {notice}
            </div>
          )}
        </div>
        <div className="hx-agent-browser__status">
          <span>
            {active?.control === 'person'
              ? label('personControl')
              : active && now - active.agentAt < 5000
                ? label('working')
                : label('agentControl')}
          </span>
          {!tabs.following && (
            <button
              type="button"
              onClick={() => {
                setDraft(null);
                send({ t: 'follow' });
              }}
            >
              {label('follow')}
            </button>
          )}
          {active && (
            <button
              type="button"
              onClick={() => send({ t: 'control', holder: active.control === 'person' ? 'agent' : 'person' })}
            >
              {label(active.control === 'person' ? 'release' : 'take')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
