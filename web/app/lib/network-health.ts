import { create } from 'zustand';
import { prefixAppPath } from '@/lib/app-path';
import { recordSocketState } from '@/lib/net-telemetry';

/**
 * What the link to the server feels like right now, for an honest status in the header:
 * durations of small requests and socket heartbeats that the server answers instantly, sampled round trips that are
 * still open far too long (a stalled link completes nothing), plus which sockets are reconnecting.
 * Heavy endpoints (git, link previews) stay out: their time is the server's work, not the network's.
 *
 * Most of those requests are answered by the machine the agents run on, so a loaded machine looked exactly
 * like a slow network. Samples now say who answered them: the machine, or the gateway itself. When the machine is
 * slow, a request the gateway answers on its own tells the two apart.
 */

export type TNetworkQuality = 'ok' | 'machine' | 'slow' | 'reconnecting';
/** Who answers the sampled round trip: the machine of the agents, or the gateway in front of it. */
export type TLatencyVia = 'machine' | 'gateway';

export const NETWORK_HEALTH = {
  /** How many recent samples the verdict looks at. */
  windowSize: 5,
  /** Fewer samples than this say nothing yet. */
  minSamples: 3,
  /** Median above this reads as a slow network. */
  slowMedianMs: 1_500,
  /** A sampled round trip still open after this reads as a stalled link, whatever the finished ones say. */
  stalledAfterMs: 3_000,
  /** The gateway is only asked while the machine looks slow, so fewer of its answers make a verdict. */
  gatewayMinSamples: 2,
  /** How often the gateway is asked while the machine stays slow. */
  gatewayProbeGapMs: 10_000,
  /** A socket back within this is not shown as reconnecting: a one-second blip only made the header flicker. */
  reconnectGraceMs: 2_000,
} as const;

export const medianOf = (values: readonly number[]): number => {
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  if (sorted.length % 2) return sorted[middle];
  return (sorted[middle - 1] + sorted[middle]) / 2;
};

export const isSlowNetwork = (
  samples: readonly number[],
  minSamples: number = NETWORK_HEALTH.minSamples,
): boolean => {
  const recent = samples.slice(-NETWORK_HEALTH.windowSize);
  return recent.length >= minSamples && medianOf(recent) > NETWORK_HEALTH.slowMedianMs;
};

interface ILinkSignals {
  gatewaySamples?: readonly number[];
  gatewayStalled?: boolean;
}

/** A slow gateway is the network; a slow machine behind a gateway that answers in time is the machine. */
export const networkQuality = (
  samples: readonly number[],
  reconnecting: boolean,
  stalled = false,
  { gatewaySamples = [], gatewayStalled = false }: ILinkSignals = {},
): TNetworkQuality => {
  if (reconnecting) return 'reconnecting';
  if (gatewayStalled || isSlowNetwork(gatewaySamples, NETWORK_HEALTH.gatewayMinSamples)) return 'slow';
  if (stalled || isSlowNetwork(samples)) return 'machine';
  return 'ok';
};

export const NETWORK_QUALITIES: readonly TNetworkQuality[] = [
  'ok',
  'machine',
  'slow',
  'reconnecting',
] as const;

export const isNetworkQuality = (value: unknown): value is TNetworkQuality =>
  typeof value === 'string' && (NETWORK_QUALITIES as readonly string[]).includes(value);

interface INetworkHealthState {
  /** Round trips the machine answered. */
  samples: number[];
  /** Round trips the gateway answered on its own. */
  gatewaySamples: number[];
  /** Keys of sockets that lost their connection and are trying again. */
  reconnecting: Record<string, true>;
  /** A sampled round trip to the machine has been open longer than it may take. */
  stalled: boolean;
  /** The same for a round trip to the gateway. */
  gatewayStalled: boolean;
  /** Verdicts of other frames of this page (a remote machine's runtime iframe) about their own sockets and requests. */
  remote: Record<string, TNetworkQuality>;
  /**
   * The machine of the conversation on screen is down and the page says so in its own banner: its sockets
   * failing to come back is that, not the network, and «Reconnecting…» beside the banner read as a second fault.
   */
  machineDown: boolean;
  setMachineDown: (down: boolean) => void;
  recordLatency: (ms: number, via?: TLatencyVia) => void;
  setReconnecting: (key: string, reconnecting: boolean) => void;
  setStalled: (stalled: boolean, via?: TLatencyVia) => void;
  setRemote: (key: string, quality: TNetworkQuality) => void;
}

