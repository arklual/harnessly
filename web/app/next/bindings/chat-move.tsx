// «Продолжить на другой ВМ» of a chat on the page: the sheet that asks the gateway where the chat can go and
// moves it, and what a chat that already went on elsewhere shows instead of its field. The new chat opens when it is
// there; any failure leaves this chat as it was. With the one list of every machine, a chat of another machine than
// the one the person works on offers «Продолжить здесь»: the same move, to here, in one press.
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { machineName } from '@/next/bindings/shell-model';
import {
  fetchMoveStatus,
  fetchMoveTargets,
  MoveError,
  postMove,
  type IMoved,
} from '@/next/features/chat-move/api';
import { HERE, pickHere } from '@/next/features/chat-move/here';
import HereBar, { HERE_PHASE, type THerePhase } from '@/next/features/chat-move/here-bar';
import MoveSheet, { newOpId, type IMoveChoice } from '@/next/features/chat-move/move-sheet';
import { t } from '@/next/i18n';
import { goTo, ROUTE } from '@/next/routes/routes';
import { refreshCentral, useCentral } from '@/next/stores/central';
import { useConnection } from '@/next/stores/connection';
import { conversationsStore, useConversation } from '@/next/stores/conversations';
import { useHomeMachine, useMachine } from '@/next/stores/machines';
import { useMoveEnabled, useMoveVmRequest } from '@/next/stores/chat-move';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export interface IChatMove {
  /** Where this chat went on, when it did: its field gives way to a note. */
  moved: IMoved | null;
  /** Opens the chat it went on in. */
  openMoved: () => void;
  /** «Продолжить здесь» over the field, for a chat of another machine; null for a chat of this one. */
  here: ReactNode;
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
  const here = useContinueHere(machine, conv, enabled && current === null, onMove, (target) => {
    setPrefer(target);
    setOpen(true);
  });
  return {
    moved: current,
    openMoved: () => {
      if (current) goTo({ kind: ROUTE.chat, id: current.toConv });
    },
    here,
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

type TPhase = { conv: string; phase: THerePhase; reason: string | null };

/**
 * «Продолжить здесь» of a chat of another machine than the one the person works on, while the one list is on: the
 * press asks where the chat can go, moves it here when nothing needs the person's word, and opens the sheet on this
 * machine when something does (going on without the files). A machine that is off offers nothing: the move takes the
 * chat from the machine itself.
 */
function useContinueHere(
  machine: MachineId,
  conv: string,
  offered: boolean,
  onMove: (choice: IMoveChoice) => Promise<void>,
  ask: (target: string) => void,
): ReactNode {
  const central = useCentral();
  const home = useHomeMachine();
  const record = useConversation(conv as ConvId);
  const connection = useConnection(machine);
  const view = useMachine(machine);
  const [state, setState] = useState<TPhase | null>(null);
  // One id for the presses on this chat to this machine: a second press after a failure is the same move, never a
  // second chat.
  const opId = useRef<{ conv: string; id: string } | null>(null);

  const off = central.off.has(machine) && connection?.k !== 'live';
  const shown =
    offered && central.enabled && home !== null && home !== machine && (record?.lastTurn ?? null) !== null;
  const name = view ? machineName(view.machine) : (central.names.get(machine) ?? machine);

  const onContinue = useCallback(async () => {
    if (!home || !record) return;
    const key = `${conv}>${home}`;
    const op = opId.current?.conv === key ? opId.current : (opId.current = { conv: key, id: newOpId() });
    const set = (phase: THerePhase, reason: string | null = null) => setState({ conv, phase, reason });
    set(HERE_PHASE.moving);
    try {
      const pick = pickHere(await fetchMoveTargets(machine, conv), home, {
        provider: record.provider,
        model: record.model?.id ?? null,
      });
      if (pick.k === HERE.refused) return set(HERE_PHASE.failed, pick.reason);
      if (pick.k === HERE.ask) {
        set(HERE_PHASE.idle);
        return ask(home);
      }
      const { target, agent, model } = pick;
      await onMove({ target, agent, model, withoutFiles: false, opId: op.id });
      set(HERE_PHASE.idle);
    } catch (failure) {
      set(HERE_PHASE.failed, failure instanceof MoveError ? failure.message : t('next.moveVm.failed'));
    }
  }, [home, record, conv, machine, onMove, ask]);

  if (!shown) return null;
  const mine = state?.conv === conv ? state : null;
  const phase = off ? HERE_PHASE.off : (mine?.phase ?? HERE_PHASE.idle);
  return (
    <HereBar
      machine={name}
      phase={phase}
      reason={mine?.reason ?? null}
      onContinue={() => void onContinue()}
    />
  );
}
