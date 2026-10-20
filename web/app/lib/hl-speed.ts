/**
 * Speed of the platform as the person's own browser sees it, reported to a PostHog project when the
 * runtime names one (RUNTIME_SPEED_POSTHOG_HOST and _KEY); nothing is sent otherwise.
 * Pure parts only: what counts as a long task, when the page became interactive, how events are batched and
 * what device they came from. The browser side lives in `hl-speed-client.ts`.
 *
 * Events carry timings and technical properties only: never a prompt, an answer, a title or a URL.
 */

export const SPEED_EVENT = 'platform_speed';
export const LONG_TASKS_EVENT = 'platform_long_tasks';

export type TSpeedMetric =
  | 'ttfb'
  | 'fcp'
  | 'lcp'
  | 'inp'
  | 'cls'
  /** Navigation start to the first paint of the workspace instead of the splash. */
  | 'shell'
  /** Navigation start to the first paint of the workspace with its conversation list: the working screen. */
  | 'first_screen'
  /** Navigation start to the end of the last long task before a quiet window that follows the working screen. */
  | 'interactive'
  /** Selecting a conversation to the first paint of one of its timeline entries. */
  | 'conversation_open'
  /** Pressing send to the message showing up in the agent's transcript. */
  | 'prompt_delivery';

export type TSpeedProperties = Record<string, string | number | boolean | null>;

export interface ISpeedEvent {
  event: string;
  /** ISO time of the moment measured, not of the send. */
  timestamp: string;
  properties: TSpeedProperties;
}

/** A task that kept the main thread from answering input: a long task, a long animation frame or a timer drift. */
export interface IBusySpan {
  start: number;
  end: number;
  /** Time beyond the first 50 ms, the part input actually waited for. */
  blocking: number;
  /** The longest script of the frame, where the browser names it (long animation frames only). */
  script?: string | null;
  /** Long animation frames only: time in scripts (with the microtasks after them, where React renders a store update). */
  scriptMs?: number;
  /** Long animation frames only: style and layout at the end of the frame, after the scripts. */
  layoutMs?: number;
  /** Long animation frames only: style and layout a script forced by reading geometry. */
  forcedMs?: number;
}

export const LONG_TASK_MS = 50;

export interface ILongTaskSummary {
  lt_count: number;
  lt_total_ms: number;
  lt_blocking_ms: number;
  lt_max_ms: number;
  /** Tasks over 200 ms: the ones a person notices as a freeze rather than a stutter. */
  lt_over_200: number;
  /** The script that ran longest in the longest task: where the next look starts. */
  lt_max_script: string | null;
  /** What the longest task spent its time on: scripts, the frame's style and layout, layout forced by scripts. */
  lt_max_script_ms: number | null;
  lt_max_layout_ms: number | null;
  lt_max_forced_ms: number | null;
}

export interface ILongTaskAggregate {
  add: (span: IBusySpan) => void;
  /** The summary since the last drain, then starts over. */
  drain: () => ILongTaskSummary;
  readonly count: number;
}

const round = (value: number) => Math.round(value);

const EMPTY_SUMMARY: ILongTaskSummary = {
  lt_count: 0,
  lt_total_ms: 0,
  lt_blocking_ms: 0,
  lt_max_ms: 0,
  lt_over_200: 0,
  lt_max_script: null,
  lt_max_script_ms: null,
  lt_max_layout_ms: null,
  lt_max_forced_ms: null,
};

const roundOrNull = (value: number | undefined) => (value === undefined ? null : round(value));

