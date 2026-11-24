// The word that the settings' journal or the agents' proposals moved on a machine (cmd/runtime: settings-watch,
// HRN-1270): one request per machine, however many readers, and only while the page is visible. A reader reads
// its list again when the tick moves, instead of asking every few seconds. A machine that has no such address (an
// older runtime) is ticked now and then, so its readers still see what changed.
import { useCallback, useSyncExternalStore } from 'react';
import { getPage, SHORT_POLL_MS, usesHttp1 } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { answer, isRefused, str } from '@/next/features/settings/platform-answer';
import type { MachineId } from '@/next/protocol/v2';

/** Longer than the runtime holds a watch (settingsWatchMax, 25 s). */
const WATCH_TIMEOUT_MS = 35_000;
/** A machine that did not answer the watch is asked again, and its readers ticked, this often. */
export const QUIET_TICK_MS = 60_000;

interface IEntry {
  tick: number;
  rev: string | null;
  listeners: Set<() => void>;
  running: boolean;
  timer: ReturnType<typeof setTimeout> | null;
}

const entries = new Map<MachineId, IEntry>();

const entryOf = (machine: MachineId): IEntry => {
  let entry = entries.get(machine);
  if (!entry) {
    entry = { tick: 0, rev: null, listeners: new Set(), running: false, timer: null };
    entries.set(machine, entry);
  }
  return entry;
};

const move = (entry: IEntry) => {
  entry.tick += 1;
  entry.listeners.forEach((fn) => fn());
};

const readRev = answer((o) => str(o.rev, 'rev'));

const run = async (machine: MachineId): Promise<void> => {
  const entry = entryOf(machine);
  if (entry.running) return;
  const shortPoll = usesHttp1();
  entry.running = true;
  while (entry.listeners.size > 0 && document.visibilityState === 'visible') {
    const query = shortPoll || entry.rev === null ? '' : `?rev=${encodeURIComponent(entry.rev)}`;
    const res = await getPage(
      runtimePath(platformPath(`/settings-watch${query}`), machine),
      readRev,
      WATCH_TIMEOUT_MS,
    );
    if (!('ok' in res) || isRefused(res.value)) {
      // No watch on this machine: its readers read again at a quiet pace.
      entry.running = false;
      entry.timer = setTimeout(() => {
        entry.timer = null;
        if (entry.listeners.size === 0) return;
        if (document.visibilityState === 'visible') move(entry);
        void run(machine);
      }, QUIET_TICK_MS);
      return;
    }
    const moved = entry.rev !== null && entry.rev !== res.value;
    entry.rev = res.value;
    if (moved) move(entry);
    if (shortPoll) {
      entry.timer = setTimeout(() => {
        entry.timer = null;
        void run(machine);
      }, SHORT_POLL_MS);
      break;
    }
  }
  entry.running = false;
};

let listening = false;
/** A page that comes back into view reads again at once: what moved while it was hidden was not watched. */
const onVisible = () => {
  if (document.visibilityState !== 'visible') return;
  for (const [machine, entry] of entries) {
    if (entry.listeners.size === 0) continue;
    move(entry);
    if (entry.timer === null) void run(machine);
  }
};

/** A number that moves when the settings' journal or the proposals of the machine moved. */
export const useSettingsTick = (machine: MachineId): number => {
  const subscribe = useCallback(
    (fn: () => void) => {
      const entry = entryOf(machine);
      entry.listeners.add(fn);
      if (!listening) {
        listening = true;
        document.addEventListener('visibilitychange', onVisible);
      }
      if (entry.timer === null) void run(machine);
      return () => {
        entry.listeners.delete(fn);
        if (entry.listeners.size === 0 && entry.timer) {
          clearTimeout(entry.timer);
          entry.timer = null;
        }
      };
    },
    [machine],
  );
  return useSyncExternalStore(subscribe, () => entryOf(machine).tick);
};
