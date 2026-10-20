import { appConfig } from '@/platform/config';
import type {
  CLSMetricWithAttribution,
  INPMetricWithAttribution,
  LCPMetricWithAttribution,
  MetricWithAttribution,
} from 'web-vitals/attribution';
import { streamVia } from '@/lib/event-stream';
import { selectNetworkQuality, useNetworkHealthStore } from '@/lib/network-health';
import {
  LONG_TASK_MS,
  LONG_TASKS_EVENT,
  SPEED_EVENT,
  batchBody,
  createLongTaskAggregate,
  createSpeedQueue,
  describeScript,
  longFrameSpan,
  type ILongFrameLike,
  describeDevice,
  driftSpan,
  interactiveAt,
  type IBusySpan,
  type ISpeedEvent,
  type TSpeedMetric,
  type TSpeedProperties,
} from '@/lib/hl-speed';

/**
 * Timings from the person's own browser, sent to the team's self-hosted PostHog in batches: no SDK,
 * no autocapture, no replay. Only the workspace shell reports; a machine's runtime frame is a part of its page.
 */

const ENTRY_SELECTOR = '[role="log"] [data-entry-id]';
/** Providers whose conversation is a timeline; a plain terminal tab never shows an entry. */
const TIMELINE_PROVIDERS = new Set(['claude', 'codex', 'chat']);
const NEW_CHAT_KEY = 'chat:new';
/** The newest entries are at the bottom, where an opened conversation scrolls to. */
const ENTRIES_PROBED = 80;
const OPEN_TIMEOUT_MS = 45_000;
/** Longer than this after the working screen and the page is not settling; no number beats a made-up one. */
const INTERACTIVE_GIVE_UP_MS = 60_000;
const DRIFT_TICK_MS = 100;
const STEADY_FLUSH_MS = 10 * 60_000;

type TBusySource = 'loaf' | 'longtask' | 'drift';

interface ISpeedContext {
  owner: string | null;
  machine: string | null;
  release: string | null;
  runtimeCommit: string | null;
}

export interface IConversationOpenMeta {
  /** Identifies the conversation only to skip re-selecting the one on screen; never sent. */
  key: string;
  kind: 'chat' | 'agent';
  provider: string;
  machineId: string | null;
  remote: boolean;
  /** Drawn in a machine's frame; a machine on `no-frame` is drawn by the shell itself. */
  framed: boolean;
}

interface IOpenProbe {
  meta: IConversationOpenMeta;
  startedAt: number;
  seen: WeakSet<Element>;
  warm: boolean | null;
  /** The page was hidden since the selection when this moves on: the time says nothing about opening. */
  hiddenSeq: number;
  stop: () => void;
  /** Milliseconds from the selection to each step of the opening, so a slow one names the step that waited. */
  phases: Partial<Record<TOpenPhase, number>>;
  /** What the timeline showed last: what a timeout looked like. */
  screen: string | null;
  socketTries: number;
}

export type TOpenPhase = 'tab' | 'socket' | 'init';

const context: ISpeedContext = { owner: null, machine: null, release: null, runtimeCommit: null };
let started = false;
let busySource: TBusySource = 'drift';
let firstHiddenAt = Number.POSITIVE_INFINITY;
let hiddenSeq = 0;
/** Start of the current visible stretch; `null` while the page is hidden. */
let visibleSince: number | null = null;
let pageSession = '';
let readyAt: number | null = null;
let interactive: number | null = null;
let interactiveSettled = false;
let pendingSpans: IBusySpan[] = [];
let driftUntil = 0;
let driftTimer: ReturnType<typeof setTimeout> | null = null;
let probe: IOpenProbe | null = null;
let lastShownKey: string | null = null;
let shellMarked = false;
let firstScreenMarked = false;
const loadTasks = createLongTaskAggregate();
const steadyTasks = createLongTaskAggregate();

const isoAt = (at: number) => new Date(performance.timeOrigin + at).toISOString();
const hiddenSince = (at: number) => firstHiddenAt <= at;

const connection = () =>
  (
    navigator as Navigator & {
      connection?: { effectiveType?: string; rtt?: number; downlink?: number; saveData?: boolean };
    }
  ).connection;

const isStandalone = () =>
  window.matchMedia?.('(display-mode: standalone)').matches ||
  (navigator as Navigator & { standalone?: boolean }).standalone === true;

