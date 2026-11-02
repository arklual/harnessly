// The public catalog: the search, and the install that waits for the machine. Nothing is guessed in between: the list is
// read again from the machine when it answers, and the apps list is told to read itself again.
import { useCallback, useEffect, useState } from 'react';
import { installPublic, searchPublic, type IPublicEntry } from '@/next/features/app-catalog/api';
import type { MachineId } from '@/next/protocol/v2';

/** How long the search waits for the person to stop typing. */
export const SEARCH_DELAY_MS = 250;

export interface IPublicState {
  /** null while it loads. */
  entries: IPublicEntry[] | null;
  query: string;
  setQuery: (q: string) => void;
  error: string | null;
  /** The app whose install is on its way. */
  busy: string | null;
  /** The words of the last refused install. */
  failure: string | null;
  install: (entry: IPublicEntry) => Promise<void>;
}

export const usePublicCatalog = (m: MachineId, onChanged: () => void): IPublicState => {
  const [entries, setEntries] = useState<IPublicEntry[] | null>(null);
  const [query, setQuery] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    const timer = setTimeout(() => {
      searchPublic(m, query).then(
        (next) => live && (setEntries(next), setError(null)),
        (e: Error) => live && setError(e.message),
      );
    }, SEARCH_DELAY_MS);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [m, query]);

  const install = useCallback(
    async (entry: IPublicEntry) => {
      if (busy !== null) return;
      setBusy(entry.id);
      setFailure(null);
      try {
        await installPublic(m, entry);
        setEntries(await searchPublic(m, query));
        onChanged();
      } catch (e) {
        setFailure(e instanceof Error ? e.message : String(e));
      } finally {
        setBusy(null);
      }
    },
    [m, query, busy, onChanged],
  );

  return { entries, query, setQuery, error, busy, failure, install };
};
