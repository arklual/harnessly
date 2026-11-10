import { useEffect, useEffectEvent } from 'react';
import { eventsUrlOf } from '@/next/features/designly/guest/guest-config';

/**
 * HRN-881. The hub says when the project of a live link reaches a new revision of the store (`event: rev`, the
 * revision's number); the board on screen is then loaded again. The first event is the revision the page opened at.
 * A pinned link, or a hub that reads the project from the owner's machine, answers 204 and the page stops asking.
 */
export const useGuestRevision = (filesBase: string, onNew: () => void): void => {
  const changed = useEffectEvent(onNew);
  useEffect(() => {
    if (typeof EventSource === 'undefined') return;
    const source = new EventSource(eventsUrlOf(filesBase));
    let seen: string | null = null;
    source.addEventListener('rev', (event) => {
      const rev = (event as MessageEvent<string>).data;
      if (seen !== null && rev !== seen) changed();
      seen = rev;
    });
    return () => source.close();
  }, [filesBase]);
};
