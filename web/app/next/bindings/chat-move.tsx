// «Продолжить на другой ВМ» of a chat on the page: the sheet that asks the gateway where the chat can go and
// moves it, and what a chat that already went on elsewhere shows instead of its field. The new chat opens when it is
// there; any failure leaves this chat as it was. A chat of another machine than the picked one needs no move to be
// written to: the page talks to every machine of the person, and the chat's field goes to its own machine.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { fetchMoveStatus, fetchMoveTargets, postMove, type IMoved } from '@/next/features/chat-move/api';
import MoveSheet, { type IMoveChoice } from '@/next/features/chat-move/move-sheet';
import { goTo, ROUTE } from '@/next/routes/routes';
import { refreshCentral } from '@/next/stores/central';
import { conversationsStore } from '@/next/stores/conversations';
import { useMoveEnabled, useMoveVmRequest } from '@/next/stores/chat-move';
import type { MachineId } from '@/next/protocol/v2';

export interface IChatMove {
  /** Where this chat went on, when it did: its field gives way to a note. */
  moved: IMoved | null;
  /** Opens the chat it went on in. */
  openMoved: () => void;
  sheet: ReactNode;
}

const SEEN_WAIT_MS = 10_000;
const SEEN_STEP_MS = 150;

/** The new chat arrives on its own machine's channel; the page opens it once it has come (or after a while, anyway). */
const waitForChat = async (id: string): Promise<void> => {
  for (
    let waited = 0;
    waited < SEEN_WAIT_MS && conversationsStore.get(id as never) === undefined;
    waited += SEEN_STEP_MS
  ) {
    await new Promise((resolve) => setTimeout(resolve, SEEN_STEP_MS));
  }
};

export function useChatMove(machine: MachineId, conv: string): IChatMove {
  const enabled = useMoveEnabled(machine);
  const [open, setOpen] = useState(false);
  const [prefer, setPrefer] = useState<string | null>(null);
  const [moved, setMoved] = useState<{ conv: string; moved: IMoved | null } | null>(null);

  // Where the chat went, asked once the switch is known to be on for its machine (a machine without it has no moves).
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    void fetchMoveStatus(machine, conv).then((status) => {
      if (alive) setMoved({ conv, moved: status.moved });
    });
    return () => {
      alive = false;
    };
  }, [enabled, machine, conv]);

  useMoveVmRequest(conv, () => {
    setPrefer(null);
    setOpen(true);
  });

  const load = useCallback(() => fetchMoveTargets(machine, conv), [machine, conv]);
  const onMove = useCallback(
    async (choice: IMoveChoice) => {
      const result = await postMove({
        opId: choice.opId,
        machineId: machine,
        convId: conv,
        targetMachineId: choice.target.machineId,
        provider: choice.agent.provider,
        ...(choice.agent.profile ? { profile: choice.agent.profile } : {}),
        model: { id: choice.model.id },
        withoutFiles: choice.withoutFiles,
      });
      setMoved({
        conv,
        moved: {
          toMachine: result.targetMachineId,
          toName: result.targetName,
          toConv: result.convId,
          at: Date.now(),
        },
      });
      // The one list puts the new chat in this one's place.
      void refreshCentral();
      await waitForChat(result.convId);
      goTo({ kind: ROUTE.chat, id: result.convId });
    },
    [machine, conv],
  );

  const current = moved?.conv === conv ? moved.moved : null;
  return {
    moved: current,
    openMoved: () => {
      if (current) goTo({ kind: ROUTE.chat, id: current.toConv });
    },
    sheet: enabled ? (
      <MoveSheet
        key={prefer ?? ''}
        open={open}
        onClose={() => setOpen(false)}
        load={load}
        onMove={onMove}
        prefer={prefer}
      />
    ) : null,
  };
}
