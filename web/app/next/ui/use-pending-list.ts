import { useCallback, useEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

export const PENDING = { busy: 'busy', slow: 'slow' } as const;
export type TPending = (typeof PENDING)[keyof typeof PENDING];

/** No answer by then: the row says it takes longer than usual. */
export const SLOW_MS = 3000;
/** No answer by then: the action is given up with a toast to try again. */
export const GIVE_UP_MS = 12_000;

class GaveUp extends Error {}

const reasonOf = (error: unknown): string | undefined => {
  if (error instanceof GaveUp) return t('next.pending.noAnswer');
  if (error instanceof Error && error.message) return error.message;
  return undefined;
};

/**
 * Pessimistic actions of a list: the item is `busy` from the click until the server answers, `slow` after 3 s, and
 * after 12 s the wait ends with a toast and «Повторить». The list itself changes only from the server's data, so a
 * failure leaves everything where it was.
 */
const usePendingList = () => {
  const [pending, setPending] = useState<ReadonlyMap<string, TPending>>(new Map());
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const mark = useCallback((key: string, value: TPending | null) => {
    if (!alive.current) return;
    setPending((current) => {
      const next = new Map(current);
      if (value) next.set(key, value);
      else next.delete(key);
      return next;
    });
  }, []);

  const run = useCallback(
    (key: string, failure: string, action: () => Promise<unknown>): Promise<boolean> => {
      // «Повторить» in the failure toast runs the same attempt again, under the same key.
      const attempt = async (): Promise<boolean> => {
        mark(key, PENDING.busy);
        const slow = setTimeout(() => mark(key, PENDING.slow), SLOW_MS);
        let giveUp: ReturnType<typeof setTimeout> | undefined;
        try {
          await Promise.race([
            action(),
            new Promise((_, reject) => {
              giveUp = setTimeout(() => reject(new GaveUp()), GIVE_UP_MS);
            }),
          ]);
          return true;
        } catch (error) {
          toast(failure, {
            tone: 'error',
            description: reasonOf(error),
            action: { label: t('next.pending.retry'), onClick: () => void attempt() },
          });
          return false;
        } finally {
          clearTimeout(slow);
          clearTimeout(giveUp);
          mark(key, null);
        }
      };
      return attempt();
    },
    [mark],
  );

  return { pending, run };
};

export default usePendingList;
