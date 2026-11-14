// A file the agent hands over (deliver_file) downloads by itself, at once: the person asked for it, so the
// browser saves it without a second ask, and the card stays in the chat to open it or save it again. Only a new, live
// hand-over does: not the history the page draws on opening, not an older page loaded above, not the same file in a
// second tab of this browser, and not a page the person is not looking at (another device in a pocket, a hidden tab).
import { useEffect, useRef } from 'react';
import type { FeedItem, TurnOutput } from '@/next/features/chat/types';
import type { ConvId, Ms } from '@/next/protocol/v2';

/** The hand-over as one key: the same file handed over again unchanged is not saved twice. */
export const deliveryKey = (output: TurnOutput): string => `${output.ref}\n${output.sha256 ?? ''}`;

/** Every hand-over of the drawn turns. */
const deliveriesIn = (items: readonly FeedItem[]) =>
  items.flatMap((item) =>
    item.kind === 'turn'
      ? (item.turn.outputs ?? [])
          .filter((output) => output.kind === 'delivery')
          .map((output) => ({ output, endedAt: item.turn.endedAt }))
      : [],
  );

/**
 * The hand-overs not seen before, which are now marked seen. One of a turn that ended before the page subscribed is
 * history (an older page loaded above): seen, not fresh.
 */
export const freshDeliveries = (
  items: readonly FeedItem[],
  seen: Set<string>,
  subscribedAt: Ms,
): TurnOutput[] => {
  const fresh: TurnOutput[] = [];
  for (const { output, endedAt } of deliveriesIn(items)) {
    const key = deliveryKey(output);
    if (seen.has(key)) continue;
    seen.add(key);
    if (endedAt === null || endedAt >= subscribedAt) fresh.push(output);
  }
  return fresh;
};

const CLAIM_PREFIX = 'hx-delivered:';
/** How long a claim keeps a second tab from saving the same file. */
const CLAIM_MS = 10 * 60_000;

/** Only one tab of this browser saves a hand-over: the first to write its claim. */
export const claimDownload = (conv: ConvId, key: string, now: number): boolean => {
  try {
    const name = `${CLAIM_PREFIX}${conv}\n${key}`;
    const held = localStorage.getItem(name);
    if (held !== null && now - Number(held) < CLAIM_MS) return false;
    localStorage.setItem(name, String(now));
    return true;
  } catch {
    // No storage (a private window): this tab saves it.
    return true;
  }
};

/** Several files handed over at once go one after another, so the browser takes each. */
const GAP_MS = 400;

export interface IAutoDownload {
  conv: ConvId;
  /** null: the feed is not drawn yet. */
  items: readonly FeedItem[] | null;
  /** The page has the machine's own snapshot (not a cached copy); null before. */
  subscribedAt: Ms | null;
  live: boolean;
  /** Where the file is saved from. */
  hrefOf: (output: TurnOutput) => string;
  save: (href: string) => void;
}

export const useAutoDownload = ({ conv, items, subscribedAt, live, hrefOf, save }: IAutoDownload) => {
  const seen = useRef<{ conv: ConvId; keys: Set<string> } | null>(null);
  // Runs on every render it is given new functions: a hand-over once seen is never taken again.
  useEffect(() => {
    if (!live || subscribedAt === null || items === null) return;
    // The first live picture of the chat is what was there before: seen, never saved.
    if (seen.current?.conv !== conv) {
      const keys = new Set<string>();
      freshDeliveries(items, keys, Number.POSITIVE_INFINITY);
      seen.current = { conv, keys };
      return;
    }
    const fresh = freshDeliveries(items, seen.current.keys, subscribedAt);
    if (fresh.length === 0 || document.visibilityState !== 'visible') return;
    const now = Date.now();
    const mine = fresh.filter((output) => claimDownload(conv, deliveryKey(output), now));
    // The timers are not cleared by the next render: a file once taken is saved whatever the page does next.
    mine.forEach((output, i) => window.setTimeout(() => save(hrefOf(output)), i * GAP_MS));
  }, [conv, items, subscribedAt, live, hrefOf, save]);
};
