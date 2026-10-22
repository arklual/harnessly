import { t } from '@/lib/i18n';
import {
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import {
  ArrowLeft,
  ArrowRight,
  Monitor,
  MousePointerClick,
  RefreshCw,
  Smartphone,
  Tablet,
} from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  DISMISS_REASON,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwButton,
  TwMenuButton,
  TwPopover,
  TwTooltip,
  cx,
  type TDismissReason,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { addAnnotation, annotationResolved } from '@/lib/hl-annotations-store';
import { isLibraryPort } from '@/hooks/use-design-library';
import { locateDesignElement } from '@/lib/hl-design-library-client';
import {
  acceptsBridgeEvent,
  parseBridgeMessage,
  pathInTarget,
  postToBridge,
  type IPickedElement,
  type TBridgeMessage,
} from '@/lib/hl-preview-bridge-client';
import { resolveAcrossPorts, targetLabel, urlTarget, type IUrlTarget } from '@/lib/hl-preview-client';
import { onHighlightRequest } from '@/lib/hl-preview-highlight';
import {
  DEVICE_FRAME,
  PREVIEW_BROWSER,
  PREVIEW_BROWSER_LABELS,
  PREVIEW_DEVICES,
  browserFor,
  deviceById,
  deviceLayout,
  fitDevice,
  viewportOrigin,
  type ISize,
  type TPreviewBrowserId,
} from '@/lib/hl-preview-devices';
import { DeviceChrome } from '@/components/agents/inspector/device-chrome';
import {
  frameSrc,
  patchPreview,
  readPreview,
  spendTicket,
  usePreviewState,
  type TPreviewState,
} from '@/lib/hl-preview-store';
import type { TParentMessage } from '@/lib/hl-preview/bridge-core';
import { SIGN_IN_WHERE, beginStandSignIn } from '@/lib/hl-stand-sign-in';
import { standSignInUrl } from '@/lib/hl-stand-url';
import { fetchSlotTarget } from '@/lib/hl-vault-client';
import { onVaultLogin, performVaultLogin, type IVaultFillDeps } from '@/lib/hl-vault-fill';

export type TOpenState = Extract<TPreviewState, { kind: 'open' }>;

const NO_BRIDGE = t('inspector.previewFrame.noBridge');
const COPIED_MS = 1_500;
/** A bridge usually answers the ping after a load well within this; till then «Select» says it is not ready. */
const BRIDGE_WAIT_MS = 1_500;
/** A heavy page (a board pulling a big kit) answers late: the ping is repeated at these delays after a load. */
const BRIDGE_PINGS_MS = [400, 1_000, 2_000, 3_500, 5_000] as const;

/** A stand behind the team's sign-in: the runtime showed its wall (`asked`), and the person is signing in elsewhere (`waiting`). */
const SIGN_IN = { none: 'none', asked: 'asked', waiting: 'waiting' } as const;
type TSignIn = (typeof SIGN_IN)[keyof typeof SIGN_IN];

const DEVICE_ICONS = {
  fluid: Monitor,
  'plain-phone': Smartphone,
  'iphone-14': Smartphone,
  'iphone-15-pro': Smartphone,
  'iphone-16-pro': Smartphone,
  'galaxy-s24': Smartphone,
  ipad: Tablet,
} as const;

/** The stage's content box: a fixed device is fitted into it. */
const useStageSize = (enabled: boolean) => {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<ISize>({ width: 0, height: 0 });
  useLayoutEffect(() => {
    const stage = ref.current;
    if (!enabled || !stage) return;
    const measure = () => {
      const style = getComputedStyle(stage);
      const padX = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
      const padY = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
      setSize({ width: stage.clientWidth - padX, height: stage.clientHeight - padY });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(stage);
    return () => observer.disconnect();
  }, [enabled]);
  return [ref, size] as const;
};

const pickHint = (bridged: boolean, loaded: boolean, whenBridged: string): string => {
  if (bridged) return whenBridged;
  return loaded ? NO_BRIDGE : t('inspector.previewFrame.pageLoading');
};

/** Errors of a board that land in the composer by themselves, per frame. */
const AUTO_ERROR_CHIPS = 3;

const pickedTitle = (picked: IPickedElement): string =>
  picked.component ? `${picked.component.name} · ${picked.tag}` : picked.tag;

interface IPickPopoverProps {
  picked: IPickedElement;
  anchorRef: RefObject<HTMLDivElement | null>;
  onAdd: (comment: string) => void;
  onClose: (reason?: TDismissReason) => void;
}

const PickPopover = ({ picked, anchorRef, onAdd, onClose }: IPickPopoverProps) => {
  const [comment, setComment] = useState('');
  const add = () => onAdd(comment.trim());
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    add();
  };
  return (
    <TwPopover
      open
      onClose={onClose}
      anchorRef={anchorRef}
      title={pickedTitle(picked)}
      className="tw-preview-pick-popover"
    >
      {picked.text && <p className="tw-preview-pick-text">«{picked.text}»</p>}
      <textarea
        className="tw-preview-pick-comment"
        value={comment}
        onChange={(event) => setComment(event.target.value)}
        onKeyDown={onKeyDown}
        placeholder={t('inspector.previewFrame.commentPlaceholder')}
        aria-label={t('inspector.previewFrame.commentLabel')}
        rows={3}
      />
      <div className="tw-preview-pick-actions">
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          onClick={() => onClose()}
        >
          {t('inspector.previewFrame.cancel')}
        </TwButton>
        <TwButton type="button" size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.primary} onClick={add}>
          {t('inspector.previewFrame.add')}
        </TwButton>
      </div>
    </TwPopover>
  );
};

