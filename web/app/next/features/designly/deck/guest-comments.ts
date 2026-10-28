// The guests' comments of a deck as the gateway lists them, read every 20 s while some part of the deck's screen wants
// them (the panel, the badge, the pins) and shared by all of them: one poll per deck, however many readers. `list` is
// null until the first answer, and null again where there is no gateway (loading is not an empty list).
import { createKeyedStore, flush, useKey } from '@/next/core/store';
import { fetchGuestComments, type IGuestCommentOwned } from '@/next/features/designly/access/api';
import { useEffect } from 'react';

export const GUEST_POLL_MS = 20_000;

interface IGuestRead {
  list: readonly IGuestCommentOwned[] | null;
}

const store = createKeyedStore<string, IGuestRead>();
const polls = new Map<string, { readers: number; timer: number; abort: AbortController }>();

const read = (artifact: string, abort: AbortController) => {
  void fetchGuestComments(artifact, abort.signal).then(
    (list) => {
      if (abort.signal.aborted) return;
      store.set(artifact, { list });
      flush();
    },
    () => undefined,
  );
};

/** The guests' comments; null before the first answer or without a gateway. Polls while mounted. */
export const useGuestComments = (artifact: string, on = true): readonly IGuestCommentOwned[] | null => {
  useEffect(() => {
    if (!on) return;
    const have = polls.get(artifact);
    if (have) {
      have.readers += 1;
    } else {
      const abort = new AbortController();
      read(artifact, abort);
      polls.set(artifact, {
        readers: 1,
        abort,
        timer: window.setInterval(() => read(artifact, abort), GUEST_POLL_MS),
      });
    }
    return () => {
      const poll = polls.get(artifact);
      if (!poll) return;
      poll.readers -= 1;
      if (poll.readers > 0) return;
      poll.abort.abort();
      window.clearInterval(poll.timer);
      polls.delete(artifact);
    };
  }, [artifact, on]);
  return useKey(store, on ? artifact : null)?.list ?? null;
};
