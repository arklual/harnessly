/**
 * A journal of what the page shows and why, to take apart a jerky load: which screen the timeline drew,
 * when each socket connected, what the first answers carried. Off by default; `?trace=1` turns it on for this browser
 * (`?trace=0` off), and `__tkTraceDump()` in the console copies the journal.
 */
interface ITraceEvent {
  /** Milliseconds since the page started. */
  t: number;
  area: string;
  event: string;
  data?: Record<string, unknown>;
}

const KEY = 'hl:trace';
const LIMIT = 800;

declare global {
  interface Window {
    __tkTrace?: ITraceEvent[];
    __tkTraceDump?: () => string;
  }
}

let enabled: boolean | null = null;

const isEnabled = (): boolean => {
  if (enabled !== null) return enabled;
  if (typeof window === 'undefined') return false;
  try {
    const flag = new URLSearchParams(window.location.search).get('trace');
    if (flag === '1') window.localStorage.setItem(KEY, '1');
    if (flag === '0') window.localStorage.removeItem(KEY);
    enabled = window.localStorage.getItem(KEY) === '1';
  } catch {
    enabled = false;
  }
  if (enabled) {
    window.__tkTrace = [];
    window.__tkTraceDump = () => {
      const text = JSON.stringify(window.__tkTrace ?? [], null, 1);
      void navigator.clipboard?.writeText(text).catch(() => undefined);
      return text;
    };
  }
  return enabled;
};

export const trace = (area: string, event: string, data?: Record<string, unknown>): void => {
  if (!isEnabled()) return;
  const journal = window.__tkTrace ?? (window.__tkTrace = []);
  journal.push({ t: Math.round(performance.now()), area, event, ...(data ? { data } : {}) });
  if (journal.length > LIMIT) journal.splice(0, journal.length - LIMIT);
  console.debug(`[trace] ${area}:${event}`, data ?? '');
};

/** A value the page renders from, traced each time it changes (call during render with the current value). */
const lastSeen = new Map<string, string>();
export const traceChange = (area: string, name: string, value: unknown): void => {
  if (!isEnabled()) return;
  const key = `${area}:${name}`;
  const text = JSON.stringify(value);
  if (lastSeen.get(key) === text) return;
  lastSeen.set(key, text);
  trace(area, name, { value });
};
