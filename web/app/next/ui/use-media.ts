import { useSyncExternalStore } from 'react';

/** The phone layout of the new interface (mobile.css of the design): 900 px and narrower. */
export const PHONE_QUERY = '(max-width: 900px)';

interface IQuery {
  /** The window.matchMedia it was made with: a test that swaps it gets a fresh list. */
  source: typeof window.matchMedia;
  subscribe: (notify: () => void) => () => void;
  read: () => boolean;
}

// One list per query for the whole page, with a subscribe that keeps its identity: every popover of a long list asks
// whether it is on the phone, and a new MediaQueryList with a fresh listener on each render made switching a view of
// «Поделиться» stall (HRN-675).
const QUERIES = new Map<string, IQuery>();

const queryOf = (query: string): IQuery => {
  const known = QUERIES.get(query);
  if (known && known.source === window.matchMedia) return known;
  const list = window.matchMedia(query);
  const made: IQuery = {
    source: window.matchMedia,
    subscribe: (notify) => {
      list.addEventListener('change', notify);
      return () => list.removeEventListener('change', notify);
    },
    read: () => list.matches,
  };
  QUERIES.set(query, made);
  return made;
};

const serverFalse = () => false;

/** Whether a media query matches now, kept current as the window changes. */
export const useMedia = (query: string): boolean => {
  const { subscribe, read } = queryOf(query);
  return useSyncExternalStore(subscribe, read, serverFalse);
};

export const usePhone = (): boolean => useMedia(PHONE_QUERY);