const navigationType = () => {
  const entry = performance.getEntriesByType?.('navigation')[0] as PerformanceNavigationTiming | undefined;
  return entry?.type ?? 'navigate';
};

const commonProperties = (): TSpeedProperties => {
  const net = connection();
  const device = describeDevice(navigator.userAgent, navigator.maxTouchPoints);
  return {
    hl_app: 'ai',
    $lib: 'hl-speed',
    stand: !import.meta.env.PROD,
    release: context.release ?? 'unknown',
    runtime_commit: context.runtimeCommit,
    shell_commit: (import.meta.env.VITE_COMMIT as string | undefined) ?? null,
    machine: context.machine,
    device: device.device,
    os: device.os,
    browser: device.browser,
    standalone: isStandalone(),
    net_type: net?.effectiveType ?? 'unknown',
    net_rtt: typeof net?.rtt === 'number' ? net.rtt : null,
    net_downlink: typeof net?.downlink === 'number' ? net.downlink : null,
    save_data: net?.saveData ?? null,
    viewport_w: window.innerWidth,
    navigation_type: navigationType(),
    stream_via: streamVia(),
    sw_controlled: Boolean(navigator.serviceWorker?.controller),
    page_session: pageSession,
  };
};

const transmit = (events: ISpeedEvent[]) => {
  const { speedPosthogHost, speedPosthogKey } = appConfig();
  if (!speedPosthogHost || !speedPosthogKey) return;
  const body = JSON.stringify(
    batchBody(speedPosthogKey, context.owner ?? 'ai-anonymous', commonProperties(), events),
  );
  const url = `${speedPosthogHost.replace(/\/$/, '')}/batch/`;
  // text/plain keeps the request simple for CORS: no preflight, and a beacon survives the page going away.
  try {
    if (navigator.sendBeacon?.(url, new Blob([body], { type: 'text/plain' }))) return;
  } catch {
    // Some webviews throw instead of returning false.
  }
  void fetch(url, {
    method: 'POST',
    body,
    keepalive: true,
    credentials: 'omit',
    headers: { 'Content-Type': 'text/plain' },
  }).catch(() => {});
};

const queue = createSpeedQueue({ send: transmit });

const push = (event: string, at: number, properties: TSpeedProperties) => {
  queue.push({ event, timestamp: isoAt(at), properties });
  if (document.visibilityState === 'hidden') queue.flush();
};

const pushMetric = (metric: TSpeedMetric, value: number, at: number, extra: TSpeedProperties = {}) => {
  push(SPEED_EVENT, at, {
    metric,
    value: metric === 'cls' ? Math.round(value * 10_000) / 10_000 : Math.round(value),
    link_quality: selectNetworkQuality(useNetworkHealthStore.getState()),
    ...extra,
  });
};

const afterNextPaint = (callback: (at: number) => void) => {
  requestAnimationFrame(() => {
    const channel = new MessageChannel();
    channel.port1.onmessage = () => callback(performance.now());
    channel.port2.postMessage(null);
  });
};

const recordBusy = (span: IBusySpan) => {
  if (interactive !== null && span.start >= interactive) steadyTasks.add(span);
  else loadTasks.add(span);
  if (!interactiveSettled && span.blocking > 0) pendingSpans.push(span);
};

const flushLongTasks = (at: number) => {
  const visible = visibleSince === null ? 0 : at - visibleSince;
  visibleSince = document.visibilityState === 'visible' ? at : null;
  if (loadTasks.count > 0)
    push(LONG_TASKS_EVENT, at, { phase: 'load', lt_source: busySource, ...loadTasks.drain() });
  if (visible >= 1_000)
    push(LONG_TASKS_EVENT, at, {
      phase: 'steady',
      lt_source: busySource,
      window_ms: Math.round(visible),
      ...steadyTasks.drain(),
    });
};

const driftTick = (expectedAt: number, seq: number) => {
  const now = performance.now();
  driftTimer = null;
  if (seq === hiddenSeq && document.visibilityState === 'visible') {
    const span = driftSpan(expectedAt, now);
    if (span) recordBusy(span);
  }
  if (now < driftUntil && document.visibilityState === 'visible') scheduleDrift();
};

const scheduleDrift = () => {
  const seq = hiddenSeq;
  const expectedAt = performance.now() + DRIFT_TICK_MS;
  driftTimer = setTimeout(() => driftTick(expectedAt, seq), DRIFT_TICK_MS);
};