export const createLongTaskAggregate = (): ILongTaskAggregate => {
  let summary: ILongTaskSummary = { ...EMPTY_SUMMARY };
  return {
    add: (span) => {
      const duration = Math.max(0, span.end - span.start);
      summary.lt_count += 1;
      summary.lt_total_ms += duration;
      summary.lt_blocking_ms += Math.max(0, span.blocking);
      if (duration >= summary.lt_max_ms) {
        summary.lt_max_script = span.script ?? null;
        summary.lt_max_script_ms = roundOrNull(span.scriptMs);
        summary.lt_max_layout_ms = roundOrNull(span.layoutMs);
        summary.lt_max_forced_ms = roundOrNull(span.forcedMs);
      }
      summary.lt_max_ms = Math.max(summary.lt_max_ms, duration);
      if (duration > 200) summary.lt_over_200 += 1;
    },
    drain: () => {
      const out: ILongTaskSummary = {
        lt_count: summary.lt_count,
        lt_total_ms: round(summary.lt_total_ms),
        lt_blocking_ms: round(summary.lt_blocking_ms),
        lt_max_ms: round(summary.lt_max_ms),
        lt_over_200: summary.lt_over_200,
        lt_max_script: summary.lt_max_script,
        lt_max_script_ms: summary.lt_max_script_ms,
        lt_max_layout_ms: summary.lt_max_layout_ms,
        lt_max_forced_ms: summary.lt_max_forced_ms,
      };
      summary = { ...EMPTY_SUMMARY };
      return out;
    },
    get count() {
      return summary.lt_count;
    },
  };
};

/** How long the main thread has to stay free of long tasks for the page to count as interactive, as in Lighthouse's TTI. */
export const QUIET_WINDOW_MS = 5_000;

/**
 * Time to interactive after `readyAt`: the end of the last busy span before the first quiet window of `quietMs`.
 * `null` while that window has not passed yet at `now`.
 */
export const interactiveAt = (
  readyAt: number,
  spans: readonly IBusySpan[],
  now: number,
  quietMs = QUIET_WINDOW_MS,
): number | null => {
  let candidate = readyAt;
  for (const span of [...spans].sort((a, b) => a.start - b.start)) {
    if (span.end <= candidate) continue;
    if (span.start - candidate >= quietMs) return candidate;
    candidate = Math.max(candidate, span.end);
  }
  return now - candidate >= quietMs ? candidate : null;
};

/**
 * A setTimeout that fires late by more than a long task means something held the main thread. Safari has neither
 * long tasks nor long animation frames, so this is how it reports at all.
 */
export const driftSpan = (expectedAt: number, firedAt: number): IBusySpan | null => {
  const late = firedAt - expectedAt;
  if (late <= LONG_TASK_MS) return null;
  return { start: expectedAt, end: firedAt, blocking: late - LONG_TASK_MS };
};

export type TDeviceType = 'desktop' | 'mobile' | 'tablet';

export interface IDeviceInfo {
  device: TDeviceType;
  os: 'ios' | 'android' | 'macos' | 'windows' | 'linux' | 'other';
  browser: 'chrome' | 'safari' | 'firefox' | 'edge' | 'electron' | 'other';
}

/** `touchPoints` tells an iPad, whose Safari introduces itself as a Mac, from a real Mac. */
export const describeDevice = (userAgent: string, touchPoints = 0): IDeviceInfo => {
  const ua = userAgent;
  const iPadAsMac = /Macintosh/.test(ua) && touchPoints > 1;
  const os: IDeviceInfo['os'] =
    /iPhone|iPad|iPod/.test(ua) || iPadAsMac
      ? 'ios'
      : /Android/.test(ua)
        ? 'android'
        : /Mac OS X|Macintosh/.test(ua)
          ? 'macos'
          : /Windows/.test(ua)
            ? 'windows'
            : /Linux|X11/.test(ua)
              ? 'linux'
              : 'other';
  const device: TDeviceType =
    /iPad/.test(ua) || iPadAsMac || (os === 'android' && !/Mobile/.test(ua))
      ? 'tablet'
      : /iPhone|iPod|Mobile/.test(ua)
        ? 'mobile'
        : 'desktop';
  const browser: IDeviceInfo['browser'] = /Electron\//.test(ua)
    ? 'electron'
    : /Edg\//.test(ua)
      ? 'edge'
      : /Firefox\/|FxiOS\//.test(ua)
        ? 'firefox'
        : /Chrome\/|CriOS\//.test(ua)
          ? 'chrome'
          : /Safari\//.test(ua) || os === 'ios'
            ? 'safari'
            : 'other';
  return { device, os, browser };
};

