// The move of a machine's data into the hub as «Машины» and the chat's bar show it, and «Продолжить с
// агентом»: a new chat on that machine with the brief in its field, for the person to send.
import {
  moveBrief,
  moveCodes,
  moveLine,
  moveStopped,
  type IMachineMove,
} from '@/next/features/settings/machine-move';
import type { IMoveCard } from '@/next/features/settings/move-card';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, HOME } from '@/next/routes/routes';
import { NEW_CHAT, setDraftText } from '@/next/stores/drafts';
import { newChatMachineStore } from '@/next/stores/new-chat';

export const continueMoveWithAgent = (machine: MachineId, name: string, move: IMachineMove) => {
  newChatMachineStore.set(machine);
  setDraftText(NEW_CHAT, moveBrief(move, name));
  goTo(HOME);
};

/** The card under the machine's row; null when there is nothing to show. */
export const moveCardOf = (
  machine: MachineId,
  name: string,
  move: IMachineMove | null | undefined,
  busy: boolean,
  onRetry: () => void,
): IMoveCard | null => {
  const line = move ? moveLine(move, name) : null;
  if (!move || line === null) return null;
  return {
    line,
    stopped: moveStopped(move),
    codes: moveCodes(move),
    failures: move.failures,
    busy,
    onRetry,
    onAgent: () => continueMoveWithAgent(machine, name, move),
  };
};
