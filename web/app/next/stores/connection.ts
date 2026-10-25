// The page's view of each machine's socket (spec §2): connecting, live, stale, offline with the moment of the next
// try. The banner «Нет связи · через N с · Сейчас» reads it; «Сейчас» is reconnectNow.

import { useState } from 'react';
import { createKeyedStore, useKey } from '@/next/core/store';
import { t } from '@/next/i18n';
import type { Connection, MachineId, Ms } from '@/next/protocol/v2';

export const connectionStore = createKeyedStore<MachineId, Connection>();

export const useConnection = (m: MachineId | null): Connection | undefined => useKey(connectionStore, m);

/** Whole seconds until the next try, by the page's clock; null when no try is planned (unauthorized). */
export function secondsToRetry(c: Connection, now: Ms): number | null {
  if (c.k !== 'offline' || !Number.isFinite(c.retryAt)) return null;
  return Math.max(0, Math.ceil((c.retryAt - now) / 1000));
}

type Reconnect = (m: MachineId) => void;
let reconnect: Reconnect = () => {};

/** The realtime layer registers how «Сейчас» reconnects a machine. */
export function onReconnectNow(fn: Reconnect): void {
  reconnect = fn;
}

export const reconnectNow = (m: MachineId): void => reconnect(m);

/**
 * How many times the machine's link came back (stale or offline, then live) while this component was mounted. A load
 * that listed this in its dependencies asks again by itself when the link returns, quietly: what was on screen stays
 * until the new answer is there.
 */
export function useLinkRestored(m: MachineId | null): number {
  const connection = useConnection(m);
  const k = connection?.k;
  const [state, setState] = useState({ down: false, restored: 0 });
  if (k === 'stale' || k === 'offline') {
    if (!state.down) setState({ down: true, restored: state.restored });
  } else if (k === 'live' && state.down) {
    setState({ down: false, restored: state.restored + 1 });
  }
  return state.restored;
}

/** Whether the machine's link is down now (stale or offline); false while it is only connecting. */
export function useLinkDown(m: MachineId | null): boolean {
  const connection = useConnection(m);
  return connection?.k === 'stale' || connection?.k === 'offline';
}

/** The words for a link that is down; null when it is not. */
export function linkReason(c: Connection | undefined): string | null {
  if (c?.k === 'stale') return t('next.failure.timeout');
  if (c?.k !== 'offline') return null;
  return t(c.cause === 'machine-unreachable' ? 'next.connection.machineAway' : 'next.failure.network');
}
