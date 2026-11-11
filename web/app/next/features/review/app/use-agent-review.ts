// «Пройти ревью»: the request goes to the chat beside the review, and the chat's answers are read back until one has a
// block of suggestions. What the person did with a suggestion (taken into the draft, applied, thrown away) is kept in
// this browser: it only hides the card, the suggestion itself stays in the chat.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { latestSuggestions, type IAgentSuggestion } from '@/next/features/review/app/agent-review';

export interface IAgentAnswer {
  at: number;
  text: string;
}

export interface IAgentReviewIo {
  /** The agent's answers in the chat, oldest first; null when the chat could not be read. */
  read: () => Promise<readonly IAgentAnswer[] | null>;
  send: (text: string) => void;
}

export const AGENT_STATE = { idle: 'idle', asking: 'asking', failed: 'failed' } as const;
export type TAgentState = (typeof AGENT_STATE)[keyof typeof AGENT_STATE];

export interface IAgentReviewApi {
  state: TAgentState;
  /** The suggestions of the agent's newest block that the person has not settled yet. */
  suggestions: readonly IAgentSuggestion[];
  /** The agent answered with a block and nothing is left in it. */
  done: boolean;
  ask: (request: string) => void;
  settle: (id: string) => void;
}

const POLL_MS = 5000;
const GIVE_UP_MS = 20 * 60_000;
const KEPT = 400;

const storeKey = (scope: string) => `hx-review-agent:${scope}`;

const readSettled = (scope: string): ReadonlySet<string> => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(storeKey(scope)) ?? '[]');
    return new Set(Array.isArray(raw) ? raw.filter((v): v is string => typeof v === 'string') : []);
  } catch {
    return new Set();
  }
};

const keepSettled = (scope: string, ids: ReadonlySet<string>) => {
  try {
    localStorage.setItem(storeKey(scope), JSON.stringify([...ids].slice(-KEPT)));
  } catch {
    // Without storage the choice holds for this visit only.
  }
};

export const useAgentReview = (scope: string, io: IAgentReviewIo | null): IAgentReviewApi | null => {
  const [found, setFound] = useState<{ scope: string; list: IAgentSuggestion[] | null } | null>(null);
  const [asked, setAsked] = useState<{ scope: string; at: number } | null>(null);
  const [failed, setFailed] = useState(false);
  const [settled, setSettled] = useState<{ scope: string; ids: ReadonlySet<string> }>(() => ({
    scope,
    ids: readSettled(scope),
  }));
  if (settled.scope !== scope) setSettled({ scope, ids: readSettled(scope) });

  const askedAt = asked?.scope === scope ? asked.at : null;

  // Read once when the review opens (an earlier pass may be in the chat already), and while an answer is awaited.
  useEffect(() => {
    if (!io) return;
    let live = true;
    let timer = 0;
    const pass = async () => {
      const answers = await io.read();
      if (!live) return;
      if (answers === null) {
        if (askedAt === null) setFailed(true);
      } else {
        setFailed(false);
        const fresh = askedAt === null ? answers : answers.filter((a) => a.at >= askedAt);
        const list = latestSuggestions(fresh.map((a) => a.text));
        if (list !== null) {
          setFound({ scope, list });
          if (askedAt !== null) setAsked(null);
          return;
        }
        if (askedAt === null) setFound({ scope, list: null });
      }
      if (askedAt !== null && Date.now() - askedAt < GIVE_UP_MS)
        timer = window.setTimeout(() => void pass(), POLL_MS);
      else if (askedAt !== null) setAsked(null);
    };
    void pass();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [io, scope, askedAt]);

  const ask = useCallback(
    (request: string) => {
      if (!io) return;
      setAsked({ scope, at: Date.now() });
      io.send(request);
    },
    [io, scope],
  );

  const settle = useCallback(
    (id: string) =>
      setSettled((s) => {
        const ids = new Set(s.ids).add(id);
        keepSettled(s.scope, ids);
        return { scope: s.scope, ids };
      }),
    [],
  );

  return useMemo(() => {
    if (!io) return null;
    const list = found?.scope === scope ? found.list : null;
    const suggestions = (list ?? []).filter((s) => !settled.ids.has(s.id));
    let state: TAgentState = AGENT_STATE.idle;
    if (askedAt !== null) state = AGENT_STATE.asking;
    else if (failed) state = AGENT_STATE.failed;
    return { state, suggestions, done: list !== null && suggestions.length === 0, ask, settle };
  }, [io, found, scope, settled, askedAt, failed, ask, settle]);
};
