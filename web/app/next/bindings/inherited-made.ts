// What the chat's delegated chats made, off GET …/made, where the runtime marks each thing with
// the subchat it came through: the merge requests go to the block under a finished task, and the rest stand under the
// card «Поручено» of the subchat that made them. Asked when the chat's agent is done and as its subchats move.
import { useEffect, useState } from 'react';
import { loadMade } from '@/next/commands/pages';
import type { TurnOutput } from '@/next/features/chat/types';
import type { ConvId, MachineId, MadeItem, MadeKind, OutputKind } from '@/next/protocol/v2';
import { hasFeature } from '@/next/stores/machines';

const NONE: readonly MadeItem[] = [];
/** The pause after a subchat moved before the list is asked again. */
export const INHERITED_DEBOUNCE_MS = 1000;

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
        if (live && 'ok' in outcome) setMade({ conv, list: outcome.value.items.filter((item) => item.from) });
      });
    }, INHERITED_DEBOUNCE_MS);
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [machine, conv, on, announced, moved]);
  return made.conv === conv ? made.list : NONE;
};
