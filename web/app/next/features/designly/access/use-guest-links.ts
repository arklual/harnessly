// The links without sign-in of a project: loading, absent (the gateway has none: only invites are offered) or ready.
import { useEffect, useState } from 'react';
import { fetchGuestLinks, type IGuestLink } from '@/next/features/designly/access/api';

export type TGuestLinks = { k: 'loading' } | { k: 'absent' } | { k: 'ready'; links: IGuestLink[] };

export const useGuestLinks = (project: string) => {
  const [state, setState] = useState<TGuestLinks>({ k: 'loading' });
  useEffect(() => {
    const abort = new AbortController();
    fetchGuestLinks(project, abort.signal).then(
      (links) => setState(links ? { k: 'ready', links } : { k: 'absent' }),
      () => undefined,
    );
    return () => abort.abort();
  }, [project]);
  return { state, put: (links: IGuestLink[]) => setState({ k: 'ready', links }) };
};
