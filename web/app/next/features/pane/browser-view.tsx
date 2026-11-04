// A browser tab of the pane (.hx-tbrowser): back, forward, reload, the omnibox; with no address yet, the pinned pages and
// the servers that listen on the machine, the chat's own first. The page is the address opened in a preview slot.
import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import {
  DeviceMenu,
  MoreMenu,
  PickCard,
  VaultMenu,
  type IVaultView,
} from '@/next/features/pane/browser-tools';
import { AgentAsk, AgentBar, AgentCursor, type IAgentCursorAt } from '@/next/features/pane/agent-overlay';
import SiteDevtools from '@/next/features/designly/site-devtools';
import type { TDevTab } from '@/next/features/designly/devtools-data';
import type { IDomBridge } from '@/next/features/designly/use-dom-bridge';
import type { MachineId } from '@/next/protocol/v2';
import { usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/site-tools.css';
import { screenFit, screenSize, type IBrowserScreen } from '@/next/features/pane/browser-device';
import type { IFramePicked } from '@/next/features/pane/frame-bridge';
import { openOutside } from '@/next/features/chat/pane-sites';
import Icon from '@/next/ui/icon';
import Omnibox from '@/next/features/pane/omnibox';
import { parseAddress, type IAddress } from '@/next/features/pane/address';
import { isPinned, pinLabel, type IPin } from '@/next/features/pane/pins';
import type { IListeningPort } from '@/next/core/net/preview';
import { t } from '@/next/i18n';
import { SIGN_IN, type TSignIn } from '@/next/features/pane/stand-sign-in';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';

export interface IBrowserViewProps {
  /** What the bar shows; empty for a new tab. */
  address: string;
  /** What the page is called, when it was chosen by name. */
  title: string | null;
  canBack: boolean;
  canForward: boolean;
  /** The slot's entry for the page; null while it is asked for. */
  src: string | null;
  error: string | null;
  /** The machine's servers; null while they load. */
  ports: IListeningPort[] | null;
  portsError: string | null;
  /** Asks for the servers again. */
  onRetryPorts: () => void;
  /** The omnibox was entered: the servers may have changed. */
  onWantPorts: () => void;
  /** The person's pinned pages; null until the person is known. */
  pins: readonly IPin[] | null;
  /** The pages visited lately, the latest first. */
  visits: readonly string[];
  onPin: (pin: IPin) => void;
  onUnpin: (text: string) => void;
  reloadKey: number;
  /** The iframe: the tab talks to the page in it (the stand's wall asks for «Войти»). */
  onFrameLoad: (frame: HTMLIFrameElement) => void;
  /** The stand wants a sign-in (asked), or the person is signing in elsewhere (waiting). */
  signIn: TSignIn;
  onSignIn: () => void;
  onSignInRefresh: () => void;
  /** The app's theme: the page inside is told it (prefers-color-scheme). */
  theme: 'light' | 'dark';
  onGo: (address: IAddress) => void;
  onHistory: (step: -1 | 1) => void;
  onReload: () => void;
  onNewTab: () => void;
  /** Picking an element on the page is on; null when the page has no bridge to pick with (it has not loaded). */
  picking: boolean | null;
  onPick: () => void;
  /** The element the person picked, whose card asks for a comment; null for none. */
  picked: IFramePicked | null;
  onPickSend: (comment: string) => void;
  onPickCancel: () => void;
  /** What the page is looked at on. */
  screen: IBrowserScreen;
  onScreen: (screen: IBrowserScreen) => void;
  /** The page's own address, which the person's browser can open; null for a server of the machine. */
  outsideUrl: string | null;
  /** The chat's agent acting on the page; null where no agent can. */
  agent: IBrowserAgentView | null;
  /** DevTools of the page under it; null where there are none. */
  devtools: IBrowserDevtools | null;
  /** The safe's entries for the page's site; null where the safe has none for it. */
  vault: IVaultView | null;
}

export interface IBrowserDevtools {
  machine: MachineId;
  /** The page's slot (its origin); null while it opens. */
  slot: string | null;
  open: boolean;
  onToggle: () => void;
  tab: TDevTab;
  onTab: (tab: TDevTab) => void;
  dom: IDomBridge;
  inspect: { on: boolean; disabled: boolean; onToggle: () => void };
  onSend: (words: string) => void;
}

/** The panel's height, dragged by its top edge (the design's DevTools keep theirs the same way). */
const DEV_H = { min: 120, max: 520, start: 240 } as const;
const THROTTLE_OUT_MS = 160;

export interface IBrowserAgentView {
  cursor: IAgentCursorAt | null;
  /** The site the agent asks to act on. */
  asking: string | null;
  working: boolean;
  paused: boolean;
  onAllow: () => void;
  onDeny: () => void;
  onStop: () => void;
  onResume: () => void;
}

const translate = (key: string) => t(key);

const BrowserView = (props: IBrowserViewProps) => {
  const { address, canBack, canForward, src, error, ports, portsError, onRetryPorts, reloadKey, pins } =
    props;
  const { visits } = props;
  const sources = useMemo(() => ({ ports, pins, visits, translate }), [ports, pins, visits]);
  const pinnable = pins !== null && address !== '' && !address.startsWith('?');
  const pinned = pins !== null && isPinned(pins, address);
  const stage = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const [devH, setDevH] = useState<number>(DEV_H.start);
  const [throttleOpen, setThrottleOpen] = useState(false);
  const throttle = usePresentFlag(throttleOpen, THROTTLE_OUT_MS);
  useEffect(() => {
    root.current?.style.setProperty('--dev-h', `${devH}px`);
  }, [devH]);
  const dragDev = (event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    const from = event.clientY;
    const was = devH;
    let now = was;
    const move = (e: PointerEvent) => {
      now = Math.max(DEV_H.min, Math.min(DEV_H.max, was + from - e.clientY));
      root.current?.style.setProperty('--dev-h', `${now}px`);
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      setDevH(now);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };
  const frame = useRef<HTMLIFrameElement | null>(null);
  const size = screenSize(props.screen);
  const vw = size?.w ?? 0;
  const vh = size?.h ?? 0;
  const [fit, setFit] = useState(1);

  // A device's screen is scaled to show whole in the pane: its size and the scale are the stage's CSS variables.
  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    if (vw === 0) {
      el.style.removeProperty('--vw');
      el.style.removeProperty('--vh');
      el.style.removeProperty('--fit');
      return;
    }
    const measure = () => {
      const next = screenFit({ w: vw, h: vh }, { w: el.clientWidth, h: el.clientHeight });
      el.style.setProperty('--vw', `${vw}px`);
      el.style.setProperty('--vh', `${vh}px`);
      el.style.setProperty('--fit', String(next));
      setFit(next);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [vw, vh, src]);

  return (
    <div className="hx-tv" data-tool="browser">
      <div ref={root} className="hx-tbrowser" data-dev={props.devtools?.open || undefined}>
        <div className="hx-tbrowser__bar">
          <button
            type="button"
            className="hx-head-btn"
            data-on={canBack}
            aria-label={t('next.site.back')}
            onClick={() => props.onHistory(-1)}
          >
            <Icon name="arrow-left" size="sm" />
          </button>
          <button
            type="button"
            className="hx-head-btn hx-tbrowser__wide"
            data-on={canForward}
            aria-label={t('next.site.forward')}
            onClick={() => props.onHistory(1)}
          >
            <Icon name="arrow-right" size="sm" />
          </button>
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.site.reload')}
            onClick={props.onReload}
          >
            <Icon name="rotate-cw" size="sm" />
          </button>
          <Omnibox address={address} sources={sources} onGo={props.onGo} onEnter={props.onWantPorts} />
          {address !== '' && (
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              data-active={props.picking === true || undefined}
              aria-pressed={props.picking === true}
              aria-disabled={props.picking === null || undefined}
              aria-label={t('next.pane.pick')}
              onClick={() => props.picking !== null && props.onPick()}
            >
              <Icon name="square-mouse-pointer" size="sm" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.pane.pick')}
              </span>
            </button>
          )}
          {address !== '' && props.vault !== null && props.vault.entries.length > 0 && (
            <VaultMenu {...props.vault} />
          )}
          {address !== '' && (
            <span className="hx-tbrowser__wide">
              <DeviceMenu screen={props.screen} onScreen={props.onScreen} />
            </span>
          )}
          {address !== '' && props.devtools && (
            <button
              type="button"
              className="hx-head-btn hx-tipped hx-tbrowser__wide"
              data-active={props.devtools.open || undefined}
              aria-pressed={props.devtools.open}
              aria-label={t('next.site.devtools')}
              onClick={props.devtools.onToggle}
            >
              <Icon name="panel-bottom" size="sm" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.site.devtools')}
              </span>
            </button>
          )}
          {props.outsideUrl !== null && (
            <button
              type="button"
              className="hx-head-btn hx-tipped hx-tbrowser__wide"
              aria-label={t('next.link.openOutside')}
              onClick={() => props.outsideUrl !== null && openOutside(props.outsideUrl)}
            >
              <Icon name="external-link" size="sm" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.link.openOutside')}
              </span>
            </button>
          )}
          {pinnable && (
            <button
              type="button"
              className="hx-head-btn hx-tipped hx-tbrowser__wide"
              data-pinned={pinned}
              aria-pressed={pinned}
              aria-label={pinned ? t('next.pane.unpin') : t('next.pane.pin')}
              onClick={() =>
                pinned
                  ? props.onUnpin(address)
                  : props.onPin({ text: address, name: props.title ?? undefined })
              }
            >
              <Icon name={pinned ? 'pin-off' : 'pin'} size="sm" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {pinned ? t('next.pane.unpin') : t('next.pane.pin')}
              </span>
            </button>
          )}
          <button
            type="button"
            className="hx-head-btn hx-tipped hx-tbrowser__wide"
            aria-label={t('next.pane.newBrowserTab')}
            onClick={props.onNewTab}
          >
            <Icon name="plus" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.pane.newBrowserTab')}
            </span>
          </button>
          <MoreMenu
            canForward={canForward}
            onForward={() => props.onHistory(1)}
            screen={props.screen}
            onScreen={props.onScreen}
            outsideUrl={props.outsideUrl}
            onOutside={() => props.outsideUrl !== null && openOutside(props.outsideUrl)}
            pin={
              pinnable
                ? {
                    pinned,
                    onToggle: () =>
                      pinned
                        ? props.onUnpin(address)
                        : props.onPin({ text: address, name: props.title ?? undefined }),
                  }
                : null
            }
            onNewTab={props.onNewTab}
            devtools={
              address !== '' && props.devtools
                ? { open: props.devtools.open, onToggle: props.devtools.onToggle }
                : null
            }
          />
        </div>
        {props.agent && (
          <>
            <AgentAsk host={props.agent.asking} onAllow={props.agent.onAllow} onDeny={props.agent.onDeny} />
            <AgentBar
              working={props.agent.working}
              paused={props.agent.paused}
              onStop={props.agent.onStop}
              onResume={props.agent.onResume}
            />
          </>
        )}
        {props.signIn !== SIGN_IN.none && (
          <div className="hx-tbrowser__offer" role="group" aria-label={t('next.pane.signInLabel')}>
            <span className="hx-tbrowser__offer-text">
              {props.signIn === SIGN_IN.asked ? t('next.pane.signInAsked') : t('next.pane.signInWaiting')}
            </span>
            {props.signIn === SIGN_IN.asked ? (
              <button type="button" className="hx-tbrowser__offer-btn" data-primary onClick={props.onSignIn}>
                {t('next.pane.signIn')}
              </button>
            ) : (
              <button type="button" className="hx-tbrowser__offer-btn" onClick={props.onSignInRefresh}>
                {t('next.pane.signInRefresh')}
              </button>
            )}
          </div>
        )}
        {address === '' && (
          <div className="hx-tbrowser__home">
            {pins !== null && pins.length > 0 && (
              <>
                <div className="hx-tv__label">{t('next.pane.pinned')}</div>
                <div className="hx-srv">
                  {pins.map((pin, i) => {
                    const next = parseAddress(pin.text);
                    const label = pinLabel(pin, translate);
                    return (
                      <div key={pin.text} className="hx-srv__row" data-i={Math.min(i, 8)}>
                        <button
                          type="button"
                          className="hx-srv__card"
                          onClick={() => next && props.onGo({ ...next, title: label })}
                        >
                          <span className="hx-srv__ico">
                            <Icon name="pin" size="sm" />
                          </span>
                          <span className="hx-srv__main">
                            <span className="hx-srv__name">{label}</span>
                            {label !== pin.text && <span className="hx-srv__where">{pin.text}</span>}
                          </span>
                        </button>
                        <button
                          type="button"
                          className="hx-srv__off"
                          aria-label={t('next.pane.unpin')}
                          onClick={() => props.onUnpin(pin.text)}
                        >
                          <Icon name="x" size="sm" />
                        </button>
                      </div>
                    );
                  })}
                </div>
              </>
            )}
            <div className="hx-tv__label">{t('next.pane.servers')}</div>
            {ports === null && portsError === null && (
              <div className="hx-tbrowser__note">{t('next.pane.serversLoading')}</div>
            )}
            {portsError !== null && (
              <LoadFailed
                size={LOAD_FAILED_SIZE.inline}
                title={t('next.load.ports')}
                reason={portsError}
                onRetry={onRetryPorts}
              />
            )}
            {ports?.length === 0 && <div className="hx-tbrowser__note">{t('next.pane.noServers')}</div>}
            {ports && ports.length > 0 && (
              <div className="hx-srv">
                {ports.map((p, i) => {
                  const next = parseAddress(String(p.port));
                  const name = p.title ?? p.command ?? `:${p.port}`;
                  const where = [`localhost:${p.port}`, p.title ? p.command : null]
                    .filter(Boolean)
                    .join(' · ');
                  return (
                    <button
                      key={p.port}
                      type="button"
                      className="hx-srv__card"
                      data-i={Math.min(i, 8)}
                      onClick={() => next && props.onGo({ ...next, title: name })}
                    >
                      <span className="hx-srv__ico">
                        {p.icon ? (
                          <img src={p.icon} alt="" width={16} height={16} />
                        ) : (
                          <Icon name="globe" size="sm" />
                        )}
                      </span>
                      <span className="hx-srv__main">
                        <span className="hx-srv__name">{name}</span>
                        {p.description && <span className="hx-srv__about">{p.description}</span>}
                        <span className="hx-srv__where">
                          {where}
                          {p.group === 'chat' && <b>{t('next.pane.thisChat')}</b>}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        )}
        {address !== '' && error !== null && (
          <LoadFailed title={t('next.load.page')} reason={error} onRetry={props.onReload} />
        )}
        {address !== '' && error === null && src === null && <div className="hx-tbrowser__page" />}
        {address !== '' && src !== null && (
          <div ref={stage} className="hx-tbrowser__stage" data-device={props.screen.device}>
            <iframe
              key={reloadKey}
              ref={frame}
              onLoad={(event) => props.onFrameLoad(event.currentTarget)}
              className="hx-tbrowser__frame"
              data-theme={props.theme}
              data-picking={props.picking === true || undefined}
              src={src}
              title={address}
              allow="clipboard-write"
            />
            <PickCard
              picked={props.picked}
              frame={frame}
              stage={stage}
              fit={size === null ? 1 : fit}
              onSend={props.onPickSend}
              onCancel={props.onPickCancel}
            />
            <AgentCursor cursor={props.agent?.cursor ?? null} frame={frame} fit={size === null ? 1 : fit} />
          </div>
        )}
        {address !== '' && props.devtools && (
          <SiteDevtools
            machine={props.devtools.machine}
            slot={props.devtools.slot}
            artifact={null}
            board={address}
            open={props.devtools.open}
            tab={props.devtools.tab}
            onTab={props.devtools.onTab}
            dom={props.devtools.dom}
            inspect={props.devtools.inspect}
            throttle={{
              mounted: throttle.mounted,
              state: throttle.state,
              onToggle: () => setThrottleOpen((on) => !on),
              onClose: () => setThrottleOpen(false),
            }}
            onSend={props.devtools.onSend}
            onClose={props.devtools.onToggle}
            onGrab={dragDev}
          />
        )}
      </div>
    </div>
  );
};

export default BrowserView;