/** Safari has no long task API; a timer watches the main thread only while something is being measured. */
const watchDrift = (ms: number) => {
  if (busySource !== 'drift') return;
  driftUntil = Math.max(driftUntil, performance.now() + ms);
  if (driftTimer === null && document.visibilityState === 'visible') scheduleDrift();
};

const observeBusy = (): TBusySource => {
  const supported =
    typeof PerformanceObserver === 'undefined' ? [] : (PerformanceObserver.supportedEntryTypes ?? []);
  try {
    if (supported.includes('long-animation-frame')) {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries() as unknown as ILongFrameLike[])
          recordBusy(longFrameSpan(entry));
      }).observe({ type: 'long-animation-frame', buffered: true });
      return 'loaf';
    }
    if (supported.includes('longtask')) {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          recordBusy({
            start: entry.startTime,
            end: entry.startTime + entry.duration,
            blocking: Math.max(0, entry.duration - LONG_TASK_MS),
          });
        }
      }).observe({ type: 'longtask', buffered: true });
      return 'longtask';
    }
  } catch {
    // An observer that refuses its type leaves the timer.
  }
  return 'drift';
};

const settleInteractive = () => {
  if (interactiveSettled || readyAt === null) return;
  const now = performance.now();
  if (hiddenSince(now)) {
    interactiveSettled = true;
    pendingSpans = [];
    return;
  }
  const at = interactiveAt(readyAt, pendingSpans, now);
  if (at === null && now - readyAt < INTERACTIVE_GIVE_UP_MS) {
    setTimeout(settleInteractive, 1_000);
    return;
  }
  interactiveSettled = true;
  pendingSpans = [];
  driftUntil = 0;
  if (at === null) return;
  interactive = at;
  pushMetric('interactive', at, now, { busy_source: busySource });
};

const onVisibility = () => {
  const now = performance.now();
  if (document.visibilityState === 'hidden') {
    firstHiddenAt = Math.min(firstHiddenAt, now);
    hiddenSeq += 1;
    stopProbe();
    flushLongTasks(now);
    queue.flush();
    return;
  }
  visibleSince ??= now;
};

/** What moved, what was hit and which script held the thread: the number alone said only how bad. */
const vitalAttribution = (metric: MetricWithAttribution): TSpeedProperties => {
  if (metric.name === 'CLS') {
    const { attribution } = metric as CLSMetricWithAttribution;
    return {
      shift_target: attribution.largestShiftTarget?.slice(0, 200) ?? null,
      shift_value: attribution.largestShiftValue ?? null,
    };
  }
  if (metric.name === 'INP') {
    const { attribution } = metric as INPMetricWithAttribution;
    return {
      inp_target: attribution.interactionTarget?.slice(0, 200) ?? null,
      inp_type: attribution.interactionType ?? null,
      inp_input_delay: Math.round(attribution.inputDelay),
      inp_processing: Math.round(attribution.processingDuration),
      inp_presentation: Math.round(attribution.presentationDelay),
      inp_script: attribution.longestScript ? describeScript([attribution.longestScript.entry]) : null,
      inp_script_part: attribution.longestScript?.subpart ?? null,
    };
  }
  if (metric.name === 'LCP') {
    const { attribution } = metric as LCPMetricWithAttribution;
    return {
      lcp_target: attribution.target?.slice(0, 200) ?? null,
      lcp_render_delay: Math.round(attribution.elementRenderDelay),
    };
  }
  return {};
};

const reportVital = (metric: MetricWithAttribution) => {
  pushMetric(metric.name.toLowerCase() as TSpeedMetric, metric.value, performance.now(), {
    rating: metric.rating,
    vital_navigation: metric.navigationType,
    ...vitalAttribution(metric),
  });
};

/** Starts reporting for this page; later calls only refresh who and where. */
export const startSpeedTelemetry = (owner: string | null) => {
  if (owner) context.owner = owner;
  if (started || typeof window === 'undefined' || window.parent !== window) return;
  started = true;
  pageSession = crypto.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  if (document.visibilityState === 'hidden') firstHiddenAt = 0;
  else visibleSince = performance.now();
  busySource = observeBusy();
  document.addEventListener('visibilitychange', onVisibility);
  window.addEventListener('pagehide', () => {
    flushLongTasks(performance.now());
    queue.flush();
  });
  setInterval(() => {
    if (document.visibilityState === 'visible') flushLongTasks(performance.now());
  }, STEADY_FLUSH_MS);
  void import('web-vitals/attribution').then(({ onCLS, onFCP, onINP, onLCP, onTTFB }) => {
    onTTFB(reportVital);
    onFCP(reportVital);
    onLCP(reportVital);
    onINP(reportVital);
    onCLS(reportVital);
  });
};

