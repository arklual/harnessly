// How the page itself fares, once a minute in the machine's journal: the person says «it lags», and the agent
// reads what their own browser went through (harnessly_logs, ev "perf") instead of guessing on another machine.
//
// One line a minute while something happened: the slowest interactions (INP-like: the longest event of each key or
// click until its paint), long tasks and long animation frames with the scripts that made them, the renders of the
// heavy components (shell, dock, field, feed, sidebar), memory and DOM size, and the page's requests by route. A minute
// with slow input or a busy main thread is a warn, the rest is info.
//
// Cheap by design: observers the browser fills anyway, counters that are an addition, the DOM counted once a minute.
// Nothing the person typed: an interaction is its kind and the class of its target, a script is its file and function.
// Off with localStorage `hx-perf` = `0`.
import { journal } from '@/next/core/journal';

/** How often the summary is written. */
export const PERF_EVERY_MS = 60_000;
/** An event shorter than this is not looked at (the browser's own floor is 16). */
const EVENT_MIN_MS = 40;
/** A minute is a warn when its slowest interaction or its blocked time passes these. */
const WARN_INTERACTION_MS = 200;
const WARN_BLOCKING_MS = 2000;

/** The components whose renders are counted. */
export type TPerfPart = 'shell' | 'dock' | 'field' | 'feed' | 'sidebar';

const renders: Record<TPerfPart, number> = { shell: 0, dock: 0, field: 0, feed: 0, sidebar: 0 };
let on = false;

/** One render of a heavy component; a no-op while the journal of performance is off. */
export const countRender = (part: TPerfPart): void => {
  if (on) renders[part] += 1;
};

const requests = new Map<string, number>();

/** A route of the page's requests: the method and the path with its ids folded. */
export const routeOf = (method: string, path: string): string =>
  `${method} ${path
    .replace(/\/(tab|conv|c|m|u|up|d|p)[-_][A-Za-z0-9_-]+/g, '/$1-…')
    .replace(/\/[A-Za-z0-9_-]{16,}/g, '/…')
    .replace(/\/\d+(?=\/|$)/g, '/n')}`;

/** A request of the page (the journal's fetch sees each): counted by route for the minute. */
export const countRequest = (method: string, path: string): void => {
  if (!on) return;
  const key = routeOf(method, path);
  requests.set(key, (requests.get(key) ?? 0) + 1);
};

interface IMinute {
  interactions: Map<number, { dur: number; what: string }>;
  longTasks: number;
  longTaskMs: number;
  blockingMs: number;
  frames: number;
  frameMs: number;
  scripts: Map<string, number>;
}

const fresh = (): IMinute => ({
  interactions: new Map(),
  longTasks: 0,
  longTaskMs: 0,
  blockingMs: 0,
  frames: 0,
  frameMs: 0,
  scripts: new Map(),
});

let minute = fresh();

/** What an event acted on, without its text: the element and its first class. */
const targetOf = (t: EventTarget | null | undefined): string => {
  if (!(t instanceof Element)) return '';
  const cls = (t.getAttribute('class') ?? '').split(/\s+/).find((c) => c.startsWith('hx-'));
  return cls ? `.${cls}` : t.tagName.toLowerCase();
};

/** A script of a long animation frame: its file and function. */
interface ILoafScript {
  duration: number;
  sourceURL?: string;
  sourceFunctionName?: string;
  invoker?: string;
}

const scriptOf = (s: ILoafScript): string => {
  const file =
    (s.sourceURL ?? '')
      .split('/')
      .pop()
      ?.replace(/-[\w-]{8}\.js$/, '.js') ?? '';
  const fn = s.sourceFunctionName || s.invoker || '';
  return `${file}${fn ? `:${fn}` : ''}`.slice(0, 60) || '?';
};

const observe = (type: string, fn: (list: PerformanceObserverEntryList) => void, extra: object = {}) => {
  if (typeof PerformanceObserver === 'undefined') return null;
  if (!PerformanceObserver.supportedEntryTypes?.includes(type)) return null;
  try {
    const o = new PerformanceObserver(fn);
    o.observe({ type, buffered: false, ...extra } as PerformanceObserverInit);
    return o;
  } catch {
    return null;
  }
};

const ms = (n: number) => Math.round(n);
const pct = (sorted: number[], p: number) =>
  sorted[Math.min(sorted.length - 1, Math.floor(p * sorted.length))] ?? 0;
