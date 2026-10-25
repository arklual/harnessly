import { useSyncExternalStore } from 'react';

/** The phone layout of the new interface (mobile.css of the design): 900 px and narrower. */
export const PHONE_QUERY = '(max-width: 900px)';

/** Whether a media query matches now, kept current as the window changes. */
export const useMedia = (query: string): boolean =>
  useSyncExternalStore(
    (notify) => {
      const list = window.matchMedia(query);
      list.addEventListener('change', notify);
      return () => list.removeEventListener('change', notify);
    },
    () => window.matchMedia(query).matches,
    () => false,
  );

export const usePhone = (): boolean => useMedia(PHONE_QUERY);
