// A conversation's forks and handoffs on the stores: the line of where it came from, the links to the chats it names,
// «Ответвить отсюда» under an answer, the handoff a model of another agent starts in the picker, and «Влить обратно»
// of a fork. Each change waits for the machine's answer; the new chat opens when it is there.
import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { forkConversation, handoffConversation, loadHandoffDraft, mergeConversation } from '@/next/commands';
import { pageText, settled } from '@/next/bindings/outcome';
import { modelName, pickerAgentOf, pickerAgents, type IPickerAgent } from '@/next/features/composer/agents';
import type { IChatLinks } from '@/next/features/chat/handoff';
import HandoffSheet, {
  SHEET_MODE,
  type TDraftOutcome,
  type TSheetMode,
} from '@/next/features/chat/handoff-sheet';
import type { OriginItem } from '@/next/features/chat/types';
import type {
  AgentModel,
  ConvId,
  Conversation,
  HandoffDraftRequest,
  MachineId,
  Model,
  Provider,
} from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { conversationsStore } from '@/next/stores/conversations';
import { hasFeature, useMachine } from '@/next/stores/machines';
import { UI_REQUEST, useUiRequest } from '@/next/stores/ui-requests';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

const HANDOFF_MODES = [SHEET_MODE.excerpt, SHEET_MODE.note] as const;
const MERGE_MODES = [SHEET_MODE.result] as const;

interface ITarget {
  agent: IPickerAgent;
  model: AgentModel;
}

export interface IChatHandoff {
  origin: OriginItem | null;
  /** The turn a fork branched at, where the line of its origin stands. */
  originTurn: string | null;
  links: IChatLinks;
  onForkTurn?: (turnId: string) => Promise<void>;
  /** For the picker: the other agents a handoff goes to; null where the machine cannot hand off. */
  handoff: {
    agents: readonly IPickerAgent[];
    onPick: (target: IPickerAgent, model: AgentModel) => void;
  } | null;
  sheets: ReactNode;
}

const openChat = (id: string) => goTo({ kind: ROUTE.chat, id });
const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

const draftOf = async (m: MachineId, conv: ConvId, req: HandoffDraftRequest): Promise<TDraftOutcome> => {
  const res = await loadHandoffDraft(m, conv, req);
  return 'ok' in res ? { ok: true, draft: res.value } : { ok: false, reason: pageText(res) };
};

/** A new chat's command settled: the chat opens; a refusal rejects with its reason, for the sheet or the toast. */
const opened = async (handle: { conversation: Promise<ConvId | null> } & Parameters<typeof settled>[0]) => {
  await settled(handle);
  const id = await handle.conversation;
  if (id) openChat(id);
};

export function useChatHandoff(
  machine: MachineId,
  conv: ConvId,
  record: Conversation | undefined,
): IChatHandoff {
  const view = useMachine(machine);
  const agents = view?.machine.agents;
  const [target, setTarget] = useState<ITarget | null>(null);
  const [merging, setMerging] = useState(false);

  const label = useCallback(
    (provider: Provider, model: Model | null): string | null => {
      if (!model) return null;
      const agent = pickerAgentOf(agents ?? [], provider);
      return agent ? modelName(agent, model.id) : model.id;
    },
    [agents],
  );

  const source = record?.origin;
  const origin = useMemo<OriginItem | null>(
    () =>
      source && record
        ? {
            kind: 'origin',
            key: 'origin',
            at: source.at,
            fork: source.k === 'fork',
            fromId: source.fromId,
            title: source.fromTitle,
            from: label(source.fromProvider, source.fromModel),
            to: label(record.provider, record.model),
          }
        : null,
    [source, record, label],
  );

  const links = useMemo<IChatLinks>(
    () => ({ has: (id) => isConvId(id) && conversationsStore.get(id) !== undefined, open: openChat }),
    [],
  );

  const canFork = hasFeature(machine, 'fork');
  const onForkTurn = useCallback(
    async (turnId: string) => {
      try {
        await opened(forkConversation(machine, conv, turnId));
      } catch (failure) {
        toast(t('next.handoff.forkFailed'), {
          tone: 'error',
          description: failure instanceof Error ? failure.message : undefined,
        });
      }
    },
    [machine, conv],
  );

  const canHandoff = hasFeature(machine, 'handoff') && record !== undefined;
  const provider = record?.provider;
  const others = useMemo(
    () => pickerAgents(agents ?? []).filter((a) => a.available && a.provider !== provider),
    [agents, provider],
  );
  const handoff = useMemo(
    () =>
      canHandoff && others.length > 0
        ? { agents: others, onPick: (agent: IPickerAgent, model: AgentModel) => setTarget({ agent, model }) }
        : null,
    [canHandoff, others],
  );

  // «Влить обратно» of the header's menu: only a fork has a chat to merge into.
  const parent = source?.k === 'fork' ? source : null;
  useUiRequest(UI_REQUEST.merge, () => {
    if (parent && hasFeature(machine, 'fork')) setMerging(true);
  });

  const from = record ? label(record.provider, record.model) : null;
  const sheets = (
    <>
      {canHandoff && (
        <HandoffSheet
          open={target !== null}
          onClose={() => setTarget(null)}
          title={t('next.handoff.title', {
            agent: target?.agent.short ?? '',
            model: target?.model.label ?? '',
          })}
          lead={t('next.handoff.lead', { from: from ?? t('next.handoff.thisChat') })}
          modes={HANDOFF_MODES}
          sendLabel={t('next.handoff.send')}
          onDraft={(mode: TSheetMode) =>
            draftOf(
              machine,
              conv,
              target ? { mode, provider: target.agent.provider, model: target.model.id } : { mode },
            )
          }
          onSend={async (mode, text) => {
            if (!target || mode === SHEET_MODE.result) return;
            const model = { id: target.model.id, effort: target.model.defaultEffort };
            await opened(
              handoffConversation(machine, conv, { provider: target.agent.provider, model }, mode, text),
            );
          }}
        />
      )}
      {parent && (
        <HandoffSheet
          open={merging}
          onClose={() => setMerging(false)}
          title={t('next.handoff.mergeTitle', { title: parent.fromTitle || t('next.shellData.untitled') })}
          lead={t('next.handoff.mergeLead')}
          modes={MERGE_MODES}
          sendLabel={t('next.handoff.mergeSend')}
          onDraft={(mode) => draftOf(machine, conv, { mode })}
          onSend={async (_, text) => {
            await settled(mergeConversation(machine, conv, text));
            toast(t('next.handoff.merged', { title: parent.fromTitle || t('next.shellData.untitled') }), {
              action: { label: t('next.handoff.openParent'), onClick: () => openChat(parent.fromId) },
            });
          }}
        />
      )}
    </>
  );

  return {
    origin,
    originTurn: source?.k === 'fork' ? (source.atTurn ?? null) : null,
    links,
    onForkTurn: canFork ? onForkTurn : undefined,
    handoff,
    sheets,
  };
}