interface IPreviewFrameProps {
  previewKey: string;
  state: TOpenState;
  onEntered: () => void;
  onStale: () => void;
  /** Without the bridge the page cannot reload itself: the frame is opened again. */
  onReload: () => void;
  /** The person confirmed opening the site the page redirected to. */
  onOpenUrl: (target: IUrlTarget) => void;
  /** An address field in place of the copyable path (the browser tab of the right panel). */
  address?: (path: string) => ReactNode;
  /** Buttons after the device frame menu. */
  actions?: ReactNode;
  /** What the toolbar starts with, before the navigation: the centre section puts its back arrow and board switcher here. */
  leading?: ReactNode;
  /** Without the page's own back and forward (a gallery of boards has no history worth stepping through). */
  hideHistory?: boolean;
  /** The tooltip of «Select» while the page's bridge is up. */
  pickTip?: string;
}

/**
 * The src is read once per mount: marking the frame as entered must not reload it. A frame mounted again elsewhere
 * (floating window, right panel, dialog) before the first one loaded asks for a fresh ticket through `onStale`.
 * The toolbar talks to the in-page bridge; a page that refused it (CSP) keeps only reload.
 */
export const PreviewFrame = ({
  previewKey,
  state,
  onEntered,
  onStale,
  onReload,
  onOpenUrl,
  address,
  actions,
  leading,
  hideHistory = false,
  pickTip = t('inspector.previewFrame.pickTip'),
}: IPreviewFrameProps) => {
  const [src] = useState(() => frameSrc(state));
  const [{ device: deviceId, browsers }] = usePreviewState(previewKey);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const pickRef = useRef<HTMLButtonElement>(null);
  const bridgeWait = useRef<number | null>(null);
  const bridgePings = useRef<number[]>([]);
  /** A load happened: a hello from now on is this document's, however late it comes. */
  const afterLoad = useRef(false);
  const stopPings = () => {
    for (const timer of bridgePings.current) window.clearTimeout(timer);
    bridgePings.current = [];
  };
  const bridgeListeners = useRef(new Set<(message: TBridgeMessage) => void>());
  const [loaded, setLoaded] = useState(false);
  const [bridged, setBridged] = useState(false);
  /** The nonce of the document whose bridge said hello last; a fill token is minted for it. */
  const [bridgeNonce, setBridgeNonce] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);
  const [picked, setPicked] = useState<IPickedElement | null>(null);
  const [path, setPath] = useState(state.path);
  const [copied, setCopied] = useState(false);
  const [offer, setOffer] = useState<IUrlTarget | null>(null);
  const [signIn, setSignIn] = useState<TSignIn>(SIGN_IN.none);
  const device = deviceById(deviceId);
  const browser = browserFor(device, browsers[device.id]);
  const layout = deviceLayout(device, browser);
  const [stageRef, stage] = useStageSize(layout !== null);
  const { origin } = state.preview;
  // The address comes from what the tab opened, never from the page: a page of a dev server cannot ask for a sign-in.
  const signInUrl = state.target.kind === 'url' ? standSignInUrl(`${state.target.origin}${path}`) : null;

  const stale = useEffectEvent(onStale);
  useEffect(() => {
    if (src === null) stale();
    else spendTicket(state.load);
  }, [src, state.load]);

  /** The address follows the page, so reopening the frame elsewhere lands where the person is. */
  const navigated = useEffectEvent((url: string) => {
    const next = pathInTarget(url, origin);
    if (next === null) return;
    setPath(next);
    const current = readPreview(previewKey).state;
    if (current.kind === 'open' && current.load === state.load && current.path !== next)
      patchPreview(previewKey, { state: { ...current, path: next } });
  });

  // A board that failed sends its error here: it waits in the composer as a chip, once per error and page,
  // so the person only presses send and the agent gets the message, the stack and the board's file.
  const attachedErrors = useRef(new Set<string>());
  const attachBoardError = (error: { url: string; message: string; stack: string }) => {
    const { target } = state;
    if (target.kind !== 'port' || !isLibraryPort(target.port)) return;
    const boardPath = pathInTarget(error.url, origin) ?? path;
    const key = `${boardPath}\n${error.message}`;
    // A page is foreign code: a few chips at most, however many errors it throws.
    if (attachedErrors.current.has(key) || attachedErrors.current.size >= AUTO_ERROR_CHIPS) return;
    attachedErrors.current.add(key);
    const element = {
      selector: t('inspector.previewFrame.errorChip'),
      tag: t('inspector.previewFrame.errorChip'),
      text: error.message.slice(0, 120),
      outerHTML: `${error.message}\n${error.stack}`.slice(0, 2048),
      component: null,
      framework: 'unknown' as const,
      path: boardPath,
      viewport: { w: 0, h: 0 },
    };
    const added = addAnnotation(previewKey, {
      comment: t('inspector.previewFrame.boardErrorComment'),
      target: { port: target.port },
      element,
    });
    locateDesignElement(state.preview.machineId, element.path, '').then(
      (matches) => annotationResolved(previewKey, added.id, matches),
      () => annotationResolved(previewKey, added.id, null),
    );
  };

  const onMessage = useEffectEvent((event: MessageEvent) => {
    if (!acceptsBridgeEvent(event, frameRef.current?.contentWindow ?? null, origin)) return;
    const message = parseBridgeMessage(event.data);
    if (!message) return;
    for (const listener of bridgeListeners.current) listener(message);
    if (message.type === 'tk:hello') {
      // A hello after a load is the document's on screen, however late: a heavy board answers after the first ping.
      if (afterLoad.current) {
        if (bridgeWait.current !== null) window.clearTimeout(bridgeWait.current);
        bridgeWait.current = null;
        stopPings();
        setBridged(true);
      }
      setBridgeNonce(message.nonce);
      setPicking(false);
      navigated(message.url);
    } else if (message.type === 'tk:nav') navigated(message.url);
    else if (message.type === 'tk:open') setOffer(urlTarget(new URL(message.url)));
    else if (message.type === 'tk:sign-in') setSignIn(signInUrl ? SIGN_IN.asked : SIGN_IN.none);
    else if (message.type === 'tk:pick') setPicking(message.on);
    else if (message.type === 'tk:picked') {
      setPicking(false);
      setPicked(message);
    } else if (message.type === 'tk:board-error') attachBoardError(message);
  });
  useEffect(() => {
    const listener = (event: MessageEvent) => onMessage(event);
    window.addEventListener('message', listener);
    const pings = bridgePings;
    return () => {
      window.removeEventListener('message', listener);
      for (const timer of pings.current) window.clearTimeout(timer);
      if (bridgeWait.current !== null) window.clearTimeout(bridgeWait.current);
    };
  }, []);

  const send = (message: TParentMessage) =>
    postToBridge(frameRef.current?.contentWindow ?? null, origin, message);

  /**
   * Every load of the frame (first open, reload, a full navigation inside it) is a new document: pick mode ends and the
   * bridge must answer the ping, or the page is treated as having none.
   */
  const frameLoaded = () => {
    afterLoad.current = true;
    stopPings();
    setLoaded(true);
    setPicking(false);
    setOffer(null);
    setSignIn(SIGN_IN.none);
    if (bridgeWait.current !== null) window.clearTimeout(bridgeWait.current);
    setBridgeNonce(null);
    bridgeWait.current = window.setTimeout(() => {
      bridgeWait.current = null;
      setBridged(false);
    }, BRIDGE_WAIT_MS);
    send({ type: 'tk:ping' });
    bridgePings.current = BRIDGE_PINGS_MS.map((delay) =>
      window.setTimeout(() => send({ type: 'tk:ping' }), delay),
    );
  };

  const startSignIn = () => {
    if (signInUrl && beginStandSignIn(signInUrl, window) === SIGN_IN_WHERE.tab) setSignIn(SIGN_IN.waiting);
  };

  // The sign-in happens in another tab: coming back to this one opens the stand again with the session it now has.
  const reopenSignedIn = () => {
    setSignIn(SIGN_IN.none);
    onReload();
  };
  const signedIn = useEffectEvent(reopenSignedIn);
  useEffect(() => {
    if (signIn !== SIGN_IN.waiting) return;
    let done = false;
    const back = () => {
      if (done || document.visibilityState !== 'visible') return;
      done = true;
      signedIn();
    };
    window.addEventListener('focus', back);
    document.addEventListener('visibilitychange', back);
    return () => {
      window.removeEventListener('focus', back);
      document.removeEventListener('visibilitychange', back);
    };
  }, [signIn]);

  const nextMessage: IVaultFillDeps['next'] = (type, timeoutMs) =>
    new Promise((resolve) => {
      const listener = (message: TBridgeMessage) => {
        if (message.type !== type) return;
        done();
        resolve(message as never);
      };
      const timer = window.setTimeout(() => {
        done();
        resolve(null);
      }, timeoutMs);
      const done = () => {
        window.clearTimeout(timer);
        bridgeListeners.current.delete(listener);
      };
      bridgeListeners.current.add(listener);
    });

  /** A login with the safe lands here only while this frame shows the tab's preview. */
  const vaultLogin = useEffectEvent((site: string, mint: IVaultFillDeps['mint']) =>
    performVaultLogin(site, {
      target: state.target,
      slot: origin,
      nonce: bridged ? bridgeNonce : null,
      slotTarget: () => fetchSlotTarget(origin),
      mint,
      send,
      next: nextMessage,
    }),
  );
  useEffect(() => onVaultLogin(previewKey, (site, mint) => vaultLogin(site, mint)), [previewKey]);

  const highlight = useEffectEvent((selector: string) => {
    if (!bridged) return false;
    send({ type: 'tk:highlight', selector });
    return true;
  });
  useEffect(() => onHighlightRequest(previewKey, (selector) => highlight(selector)), [previewKey]);

  const togglePick = () => {
    if (!bridged) {
      // The bridge may only have been late: ask again, a hello turns «Select» on.
      send({ type: 'tk:ping' });
      return;
    }
    send({ type: 'tk:pick', on: !picking });
    setPicking(!picking);
  };

  /**
   * Focus is outside the page after pressing «Select»: Escape here leaves pick mode too. In capture and stopped, so
   * window listeners of the app (a permission prompt answers Escape with «no») never see this Escape.
   */
  const stopPicking = useEffectEvent((event: globalThis.KeyboardEvent) => {
    if (event.key !== 'Escape' || event.isComposing) return;
    event.preventDefault();
    event.stopPropagation();
    send({ type: 'tk:pick', on: false });
    setPicking(false);
  });
  useEffect(() => {
    if (!picking) return;
    const listener = (event: globalThis.KeyboardEvent) => stopPicking(event);
    window.addEventListener('keydown', listener, true);
    return () => window.removeEventListener('keydown', listener, true);
  }, [picking]);

  /** The anchor goes away with the popover: focus returns to «Select» unless the person clicked elsewhere. */
  const closePicked = (reason?: TDismissReason) => {
    setPicked(null);
    if (reason === DISMISS_REASON.outside || reason === DISMISS_REASON.focusOutside) return;
    window.requestAnimationFrame(() => pickRef.current?.focus());
  };

  const reload = () => {
    if (bridged) send({ type: 'tk:go', action: 'reload' });
    else onReload();
  };

  const copyPath = async () => {
    try {
      await navigator.clipboard.writeText(path);
    } catch {
      return;
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), COPIED_MS);
  };

  const annotate = (comment: string) => {
    if (!picked) return;
    const element = {
      selector: picked.selector,
      tag: picked.tag,
      text: picked.text,
      outerHTML: picked.outerHTML,
      component: picked.component,
      framework: picked.framework,
      path: pathInTarget(picked.url, origin) ?? path,
      viewport: picked.viewport,
    };
    const { target } = state;
    const added = addAnnotation(previewKey, {
      comment,
      target: target.kind === 'port' ? { port: target.port } : { origin: target.origin },
      element,
    });
    closePicked();
    // A board of the design library: the agent gets the board's file on disk (and the line with the text).
    if (target.kind === 'port' && isLibraryPort(target.port)) {
      locateDesignElement(state.preview.machineId, element.path, element.text).then(
        (matches) => annotationResolved(previewKey, added.id, matches),
        () => annotationResolved(previewKey, added.id, null),
      );
      return;
    }
    // Files of a component are looked up in the worktrees of dev servers: an external site has none.
    if (!element.component || target.kind !== 'port') return;
    resolveAcrossPorts([target.port, ...state.preview.neighbours], element.component).then(
      (matches) => annotationResolved(previewKey, added.id, matches),
      () => annotationResolved(previewKey, added.id, null),
    );
  };

  const fit = layout ? fitDevice(layout.body, stage) : null;
  const scale = fit?.scale ?? 1;
  const origin0 = layout ? viewportOrigin(device, layout) : { x: 0, y: 0 };
  const geometry = {
    ...(layout && fit
      ? {
          '--tw-device-w': `${layout.body.width}px`,
          '--tw-device-h': `${layout.body.height}px`,
          '--tw-device-fit-w': `${fit.width}px`,
          '--tw-device-fit-h': `${fit.height}px`,
          '--tw-device-scale': String(fit.scale),
          '--tw-bezel': `${device.bezel}px`,
          '--tw-radius': `${device.radius}px`,
          '--tw-screen-w': `${layout.screen.width}px`,
          '--tw-screen-h': `${layout.screen.height}px`,
          '--tw-vp-y': `${layout.viewportTop}px`,
          '--tw-vp-h': `${layout.viewport.height}px`,
          '--tw-status-h': `${device.statusBar}px`,
          '--tw-cutout-w': `${device.cutout}px`,
          '--tw-top-h': `${layout.topBar || device.statusBar}px`,
          '--tw-bottom-h': `${layout.bottomBar || device.homeInset}px`,
        }
      : {}),
    ...(picked
      ? {
          '--tw-pick-x': `${(origin0.x + picked.rect.x) * scale}px`,
          '--tw-pick-y': `${(origin0.y + picked.rect.y) * scale}px`,
          '--tw-pick-w': `${picked.rect.width * scale}px`,
          '--tw-pick-h': `${picked.rect.height * scale}px`,
        }
      : {}),
  } as CSSProperties;

  const DeviceIcon = DEVICE_ICONS[device.id];
  const framed = device.frame !== DEVICE_FRAME.none;
  const host = targetLabel(state.target);
  const deviceLabel =
    browser === PREVIEW_BROWSER.none || device.browsers.length < 2
      ? device.label
      : `${device.label} · ${PREVIEW_BROWSER_LABELS[browser]}`;
  const viewportHint = (option: TPreviewBrowserId) => {
    const optionLayout = deviceLayout(device, option);
    return optionLayout ? `${optionLayout.viewport.width}×${optionLayout.viewport.height}` : undefined;
  };
  const menu: TTwMenuEntry[] = [
    ...PREVIEW_DEVICES.map((option) => ({
      kind: TW_MENU_KIND.radio,
      id: option.id,
      label: option.screen
        ? `${option.label} · ${option.screen.width}×${option.screen.height}`
        : option.label,
      checked: option.id === device.id,
      onSelect: () => patchPreview(previewKey, { device: option.id }),
    })),
    ...(device.browsers.length > 1
      ? [
          { kind: TW_MENU_KIND.separator, id: 'browser-separator' },
          { kind: TW_MENU_KIND.label, id: 'browser-label', label: t('inspector.previewFrame.browserBars') },
          ...device.browsers.map((option) => ({
            kind: TW_MENU_KIND.radio,
            id: `browser:${option}`,
            label: PREVIEW_BROWSER_LABELS[option],
            hint: viewportHint(option),
            checked: option === browser,
            onSelect: () => patchPreview(previewKey, { browsers: { ...browsers, [device.id]: option } }),
          })),
        ]
      : []),
  ];

  const previewFrame = (
    <iframe
      ref={frameRef}
      className="tw-preview-frame"
      src={src ?? undefined}
      title={t('inspector.previewFrame.frameTitle', { target: targetLabel(state.target) })}
      sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-modals allow-downloads"
      allow="clipboard-read; clipboard-write"
      onLoad={() => {
        frameLoaded();
        if (!state.entered) onEntered();
      }}
    />
  );

  return (
    <div className="tw-preview-view">
      <div className="tw-preview-toolbar" role="toolbar" aria-label={t('inspector.previewFrame.navigation')}>
        {leading}
        {!hideHistory && (
          <>
            <TwButton
              type="button"
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              aria-label={t('inspector.previewFrame.back')}
              title={t('inspector.previewFrame.back')}
              disabled={!bridged}
              onClick={() => send({ type: 'tk:go', action: 'back' })}
            >
              <ArrowLeft />
            </TwButton>
            <TwButton
              type="button"
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              aria-label={t('inspector.previewFrame.forward')}
              title={t('inspector.previewFrame.forward')}
              disabled={!bridged}
              onClick={() => send({ type: 'tk:go', action: 'forward' })}
            >
              <ArrowRight />
            </TwButton>
          </>
        )}
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          aria-label={t('inspector.previewFrame.reload')}
          title={t('inspector.previewFrame.reload')}
          disabled={src === null}
          onClick={reload}
        >
          <RefreshCw />
        </TwButton>
        {address ? (
          address(path)
        ) : (
          <button
            type="button"
            className="tw-preview-path"
            title={copied ? t('inspector.previewFrame.copied') : t('inspector.previewFrame.copyPath')}
            aria-label={t('inspector.previewFrame.pathCopyLabel', { path })}
            onClick={() => void copyPath()}
          >
            <span className="tw-preview-path-text">{path}</span>
            <span className="tw-preview-path-copied" aria-live="polite">
              {copied ? t('inspector.previewFrame.copied') : ''}
            </span>
          </button>
        )}
        <TwTooltip
          content={
            signIn === SIGN_IN.none
              ? pickHint(bridged, loaded, pickTip)
              : t('inspector.previewFrame.signInFirst')
          }
        >
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={picking ? BUTTON_VARIANT.primary : BUTTON_VARIANT.ghost}
            icon={<MousePointerClick />}
            ref={pickRef}
            aria-pressed={picking}
            aria-disabled={!bridged || undefined}
            className="tw-preview-pick"
            onClick={togglePick}
          >
            {t('inspector.previewFrame.select')}
          </TwButton>
        </TwTooltip>
        <TwMenuButton
          items={menu}
          menuLabel={t('inspector.previewFrame.deviceFrame')}
          align={LAYER_ALIGN.end}
          className="tw-icon-button tw-preview-more"
          aria-label={t('inspector.previewFrame.frameNamed', { device: deviceLabel })}
          title={t('inspector.previewFrame.frameNamed', { device: deviceLabel })}
        >
          <DeviceIcon />
        </TwMenuButton>
        {actions}
      </div>
      {offer && (
        <div className="tw-preview-offer" role="group" aria-label={t('inspector.previewFrame.redirectLabel')}>
          <span className="tw-preview-offer-text">
            {t('inspector.previewFrame.redirectOffer', { origin: offer.origin })}
          </span>
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            onClick={() => setOffer(null)}
          >
            {t('inspector.previewFrame.cancel')}
          </TwButton>
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            onClick={() => {
              onOpenUrl(offer);
              setOffer(null);
            }}
          >
            {t('inspector.previewFrame.open')}
          </TwButton>
        </div>
      )}
      {signIn !== SIGN_IN.none && (
        <div className="tw-preview-offer" role="group" aria-label={t('inspector.previewFrame.signInLabel')}>
          <span className="tw-preview-offer-text">
            {signIn === SIGN_IN.asked
              ? t('inspector.previewFrame.signInAsked')
              : t('inspector.previewFrame.signInPending')}
          </span>
          {signIn === SIGN_IN.asked ? (
            <TwButton
              type="button"
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.primary}
              onClick={startSignIn}
            >
              {t('inspector.previewFrame.signIn')}
            </TwButton>
          ) : (
            <TwButton
              type="button"
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              onClick={reopenSignedIn}
            >
              {t('inspector.previewFrame.refresh')}
            </TwButton>
          )}
        </div>
      )}
      <div
        ref={stageRef}
        className={cx('tw-preview-stage', layout && 'tw-preview-stage-device')}
        style={geometry}
      >
        {src === null ? (
          <div
            className="tw-preview-frame tw-skeleton"
            role="status"
            aria-label={t('inspector.previewFrame.opening', { target: targetLabel(state.target) })}
          />
        ) : (
          <div
            className={cx(
              'tw-preview-device',
              !layout && 'tw-preview-device-fluid',
              layout && (framed ? 'tw-device-framed' : 'tw-device-plain'),
            )}
            data-device={device.id}
            data-browser={browser}
          >
            <div className="tw-device-body">
              <div className="tw-device-screen">
                {previewFrame}
                {layout && framed && <DeviceChrome device={device} browser={browser} host={host} />}
              </div>
            </div>
            {picked && <div ref={anchorRef} className="tw-preview-pick-anchor" aria-hidden />}
          </div>
        )}
      </div>
      {picked && (
        <PickPopover
          key={`${picked.selector}#${picked.rect.x},${picked.rect.y}`}
          picked={picked}
          anchorRef={anchorRef}
          onAdd={annotate}
          onClose={closePicked}
        />
      )}
    </div>
  );
};