export const updateSpeedContext = (next: Partial<Omit<ISpeedContext, 'owner'>>) => {
  Object.assign(context, next);
};

/** Only a page that opened on the workspace measures its load; a client-side move from another page is not a load. */
const loadedOnWorkspace = () => !/\/docs\//.test(location.pathname);

/** The workspace replaced the splash. */
export const markShellPainted = () => {
  if (!started || shellMarked || !loadedOnWorkspace()) return;
  shellMarked = true;
  watchDrift(INTERACTIVE_GIVE_UP_MS);
  afterNextPaint((at) => {
    if (!hiddenSince(at)) pushMetric('shell', at, at);
  });
};

/** The workspace shows its conversation list: the screen a person came to work in. */
export const markFirstScreenPainted = () => {
  if (!started || firstScreenMarked || !loadedOnWorkspace()) return;
  firstScreenMarked = true;
  afterNextPaint((at) => {
    if (hiddenSince(at)) {
      interactiveSettled = true;
      return;
    }
    pushMetric('first_screen', at, at);
    readyAt = at;
    watchDrift(INTERACTIVE_GIVE_UP_MS);
    setTimeout(settleInteractive, 1_000);
  });
};

const documentsOnScreen = (): Document[] => {
  const docs = [document];
  for (const frame of document.querySelectorAll('iframe')) {
    try {
      if (frame.contentDocument) docs.push(frame.contentDocument);
    } catch {
      // A cross-origin frame (a preview, the design canvas) never holds a timeline.
    }
  }
  return docs;
};

/** Visibility from styles alone: reading them needs no layout, unlike a rectangle. */
const visibleByStyle = (element: Element) => {
  const view = element.ownerDocument.defaultView;
  if (!view) return false;
  if (view.getComputedStyle(element).visibility === 'hidden') return false;
  const frame = view.frameElement;
  if (frame && frame.ownerDocument.defaultView?.getComputedStyle(frame).visibility === 'hidden') return false;
  const log = element.closest('[role="log"]');
  return !log || Number(view.getComputedStyle(log).opacity) > 0;
};

const onScreen = (element: Element) => {
  const view = element.ownerDocument.defaultView;
  if (!view) return false;
  const rect = element.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0 || rect.bottom <= 0 || rect.top >= view.innerHeight) return false;
  return visibleByStyle(element);
};

const probedEntries = function* () {
  for (const doc of documentsOnScreen()) {
    const entries = doc.querySelectorAll(ENTRY_SELECTOR);
    for (let i = entries.length - 1; i >= Math.max(0, entries.length - ENTRIES_PROBED); i--) yield entries[i];
  }
};

const stopProbe = () => {
  const current = probe;
  probe = null;
  if (!current) return;
  current.stop();
  if (interactiveSettled) driftUntil = 0;
};

const pushOpen = (current: IOpenProbe, result: 'shown' | 'timeout', value: number, at: number) =>
  pushMetric('conversation_open', value, at, {
    result,
    conv_kind: current.meta.kind,
    conv_provider: current.meta.provider,
    conv_machine: current.meta.machineId,
    remote: current.meta.remote,
    framed: current.meta.framed,
    frame_warm: current.warm,
    open_tab_ms: current.phases.tab ?? null,
    open_socket_ms: current.phases.socket ?? null,
    open_init_ms: current.phases.init ?? null,
    open_screen: current.screen,
    open_socket_tries: current.socketTries,
  });

/** A step of the opening reached in this document; only its first time after the selection counts. */
export const markOpenPhase = (phase: TOpenPhase) => {
  if (!probe || probe.phases[phase] !== undefined) return;
  probe.phases[phase] = Math.round(performance.now() - probe.startedAt);
};

export const noteOpenSocketTry = () => {
  if (probe) probe.socketTries += 1;
};

export const noteOpenScreen = (screen: string) => {
  if (probe) probe.screen = screen;
};

/**
 * Waits for the first entry of the opened conversation through observers: polling rectangles every frame
 * forced a layout in each frame of the opening, the very frames it measured.
 */