export const useNetworkHealthStore = create<INetworkHealthState>((set) => ({
  samples: [],
  gatewaySamples: [],
  reconnecting: {},
  stalled: false,
  gatewayStalled: false,
  remote: {},
  machineDown: false,
  setMachineDown: (down) => set((state) => (state.machineDown === down ? state : { machineDown: down })),
  recordLatency: (ms, via = 'machine') =>
    set((state) =>
      via === 'gateway'
        ? { gatewaySamples: [...state.gatewaySamples, ms].slice(-NETWORK_HEALTH.windowSize) }
        : { samples: [...state.samples, ms].slice(-NETWORK_HEALTH.windowSize) },
    ),
  setStalled: (stalled, via = 'machine') =>
    set((state) => {
      if (via === 'gateway') return state.gatewayStalled === stalled ? state : { gatewayStalled: stalled };
      return state.stalled === stalled ? state : { stalled };
    }),
  setReconnecting: (key, reconnecting) =>
    set((state) => {
      if (reconnecting === Boolean(state.reconnecting[key])) return state;
      const next = { ...state.reconnecting };
      if (reconnecting) next[key] = true;
      else delete next[key];
      return { reconnecting: next };
    }),
  setRemote: (key, quality) =>
    set((state) => {
      if ((state.remote[key] ?? 'ok') === quality) return state;
      const next = { ...state.remote };
      if (quality === 'ok') delete next[key];
      else next[key] = quality;
      return { remote: next };
    }),
}));

const QUALITY_RANK: Record<TNetworkQuality, number> = { ok: 0, machine: 1, slow: 2, reconnecting: 3 };

export const selectNetworkQuality = (state: INetworkHealthState): TNetworkQuality => {
  if (state.machineDown) return 'ok';
  const local = networkQuality(
    state.samples,
    Object.keys(state.reconnecting).length > 0,
    state.stalled,
    state,
  );
  return Object.values(state.remote).reduce(
    (worst, quality) => (QUALITY_RANK[quality] > QUALITY_RANK[worst] ? quality : worst),
    local,
  );
};

/** The runtime iframe of a remote machine tells the page around it how its link feels; the header chip lives there. */
export const NETWORK_MESSAGE_TYPE = 'hl:network-quality';

const pageHidden = () => typeof document !== 'undefined' && document.visibilityState === 'hidden';

interface IOpenSample {
  startedAt: number;
  hidesBefore: number;
  stallAfterMs: number;
  via: TLatencyVia;
}

const openSamples = new Set<IOpenSample>();
let hideCount = 0;
let stallCheck: ReturnType<typeof setTimeout> | null = null;

/** Only samples that never overlapped a hidden tab count; the check re-arms for the next one that could stall. */
const refreshStalled = () => {
  if (stallCheck !== null) clearTimeout(stallCheck);
  stallCheck = null;
  const now = performance.now();
  const stalled: Record<TLatencyVia, boolean> = { machine: false, gateway: false };
  let nextDue = Number.POSITIVE_INFINITY;
  if (!pageHidden()) {
    for (const sample of openSamples) {
      if (sample.hidesBefore !== hideCount) continue;
      const due = sample.startedAt + sample.stallAfterMs;
      if (due <= now) stalled[sample.via] = true;
      else nextDue = Math.min(nextDue, due);
    }
  }
  const store = useNetworkHealthStore.getState();
  store.setStalled(stalled.machine, 'machine');
  store.setStalled(stalled.gateway, 'gateway');
  if (stalled.machine) probeGateway();
  if (Number.isFinite(nextDue)) stallCheck = setTimeout(refreshStalled, nextDue - now);
};

