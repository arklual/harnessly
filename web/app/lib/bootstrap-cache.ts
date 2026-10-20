/**
 * The last bootstrap answer of each machine, so a repeat visit draws the working
 * screen at once while the fresh answer loads in the background. An entry belongs to one
 * person: the gateway names them in the `hl-who` cookie on every page it serves, and an
 * entry written for anyone else is dropped unread. Pages the runtime serves itself (one
 * owner per runtime) carry no cookie and share the empty name.
 */
const KEY_PREFIX = 'hl:bootstrap:';
const WHO_COOKIE = 'hl-who';
/** Bump when the bootstrap answer changes shape so a page never renders from an older one. */
const VERSION = 1;

interface ICacheEntry<T> {
  v: number;
  who: string;
  data: T;
}

const storageKey = (machineId: string | undefined) => `${KEY_PREFIX}${machineId ?? ''}`;

export const currentWho = (): string => {
  const pair = document.cookie.split('; ').find((part) => part.startsWith(`${WHO_COOKIE}=`));
  return pair ? decodeURIComponent(pair.slice(WHO_COOKIE.length + 1)) : '';
};

export const clearBootstrapCache = () => {
  try {
    for (let i = localStorage.length - 1; i >= 0; i -= 1) {
      const key = localStorage.key(i);
      if (key?.startsWith(KEY_PREFIX)) localStorage.removeItem(key);
    }
  } catch {
    /* storage unavailable */
  }
};

export const readCachedBootstrap = <T extends object>(machineId: string | undefined): T | null => {
  try {
    const raw = localStorage.getItem(storageKey(machineId));
    if (!raw) return null;
    const entry = JSON.parse(raw) as Partial<ICacheEntry<T>>;
    if (entry.who !== currentWho()) {
      clearBootstrapCache();
      return null;
    }
    if (entry.v !== VERSION || !entry.data || typeof entry.data !== 'object') {
      localStorage.removeItem(storageKey(machineId));
      return null;
    }
    return entry.data;
  } catch {
    return null;
  }
};

export const writeCachedBootstrap = <T extends object>(machineId: string | undefined, data: T) => {
  try {
    const entry: ICacheEntry<T> = { v: VERSION, who: currentWho(), data };
    localStorage.setItem(storageKey(machineId), JSON.stringify(entry));
  } catch {
    /* quota or storage unavailable: the next visit waits for the network */
  }
};
