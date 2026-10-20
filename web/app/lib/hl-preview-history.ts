import { parsePreviewTarget, targetAddress, type IUrlTarget } from '@/lib/hl-preview-client';

/** The last addresses typed into the preview, per machine: a site is opened again in one click. */
export const URL_HISTORY_LIMIT = 5;

const storageKey = (machine: string): string => `hl-preview-urls:${machine}`;

/** The address goes to the front; the same address earlier in the list is dropped. */
export const rememberUrl = (history: readonly string[], address: string): string[] =>
  [address, ...history.filter((known) => known !== address)].slice(0, URL_HISTORY_LIMIT);

/** Storage is user data: only strings that still parse as an external address survive. */
export const readUrlHistory = (storage: Pick<Storage, 'getItem'> | null, machine: string): IUrlTarget[] => {
  let raw: string | null;
  try {
    raw = storage?.getItem(storageKey(machine)) ?? null;
  } catch {
    return [];
  }
  if (raw === null) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const targets: IUrlTarget[] = [];
  for (const entry of parsed) {
    if (typeof entry !== 'string') continue;
    let target;
    try {
      target = parsePreviewTarget(entry);
    } catch {
      continue;
    }
    if (target?.kind === 'url') targets.push(target);
  }
  return targets.slice(0, URL_HISTORY_LIMIT);
};

/** Returns the new list; a storage that refuses to write (private mode, quota) only loses the memory across reloads. */
export const writeUrlHistory = (
  storage: Pick<Storage, 'getItem' | 'setItem'> | null,
  machine: string,
  target: IUrlTarget,
): IUrlTarget[] => {
  const next = rememberUrl(readUrlHistory(storage, machine).map(targetAddress), targetAddress(target));
  try {
    storage?.setItem(storageKey(machine), JSON.stringify(next));
  } catch {
    // The list still serves this page view through the caller's state.
  }
  return next.flatMap((address) => {
    const parsed = parsePreviewTarget(address);
    return parsed?.kind === 'url' ? [parsed] : [];
  });
};