const top = (m: Map<string, number>, n: number, unit = '') =>
  [...m.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, n)
    .map(([k, v]) => `${k} ${ms(v)}${unit}`)
    .join(', ');

/** The summary line of the minute that ends, or null when nothing worth a line happened. */
export const summarize = (m: IMinute, counts: Record<TPerfPart, number>, reqs: Map<string, number>) => {
  const inter = [...m.interactions.values()].sort((a, b) => a.dur - b.dur);
  const worst = inter.at(-1);
  const reqTotal = [...reqs.values()].reduce((a, b) => a + b, 0);
  const rendersTotal = Object.values(counts).reduce((a, b) => a + b, 0);
  if (inter.length === 0 && m.longTasks === 0 && m.frames === 0 && reqTotal === 0 && rendersTotal === 0)
    return null;
  const memory = (globalThis.performance as Performance & { memory?: { usedJSHeapSize: number } }).memory;
  const parts = [
    // The slowest interaction first: it is what the person felt.
    inter.length > 0
      ? `input ${inter.length}>${EVENT_MIN_MS}ms p75=${ms(
          pct(
            inter.map((i) => i.dur),
            0.75,
          ),
        )} max=${ms(worst?.dur ?? 0)} (${worst?.what ?? ''})`
      : 'input ok',
    `long ${m.longTasks}/${ms(m.longTaskMs)}ms blocked=${ms(m.blockingMs)}ms`,
    m.frames > 0 ? `loaf ${m.frames}/${ms(m.frameMs)}ms [${top(m.scripts, 3, 'ms')}]` : '',
    `renders ${(Object.keys(counts) as TPerfPart[]).map((k) => `${k}=${counts[k]}`).join(' ')}`,
    `${memory ? `heap=${Math.round(memory.usedJSHeapSize / 1048576)}MB ` : ''}dom=${typeof document === 'undefined' ? '?' : document.getElementsByTagName('*').length}`,
    `req ${reqTotal}/min [${top(reqs, 4)}]`,
  ].filter(Boolean);
  const bad = (worst?.dur ?? 0) >= WARN_INTERACTION_MS || m.blockingMs >= WARN_BLOCKING_MS;
  return { level: bad ? ('warn' as const) : ('info' as const), text: parts.join(' · ') };
};

const disabled = (): boolean => {
  try {
    return localStorage.getItem('hx-perf') === '0';
  } catch {
    return false;
  }
};

/** Starts the minute's observers and the summary; returns the stop. */
export const startPerfJournal = (): (() => void) => {
  if (typeof window === 'undefined' || on || disabled()) return () => undefined;
  on = true;
  const observers = [
    observe(
      'event',
      (list) => {
        for (const e of list.getEntries() as PerformanceEventTiming[]) {
          const id = e.interactionId ?? 0;
          if (!id) continue;
          const had = minute.interactions.get(id);
          if (!had || e.duration > had.dur)
            minute.interactions.set(id, { dur: e.duration, what: `${e.name}${targetOf(e.target)}` });
        }
      },
      { durationThreshold: EVENT_MIN_MS },
    ),
    observe('longtask', (list) => {
      for (const e of list.getEntries()) {
        minute.longTasks += 1;
        minute.longTaskMs += e.duration;
        minute.blockingMs += Math.max(0, e.duration - 50);
      }
    }),
    observe('long-animation-frame', (list) => {
      for (const e of list.getEntries() as (PerformanceEntry & { scripts?: ILoafScript[] })[]) {
        minute.frames += 1;
        minute.frameMs += e.duration;
        for (const s of e.scripts ?? []) {
          const key = scriptOf(s);
          minute.scripts.set(key, (minute.scripts.get(key) ?? 0) + s.duration);
        }
        // A page open for hours: the scripts of the minute stay a short list.
        if (minute.scripts.size > 200) minute.scripts.clear();
      }
    }),
  ].filter((o) => o !== null);

  const flush = () => {
    const line = summarize(minute, renders, requests);
    minute = fresh();
    for (const k of Object.keys(renders) as TPerfPart[]) renders[k] = 0;
    requests.clear();
    if (line) journal({ level: line.level, src: 'web', ev: 'perf', text: line.text });
  };
  const timer = window.setInterval(() => {
    // A hidden page does nothing worth a line, and its timers are throttled anyway.
    if (document.visibilityState === 'visible') flush();
  }, PERF_EVERY_MS);
  return () => {
    window.clearInterval(timer);
    for (const o of observers) o.disconnect();
    on = false;
  };
};