/** Answered by the gateway without asking any machine; a runtime served on its own answers it too. */
export const GATEWAY_PING_PATH = '/api/platform/gateway-ping';
let lastProbeAt = Number.NEGATIVE_INFINITY;

/** While the machine looks slow, asks the gateway once in a while: its answer says whether the network is to blame. */
const probeGateway = () => {
  if (typeof window === 'undefined' || typeof fetch !== 'function' || pageHidden()) return;
  const now = performance.now();
  if (now - lastProbeAt < NETWORK_HEALTH.gatewayProbeGapMs) return;
  lastProbeAt = now;
  const sample = startLatencySample({ via: 'gateway' });
  fetch(prefixAppPath(GATEWAY_PING_PATH), { cache: 'no-store' })
    .then((response) => {
      if (response.ok) sample.finish();
    })
    .catch(() => undefined)
    .finally(sample.cancel);
};

let watchingVisibility = false;
const watchVisibility = () => {
  if (watchingVisibility || typeof document === 'undefined') return;
  watchingVisibility = true;
  document.addEventListener('visibilitychange', () => {
    if (pageHidden()) hideCount += 1;
    refreshStalled();
  });
};

export interface ILatencySample {
  /** The answer is in: records the round trip. */
  finish: () => void;
  /** The round trip says nothing about the link (failed, aborted, waited on a slow server): drops it. A no-op after `finish`. */
  cancel: () => void;
}

const IGNORED_SAMPLE: ILatencySample = { finish: () => undefined, cancel: () => undefined };

/**
 * Starts timing one round trip. Every sample must end in `finish` or `cancel` (use `finally`):
 * while it is open longer than `stallAfterMs` the link reads as slow.
 * Samples that overlap a hidden tab are dropped: a sleeping laptop is not a slow network.
 * `stallAfterMs` is for requests the server may legitimately hold longer than a plain round trip.
 */
export const startLatencySample = ({
  stallAfterMs = NETWORK_HEALTH.stalledAfterMs,
  via = 'machine',
}: { stallAfterMs?: number; via?: TLatencyVia } = {}): ILatencySample => {
  watchVisibility();
  if (pageHidden()) return IGNORED_SAMPLE;
  const sample: IOpenSample = { startedAt: performance.now(), hidesBefore: hideCount, stallAfterMs, via };
  openSamples.add(sample);
  refreshStalled();
  const settle = (record: boolean) => {
    if (!openSamples.delete(sample)) return;
    if (record && hideCount === sample.hidesBefore && !pageHidden()) {
      const store = useNetworkHealthStore.getState();
      store.recordLatency(performance.now() - sample.startedAt, via);
      if (via === 'machine' && isSlowNetwork(useNetworkHealthStore.getState().samples)) probeGateway();
    }
    refreshStalled();
  };
  return { finish: () => settle(true), cancel: () => settle(false) };
};

const reconnectShownAfter = new Map<string, ReturnType<typeof setTimeout>>();

/** The telemetry counts every drop at once; the page shows only one that lasts past the grace period. */
export const reportReconnecting = (key: string, reconnecting: boolean) => {
  recordSocketState(key, reconnecting);
  const pending = reconnectShownAfter.get(key);
  if (!reconnecting) {
    if (pending !== undefined) clearTimeout(pending);
    reconnectShownAfter.delete(key);
    useNetworkHealthStore.getState().setReconnecting(key, false);
    return;
  }
  if (pending !== undefined || useNetworkHealthStore.getState().reconnecting[key]) return;
  reconnectShownAfter.set(
    key,
    setTimeout(() => {
      reconnectShownAfter.delete(key);
      useNetworkHealthStore.getState().setReconnecting(key, true);
    }, NETWORK_HEALTH.reconnectGraceMs),
  );
};
