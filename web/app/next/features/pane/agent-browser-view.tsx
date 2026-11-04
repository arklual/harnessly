import { profileName } from '@/next/features/browser/profile-fields';
import { ProfilePicker } from '@/next/features/browser/profile-picker';
import type { MachineId } from '@/next/protocol/v2';
import { useEffect, useRef, useState, type PointerEvent } from 'react';
import { nextClick, pointerButton } from './agent-browser-input';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { pagePoint, type IBrowserTab, type IViewport } from './agent-browser-frame';
import { useBrowserSocket } from './agent-browser-socket';
import { LoginBanner } from '@/next/features/browser/login-banner';
import { useChatPaneView } from '@/next/stores/pane-state';

const presets: IViewport[] = [
  { mode: 'fixed', w: 1280, h: 800, dpr: 1 },
  { mode: 'fixed', w: 1440, h: 900, dpr: 1 },
  { mode: 'fixed', w: 1920, h: 1080, dpr: 1 },
  { mode: 'mobile', w: 390, h: 844, dpr: 2 },
  { mode: 'mobile', w: 820, h: 1180, dpr: 2 },
];
const label = (key: string) => t(`next.agentBrowser.${key}`);

/** A conversation's managed browser, with person input independent of the agent's working tab. */
export default function AgentBrowserView({
  url,
  machine,
  conv,
}: {
  url: string;
  machine?: MachineId;
  conv?: string;
}) {
  const { pause, resume, canvas, page, status, tabs, menu, setMenu, notice, framed, imageSize, send } =
    useBrowserSocket(url);
  const active = tabs.tabs.find((tab) => tab.id === tabs.current);
  const [loginTab, setLoginTab] = useChatPaneView<string | null>(conv ?? null, 'browser-login-tab', null);
  useEffect(() => {
    if (!loginTab || !tabs.tabs.some((tab) => tab.id === loginTab)) return;
    send({ t: 'tab.select', id: loginTab });
    setLoginTab(null);
  }, [loginTab, tabs.tabs, send, setLoginTab]);
  const area = useRef<HTMLDivElement>(null),
    address = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const [sizeOpen, setSizeOpen] = useState(false);
  const keyboard = useRef<HTMLTextAreaElement>(null);
  const clicks = useRef<ReturnType<typeof nextClick> | null>(null);
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
  const mode = active?.viewport.mode || (active?.by === 'agent' ? 'fixed' : 'fit');
  const own = active?.owner === 'this';
  const activeId = active?.id;
  useEffect(() => {
    clicks.current = null;
  }, [activeId]);
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
    if (!own || !activeId || mode !== 'fit' || dialog || status?.state !== 'up') return;
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
  }, [bounds, mode, own, activeId, dialog, viewportWidth, viewportHeight, status?.state, send]);
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
  useEffect(() => {
    const el = keyboard.current;
    if (!el) return;
    const before = (event: InputEvent) => {
      const key =
        event.inputType === 'deleteContentBackward'
          ? 'Backspace'
          : /insertLineBreak|insertParagraph/.test(event.inputType)
            ? 'Enter'
            : '';
      if (key) {
        event.preventDefault();
        el.value = '';
        send({ t: 'key', key, code: key, keyCode: key === 'Backspace' ? 8 : 13 });
      }
    };
    el.addEventListener('beforeinput', before);
    return () => el.removeEventListener('beforeinput', before);
  }, [send]);
  useEffect(() => {
    if (!sizeOpen) return;
    const close = (event: globalThis.PointerEvent) => {
      if (!(event.target as Element).closest('.hx-agent-browser__sizes, .hx-agent-browser__size-button'))
        setSizeOpen(false);
    };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, [sizeOpen]);
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
    if (type === 'mousePressed') clicks.current = nextClick(clicks.current, event, performance.now());
    send({
      t: 'mouse',
      type,
      ...p,
      button: pointerButton(event.button, event.buttons),
      buttons: type === 'mousePressed' ? event.buttons || 1 : event.buttons,
      clickCount: type === 'mouseMoved' ? 0 : (clicks.current?.count ?? 1),
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
  const menuTop = Math.max(0, Math.min(bounds.h - 120, ((menu?.y ?? 0) + (menu?.h ?? 0)) * scale));
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
          {machine && conv ? (
            <ProfilePicker
              key={`${machine}:${conv}`}
              machine={machine}
              conv={conv}
              currentId={status?.profile.id}
              onPause={pause}
              onResume={() => {
                setDraft(null);
                resume();
              }}
            />
          ) : (
            <span
              className="hx-agent-browser__profile"
              title={status ? profileName(status.profile) : undefined}
            >
              {status ? profileName(status.profile) : null}
            </span>
          )}
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
          <div
            className="hx-cmenu hx-agent-browser__sizes"
            role="dialog"
            aria-label={label('size')}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                event.stopPropagation();
                setSizeOpen(false);
              }
            }}
          >
            <button
              type="button"
              className="hx-cmenu__item"
              aria-current={mode === 'fit' ? 'page' : undefined}
              onClick={() => {
                viewport({
                  mode: 'fit',
                  w: Math.max(320, Math.floor(bounds.w)),
                  h: Math.max(240, Math.floor(bounds.h)),
                  dpr: 1,
                });
              }}
            >
              <span className="hx-cmenu__txt">{label('fit')}</span>
              <Icon name="check" size="sm" className="hx-cmenu__check" />
            </button>
            {presets.map((v, index) => (
              <button
                type="button"
                key={index}
                className="hx-cmenu__item"
                aria-current={
                  mode === v.mode && active?.viewport.w === v.w && active?.viewport.h === v.h
                    ? 'page'
                    : undefined
                }
                onClick={() => {
                  viewport(v);
                }}
              >
                <span className="hx-cmenu__txt">
                  {index === 3 ? `${label('phone')} · ` : index === 4 ? `${label('tablet')} · ` : ''}
                  {v.w}×{v.h}
                </span>
                <Icon name="check" size="sm" className="hx-cmenu__check" />
              </button>
            ))}
            <form
              onSubmit={(event) => {
                event.preventDefault();
                viewport({ mode: 'fixed', w: width, h: height, dpr: 1 });
              }}
            >
              <label>
                <span>{label('custom')}</span>
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
                if (!event.buttons && performance.now() - moveAt.current < 50) return;
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
                  t:
                    (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'c'
                      ? 'clipboard.read'
                      : 'key',
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
            {active && mode === 'fixed' && (
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
              {status?.state === 'preparing' && status.hint && <p>{status.hint}</p>}
              {status?.state === 'error' && (
                <>
                  <button type="button" onClick={() => send({ t: 'browser.retry' })}>
                    {label('retry')}
                  </button>
                  {status.command && (
                    <div className="hx-agent-browser__command">
                      <pre>
                        <code>{status.command}</code>
                      </pre>
                      <button
                        type="button"
                        onClick={() => {
                          void navigator.clipboard
                            .writeText(status.command!)
                            .catch((error: unknown) =>
                              console.warn('Could not copy browser setup command:', error),
                            );
                        }}
                      >
                        {label('copy')}
                      </button>
                    </div>
                  )}
                </>
              )}
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
              {label(notice.key)}
            </div>
          )}
        </div>
        <textarea
          ref={keyboard}
          className="hx-agent-browser__keyboard-input"
          aria-label={label('keyboardInput')}
          tabIndex={-1}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          rows={1}
          onInput={(event) => {
            const text = event.currentTarget.value;
            event.currentTarget.value = '';
            if (text && !(event.nativeEvent as InputEvent).isComposing) send({ t: 'text', text });
          }}
          onCompositionEnd={(event) => {
            event.currentTarget.value = '';
            if (event.data) send({ t: 'text', text: event.data });
          }}
          onKeyDown={(event) => {
            if (event.key === 'Backspace' || event.key === 'Enter') {
              event.preventDefault();
              event.stopPropagation();
              event.currentTarget.value = '';
              send({
                t: 'key',
                key: event.key,
                code: event.code,
                keyCode: event.key === 'Backspace' ? 8 : 13,
              });
            }
          }}
          onBlur={(event) => {
            event.currentTarget.value = '';
          }}
        />
        <div className="hx-agent-browser__status">
          {active?.login && machine && <LoginBanner machine={machine} ask={active.login} />}
          {active?.control === 'person' && active.scripted && (
            <p className="hx-agent-browser__scripted-warning" role="alert">
              {label('scriptedWarning')}
            </p>
          )}
          <button
            type="button"
            className="hx-agent-browser__keyboard"
            disabled={!active}
            onClick={() => keyboard.current?.focus({ preventScroll: true })}
          >
            {label('keyboard')}
          </button>
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