const watchEntries = (current: IOpenProbe) => {
  const shown = (entry: Element) => {
    if (
      probe !== current ||
      current.hiddenSeq !== hiddenSeq ||
      current.seen.has(entry) ||
      !visibleByStyle(entry)
    )
      return;
    stopProbe();
    lastShownKey = current.meta.key;
    afterNextPaint((at) => {
      if (current.hiddenSeq === hiddenSeq) pushOpen(current, 'shown', at - current.startedAt, at);
    });
  };
  const intersections = new IntersectionObserver((records) => {
    for (const record of records) if (record.isIntersecting) shown(record.target);
  });
  const watched = new WeakSet<Element>();
  const watch = (entry: Element) => {
    if (watched.has(entry) || current.seen.has(entry)) return;
    watched.add(entry);
    intersections.observe(entry);
  };
  const mutations = new MutationObserver((records) => {
    for (const record of records) {
      for (const node of record.addedNodes) {
        if (node.nodeType !== Node.ELEMENT_NODE) continue;
        const element = node as Element;
        if (element.matches(ENTRY_SELECTOR)) watch(element);
        element.querySelectorAll(ENTRY_SELECTOR).forEach(watch);
      }
    }
  });
  const observed = new WeakSet<Document>();
  const attach = () => {
    for (const doc of documentsOnScreen()) {
      if (observed.has(doc) || !doc.body) continue;
      observed.add(doc);
      mutations.observe(doc.body, { childList: true, subtree: true });
      doc.querySelectorAll(ENTRY_SELECTOR).forEach(watch);
    }
  };
  attach();
  // A machine's frame may load its document after the selection; a light rescan picks it up without touching layout.
  const rescan = setInterval(attach, 250);
  const timeout = setTimeout(() => {
    if (probe !== current) return;
    stopProbe();
    const now = performance.now();
    pushOpen(current, 'timeout', now - current.startedAt, now);
  }, OPEN_TIMEOUT_MS);
  current.stop = () => {
    intersections.disconnect();
    mutations.disconnect();
    clearInterval(rescan);
    clearTimeout(timeout);
  };
};

/**
 * A conversation was selected: from here to the first paint of one of its timeline entries, wherever it renders
 * (the shell itself or a machine's runtime frame). Entries already on screen belong to what was open before.
 */
export const markConversationOpen = (meta: IConversationOpenMeta) => {
  if (!started || document.visibilityState !== 'visible') return;
  stopProbe();
  if (!TIMELINE_PROVIDERS.has(meta.provider) || meta.key === NEW_CHAT_KEY) return;
  const startedAt = performance.now();
  const seen = new WeakSet<Element>();
  let shown = 0;
  for (const entry of probedEntries()) {
    if (!onScreen(entry)) continue;
    seen.add(entry);
    shown += 1;
  }
  if (shown > 0 && meta.key === lastShownKey) return;
  const warm =
    meta.framed && meta.machineId
      ? Boolean(
          document.querySelector(
            `iframe.tw-frame[src*="machine=${CSS.escape(encodeURIComponent(meta.machineId))}"]`,
          ),
        )
      : null;
  probe = {
    meta,
    startedAt,
    seen,
    warm,
    hiddenSeq,
    stop: () => {},
    phases: {},
    screen: null,
    socketTries: 0,
  };
  watchDrift(OPEN_TIMEOUT_MS);
  watchEntries(probe);
};

/**
 * How long a sent message took to reach the agent: the press is the pending row's time, the arrival is when
 * its transcript entry was matched to it. `queued` tells a message held for the end of a turn from one that went in.
 */
export const reportPromptDelivery = (
  sentAt: number,
  deliveredAt: number,
  meta: { provider: string | null; queued: boolean; steered: boolean; machineId?: string | null },
  steps: TSpeedProperties = {},
) => {
  const value = deliveredAt - sentAt;
  if (!(value >= 0) || value > 30 * 60_000) return;
  // Whose agent got it, and whether it was sent from a machine's frame or by the shell itself.
  // `at` is on the page's performance clock; the delivery times are wall-clock ones.
  pushMetric('prompt_delivery', value, deliveredAt - performance.timeOrigin, {
    conv_provider: meta.provider,
    queued: meta.queued,
    steered: meta.steered,
    conv_machine: meta.machineId ?? null,
    framed: window.parent !== window,
    ...steps,
  });
};
