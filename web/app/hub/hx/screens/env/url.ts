// The state of the environment screen that lives in the address: `es=<entryId>` is the open entry, `catalog=1` the open
// catalog. Opening pushes an entry so Back closes; closing goes back when this screen pushed it and rewrites the address
// otherwise (a link opened straight on a card).
import { useCallback } from 'react';

import { useQuery } from '../../../lib/router';

const MARK = 'hx-env';
export const CARDS = ['es', 'catalog'] as const;

const write = (next: URLSearchParams, mode: 'push' | 'replace') => {
  const s = next.toString();
  const url = `${window.location.pathname}${s ? `?${s}` : ''}`;
  if (mode === 'push') window.history.pushState({ [MARK]: true }, '', url);
  else window.history.replaceState(window.history.state, '', url);
  // The router reads the address on popstate; this tells it the address changed.
  window.dispatchEvent(new PopStateEvent('popstate'));
};

export const useEnvUrl = () => {
  const q = useQuery();
  const es = q.get('es');
  const catalog = q.get('catalog') === '1';

  const open = useCallback((patch: Record<string, string | null>) => {
    const next = new URLSearchParams(window.location.search);
    const wasOpen = CARDS.some((k) => next.has(k));
    for (const [k, v] of Object.entries(patch)) {
      if (v === null) next.delete(k);
      else next.set(k, v);
    }
    write(next, wasOpen ? 'replace' : 'push');
  }, []);

  const close = useCallback(() => {
    const hist = window.history.state as Record<string, unknown> | null;
    if (hist && hist[MARK]) {
      window.history.back();
      return;
    }
    const next = new URLSearchParams(window.location.search);
    for (const k of CARDS) next.delete(k);
    write(next, 'replace');
  }, []);

  return {
    es,
    catalog,
    openEntry: (id: string) => open({ es: id, catalog: null }),
    openCatalog: () => open({ catalog: '1', es: null }),
    close,
  };
};
