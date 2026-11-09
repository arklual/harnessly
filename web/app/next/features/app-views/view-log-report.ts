// What a view reported, handed to the machine: its console errors and warnings, uncaught exceptions, rejected
// promises and failed bridge calls go to the runtime, where `harnessly app logs <id>`, `app explain` and the runtime's
// own log read them. Gathered for a moment and sent in one request; bounded, so a view that throws in a loop costs a
// few requests and no more.
import { reportViewLog, type IViewLogLine } from '@/next/features/app-views/api';
import type { IBridgeLogEntry } from '@/next/features/app-views/bridge';
import type { MachineId } from '@/next/protocol/v2';

/** How long lines gather before they are sent; how many one request carries; how many may wait at most. */
export const VIEW_LOG_FLUSH_MS = 2000;
export const VIEW_LOG_BATCH = 50;
export const VIEW_LOG_QUEUE = 200;
/** The longest text sent, in characters (the runtime cuts at 1000 too). */
export const VIEW_LOG_TEXT = 1000;

interface IQueue {
  machine: MachineId;
  app: string;
  view: string;
  lines: IViewLogLine[];
}

const queues = new Map<string, IQueue>();
let timer: ReturnType<typeof setTimeout> | null = null;
let send: typeof reportViewLog = reportViewLog;

/** Tests put their own sender. */
export const setViewLogSender = (next: typeof reportViewLog | null): void => {
  send = next ?? reportViewLog;
};

/** Sends what waits now. */
export const flushViewLog = (): void => {
  if (timer !== null) clearTimeout(timer);
  timer = null;
  for (const [key, q] of queues) {
    queues.delete(key);
    for (let i = 0; i < q.lines.length; i += VIEW_LOG_BATCH) {
      void send(q.machine, q.app, q.view, q.lines.slice(i, i + VIEW_LOG_BATCH)).catch(() => undefined);
    }
  }
};

/** Puts one line of a view in the queue; past VIEW_LOG_QUEUE waiting lines of a view, more are dropped. */
export const queueViewLog = (machine: MachineId, app: string, view: string, line: IViewLogLine): void => {
  const key = `${machine}|${app}|${view}`;
  const q = queues.get(key) ?? { machine, app, view, lines: [] };
  if (q.lines.length >= VIEW_LOG_QUEUE) return;
  q.lines.push({ ...line, text: line.text.slice(0, VIEW_LOG_TEXT) });
  queues.set(key, q);
  timer ??= setTimeout(flushViewLog, VIEW_LOG_FLUSH_MS);
};

/** A line of the view's console (harnessly/debug.log) as the machine keeps it; a level it does not keep is null. */
export const consoleLine = (level: string, text: string, at: number): IViewLogLine | null =>
  level === 'error' || level === 'warn' ? { level, text, at } : null;

/** A failed bridge call as the machine keeps it; a call that went through is null. */
export const bridgeLine = (entry: IBridgeLogEntry): IViewLogLine | null => {
  if (entry.ok || entry.method.startsWith('console.')) return null;
  const why = [entry.code, entry.message].filter(Boolean).join(': ');
  return { level: 'bridge', text: why ? `${entry.method} — ${why}` : entry.method, at: entry.at };
};
