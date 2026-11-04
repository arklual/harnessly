// The entries of the person's safe, for the «Войти из сейфа» button of the pane's browser. Loaded once, when the first
// button asks; null while they load, [] when the safe is off or does not answer (the button then has nothing to offer).
// Reloaded when the person changes the safe (VAULT_CHANGED_EVENT).
import { useSyncExternalStore } from 'react';
import {
  fetchVaultEnabled,
  fetchVaultEntries,
  VAULT_CHANGED_EVENT,
  type IVaultEntry,
} from '@/next/core/net/vault';

const listeners = new Set<() => void>();
let entries: readonly IVaultEntry[] | null = null;
let started = false;
let round = 0;

const load = async (): Promise<void> => {
  const mine = ++round;
  let next: readonly IVaultEntry[] = [];
  try {
    if (await fetchVaultEnabled()) next = await fetchVaultEntries();
  } catch {
    // The safe is off or out of reach: nothing to offer.
  }
  if (mine !== round) return;
  entries = next;
  for (const listener of [...listeners]) listener();
};

export const vaultEntriesSnapshot = (): readonly IVaultEntry[] | null => {
  if (!started) {
    started = true;
    void load();
  }
  return entries;
};

/** Forgets what was loaded, for tests. */
export const resetVaultEntries = (): void => {
  entries = null;
  started = false;
  round++;
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  const reload = () => void load();
  window.addEventListener(VAULT_CHANGED_EVENT, reload);
  return () => {
    listeners.delete(listener);
    window.removeEventListener(VAULT_CHANGED_EVENT, reload);
  };
};

export const useVaultEntries = (): readonly IVaultEntry[] | null =>
  useSyncExternalStore(subscribe, vaultEntriesSnapshot, vaultEntriesSnapshot);
