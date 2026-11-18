// What the chat's delegated chats made, off GET …/made, where the runtime marks each thing with
// the subchat it came through: the merge requests go to the block under a finished task, and the rest stand under the
// card «Поручено» of the subchat that made them. Asked when the chat's agent is done and as its subchats move.
import { useEffect, useState } from 'react';
import { loadMade } from '@/next/commands/pages';
import type { TurnOutput } from '@/next/features/chat/types';
import type { ILiftedOutput } from '@/next/features/chat/delegation';
import type { ConvId, MachineId, MadeFrom, MadeItem, MadeKind, OutputKind } from '@/next/protocol/v2';
import { hasFeature } from '@/next/stores/machines';

const NONE: readonly MadeItem[] = [];
/** The pause after a subchat moved before the list is asked again. */
export const INHERITED_DEBOUNCE_MS = 1000;

/** Two answers of …/made that say the same: the items are plain data off the wire. */
const sameMade = (a: readonly MadeItem[], b: readonly MadeItem[]): boolean =>
  a.length === b.length && a.every((item, index) => JSON.stringify(item) === JSON.stringify(b[index]));

/** The card a made thing is in the feed: a document or a board is a page of the portal, a design is its id, a file
 *  handed over is its path. */
const OUTPUT_OF = {
  doc: 'artifact',
  board: 'artifact',
  design: 'design',
  task: 'task',
  mr: 'mr',
  stand: 'stand',
  commit: 'commit',
  file: 'delivery',
} as const satisfies Record<MadeKind, OutputKind>;

/** What one subchat made, the newest first, as the feed's cards; the chat's own things and subagents' are not here. */
export const subchatOutputs = (items: readonly MadeItem[], subchat: string): TurnOutput[] =>
  items
    .filter((item) => item.from?.kind === 'subchat' && item.from.id === subchat)
    .map((item) => {
      const output: TurnOutput = {
        kind: OUTPUT_OF[item.kind],
        op: item.op,
        ref: item.ref,
        title: item.title,
      };
      // A file handed over is named by its file, not its whole path.
      return item.kind === 'file'
        ? { ...output, name: item.ref.slice(item.ref.lastIndexOf('/') + 1) }
        : output;
    });

/** A turn of the chat as the lifting needs it: its key in the feed and when it ran (endedAt null: still running). */
export interface ITurnSpan {
  key: string;
  startedAt: number;
  endedAt: number | null;
}

/** The made thing as a card of the feed, with who made it for the mark «из подчата …». */
const outputOf = (item: MadeItem & { from: MadeFrom }): ILiftedOutput => {
  const output: ILiftedOutput = {
    kind: OUTPUT_OF[item.kind],
    op: item.op,
    ref: item.ref,
    title: item.title,
    via: item.from,
  };
  return item.kind === 'file' ? { ...output, name: item.ref.slice(item.ref.lastIndexOf('/') + 1) } : output;
};

/**
 * What the chat's subchats (and theirs, and its subagents) made, by the turn of this chat it belongs to:
 * the person works in the root chat, so a thing a helper made shows in «Сделано за ход» of the turn that was running
 * or had just ended when it was made: the first turn that ended at or after it, the running one, or — made after the
 * last turn — the last one. Each thing once, by its latest change.
 */
export const liftedByTurn = (
  items: readonly MadeItem[],
  turns: readonly ITurnSpan[],
): Map<string, ILiftedOutput[]> => {
  const out = new Map<string, ILiftedOutput[]>();
  if (turns.length === 0) return out;
  const ordered = [...turns].sort((a, b) => a.startedAt - b.startedAt);
  const latest = new Map<string, MadeItem & { from: MadeFrom }>();
  for (const item of items) {
    if (!item.from) continue;
    const key = `${item.kind}:${item.ref}`;
    const seen = latest.get(key);
    if (!seen || item.at > seen.at) latest.set(key, { ...item, from: item.from });
  }
  for (const item of latest.values()) {
    const turn =
      ordered.find((t) => t.endedAt === null || t.endedAt >= item.at) ?? ordered[ordered.length - 1];
    if (!turn) continue;
    const list = out.get(turn.key) ?? [];
    list.push(outputOf(item));
    out.set(turn.key, list);
  }
  return out;
};

/**
 * The things the chat inherits. `on`: there is a reason to ask (the agent is done, or the chat delegated); `moved`
 * changes as the chat and its subchats move, and each change asks again a second later.
 */
export const useInheritedMade = (
  machine: MachineId,
  conv: ConvId,
  on: boolean,
  moved: string,
): readonly MadeItem[] => {
  const [made, setMade] = useState<{ conv: ConvId; list: readonly MadeItem[] }>({ conv, list: NONE });
  const announced = hasFeature(machine, 'made');
  useEffect(() => {
    if (!on || !announced) return;
    let live = true;
    const timer = window.setTimeout(() => {
      void loadMade(machine, conv).then((outcome) => {
        if (!live || !('ok' in outcome)) return;
        const list = outcome.value.items.filter((item) => item.from);
        // The same answer keeps the same list: a new one, even empty, redraws every turn of the feed.
        setMade((current) =>
          current.conv === conv && sameMade(current.list, list)
            ? current
            : { conv, list: list.length ? list : NONE },
        );
      });
    }, INHERITED_DEBOUNCE_MS);
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [machine, conv, on, announced, moved]);
  return made.conv === conv ? made.list : NONE;
};
