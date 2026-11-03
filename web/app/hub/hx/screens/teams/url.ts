// The state of the team screens that lives in the address: which card is open (`p` a person, `t` a team, `dlg` a
// drawer) and which tenant a team page shows (`tn`). Opening pushes an entry, so Back closes; closing goes back when
// this screen pushed the entry, and rewrites it otherwise (a link opened straight on a card).
import { useCallback } from 'react';

import { useLocation, useQuery } from '../../../lib/router';

const MARK = 'hx-teams';

const write = (next: URLSearchParams, mode: 'push' | 'replace') => {
  const s = next.toString();
  const url = `${window.location.pathname}${s ? `?${s}` : ''}`;
  if (mode === 'push') window.history.pushState({ [MARK]: true }, '', url);
  else window.history.replaceState(window.history.state, '', url);
  // The router reads the address on popstate; this tells it the address changed.
  window.dispatchEvent(new PopStateEvent('popstate'));
};

/** `cards`: the params that open a card over the screen (the rest of the address is the page itself). */
export const useUrl = (cards: readonly string[]) => {
  const q = useQuery();
  const { path } = useLocation();
  const get = (k: string): string | null => q.get(k);

  /** Sets (string) or removes (null) params; a new card is a new entry, a change inside one is not. */
  const open = useCallback(
    (patch: Record<string, string | null>) => {
      const next = new URLSearchParams(window.location.search);
      const wasOpen = cards.some((k) => next.has(k));
      for (const [k, v] of Object.entries(patch)) {
        if (v === null) next.delete(k);
        else next.set(k, v);
      }
      write(next, wasOpen ? 'replace' : 'push');
    },
    [cards],
  );

  const close = useCallback(() => {
    const hist = window.history.state as Record<string, unknown> | null;
    if (hist && hist[MARK]) {
      window.history.back();
      return;
    }
    const next = new URLSearchParams(window.location.search);
    for (const k of cards) next.delete(k);
    write(next, 'replace');
  }, [cards]);

  /** Changes a param that is not a card (the tenant tab): no new entry. */
  const put = useCallback((patch: Record<string, string | null>) => {
    const next = new URLSearchParams(window.location.search);
    for (const [k, v] of Object.entries(patch)) {
      if (v === null) next.delete(k);
      else next.set(k, v);
    }
    write(next, 'replace');
  }, []);

  return { get, open, close, put, path };
};
