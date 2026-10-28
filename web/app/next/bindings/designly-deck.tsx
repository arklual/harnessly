// A deck open over the machine: the project of the kind `slides` from the library, its state from the deck routes
// (kept in stores/deck.ts and read again while the deck is on screen) and the deck's shell. designly-site.tsx renders
// it for such a project, in the window and in the right pane, and hands it the preview slot it opened.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import DeckView, { type IDeckViewProps } from '@/next/features/designly/deck/deck-view';
import { fetchDeck } from '@/next/features/designly/deck/api';
import { staleSlidesKey } from '@/next/features/designly/deck/deck-making';
import { setDeck, useDeck } from '@/next/stores/deck';

export interface IDesignlyDeckProps extends IDeckViewProps {
  /** The deck has slides the project's boards do not list yet (the agent made them): the library is to be read again. */
  onStale: () => void;
}

/** The deck is read again this often while its page is visible, ms (the runtime's `rev` tells a change). */
const POLL_MS = 3000;

/** Reads the deck's state into the store and keeps it fresh; answers why a read failed, null while it is fine. */
const useDeckState = (machine: IDeckViewProps['machine'], id: string): string | null => {
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let abort: AbortController | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let live = true;
    const read = () => {
      if (timer) clearTimeout(timer);
      timer = null;
      abort?.abort();
      const now = new AbortController();
      abort = now;
      fetchDeck(machine, id, now.signal).then(
        (state) => {
          if (!live) return;
          setDeck(id, state);
          setError(null);
          schedule();
        },
        (failure: Error) => {
          if (!live || now.signal.aborted) return;
          setError(failure.message);
          schedule();
        },
      );
    };
    const schedule = () => {
      if (live && document.visibilityState === 'visible') timer = setTimeout(read, POLL_MS);
    };
    const visible = () => {
      if (document.visibilityState === 'visible') read();
      else if (timer) clearTimeout(timer);
    };
    read();
    document.addEventListener('visibilitychange', visible);
    return () => {
      live = false;
      abort?.abort();
      if (timer) clearTimeout(timer);
      document.removeEventListener('visibilitychange', visible);
    };
  }, [machine, id]);
  return error;
};

const DesignlyDeck = ({ onStale, ...props }: IDesignlyDeckProps) => {
  const deckError = useDeckState(props.machine, props.id);
  // The deck is read every few seconds, the library only on demand: a slide the agent has just made shows up here first.
  const stale = staleSlidesKey(useDeck(props.id)?.slides, props.boards);
  const staleRef = useRef(onStale);
  useLayoutEffect(() => {
    staleRef.current = onStale;
  });
  useEffect(() => {
    if (stale !== '') staleRef.current();
  }, [stale]);
  return <DeckView {...props} deckError={deckError} />;
};

export default DesignlyDeck;
