// A failed take recognized again: one request per take whoever asks (the person's «Распознать», or the field
// by itself once the link is back), and its state for the bar over the field — busy while the machine recognizes, the
// reason in words when it could not. A second ask while one runs joins it instead of being dropped.
import { createKeyedStore } from '@/next/core/store';
import { dropTake, finishTake } from '@/next/features/composer/voice-takes';

export const RETRY = { busy: 'busy', failed: 'failed' } as const;

export type RetryState = { k: typeof RETRY.busy } | { k: typeof RETRY.failed; reason: string };

export const OUTCOME = { said: 'said', silence: 'silence', failed: 'failed' } as const;

export type RetryOutcome =
  | { k: typeof OUTCOME.said; text: string }
  | { k: typeof OUTCOME.silence }
  | { k: typeof OUTCOME.failed; reason: string };

/** Per take: being recognized, or failed the last time with the reason. No entry: not asked yet, or over. */
export const retryStore = createKeyedStore<string, RetryState>();

const inflight = new Map<string, Promise<RetryOutcome>>();

/**
 * Recognizes the take `id` with `run`. What was said goes to `onSaid` (the field) and the take is done; silence drops
 * it; a failure keeps it with the reason. A call while one runs for the same take returns that one's outcome.
 */
export const recognizeTake = (
  id: string,
  run: () => Promise<string>,
  onSaid: (text: string) => void,
  reasonOf: (failure: unknown) => string,
): Promise<RetryOutcome> => {
  const running = inflight.get(id);
  if (running) return running;
  retryStore.set(id, { k: RETRY.busy });
  const outcome = (async (): Promise<RetryOutcome> => {
    try {
      const said = (await run()).trim();
      retryStore.delete(id);
      if (!said) {
        dropTake(id);
        return { k: OUTCOME.silence };
      }
      onSaid(said);
      finishTake(id, said);
      return { k: OUTCOME.said, text: said };
    } catch (failure) {
      const reason = reasonOf(failure);
      retryStore.set(id, { k: RETRY.failed, reason });
      return { k: OUTCOME.failed, reason };
    } finally {
      inflight.delete(id);
    }
  })();
  inflight.set(id, outcome);
  return outcome;
};

/** The take is deleted by the person: its state goes with it. */
export const forgetRetry = (id: string) => retryStore.delete(id);
