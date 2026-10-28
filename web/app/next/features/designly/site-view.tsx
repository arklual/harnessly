// An open design as the design draws it (.hx-site): a browser with the design's pages in tabs, its bar — history,
// the address with the pages, the device and size, more — and the page itself in the chosen viewport, in a case when
// the device has one. The page is the board on the machine's library server, opened in a preview slot of its own.
import {
  Fragment,
  useCallback,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react';
import {
  BARS,
  BROWSERS,
  browserOf,
  DEVICES,
  deviceOf,
  overviewBox,
  overviewOf,
  SCROLL_START,
  trackScroll,
  viewportOf,
  viewportVars,
  VP_KIND,
  type ISize,
  type TBars,
} from '@/next/features/designly/devices';
import { Overview, PhoneBottom, PhonePopover, PhoneTop } from '@/next/features/designly/site-device';
import { qrModules } from '@/next/features/designly/qr';
import { PaneChatModes, SiteModes, type TSiteMode } from '@/next/features/designly/site-chat';
import { PANE_CHAT, type TPaneChat } from '@/next/features/designly/pane-chat';
import {
  DEV_H,
  dragDevtools,
  dragTweaks,
  dragViewport,
  type IFloat,
  type IPoint,
  type TDock,
} from '@/next/features/designly/site-drag';
import {
  GUEST_KEY_CODE,
  guestKeyValue,
  readBridgeMessage,
  type IPicked,
  type ITweak,
  type TTweakValue,
} from '@/next/features/designly/page-bridge';
import { DEV_TAB, type TDevTab } from '@/next/features/designly/devtools-data';
import SiteDevtools from '@/next/features/designly/site-devtools';
import type { IStyleItem } from '@/next/features/designly/element-style';
import { fromFrame } from '@/next/features/designly/bridge-origin';
import { AnchorContext, createAnchorStore, type IAnchorItem } from '@/next/features/designly/note-anchor';
import { useDomBridge } from '@/next/features/designly/use-dom-bridge';
import SiteLoader from '@/next/features/designly/site-loader';
import { loadStageOf, type ILoadProgress } from '@/next/features/designly/site-load';
import { useSiteLoad } from '@/next/features/designly/use-site-load';
import SiteTweaks from '@/next/features/designly/site-tweaks';
import { PaneDock, ZoomControls } from '@/next/features/designly/site-dock';
import { useViewMemory } from '@/next/features/designly/use-view-memory';
import type { IViewMemory } from '@/next/features/designly/view-memory';
import { useCanvasZoom } from '@/next/features/designly/use-canvas-zoom';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { SheetGrip, SheetScrim, useSheetDrag } from '@/next/ui/sheet';
import Swap from '@/next/ui/swap';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { usePhone } from '@/next/ui/use-media';
import { useHScroll } from '@/next/ui/use-h-scroll';
import { usePresence, usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/designly.css';
import '@/next/styles/vendor-devices.css';
import '@/next/styles/site.css';
import '@/next/styles/site-tools.css';
import '@/next/styles/site-pane.css';

const POP_MS = 200;
/** The pill under the page in the pane: a device keeps this much free under it (the pill, its gap and its margin). */
const DOCK_ROOM = 64;
const VP_PAD = 48;
const TAB_OUT_MS = 200;
const TWEAKS_OUT_MS = 200;
/** The island stays narrow this long after the page's last scroll message. */
const SCROLL_MS = 700;

export interface ISitePage {
  /** The board's path in the project, the page's key. */
  path: string;
  name: string;
}

export interface ISiteTab {
  id: string;
  /** The page on screen in this tab and its history. */
  hist: readonly string[];
  hi: number;
}

/** Where the chat stands for a design in the right pane, and its switch (in «Ещё»). */
export interface ISitePaneChat {
  mode: TPaneChat;
  onMode: (mode: TPaneChat, section: { width: number; height: number }) => void;
  /** The stage's mode changed (a drag put the chat where the pane has no room for it): the pane puts it back. */
  sync: (section: { width: number; height: number }) => void;
  /** The pane opened: the kept choice is applied once the section has its size. */
  start: (section: { width: number; height: number }) => void;
}

/** A design shown in the right pane: the bar has a share, there is no strip of tabs; the chat is the left one, or
 *  also over the design or at its foot (`chat`). */
export interface ISitePane {
  onShare: () => void;
  chat?: ISitePaneChat | null;
}

/** The project's access when a gateway keeps it: the bar's share button opens the dialog instead of copying the link. */
export interface ISiteAccess {
  /** «Поделиться» for a person who manages, «Запросить правку» for one who may only look. */
  label: string;
  onOpen: () => void;
  /** The person manages the project: a deck opens its own share popover instead of the dialog. */
  manage?: boolean;
}

/**
 * A visitor of a guest link: the design is looked at the way its owner looks at it (the same bar, devices, sizes, zoom
 * and board settings), without editing, chats, sharing or tools. The board is served with a sandbox, so its window has
 * no origin of its own to speak to: it is addressed as `*` and trusted for being the frame's window.
 */
export interface ISiteGuest {
  /** What the bar's pick button is called and its tip («Комментировать»): the block picked becomes a comment. */
  pickLabel: string;
  pickTip: string;
  /** «Your name: Ann» in «Ещё»: the guest changes the name they comment under; null until there is one. */
  account?: { label: string; onOpen: () => void } | null;
  /** «Во весь экран» on the bar: the page alone over the whole window, without the bar and the device. */
  full?: { label: string; tip: string; onOpen: () => void };
}

export interface ISiteViewProps {
  title: string;
  pages: readonly ISitePage[];
  tabs: readonly ISiteTab[];
  tab: string;
  /** The board's address for the frame; null while the slot opens. */
  src: string | null;
  /** The same board at an address the slot lets in without a ticket (its cookie is set); the frames of «Обзор» use it. */
  plainSrc?: string | null;
  /** The address of this page for another device (a code to point a phone at); null while there is none to give. */
  phoneUrl?: string | null;
  /** The slot did not open: why. */
  error: string | null;
  /** Bumped by a reload: the frame loads again. */
  reloadKey: number;
  onPickTab: (id: string) => void;
  onCloseTab: (id: string) => void;
  onNewTab: (path: string) => void;
  onNavigate: (path: string) => void;
  onHistory: (step: -1 | 1) => void;
  onReload: (hard: boolean) => void;
  /** Opens the current board in its own browser tab; absent while there is no board URL to open. */
  onOpenOutside?: () => void;
  onCopyLink: () => void;
  onRetry: () => void;
  onOpenDrawer: () => void;
  onToggleMax: () => void;
  maxed: boolean;
  /** The chat beside the site and the field over it; without it the design is only looked at. */
  stage?: ISiteStage;
  /** Set when the design is shown in the right pane. */
  pane?: ISitePane;
  /** The design whose view (device, canvas, page) is kept in the browser; without it nothing is. */
  memoryKey?: string;
  /** The view a design starts with where the browser has kept none (a guest link carries the owner's). */
  viewSeed?: IViewMemory;
  /** Set for a visitor of a guest link. */
  guest?: ISiteGuest;
  /** The page alone over the view's whole box: no bar, no device, no zoom (a guest's full screen). */
  full?: boolean;
  /** Set where the gateway keeps the project's access and the person has something to do there. */
  access?: ISiteAccess;
  /** Set where the person may publish the design as a site: the bar's «Опубликовать» opens the dialog. */
  publish?: { onOpen: () => void };
  /** The page's preview slot, whose record DevTools read; without it the panel is not offered. */
  devtools?: {
    machine: MachineId;
    slot: string | null;
    /** The design the page is a board of. */ artifact: string;
  };
}

export interface ISiteStage {
  mode: TSiteMode;
  /** The section's size goes with it: the first window of «Окно» is placed by it. */
  onMode: (mode: TSiteMode, section: { width: number; height: number }) => void;
  /** The island is folded into its pill. */
  min: boolean;
  run: 'idle' | 'run' | 'done';
  /** The agent of the design's chat is starting or working on a turn (the chat's own «Собираю контекст…»). */
  busy?: boolean;
  float: IFloat | null;
  /** The side of the docked chat, and its width. */
  dock: TDock;
  chatW: number;
  chat: ReactNode;
  island: ReactNode;
  /** Over the page: the note at a picked block, the bar of the edits. */
  overlay: ReactNode;
  /** On the page, in its viewport's pixels: the pins of the notes. */
  pins: ReactNode;
  /** A click on the page picks a block (through the slot's bridge) instead of acting on it. */
  pick: boolean;
  /** A finger held on a block picks it too (a guest on a phone), without turning picking on. */
  hold?: boolean;
  /** `at` is the block's bottom-left corner in the section's pixels, where its note opens. */
  onPicked: (picked: IPicked, at: { x: number; y: number; right: boolean }) => void;
  /** The settings the board offers now (its `tk:tweaks`), as the page said them. */
  onControls?: (controls: ITweak[]) => void;
  /** The page ended the pick itself: Esc in it, or it went to another address. */
  onPickEnd: () => void;
  /** The page on screen has the slot's bridge (it said hello), or a new page has not yet. */
  onBridge: (ready: boolean) => void;
  /** The looks the page should wear (the notes' edits of the picked blocks); the view sends them to the bridge. */
  styles: IStyleItem[];
  /** The bridge answered the looks: these blocks it no longer has. */
  onStyled: (gone: number[]) => void;
  /** The blocks the pins stand on: the view asks the bridge to follow them and hands its word to the pins. */
  anchors: IAnchorItem[];
  /** Whether the page is being picked from (the mode of edits always picks); null while there is no bridge to pick with. */
  picking: boolean | null;
  /** Turns picking on or off: the same switch as the island's button. */
  onPick: () => void;
  /** On a phone in «Чат»: the site is opened over the chat. */
  sview: boolean;
  onSview: (open: boolean) => void;
  /** Words from the page's tools (a console line, a request) go into the field. */
  onAttach: (words: string) => void;
  /**
   * A deck's context for the chat: one transient chip («Слайд 3 · Заголовок») sent with the next message as an
   * `<annotation>`; null takes it away. Absent from a stage that has no chat field.
   */
  context?: (ctx: { label: string; text: string; selected?: boolean } | null) => void;
  /** The chat alone, without the window it stands in (a deck's panel hosts it); absent where the stage has no chat. */
  deckChat?: ReactNode;
  /** Sends a message on the person's behalf (a deck's «Короче», a layout change, «＋ слайд»). */
  ask?: (text: string) => void;
}

interface IMoreItem {
  id: string;
  icon: string;
  label: string;
  /** A separator above it. */
  sep?: boolean;
  kbd?: string;
  /** Set for an item that is on or off. */
  on?: boolean;
  disabled?: boolean;
  pick: () => void;
}

const POP = {
  pages: 'pages',
  device: 'device',
  more: 'more',
  phone: 'phone',
  add: 'add',
  throttle: 'throttle',
} as const;
type TPop = (typeof POP)[keyof typeof POP];

/** One popover at a time; it leaves with its own animation. */
const usePop = () => {
  const [pop, setPop] = useState<TPop | null>(null);
  const { value, state } = usePresence(pop, POP_MS);
  return {
    pop,
    shownPop: value,
    state,
    toggle: (id: TPop) => setPop((current) => (current === id ? null : id)),
    open: (id: TPop) => setPop(id),
    close: () => setPop(null),
  };
};

const useAreaSize = (ref: React.RefObject<HTMLElement | null>): ISize | null => {
  const [size, setSize] = useState<ISize | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return size;
};

const SiteView = ({
  title,
  pages,
  tabs,
  tab,
  src,
  plainSrc = null,
  phoneUrl = null,
  error,
  reloadKey,
  onPickTab,
  onCloseTab,
  onNewTab,
  onNavigate,
  onHistory,
  onReload,
  onOpenOutside,
  onCopyLink,
  onRetry,
  onOpenDrawer,
  onToggleMax,
  maxed,
  stage,
  pane,
  access,
  publish,
  devtools,
  memoryKey,
  viewSeed,
  guest,
  full = false,
}: ISiteViewProps) => {
  const pop = usePop();
  // A phone has one column: the chat docked to the right is stacked like the left one there.
  const phone = usePhone();
  const memory = useViewMemory(memoryKey ?? null, viewSeed);
  const [deviceId, setDeviceId] = useState(memory.initial.device);
  const [size, setSize] = useState<ISize | null>(memory.initial.size);
  const [rot, setRot] = useState(memory.initial.rot);
  // A phone in a case draws its interface (the status bar, the browser's bars); the bars fold as the page is scrolled.
  const [chrome, setChrome] = useState(memory.initial.chrome);
  const [browserId, setBrowserId] = useState<string | null>(memory.initial.browser);
  const [bars, setBars] = useState<TBars>(BARS.full);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  /** What the page of a frame said of its way (the bridge's progress); a frame of another page starts without. */
  const [progress, setProgress] = useState<(ILoadProgress & { key: string }) | null>(null);
  const [closing, setClosing] = useState<string | null>(null);
  // DevTools under the page, and the settings the board offers in a panel over it.
  const [dev, setDev] = useState(false);
  const [devTab, setDevTab] = useState<TDevTab>(DEV_TAB.console);
  const [devH, setDevH] = useState<number>(DEV_H.start);
  const [tweaksOpen, setTweaksOpen] = useState(false);
  const [tweaksPos, setTweaksPos] = useState<IPoint | null>(null);
  /** What the board of a frame offered; a frame of another page (or a reloaded one) starts without. */
  const [offer, setOffer] = useState<{ key: string; controls: ITweak[] } | null>(null);
  const areaRef = useRef<HTMLDivElement>(null);
  const sectionRef = useRef<HTMLElement>(null);
  // The strip of tabs: faded where more is behind, and the current tab is brought into view.
  const tabsRef = useRef<HTMLDivElement>(null);
  useHScroll(tabsRef);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const anchorsRef = useRef<HTMLDivElement>(null);
  // Where the page says the pinned blocks are: the pins read it in the frame it comes, without a render.
  const [anchorStore] = useState(createAnchorStore);
  const area = useAreaSize(areaRef);

  const current = tabs.find((x) => x.id === tab) ?? tabs[0];
  const pagePath = current?.hist[current.hi] ?? null;
  const page = pages.find((p) => p.path === pagePath) ?? null;
  // The address reads the page and where it is; a site named by its own address shows it once.
  const addrName = page?.name ?? title;
  const addrPath = pagePath ? `${title}/${pagePath}` : title;
  // The full screen shows the page as «Адаптивно» does, over the whole box; the device chosen comes back after it.
  const device = deviceOf(full ? 'fill' : deviceId);
  // The page's tree for DevTools' «Elements», read through the board's own script while that tab is in view.
  const dom = useDomBridge(
    frameRef,
    src,
    reloadKey,
    dev && devTab === DEV_TAB.elements && devtools !== undefined,
  );
  // The pane's keys are the page's alone: its chat is beside it, and a key there must not act on the page.
  const keys = pane === undefined;
  // The pane's pill stands under the page like the field over the site's: a device keeps room for it (and for the
  // field over the pill when the pane's chat is «Снизу»).
  const paneBottomChat = pane?.chat?.mode === PANE_CHAT.bottom;
  const underField =
    guest === undefined &&
    ((keys && (stage?.mode === 'island' || stage?.mode === 'notes')) || pane !== undefined);
  const paneRoom = paneBottomChat ? 2 * DOCK_ROOM : DOCK_ROOM;
  // A phone has no floating pill and no zoom over the page (the design's mobile.css): the address switches the pages,
  // the device's menu holds the zoom, «…» the settings, so the page's own header and tab bar stay free.
  const dockShown = pane !== undefined && !phone;
  const vp = area
    ? viewportOf(device, area, full ? null : size, full ? false : rot, dockShown ? VP_PAD + paneRoom : VP_PAD)
    : null;
  const framed = vp?.kind === VP_KIND.frame;
  const overview = vp?.kind === VP_KIND.overview;
  const phoneBrowser = framed && !vp.rot && chrome ? browserOf(device, browserId) : null;
  const browsers = phoneBrowser && device.os ? BROWSERS[device.os] : [];
  // «Обзор» keeps room at the bottom for the field, or for a guest's list of comments.
  const ovwRoom = underField || guest !== undefined;
  const cards = vp && overview ? overviewOf({ w: vp.vw, h: vp.vh }, ovwRoom) : [];
  const qr = useMemo(() => (phoneUrl ? qrModules(phoneUrl) : null), [phoneUrl]);
  // The canvas: the page's box zooms and pans over its area; another device or size starts at the fit again.
  const canvas = useCanvasZoom(
    areaRef,
    sectionRef,
    vp?.fit ?? 1,
    vp !== null && error === null && !full,
    `${device.id}|${size?.w ?? 0}x${size?.h ?? 0}|${rot}|${full}`,
    overview ? overviewBox(cards, ovwRoom) : { w: vp ? vp.bw * vp.fit : 0, h: vp ? vp.bh * vp.fit : 0 },
    { start: memory.initial, onChange: memory.save },
  );
  // What the device says is kept with the canvas's view; the page is restored once, then followed.
  const { save } = memory;
  useEffect(() => {
    save({ device: deviceId, size, rot, chrome, browser: browserId });
  }, [save, deviceId, size, rot, chrome, browserId]);
  const pageRestored = useRef(memory.initial.page === null);
  const keepPage = useEffectEvent((path: string) => {
    if (!pageRestored.current) {
      pageRestored.current = true;
      const wanted = memory.initial.page;
      // Only a design opened on its first page is taken back: a link to another page (a note's chip) wins.
      const first = path === pages[0]?.path;
      if (wanted !== null && wanted !== path && first && pages.some((p) => p.path === wanted)) {
        onNavigate(wanted);
        return;
      }
    }
    memory.save({ page: path });
  });
  useEffect(() => {
    if (memoryKey !== undefined && pagePath) keepPage(pagePath);
  }, [memoryKey, pagePath]);
  const latestCanvas = useRef(canvas);
  useEffect(() => {
    latestCanvas.current = canvas;
  });

  // The viewport's sizes go to the CSS through custom properties on its element.
  useLayoutEffect(() => {
    const el = areaRef.current;
    if (!el || !vp) return;
    for (const [name, value] of Object.entries(viewportVars(vp, device))) el.style.setProperty(name, value);
  });

  // DevTools' height goes to the CSS the same way (the field over the page stands above it).
  useLayoutEffect(() => {
    sectionRef.current?.style.setProperty('--dev-h', `${devH}px`);
  }, [devH]);

  // The chat's width goes to the CSS the same way.
  const chatW = stage?.chatW;
  useLayoutEffect(() => {
    if (chatW !== undefined) sectionRef.current?.style.setProperty('--site-chat', `${chatW}px`);
  }, [chatW]);

  // The window of «Окно» goes to the CSS through custom properties on the section.
  const float = stage?.float ?? null;
  useLayoutEffect(() => {
    const el = sectionRef.current;
    if (!el || !float) return;
    el.style.setProperty('--fx', `${float.x}px`);
    el.style.setProperty('--fy', `${float.y}px`);
    el.style.setProperty('--fw', `${float.w}px`);
    el.style.setProperty('--fh', `${float.h}px`);
  }, [float]);

  // The pane's choice of the chat's place: applied once the section has its size, kept when a drag moves the chat.
  const paneChat = pane?.chat ?? null;
  const sectionSize = () => {
    const rect = sectionRef.current?.getBoundingClientRect();
    return { width: rect?.width ?? 0, height: rect?.height ?? 0 };
  };
  const latestPaneChat = useRef(paneChat);
  useEffect(() => {
    latestPaneChat.current = paneChat;
  });
  useLayoutEffect(() => {
    // Once, when the pane opens.
    latestPaneChat.current?.start(sectionSize());
  }, []);
  const stageMode = stage?.mode;
  useEffect(() => {
    latestPaneChat.current?.sync(sectionSize());
  }, [stageMode]);
  const paneOwnChat = paneChat !== null && paneChat.mode !== PANE_CHAT.left;

  // The slot's bridge: the pick goes to the page while it is wanted (again after the page loads anew), its answers come
  // only from the slot's window and origin.
  const pick = stage?.pick ?? false;
  const latestStage = useRef(stage);
  // The keys of the page act on the latest of what they toggle (they listen once).
  const latestPick = useRef<() => void>(() => undefined);
  const latestInspect = useRef<() => void>(() => undefined);
  const latestDom = useRef(dom);
  const latestReload = useRef<(hard: boolean) => void>(() => undefined);
  useEffect(() => {
    latestStage.current = stage;
  });
  // The page scrolls: the field narrows for a while, and a phone's bars fold or unfold by the direction.
  const track = useRef(SCROLL_START);
  const scrollTimer = useRef(0);
  const onPageScroll = (y: number) => {
    const section = sectionRef.current;
    if (section) {
      section.dataset.scrolling = 'true';
      window.clearTimeout(scrollTimer.current);
      scrollTimer.current = window.setTimeout(() => {
        section.dataset.scrolling = 'false';
      }, SCROLL_MS);
    }
    track.current = trackScroll(track.current, y);
    setBars(track.current.bars);
  };
  const latestScroll = useRef(onPageScroll);
  useEffect(() => {
    latestScroll.current = onPageScroll;
  });
  useEffect(() => () => window.clearTimeout(scrollTimer.current), []);
  /** The bars of a phone start unfolded again: another page, another browser, another device. */
  const unfoldBars = () => {
    track.current = SCROLL_START;
    setBars(BARS.full);
  };
  /** The frame that has been loaded for this page: before its first load it is a blank page of this window's own origin,
   *  and a message to the slot's origin would be refused there (a console error in WebKit at every new page). */
  const loadedKey = useRef<string | null>(null);
  /** The slot's frame and origin, or null while there is no page. */
  const frameOf = () => {
    const frame = frameRef.current;
    if (!frame || src === null) return null;
    // A guest's board has an opaque origin (its sandbox): nothing to name, the window itself is what is trusted.
    return { frame, origin: guest ? '*' : new URL(src, window.location.href).origin };
  };
  /** The slot's window and origin once its page has loaded, or null before. */
  const slotOf = () => (loadedKey.current === `${src}#${reloadKey}` ? frameOf() : null);
  const ping = () => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage({ type: 'tk:ping' }, slot.origin);
  };
  const sendStyles = () => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage(
      { type: 'tk:styles', items: latestStage.current?.styles ?? [] },
      slot.origin,
    );
  };
  /** The page is on a canvas: its script tells Space and the middle button's drag (a board's says no hello). */
  const onCanvas = () => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage({ type: 'tk:canvas', on: true }, slot.origin);
  };
  const sendAnchors = () => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage(
      { type: 'tk:anchors', items: latestStage.current?.anchors ?? [] },
      slot.origin,
    );
  };
  const arm = (on: boolean) => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage({ type: 'tk:pick', on }, slot.origin);
    slot?.frame.contentWindow?.postMessage(
      { type: 'tk:hold', on: latestStage.current?.hold === true },
      slot.origin,
    );
  };
  // The board's own script offers its settings: the site says it draws them (and whether the panel is open), takes the
  // list and sends the changes back.
  const tweaksOpenNow = useRef(tweaksOpen);
  useEffect(() => {
    tweaksOpenNow.current = tweaksOpen;
  });
  const hostTweaks = () => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage(
      { type: 'tk:tweaks-host', on: true, open: tweaksOpenNow.current },
      slot.origin,
    );
  };
  const sendTweak = (name: string, value: TTweakValue) => {
    const slot = slotOf();
    slot?.frame.contentWindow?.postMessage({ type: 'tk:tweak', name, value }, slot.origin);
  };
  useEffect(() => {
    const slot = frameOf();
    if (!slot) return;
    const { frame, origin } = slot;
    latestStage.current?.onBridge(false);
    anchorStore.set(null);
    // Asked again when the page of this frame has loaded already (the effect runs for other reasons too); a page still
    // on its way says «hello» itself, and onLoad asks.
    ping();
    const onMessage = (event: MessageEvent) => {
      if (!fromFrame(event, frame.contentWindow, origin)) return;
      const message = readBridgeMessage(event.data);
      const now = latestStage.current;
      if (message?.k === 'anchored') {
        anchorStore.set(message.report);
        return;
      }
      if (message?.k === 'scroll') {
        latestScroll.current(message.y);
        return;
      }
      if (message?.k === 'wheel') {
        // A pinch or ⌘-wheel over the page: the canvas takes it as its own wheel at the same spot of the screen.
        const at = frame.getBoundingClientRect();
        const k = frame.offsetWidth > 0 ? at.width / frame.offsetWidth : 1;
        areaRef.current?.dispatchEvent(
          new WheelEvent('wheel', {
            bubbles: true,
            cancelable: true,
            ctrlKey: true,
            deltaY: message.dy,
            clientX: at.left + message.x * k,
            clientY: at.top + message.y * k,
          }),
        );
        return;
      }
      if (message?.k === 'space') {
        latestCanvas.current.fromPage.space(message.down);
        return;
      }
      if (message?.k === 'pan') {
        latestCanvas.current.fromPage.pan(message.dx, message.dy, message.drag);
        return;
      }
      if (message?.k === 'progress') {
        setProgress({ key: `${src}#${reloadKey}`, state: message.state, resources: message.resources });
        return;
      }
      if (message?.k === 'tweaks') {
        setOffer({ key: `${src}#${reloadKey}`, controls: message.controls });
        latestStage.current?.onControls?.(message.controls);
        return;
      }
      if (message?.k === 'tweaks-panel') {
        setTweaksOpen(message.open);
        return;
      }
      if (message?.k === 'lib-ready') {
        hostTweaks();
        // A board of the library follows the pinned blocks itself (its script, not the slot's bridge, which says hello).
        anchorStore.set(null);
        sendAnchors();
        // Its script tells Space and the middle button's drag on a canvas too.
        frame.contentWindow?.postMessage({ type: 'tk:canvas', on: true }, origin);
        // And it picks blocks and wears the notes' looks, as the slot's bridge does.
        if (now) {
          frame.contentWindow?.postMessage({ type: 'tk:pick', on: now.pick }, origin);
          frame.contentWindow?.postMessage({ type: 'tk:hold', on: now.hold === true }, origin);
          frame.contentWindow?.postMessage({ type: 'tk:styles', items: now.styles }, origin);
          now.onBridge(true);
        }
        return;
      }
      if (message?.k === 'key') {
        // The guest page listens for its keys on its window: one pressed over the board comes there the same.
        if (guest)
          window.dispatchEvent(
            new KeyboardEvent('keydown', {
              key: guestKeyValue(message.key),
              code: GUEST_KEY_CODE[message.key],
            }),
          );
        return;
      }
      if (message?.k === 'hello') {
        hostTweaks();
        // The page is on a canvas: its bridge tells Space, the middle button's drag and the wheel it cannot scroll by.
        frame.contentWindow?.postMessage({ type: 'tk:canvas', on: true }, origin);
      }
      if (!message || !now) return;
      if (message.k === 'styled') {
        now.onStyled(message.gone);
        return;
      }
      if (message.k === 'hello') {
        frame.contentWindow?.postMessage({ type: 'tk:pick', on: now.pick }, origin);
        sendStyles();
        anchorStore.set(null);
        sendAnchors();
        now.onBridge(true);
        track.current = SCROLL_START;
        setBars(BARS.full);
      } else if (message.k === 'pick-off') now.onPickEnd();
      else {
        const box = frame.getBoundingClientRect();
        const home = sectionRef.current?.getBoundingClientRect();
        const k = frame.offsetWidth > 0 ? box.width / frame.offsetWidth : 1;
        const { rect } = message.picked;
        const x = box.left - (home?.left ?? 0) + rect.x * k;
        const y = box.top - (home?.top ?? 0) + (rect.y + rect.height) * k;
        const width = home?.width ?? 0;
        // The card keeps the corner as it is at the canvas's fit and follows the zoom from there (its CSS clamps it in).
        const at = latestCanvas.current.toFit({ x, y });
        now.onPicked(message.picked, { x: at.x, y: at.y, right: x > width / 2 });
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
    // slotOf reads the frame of this src; a new function each render must not listen again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src, reloadKey]);
  useEffect(() => {
    arm(pick);
    // arm reads the frame of this src.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pick, src, reloadKey]);
  // The looks tried on the page: the whole set goes again whenever it changes (an empty set gives every block back).
  const stylesKey = JSON.stringify(stage?.styles ?? []);
  useEffect(() => {
    sendStyles();
    // sendStyles reads the frame of this src and the latest stage; the set's key says when it changed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stylesKey, src, reloadKey]);
  // The blocks the pins stand on: the set goes again whenever it changes.
  const anchorsKey = JSON.stringify(stage?.anchors ?? []);
  useEffect(() => {
    sendAnchors();
    // sendAnchors reads the frame of this src and the latest stage; the set's key says when it changed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [anchorsKey, src, reloadKey]);
  useEffect(() => {
    hostTweaks();
    // hostTweaks reads the frame of this src.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tweaksOpen, src, reloadKey]);

  // Outside press and Esc close a popover.
  useEffect(() => {
    if (pop.pop === null) return;
    const onDown = (event: PointerEvent) => {
      const target = event.target as Element;
      if (!target.closest('.hx-browser__anchor, .hx-browser__addr, .hx-tabs__add')) pop.close();
    };
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') pop.close();
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  });

  // «V» turns picking on and off, as the tip of the bar's button says; typing in a field is not for it.
  const canPick = keys && stage !== undefined && stage.picking !== null;
  useEffect(() => {
    if (!canPick) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.code !== 'KeyV' || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      if ((event.target as Element | null)?.closest('input, textarea, select, [contenteditable]')) return;
      event.preventDefault();
      latestPick.current();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [canPick]);

  // ⌥⌘I turns DevTools on and off, ⇧⌘C inspects an element on the page, Esc stops that; as the tips of the buttons say.
  const canDev = keys && devtools !== undefined;
  useEffect(() => {
    if (!canDev) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      const mod = event.metaKey || event.ctrlKey;
      if (event.code === 'KeyI' && event.altKey && mod) {
        event.preventDefault();
        setDev((on) => !on);
      } else if (event.code === 'KeyC' && event.shiftKey && mod && !event.altKey) {
        event.preventDefault();
        latestInspect.current();
      } else if (event.key === 'Escape') {
        if (latestDom.current.inspect) latestDom.current.setInspect(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [canDev]);

  // ⌘R reloads the page of the design, ⇧⌘R clears the cache first, as the tips say; the browser's own reload is not wanted here.
  useEffect(() => {
    if (!keys) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.code !== 'KeyR' || !(event.metaKey || event.ctrlKey) || event.altKey) return;
      event.preventDefault();
      latestReload.current(event.shiftKey);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [keys]);

  const closeTab = (id: string) => {
    setClosing(id);
    window.setTimeout(() => {
      setClosing(null);
      onCloseTab(id);
    }, TAB_OUT_MS);
  };

  // A handle of the box of «Резина» writes the size on the elements while it is dragged; the state takes it on release.
  const gripViewport = (event: ReactPointerEvent<HTMLElement>, edge: 'e' | 's' | 'se') => {
    const el = areaRef.current;
    if (!el || !vp) return;
    dragViewport(event, el, {
      edge,
      size: { w: vp.vw, h: vp.vh },
      fit: vp.fit,
      onSize: (next) => {
        setDeviceId('fill');
        setRot(false);
        setSize(next);
      },
      restore: () => {
        el.dataset.dev = vp.kind;
        for (const [name, value] of Object.entries(viewportVars(vp, device)))
          el.style.setProperty(name, value);
        const label = el.closest('.hx-site')?.querySelector('.hx-browser__dev-size')?.firstChild;
        if (label) label.nodeValue = `${vp.vw} × ${vp.vh}`;
      },
    });
  };

  const applySize = () => {
    const root = anchorsRef.current;
    const w = Number.parseInt(root?.querySelector<HTMLInputElement>('[data-size="w"]')?.value ?? '', 10);
    const h = Number.parseInt(root?.querySelector<HTMLInputElement>('[data-size="h"]')?.value ?? '', 10);
    if (!(w >= 200 && h >= 200)) return;
    setDeviceId('fill');
    setRot(false);
    setSize({ w: Math.min(3840, w), h: Math.min(3840, h) });
  };
  const onSizeKey = (event: KeyboardEvent<HTMLInputElement>) => {
    const el = event.currentTarget;
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      event.preventDefault();
      const step = (event.shiftKey ? 10 : 1) * (event.key === 'ArrowUp' ? 1 : -1);
      el.value = String(Math.max(200, (Number.parseInt(el.value, 10) || 0) + step));
      applySize();
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      applySize();
    }
  };

  const frameKey = `${src}#${reloadKey}`;
  const tweaks = offer?.key === frameKey ? offer.controls : null;
  const noTweaks = tweaks === null || tweaks.length === 0;
  const tweaksPanel = usePresentFlag(tweaksOpen, TWEAKS_OUT_MS);
  const toggleDev = () => {
    if (dev && pop.pop === POP.throttle) pop.close();
    setDev(!dev);
  };
  // Inspecting a page's element and picking a block for a note are two ways to use a click on the page: one at a time.
  const togglePick = () => {
    if (dom.inspect) dom.setInspect(false);
    stage?.onPick();
  };
  const toggleInspect = () => {
    if (dom.inspect) {
      dom.setInspect(false);
      return;
    }
    if (stage?.picking) stage.onPick();
    setDev(true);
    setDevTab(DEV_TAB.elements);
    dom.setInspect(true);
  };
  const inspectDisabled = src === null;
  useEffect(() => {
    latestPick.current = togglePick;
    latestInspect.current = toggleInspect;
    latestDom.current = dom;
    latestReload.current = onReload;
  });
  const toggleTweaks = () => {
    if (tweaksOpen || !noTweaks) setTweaksOpen(!tweaksOpen);
  };
  // The wait is the whole way: the machine opens the page, then the frame loads it. A design with no board has nothing
  // to wait for; a page that has not loaded for too long is a failure, not a wait.
  const waiting = src === null && error === null && (tabs.length > 0 || title === '');
  const pending = waiting || (src !== null && loadedFor !== frameKey);
  const clock = useSiteLoad(`${tab}|${pagePath ?? ''}|${reloadKey}`, pending);
  const stalled = clock.stalled;
  const failed = error !== null || stalled;
  const loading = pending && !failed;
  const frameProgress = progress?.key === frameKey ? progress : null;
  const loadStage = loadStageOf(frameProgress, clock.slow);
  const noBack = !current || current.hi <= 0;
  const noFwd = !current || current.hi >= current.hist.length - 1;
  const sizeText = vp ? `${vp.vw} × ${vp.vh}` : '';
  const fillOn = device.id === 'fill';
  /** The host the phone's address bar shows. */
  const host = window.location.hostname;

  const pageName = (path: string) => pages.find((p) => p.path === path)?.name ?? path;

  /** What «Ещё» offers: only what works here. */
  const offered: (IMoreItem | null | undefined)[] = [
    onOpenOutside
      ? {
          id: 'outside',
          icon: 'external-link',
          label: t('next.site.openOutside'),
          pick: () => {
            pop.close();
            onOpenOutside();
          },
        }
      : null,
    guest?.account
      ? {
          id: 'guest-name',
          icon: 'user',
          label: guest.account.label,
          pick: () => {
            pop.close();
            guest.account?.onOpen();
          },
        }
      : null,
    devtools && {
      id: 'devtools',
      sep: true,
      icon: 'panel-bottom',
      label: t('next.site.devtools'),
      kbd: '⌥⌘I',
      on: dev,
      pick: () => {
        pop.close();
        toggleDev();
      },
    },
    devtools && {
      id: 'inspect',
      icon: 'square-mouse-pointer',
      label: t('next.site.inspect'),
      kbd: '⇧⌘C',
      on: dom.inspect,
      disabled: inspectDisabled,
      pick: () => {
        pop.close();
        toggleInspect();
      },
    },
    {
      id: 'tweaks',
      icon: 'sliders-horizontal',
      label: t('next.site.tweaks'),
      on: tweaksOpen,
      disabled: noTweaks && !tweaksOpen,
      pick: () => {
        pop.close();
        toggleTweaks();
      },
    },
    {
      id: 'hard',
      sep: true,
      icon: 'eraser',
      label: t('next.site.hardReload'),
      kbd: '⇧⌘R',
      pick: () => {
        pop.close();
        onReload(true);
      },
    },
    // The phone's bar keeps back, reload, the page, picking, the device and «…»: the rest comes here.
    phone
      ? {
          id: 'forward',
          icon: 'arrow-right',
          label: t('next.site.forward'),
          disabled: noFwd,
          pick: () => {
            pop.close();
            onHistory(1);
          },
        }
      : null,
    phone && (pane || access)
      ? {
          id: 'share',
          icon: 'share',
          label: access?.label ?? t('next.pane.designShare'),
          pick: () => {
            pop.close();
            if (access) access.onOpen();
            else pane?.onShare();
          },
        }
      : null,
    qr && {
      id: 'phone',
      icon: 'qr-code',
      label: t('next.site.phone'),
      pick: () => pop.open(POP.phone),
    },
    {
      id: 'link',
      icon: 'link',
      label: t('next.site.copyLink'),
      pick: () => {
        pop.close();
        onCopyLink();
      },
    },
  ];
  const moreItems = offered.filter((item) => item != null);
  // On the phone the bar's menus (pages, device, «…») are bottom sheets: a scrim under them, a swipe down closes them.
  const barMenu = pop.shownPop === POP.pages || pop.shownPop === POP.device || pop.shownPop === POP.more;
  const barMenuOf = useCallback(
    () => anchorsRef.current?.querySelector<HTMLElement>('.hx-site-menu') ?? null,
    [],
  );
  useSheetDrag(barMenuOf, pop.close, phone && barMenu);
  // In the pane the section is the pane's own unless its chat is over the design or at its foot.
  const ownMode = guest ? 'view' : (stage?.mode ?? 'island');
  const paneMode = pane && !paneOwnChat ? 'pane' : ownMode;

  return (
    <section
      className="hx-site"
      ref={sectionRef}
      data-mode={paneMode}
      data-variant={pane ? 'pane' : 'site'}
      data-dock={phone ? 'left' : (stage?.dock ?? 'left')}
      data-min={stage?.min ?? false}
      data-run={stage?.run ?? 'idle'}
      data-pick={pick}
      data-full={full}
      data-dev={dev}
      data-sview={stage ? stage.sview && stage.mode === 'chat' : false}
    >
      {stage?.chat}
      <div className="hx-site__view">
        <div className="hx-browser">
          {!pane && (
            <div className="hx-strip">
              {!guest && (
                <button
                  type="button"
                  className="hx-head-btn hx-site__menu"
                  aria-label={t('next.header.openMenu')}
                  onClick={onOpenDrawer}
                >
                  <Icon name="menu" />
                </button>
              )}
              {stage && !guest && (
                <button
                  type="button"
                  className="hx-tochat hx-site__back"
                  aria-label={t('next.site.toChat')}
                  onClick={() => stage.onSview(false)}
                >
                  <Icon name="message-square" size="sm" />
                  <span>{t('next.pane.chat')}</span>
                </button>
              )}
              <div className="hx-strip__tabs" ref={tabsRef} role="tablist" aria-label={t('next.site.tabs')}>
                {tabs.map((x) => {
                  const on = x.id === current?.id;
                  const path = x.hist[x.hi] ?? '';
                  return (
                    <div
                      key={x.id}
                      className="hx-tab"
                      role="tab"
                      tabIndex={0}
                      aria-selected={on}
                      data-kind="page"
                      data-state={closing === x.id ? 'closing' : 'open'}
                      title={`${pageName(path)} — ${title}`}
                      onClick={(event) => {
                        if ((event.target as Element).closest('.hx-tab__x') || on) return;
                        pop.close();
                        onPickTab(x.id);
                      }}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' && event.target === event.currentTarget) onPickTab(x.id);
                      }}
                    >
                      <Icon name="app-window" size="sm" className="hx-tab__ic" />
                      <span className="hx-tab__name">{pageName(path)}</span>
                      {tabs.length > 1 && (
                        <button
                          type="button"
                          className="hx-tab__x"
                          aria-label={t('next.site.closeTab')}
                          onClick={(event) => {
                            event.stopPropagation();
                            closeTab(x.id);
                          }}
                        >
                          <Icon name="x" size="sm" />
                        </button>
                      )}
                    </div>
                  );
                })}
                <div className="hx-browser__anchor hx-tabs__add-anchor">
                  <button
                    type="button"
                    className="hx-head-btn hx-tipped hx-tabs__add"
                    aria-label={t('next.site.newTab')}
                    aria-expanded={pop.pop === POP.add}
                    onClick={() => pop.toggle(POP.add)}
                  >
                    <Icon name="plus" />
                    <span className="hx-tip" data-pos="below">
                      {t('next.site.newTab')}
                    </span>
                  </button>
                  {pop.shownPop === POP.add && (
                    <div className="hx-site-menu hx-site-menu--start" data-state={pop.state} role="menu">
                      <div className="hx-dl-pop__label">{t('next.site.pages')}</div>
                      {pages.map((p) => (
                        <button
                          key={p.path}
                          type="button"
                          role="menuitem"
                          className="hx-option"
                          onClick={() => {
                            pop.close();
                            onNewTab(p.path);
                          }}
                        >
                          <span className="hx-option__text">
                            <span className="hx-option__name">{p.name}</span>
                            <span className="hx-option__desc">{p.path}</span>
                          </span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              </div>
              {stage && !guest && (
                <SiteModes
                  mode={stage.mode}
                  onMode={(mode) => {
                    const rect = sectionRef.current?.getBoundingClientRect();
                    stage.onMode(mode, { width: rect?.width ?? 0, height: rect?.height ?? 0 });
                  }}
                />
              )}
              {stage && !guest && (
                <button
                  type="button"
                  className="hx-head-btn hx-site__swap"
                  aria-label={stage.mode === 'chat' ? t('next.site.siteFull') : t('next.site.chatFull')}
                  onClick={() => {
                    const rect = sectionRef.current?.getBoundingClientRect();
                    const size = { width: rect?.width ?? 0, height: rect?.height ?? 0 };
                    stage.onMode(stage.mode === 'chat' ? 'island' : 'chat', size);
                  }}
                >
                  <Icon name={stage.mode === 'chat' ? 'maximize-2' : 'message-square'} />
                </button>
              )}
              {!guest && (
                <button
                  type="button"
                  className="hx-head-btn hx-tipped hx-strip__max"
                  aria-label={maxed ? t('next.site.showSide') : t('next.site.fullWidth')}
                  onClick={onToggleMax}
                >
                  <Icon name={maxed ? 'minimize-2' : 'maximize-2'} />
                  <span className="hx-tip" data-pos="below" data-align="end">
                    {maxed ? t('next.site.showSide') : t('next.site.fullWidth')}
                  </span>
                </button>
              )}
            </div>
          )}

          {phone && barMenu && <SheetScrim phoneOnly state={pop.state} onClose={pop.close} />}
          <div className="hx-browser__bar" ref={anchorsRef}>
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-disabled={noBack}
              aria-label={t('next.site.back')}
              onClick={() => !noBack && onHistory(-1)}
            >
              <Icon name="arrow-left" />
              <span className="hx-tip" data-pos="below" data-align="start">
                {t('next.site.back')}
              </span>
            </button>
            <button
              type="button"
              className="hx-head-btn hx-tipped hx-browser__fwd"
              aria-disabled={noFwd}
              aria-label={t('next.site.forward')}
              onClick={() => !noFwd && onHistory(1)}
            >
              <Icon name="arrow-right" />
              <span className="hx-tip" data-pos="below">
                {t('next.site.forward')}
              </span>
            </button>
            <button
              type="button"
              className="hx-head-btn hx-tipped hx-browser__reload"
              data-spin={loading}
              aria-label={t('next.site.reload')}
              onClick={() => onReload(false)}
            >
              <Icon name="rotate-cw" />
              <span className="hx-tip" data-pos="below">
                {t('next.site.reload')}
                {keys && <kbd>⌘R</kbd>}
              </span>
            </button>
            <div className="hx-browser__addr">
              <button
                type="button"
                className="hx-browser__url"
                aria-expanded={pop.pop === POP.pages}
                aria-disabled={pages.length < 2}
                onClick={() => pages.length > 1 && pop.toggle(POP.pages)}
              >
                <span className="hx-browser__page-name">{addrName}</span>
                {addrPath !== addrName && <span className="hx-browser__path">{addrPath}</span>}
                <span className="hx-browser__load" data-on={loading} />
              </button>
              {pop.shownPop === POP.pages && (
                <div
                  className="hx-site-menu hx-site-menu--center hx-sheet-phone"
                  data-state={pop.state}
                  role="menu"
                >
                  <SheetGrip />
                  <div className="hx-dl-pop__label">{t('next.site.pages')}</div>
                  {pages.map((p) => (
                    <button
                      key={p.path}
                      type="button"
                      role="menuitem"
                      className="hx-option"
                      aria-selected={p.path === pagePath}
                      onClick={() => {
                        pop.close();
                        onNavigate(p.path);
                      }}
                    >
                      <span className="hx-option__text">
                        <span className="hx-option__name">{p.name}</span>
                        <span className="hx-option__desc">{p.path}</span>
                      </span>
                      <Icon name="check" size="sm" className="hx-option__check" />
                    </button>
                  ))}
                </div>
              )}
            </div>
            {stage && (
              <button
                type="button"
                className="hx-browser__tool hx-tipped"
                aria-pressed={stage.picking ?? false}
                aria-disabled={stage.picking === null}
                aria-label={guest?.pickLabel ?? t('next.site.pick')}
                onClick={() => stage.picking !== null && togglePick()}
              >
                <Icon name="square-dashed-mouse-pointer" size="sm" />
                <span className="hx-browser__tool-label">{guest?.pickLabel ?? t('next.site.pick')}</span>
                <span className="hx-tip" data-pos="below">
                  {guest?.pickTip ?? t('next.site.pickTip')}
                  {keys && <kbd>{guest ? 'C' : 'V'}</kbd>}
                </span>
              </button>
            )}
            {guest?.full && (
              <button
                type="button"
                className="hx-browser__tool hx-tipped"
                aria-label={guest.full.label}
                onClick={guest.full.onOpen}
              >
                <Icon name="maximize-2" size="sm" />
                <span className="hx-browser__tool-label">{guest.full.label}</span>
                <span className="hx-tip" data-pos="below">
                  {guest.full.tip}
                  <kbd>F</kbd>
                </span>
              </button>
            )}
            <div className="hx-browser__anchor">
              <button
                type="button"
                className="hx-browser__dev hx-tipped"
                aria-expanded={pop.pop === POP.device}
                aria-label={t('next.site.device')}
                onClick={() => pop.toggle(POP.device)}
              >
                <Icon name={device.icon} size="sm" />
                <span className="hx-browser__dev-size">
                  {overview ? t('next.site.devices.overview') : sizeText}
                </span>
                <span className="hx-tip" data-pos="below">
                  {t('next.site.deviceTip')}
                </span>
              </button>
              {pop.shownPop === POP.device && (
                <div
                  className="hx-site-menu hx-site-menu--end hx-devmenu hx-sheet-phone"
                  data-state={pop.state}
                  role="menu"
                >
                  <SheetGrip />
                  <div className="hx-devsize" data-on={fillOn}>
                    <span className="hx-devsize__label">{t('next.site.size')}</span>
                    <input
                      key={`w${vp?.vw}`}
                      className="hx-devsize__in"
                      data-size="w"
                      inputMode="numeric"
                      defaultValue={vp?.vw}
                      aria-label={t('next.site.width')}
                      onKeyDown={onSizeKey}
                      onBlur={applySize}
                    />
                    <span className="hx-devsize__x">×</span>
                    <input
                      key={`h${vp?.vh}`}
                      className="hx-devsize__in"
                      data-size="h"
                      inputMode="numeric"
                      defaultValue={vp?.vh}
                      aria-label={t('next.site.height')}
                      onKeyDown={onSizeKey}
                      onBlur={applySize}
                    />
                    <button
                      type="button"
                      className="hx-head-btn hx-tipped"
                      aria-label={t('next.site.rotate')}
                      onClick={() => {
                        if (fillOn) {
                          if (size) setSize({ w: size.h, h: size.w });
                          else if (vp) setSize({ w: vp.vh, h: vp.vw });
                        } else setRot((r) => !r);
                      }}
                    >
                      <Icon name="rotate-ccw" size="sm" />
                      <span className="hx-tip" data-align="end">
                        {t('next.site.rotate')}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="hx-head-btn hx-tipped"
                      aria-disabled={fillOn && !size}
                      aria-label={t('next.site.fit')}
                      onClick={() => {
                        setDeviceId('fill');
                        setSize(null);
                        setRot(false);
                      }}
                    >
                      <Icon name="maximize-2" size="sm" />
                      <span className="hx-tip" data-align="end">
                        {t('next.site.fit')}
                      </span>
                    </button>
                  </div>
                  <div className="hx-devzoom">
                    <span className="hx-devsize__label">{t('next.site.zoom')}</span>
                    <ZoomControls zoom={canvas} />
                  </div>
                  {framed && !vp.rot && (
                    <button
                      type="button"
                      role="switch"
                      aria-checked={chrome}
                      className="hx-option hx-devchrome"
                      onClick={() => {
                        setChrome(!chrome);
                        unfoldBars();
                      }}
                    >
                      <span className="hx-devopt__name">{t('next.site.chromeSwitch')}</span>
                      <span className="hx-switch" data-on={chrome} aria-hidden="true">
                        <i />
                      </span>
                    </button>
                  )}
                  {phoneBrowser && browsers.length > 1 && (
                    <>
                      <div className="hx-dl-pop__label">{t('next.site.browserGroup')}</div>
                      {browsers.map((b) => (
                        <button
                          key={b.id}
                          type="button"
                          role="menuitemradio"
                          className="hx-option hx-devopt"
                          aria-selected={b.id === phoneBrowser.id}
                          onClick={() => {
                            setBrowserId(b.id);
                            unfoldBars();
                          }}
                        >
                          <Icon name="check" size="sm" className="hx-devopt__check" />
                          <span className="hx-devopt__name">{t(`next.site.browsers.${b.id}`)}</span>
                          <span className="hx-devopt__meta">{t(`next.site.browsersMeta.${b.id}`)}</span>
                        </button>
                      ))}
                      <div className="hx-dl-pop__label">{t('next.site.deviceGroup')}</div>
                    </>
                  )}
                  {DEVICES.map((d) => (
                    <button
                      key={d.id}
                      type="button"
                      role="menuitemradio"
                      className="hx-option hx-devopt"
                      aria-selected={d.id === device.id}
                      onClick={() => {
                        pop.close();
                        setDeviceId(d.id);
                        setSize(null);
                        setRot(false);
                        setBrowserId(null);
                        unfoldBars();
                      }}
                    >
                      <Icon name="check" size="sm" className="hx-devopt__check" />
                      <span className="hx-devopt__name">{t(`next.site.devices.${d.id}`)}</span>
                      <span className="hx-devopt__meta">{t(`next.site.devicesMeta.${d.id}`)}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
            {!pane && (
              <button
                type="button"
                className="hx-head-btn hx-tipped hx-browser__opt"
                aria-pressed={tweaksOpen}
                aria-disabled={noTweaks && !tweaksOpen}
                aria-label={t('next.site.tweaks')}
                onClick={toggleTweaks}
              >
                <Icon name="sliders-horizontal" />
                <span className="hx-tip" data-pos="below">
                  {noTweaks ? t('next.site.tweaksNone') : t('next.site.tweaksTip')}
                </span>
              </button>
            )}
            {devtools && !pane && (
              <button
                type="button"
                className="hx-head-btn hx-tipped hx-browser__opt"
                aria-pressed={dev}
                aria-label={t('next.site.devtools')}
                onClick={toggleDev}
              >
                <Icon name="panel-bottom" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {t('next.site.devtools')}
                  {keys && <kbd>⌥⌘I</kbd>}
                </span>
              </button>
            )}
            {publish && (
              <button
                type="button"
                className="hx-head-btn hx-tipped hx-browser__publish"
                aria-label={t('next.publish.button')}
                onClick={publish.onOpen}
              >
                <Icon name="globe" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {t('next.publish.button')}
                </span>
              </button>
            )}
            {(pane || access) && (
              <button
                type="button"
                className="hx-head-btn hx-tipped hx-browser__share"
                aria-label={access?.label ?? t('next.pane.designShare')}
                onClick={access ? access.onOpen : pane?.onShare}
              >
                <Icon name="share" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {access?.label ?? t('next.pane.designShare')}
                </span>
              </button>
            )}
            <div className="hx-browser__anchor">
              <button
                type="button"
                className="hx-head-btn hx-tipped"
                aria-expanded={pop.pop === POP.more || pop.pop === POP.phone}
                aria-label={t('next.site.more')}
                onClick={() => pop.toggle(pop.pop === POP.phone ? POP.phone : POP.more)}
              >
                <Icon name="ellipsis" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {t('next.site.more')}
                </span>
              </button>
              {pop.shownPop === POP.more && (
                <div
                  className="hx-site-menu hx-site-menu--end hx-more hx-sheet-phone"
                  data-state={pop.state}
                  role="menu"
                >
                  <SheetGrip />
                  {paneChat && (
                    <div className="hx-more__chat">
                      <span className="hx-more__label">{t('next.site.paneChat')}</span>
                      <PaneChatModes
                        mode={paneChat.mode}
                        onMode={(mode) => paneChat.onMode(mode, sectionSize())}
                      />
                    </div>
                  )}
                  {moreItems.map((item) => (
                    <Fragment key={item.id}>
                      {item.sep && <div className="hx-more__sep" />}
                      <button
                        type="button"
                        role={item.on === undefined ? 'menuitem' : 'menuitemcheckbox'}
                        className="hx-option hx-more__item"
                        aria-checked={item.on}
                        aria-disabled={item.disabled}
                        onClick={() => !item.disabled && item.pick()}
                      >
                        <Icon name={item.icon} size="sm" />
                        <span className="hx-more__label">{item.label}</span>
                        {keys && <kbd className="hx-more__kbd">{item.kbd}</kbd>}
                      </button>
                    </Fragment>
                  ))}
                </div>
              )}
              {pop.shownPop === POP.phone && qr && (
                <PhonePopover modules={qr} state={pop.state} onCopy={onCopyLink} />
              )}
            </div>
          </div>

          <div className="hx-browser__page" data-kind="page">
            <div
              ref={areaRef}
              className="hx-vparea"
              data-dev={vp?.kind ?? VP_KIND.fill}
              data-rot={vp?.rot ?? false}
            >
              {overview && (
                <Overview
                  cards={cards}
                  room={ovwRoom}
                  src={loadedFor === frameKey ? plainSrc : null}
                  onPick={(id) => {
                    setDeviceId(id);
                    setSize(null);
                    setRot(false);
                    setBrowserId(null);
                    unfoldBars();
                  }}
                />
              )}
              {failed && (
                <LoadFailed
                  size={LOAD_FAILED_SIZE.page}
                  title={error === null ? t('next.site.stalled') : t('next.site.failed')}
                  reason={error ?? t('next.site.stalledReason')}
                  onRetry={onRetry}
                />
              )}
              {!failed && (
                <div className="hx-vpbox">
                  <div className="hx-vpscale">
                    <div className={framed ? `device ${device.cls} ${device.color} hx-dev` : 'hx-bare'}>
                      <div className="device-frame">
                        <div className="device-screen hx-screen">
                          <div
                            className="hx-vp"
                            data-ui={phoneBrowser?.id ?? 'none'}
                            data-os={device.os ?? 'none'}
                            data-bars={phoneBrowser ? bars : BARS.full}
                          >
                            {phoneBrowser && (
                              <PhoneTop browser={phoneBrowser} host={host} onUnfold={unfoldBars} />
                            )}
                            <div className="hx-site__frame">
                              <Swap
                                ready={!loading}
                                mounted={src !== null}
                                className="hx-site__fswap"
                                contentClassName="hx-site__held"
                                skeleton={<SiteLoader stage={loadStage} />}
                              >
                                {src !== null && (
                                  <iframe
                                    key={frameKey}
                                    ref={frameRef}
                                    className="hx-site__iframe"
                                    src={src}
                                    title={page?.name ?? title}
                                    onLoad={() => {
                                      loadedKey.current = frameKey;
                                      setLoadedFor(frameKey);
                                      ping();
                                      hostTweaks();
                                      anchorStore.set(null);
                                      sendAnchors();
                                      onCanvas();
                                      arm(latestStage.current?.pick === true);
                                      sendStyles();
                                    }}
                                  />
                                )}
                              </Swap>
                              {stage && (
                                <AnchorContext.Provider value={anchorStore}>
                                  <div className="hx-site__pins">{stage.pins}</div>
                                </AnchorContext.Provider>
                              )}
                            </div>
                            {phoneBrowser && (
                              <PhoneBottom browser={phoneBrowser} host={host} onUnfold={unfoldBars} />
                            )}
                          </div>
                        </div>
                      </div>
                      {framed && (
                        <>
                          <div className="device-stripe" />
                          <div className="device-header" />
                          <div className="device-sensors" />
                          <div className="device-btns" />
                          <div className="device-power" />
                          <div className="device-home" />
                        </>
                      )}
                    </div>
                  </div>
                  {(['e', 's', 'se'] as const).map((edge) => (
                    <span
                      key={edge}
                      className="hx-vpgrab"
                      data-edge={edge}
                      aria-hidden="true"
                      onPointerDown={(event) => gripViewport(event, edge)}
                    />
                  ))}
                </div>
              )}
              {!failed && <div className="hx-vpshield" aria-hidden="true" />}
            </div>
            {dockShown && !failed && (
              <PaneDock
                pages={pages}
                path={pagePath}
                onGo={onNavigate}
                tweaks={{ on: tweaksOpen, disabled: noTweaks && !tweaksOpen, onToggle: toggleTweaks }}
                zoom={canvas}
              />
            )}
            {!pane && !phone && !failed && (
              <div className="hx-vpzoom">
                <ZoomControls zoom={canvas} />
              </div>
            )}
          </div>
          {tweaksPanel.mounted && tweaks && (
            <SiteTweaks
              controls={tweaks}
              state={tweaksPanel.state}
              pos={tweaksPos}
              onChange={sendTweak}
              onClose={() => setTweaksOpen(false)}
              onGrab={(event) => dragTweaks(event, tweaksPos, setTweaksPos)}
            />
          )}
          {devtools && (
            <SiteDevtools
              machine={devtools.machine}
              slot={devtools.slot}
              artifact={devtools.artifact}
              board={pagePath ?? ''}
              open={dev}
              tab={devTab}
              onTab={setDevTab}
              dom={dom}
              inspect={{ on: dom.inspect, disabled: inspectDisabled, onToggle: toggleInspect }}
              throttle={{
                mounted: pop.shownPop === POP.throttle,
                state: pop.state,
                onToggle: () => pop.toggle(POP.throttle),
                onClose: pop.close,
              }}
              onSend={stage && ((words) => stage.onAttach(words))}
              onClose={toggleDev}
              onGrab={(event) =>
                dragDevtools(event, { h: devH, onHeight: setDevH, onClose: () => setDev(false) })
              }
            />
          )}
        </div>
      </div>
      {stage?.island}
      {stage?.overlay}
    </section>
  );
};

export default SiteView;
