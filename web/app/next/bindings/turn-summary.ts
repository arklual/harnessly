// A finished turn's summary (HRN-957): when a chat's turn ends, the page asks its machine for a few lines on what the
// agent did (the cheapest model writes them, once a turn, whichever page asks); they come back on the record as its
// recap and go onto the turn's notice as `summary`. Settings → Notifications turns the asking off. A machine that
// cannot make them refuses, and the notice keeps the start of the answer.
import { useEffect } from 'react';
import { summarizeTurn } from '@/next/commands';
import { noticeEventOf } from '@/next/features/notifications/notice-events';
import type { Conversation } from '@/next/protocol/v2';
import { convsLoadedStore, useConversationList } from '@/next/stores/conversations';
import { NOTICE_KIND, setNoticeSummary, useNotify } from '@/next/stores/notices';

/** A turn that ended longer ago than this is history, not news: the page does not ask for it. */
const FRESH_MS = 10 * 60_000;

/** The turns this page asked about: one ask a turn, whatever the answer. */
const asked = new Set<string>();

/** The last turn's summary on the record: a recap written after the turn ended (the cheap model's or Claude's own). */
export const turnSummaryOf = (conv: Conversation): string | null => {
  const turn = conv.lastTurn;
  if (!turn || turn.status !== 'done' || turn.endedAt === null || !conv.recap) return null;
  return conv.recap.at >= turn.endedAt ? conv.recap.text : null;
};

export const useTurnSummaries = (): void => {
  const { summary: on } = useNotify();
  const convs = useConversationList(null);

  useEffect(() => {
    if (!on || !convs) return;
    const now = Date.now();
    for (const conv of convs) {
      const turn = conv.lastTurn;
      if (!turn || turn.status !== 'done' || turn.endedAt === null) continue;
      // The list the browser kept is not what happened: only the machine's own snapshot and what follows it are.
      if (!convsLoadedStore.has(conv.machineId)) continue;
      const summary = turnSummaryOf(conv);
      if (summary) {
        const event = noticeEventOf(conv, true);
        if (event?.kind === NOTICE_KIND.done) setNoticeSummary(event.key, summary);
        continue;
      }
      const key = `${conv.id}:${turn.id}`;
      if (now - turn.endedAt > FRESH_MS || asked.has(key)) continue;
      asked.add(key);
      summarizeTurn(conv.machineId, conv.id, turn.id);
    }
  }, [on, convs]);
};
