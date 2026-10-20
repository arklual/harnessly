import { useCallback, useEffect, useState } from 'react';
import { VAULT_CHANGED_EVENT, fetchVaultEntries, fetchVaultStatus } from '@/lib/hl-vault-client';
import type { IVaultEntry, TVaultStatus } from '@/types/vault';
import { t } from '@/lib/i18n';

export type TVaultView =
  | { kind: 'loading' }
  | { kind: 'off'; reason: string }
  | { kind: 'failed'; message: string }
  | { kind: 'ready'; status: Extract<TVaultStatus, { enabled: true }>; entries: IVaultEntry[] };

/** The person's safe as the gateway lists it (never a value), reloaded after any change made on this page. */
const useVaultEntries = (enabled = true) => {
  const [view, setView] = useState<TVaultView>({ kind: 'loading' });
  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    const load = async () => {
      const status = await fetchVaultStatus();
      if (!status.enabled) {
        if (alive) setView({ kind: 'off', reason: status.reason });
        return;
      }
      try {
        const entries = await fetchVaultEntries();
        if (alive) setView({ kind: 'ready', status, entries });
      } catch (error) {
        if (alive)
          setView({
            kind: 'failed',
            message: error instanceof Error ? error.message : t('hooks.useVaultEntries.noAnswer'),
          });
      }
    };
    void load();
    return () => {
      alive = false;
    };
  }, [enabled, version]);

  useEffect(() => {
    const reload = () => setVersion((value) => value + 1);
    window.addEventListener(VAULT_CHANGED_EVENT, reload);
    return () => window.removeEventListener(VAULT_CHANGED_EVENT, reload);
  }, []);

  const refresh = useCallback(() => setVersion((value) => value + 1), []);
  return { view, refresh };
};

export default useVaultEntries;
