/**
 * A conversation the shell has just created: its first message and the moment of the start. The shell
 * writes it right after «New conversation», the conversation's panel reads it to show that message at once with the
 * launch under it, instead of a blank frame, a spinner and an empty timeline in turn. sessionStorage is shared by
 * the shell and its same-origin runtime iframes; the key carries the machine because tab ids repeat across machines.
 */

const PREFIX = 'hl:pending-launch:';
/** A launch older than this never reached its panel; its message is not shown any more. */
export const PENDING_LAUNCH_MAX_AGE_MS = 5 * 60_000;
const MAX_LENGTH = 20_000;

export interface IPendingLaunch {
  prompt: string;
  at: number;
}

/**
 * The launch before its tab exists: the shell writes it when the request goes out, because the runtime may show the new
 * tab to its frame before the request returns. Only a tab without a session yet adopts it.
 */
export const UNASSIGNED_TAB = '*';
export const UNASSIGNED_MAX_AGE_MS = 15_000;

const keyOf = (machineId: string | undefined, tabId: string) => `${PREFIX}${machineId ?? ''}:${tabId}`;

/**
 * Calls back when the shell writes or clears a launch. A runtime frame can open the new tab before the shell's
 * request returns, so a panel that found nothing on mount learns about its launch from here.
 */
export const onPendingLaunchChange = (callback: () => void): (() => void) => {
  const handle = (event: StorageEvent) => {
    if (event.storageArea === sessionStorage && (event.key === null || event.key.startsWith(PREFIX)))
      callback();
  };
  window.addEventListener('storage', handle);
  return () => window.removeEventListener('storage', handle);
};

export const setPendingLaunch = (
  machineId: string | undefined,
  tabId: string,
  prompt: string,
  now = Date.now(),
): void => {
  if (!tabId) return;
  const text = prompt.trim().slice(0, MAX_LENGTH);
  try {
    sessionStorage.setItem(
      keyOf(machineId, tabId),
      JSON.stringify({ prompt: text, at: now } satisfies IPendingLaunch),
    );
  } catch {}
};

const readKey = (key: string, now: number): IPendingLaunch | null => {
  const raw = sessionStorage.getItem(key);
  if (raw === null) return null;
  const saved = JSON.parse(raw) as Partial<IPendingLaunch>;
  if (
    typeof saved.prompt !== 'string' ||
    typeof saved.at !== 'number' ||
    now - saved.at > PENDING_LAUNCH_MAX_AGE_MS
  ) {
    sessionStorage.removeItem(key);
    return null;
  }
  return { prompt: saved.prompt, at: saved.at };
};

const launchKeys = (): string[] => {
  const keys: string[] = [];
  for (let i = 0; i < sessionStorage.length; i++) {
    const key = sessionStorage.key(i);
    if (key?.startsWith(PREFIX)) keys.push(key);
  }
  return keys;
};

/**
 * Reads without removing: the panel may remount while Claude starts, the launch stays until its message arrives.
 * The shell names the machine by the conversation, a frame of the shell's own machine may know none; the tab id,
 * random per tab, finds the launch then.
 */
export const readPendingLaunch = (
  machineId: string | undefined,
  tabId: string | null | undefined,
  now = Date.now(),
): IPendingLaunch | null => {
  if (!tabId) return null;
  try {
    const own = readKey(keyOf(machineId, tabId), now);
    if (own || tabId === UNASSIGNED_TAB) return own;
    const other = launchKeys().find((key) => key.endsWith(`:${tabId}`));
    return other ? readKey(other, now) : null;
  } catch {
    return null;
  }
};

export const clearPendingLaunch = (machineId: string | undefined, tabId: string): void => {
  try {
    sessionStorage.removeItem(keyOf(machineId, tabId));
  } catch {}
};

/** The newest launch of a machine, for a frame that has not picked its tab yet; only a just-started one counts. */
export const latestPendingLaunch = (
  machineId: string | undefined,
  maxAgeMs: number,
  now = Date.now(),
): IPendingLaunch | null => {
  // A frame that knows its machine looks only there; one that does not takes the newest launch of the shell.
  const prefix = machineId ? `${PREFIX}${machineId}:` : PREFIX;
  let latest: IPendingLaunch | null = null;
  try {
    for (const key of launchKeys()) {
      if (!key.startsWith(prefix)) continue;
      const launch = readKey(key, now);
      if (launch && now - launch.at <= maxAgeMs && (!latest || launch.at > latest.at)) latest = launch;
    }
  } catch {}
  return latest;
};
