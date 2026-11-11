// A merge request's review in the screen: read once per link and again on demand, changed only by what the code host
// answered to an act (the answer is the whole review as it is after it).
import { useCallback, useEffect, useMemo, useState } from 'react';
import type { IMrContext } from '@/next/features/review/app/mr-threads';
import {
  forgeOf,
  type IMrAct,
  type IMrConnection,
  type IMrReview,
  type TMrAnswer,
  type TMrRefusal,
} from '@/next/features/review/app/mr-review';

export type TMrReviewState =
  | { k: 'loading' }
  | { k: 'refused'; refusal: TMrRefusal }
  | { k: 'ready'; connection: IMrConnection; review: IMrReview };

export interface IMrReviewIo {
  load: (url: string) => Promise<TMrAnswer>;
  act: (url: string, act: IMrAct) => Promise<TMrAnswer>;
}

export interface IMrReviewApi {
  url: string;
  state: TMrReviewState;
  /** Does an act; answers the refusal when GitLab or the runtime did not, null when done. */
  act: (act: IMrAct) => Promise<TMrRefusal | null>;
  reload: () => void;
}

export const useMrReview = (url: string, io: IMrReviewIo): IMrReviewApi => {
  const [loaded, setLoaded] = useState<{ url: string; state: TMrReviewState } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    void io.load(url).then((answer) => {
      if (!live) return;
      setLoaded({
        url,
        state: answer.ok
          ? { k: 'ready', connection: answer.connection, review: answer.review }
          : { k: 'refused', refusal: answer.refusal },
      });
    });
    return () => {
      live = false;
    };
  }, [url, io, attempt]);
  const state = useMemo<TMrReviewState>(
    () => (loaded?.url === url ? loaded.state : { k: 'loading' }),
    [loaded, url],
  );

  const act = useCallback(
    async (a: IMrAct) => {
      const answer = await io.act(url, a);
      if (!answer.ok) return answer.refusal;
      setLoaded({ url, state: { k: 'ready', connection: answer.connection, review: answer.review } });
      return null;
    },
    [io, url],
  );
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  return useMemo(() => ({ url, state, act, reload }), [url, state, act, reload]);
};

/** The screen's context of a review the code host answered; null while it is read or when no connection reaches it. */
export const useMrContext = (
  mr: IMrReviewApi,
  onConnect: () => void,
  ask?: (text: string) => void,
  agent?: Pick<IMrContext, 'analyze' | 'quoted'>,
): IMrContext | null => {
  const analyze = agent?.analyze;
  const quoted = agent?.quoted;
  return useMemo(
    () =>
      mr.state.k === 'ready'
        ? {
            mr,
            review: mr.state.review,
            writable: mr.state.connection.personal,
            forge: forgeOf(mr.url),
            onConnect,
            where: mr.url,
            ...(ask ? { ask } : {}),
            ...(analyze ? { analyze } : {}),
            ...(quoted ? { quoted } : {}),
          }
        : null,
    [mr, onConnect, ask, analyze, quoted],
  );
};
