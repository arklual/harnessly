// The owner looks at a design from the hub's store, not from their machine: the store's newest revision of
// the project, followed live. The hub streams it (`design/store/<project>/events`); a project the store has nothing of
// yet is asked about again now and then (the machine's sync puts it there), so the next look goes to the hub.

import { useEffect, useState } from 'react';
import { storeEventsHref } from '@/next/features/designly/api';
import type { MachineId } from '@/next/protocol/v2';

export const STORE_AT = { asking: 'asking', none: 'none', at: 'at' } as const;

export type TStoreAt =
  { k: typeof STORE_AT.asking } | { k: typeof STORE_AT.none } | { k: typeof STORE_AT.at; rev: number };

/** No answer of the hub within this long: the design opens from the machine meanwhile. */
export const STORE_ASK_MS = 4000;
/** How often a project the store has not got is asked about again (doubling up to the last). */
export const STORE_AGAIN_MS = [15_000, 30_000, 60_000] as const;

const ASKING: TStoreAt = { k: STORE_AT.asking };
const NONE: TStoreAt = { k: STORE_AT.none };

/** Reads a `rev` event's data: the revision's number, null for anything else. */
export const readStoreRev = (data: unknown): number | null => {
  const n = typeof data === 'string' ? Number(data) : NaN;
  return Number.isSafeInteger(n) && n > 0 ? n : null;
};

/**
 * The project's newest revision in the hub's store while `on`; what was learnt stays when it is off (the mode turned
 * to editing and back). Without EventSource it is none: the machine is asked, as before.
 */
export const useStoreHead = (m: MachineId, id: string, on: boolean): TStoreAt => {
  const [seen, setSeen] = useState<{ key: string; at: TStoreAt }>({ key: '', at: ASKING });
  const key = `${m}|${id}`;
  useEffect(() => {
    if (!on || typeof EventSource === 'undefined') return;
    let source: EventSource | null = null;
    let again = 0;
    let misses = 0;
    let got = false;
    const asked = window.setTimeout(() => {
      if (!got) setSeen((was) => (was.key === key && was.at.k === STORE_AT.at ? was : { key, at: NONE }));
    }, STORE_ASK_MS);
    const open = () => {
      const es = new EventSource(storeEventsHref(id));
      source = es;
      es.addEventListener('rev', (event) => {
        const rev = readStoreRev((event as MessageEvent<unknown>).data);
        if (rev === null) return;
        got = true;
        misses = 0;
        setSeen((was) =>
          was.key === key && was.at.k === STORE_AT.at && was.at.rev === rev
            ? was
            : { key, at: { k: STORE_AT.at, rev } },
        );
      });
      es.onerror = () => {
        // A dropped stream reconnects by itself; a refused one (404: not in the store, no hub) is closed for good and
        // asked about again later.
        if (es.readyState !== EventSource.CLOSED) return;
        if (!got) setSeen({ key, at: NONE });
        const wait = STORE_AGAIN_MS[Math.min(misses, STORE_AGAIN_MS.length - 1)];
        misses += 1;
        again = window.setTimeout(open, wait);
      };
    };
    open();
    return () => {
      window.clearTimeout(asked);
      window.clearTimeout(again);
      source?.close();
    };
  }, [m, id, key, on]);
  if (typeof EventSource === 'undefined') return NONE;
  return seen.key === key ? seen.at : ASKING;
};
