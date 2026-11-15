// The design the person asked to edit opens on the whole screen (features/designly/edit-ask.ts): the chat's
// last message of the person and the designs its agent works on (the pane intents `open design`) decide, once a message.
import { useEffect, useEffectEvent } from 'react';
import {
  actedOn,
  designFor,
  goesFull,
  markActed,
  type IAsk,
  type IDesignWork,
} from '@/next/features/designly/edit-ask';
import { entryStore, feedStore, key, turnStore, useFeedRows } from '@/next/stores/conversation';
import { ROUTE, useRoute } from '@/next/routes/routes';
import type { ConvId, Conversation } from '@/next/protocol/v2';
import type { IPaneIntent } from '@/next/features/pane/intents';

/** The person's last message in the chat's feed as the page has it; null for none. */
export const lastAsk = (conv: ConvId): IAsk | null => {
  const rows = feedStore.get(conv)?.rows ?? [];
  for (let i = rows.length - 1; i >= 0; i--) {
    const row = rows[i];
    const ids = row.k === 'turn' ? (turnStore.get(key(conv, row.id))?.entryIds ?? []) : [row.id];
    for (let j = ids.length - 1; j >= 0; j--) {
      const e = entryStore.get(key(conv, ids[j]));
      if (e?.t === 'user') return { id: e.id, text: e.text, at: e.at };
    }
  }
  return null;
};

/** Goes full screen with `go` when the chat's last message asks to edit a design its agent works on. */
export const useDesignFull = (
  /** The chat's record: its changes (a turn starts, a step comes) look again. */
  record: Conversation | null,
  works: readonly IDesignWork[],
  go: (design: string) => void,
): void => {
  const conv = record?.id ?? null;
  const rows = useFeedRows(conv);
  const route = useRoute();
  const onScreen = route.kind === ROUTE.site ? route.id : null;
  const act = useEffectEvent((id: ConvId) => {
    const ask = lastAsk(id);
    const design = designFor(ask, works);
    if (!ask || !design || !goesFull(ask, design, (x) => actedOn(id, x), Date.now())) return;
    markActed(id, ask.id);
    if (onScreen !== design) go(design);
  });
  useEffect(() => {
    if (conv) act(conv);
  }, [conv, record, rows, works]);
};

/** The designs the intents ask the pane to open (not reloads), with when each was asked. */
export const designWorksOf = (intents: readonly IPaneIntent[]): IDesignWork[] =>
  intents.flatMap(({ command, createdAt }) =>
    command.kind === 'open' && command.tab === 'design' && command.target && !command.reload
      ? [{ design: command.target, at: createdAt }]
      : [],
  );

export const sameWorks = (a: readonly IDesignWork[], b: readonly IDesignWork[]): boolean =>
  a.length === b.length && a.every((x, i) => x.design === b[i].design && x.at === b[i].at);

export const NO_WORKS: { conv: string | null; works: readonly IDesignWork[] } = { conv: null, works: [] };