/** sendBeacon refuses bodies over 64 KiB; a timing event is a few hundred bytes, so this stays far from it. */
export const MAX_EVENTS_PER_REQUEST = 40;

export interface ISpeedQueueOptions {
  /** Hands one request's worth of events to the transport. */
  send: (events: ISpeedEvent[]) => void;
  /** Wait for more events before sending, so a page load goes out as one request. */
  delayMs?: number;
  setTimer?: (callback: () => void, ms: number) => unknown;
  clearTimer?: (handle: unknown) => void;
}

export interface ISpeedQueue {
  push: (event: ISpeedEvent) => void;
  /** Sends everything queued now: the page is being hidden and may never run another timer. */
  flush: () => void;
  readonly size: number;
}

export const createSpeedQueue = ({
  send,
  delayMs = 5_000,
  setTimer = (callback, ms) => setTimeout(callback, ms),
  clearTimer = (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
}: ISpeedQueueOptions): ISpeedQueue => {
  let queue: ISpeedEvent[] = [];
  let timer: unknown = null;

  const flush = () => {
    if (timer !== null) clearTimer(timer);
    timer = null;
    const pending = queue;
    queue = [];
    for (let i = 0; i < pending.length; i += MAX_EVENTS_PER_REQUEST)
      send(pending.slice(i, i + MAX_EVENTS_PER_REQUEST));
  };

  return {
    push: (event) => {
      queue.push(event);
      if (queue.length >= MAX_EVENTS_PER_REQUEST) flush();
      else if (timer === null) timer = setTimer(flush, delayMs);
    },
    flush,
    get size() {
      return queue.length;
    },
  };
};

/** The body PostHog's `/batch/` endpoint takes; the person and the common properties go into every event. */
export const batchBody = (
  apiKey: string,
  distinctId: string,
  common: TSpeedProperties,
  events: readonly ISpeedEvent[],
) => ({
  api_key: apiKey,
  batch: events.map((item) => ({
    event: item.event,
    timestamp: item.timestamp,
    properties: { ...common, ...item.properties, distinct_id: distinctId, $process_person_profile: false },
  })),
});

interface IScriptTimingLike {
  duration: number;
  forcedStyleAndLayoutDuration?: number;
  invoker?: string;
  invokerType?: string;
  sourceURL?: string;
  sourceFunctionName?: string;
}

export interface ILongFrameLike {
  startTime: number;
  duration: number;
  blockingDuration?: number;
  styleAndLayoutStart?: number;
  scripts?: readonly IScriptTimingLike[];
}

/** A long animation frame as a busy span, with where its time went. */
export const longFrameSpan = (entry: ILongFrameLike): IBusySpan => {
  const end = entry.startTime + entry.duration;
  const scripts = entry.scripts ?? [];
  return {
    start: entry.startTime,
    end,
    blocking: entry.blockingDuration ?? 0,
    script: describeScript(scripts),
    scriptMs: scripts.reduce((sum, script) => sum + script.duration, 0),
    layoutMs: entry.styleAndLayoutStart ? Math.max(0, end - entry.styleAndLayoutStart) : 0,
    forcedMs: scripts.reduce((sum, script) => sum + (script.forcedStyleAndLayoutDuration ?? 0), 0),
  };
};

/** «invoker · file:function» of the longest script, short enough for an event property. */
export const describeScript = (scripts: readonly IScriptTimingLike[] | undefined): string | null => {
  const longest = scripts?.reduce<IScriptTimingLike | null>(
    (best, script) => (!best || script.duration > best.duration ? script : best),
    null,
  );
  if (!longest) return null;
  const file = (longest.sourceURL ?? '').split('?')[0].split('/').pop() || '';
  const where = [file, longest.sourceFunctionName].filter(Boolean).join(':');
  return [longest.invoker || longest.invokerType, where].filter(Boolean).join(' · ').slice(0, 200) || null;
};
