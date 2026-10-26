// The dock over the bottom of the feed, on the stores: the connection strip, the open question, the limit notice,
// the queue and the composer. Every action goes through a command and ends in the UI (spec §12): the control is busy
// at once, the draft goes only once the machine has the message, a refusal says why.
import {
  clearQuotes,
  quoteLabel,
  quoteTitle,
  removeQuote,
  useQuotes,
  withQuotes,
} from '@/next/stores/quotes';
import { useContextWindow } from '@/next/bindings/use-context-window';
import { useAgentLimits } from '@/next/bindings/use-limits';
import { limitsProviderOf } from '@/next/features/limits/model';
import { useCallback, useMemo, useState, type ReactNode } from 'react';
import {
  answerQuestion,
  cancelMessage,
  interrupt,
  moveQueued,
  promoteMessage,
  resumeQueue,
  retryMessage,
  sendMessage,
  setGoal,
  setModel,
} from '@/next/commands';
import { deviceId } from '@/next/core/device';
import { HOME, ROUTE, goTo } from '@/next/routes/routes';
import { UI_REQUEST, requestContinue, requestUi } from '@/next/stores/ui-requests';
import { useSlashSource } from '@/next/bindings/slash-source';
import type { ISlashActions } from '@/next/features/composer/slash-model';
import { failureText, settled } from '@/next/bindings/outcome';
import { isHanded } from '@/next/bindings/feed-items';
import { useUploads } from '@/next/bindings/use-uploads';
import ConnectionBanner from '@/next/features/connection/banner';
import {
  pickerAgentOf,
  removedKeyOf,
  type IModelChoice,
  type IPickerAgent,
} from '@/next/features/composer/agents';
import type { IModelPickerProps } from '@/next/features/composer/model-picker';
import Composer, { type IComposerQuote, type TSendMode } from '@/next/features/composer/composer';
import {
  formatAnnotation,
  readAnnotated,
  renumberBlock,
  withAnnotations,
} from '@/next/features/designly/annotation';
import {
  CARRIED_PREFIX,
  carryNotes,
  clearCarriedNotes,
  dropCarriedNote,
  useCarriedNotes,
} from '@/next/stores/carried-notes';
import { noteQuote } from '@/next/features/designly/site-notes';
import { clearDesignNotes, patchDesignNotes, useDesignNotes } from '@/next/stores/design-notes';
import DockBar from '@/next/features/composer/dock-bar';
import { refusalReason } from '@/next/features/chat/model-switch';
import GoalBar from '@/next/features/composer/goal-bar';
import DockStatus from '@/next/features/composer/dock-status';
import { useDockStatus } from '@/next/bindings/use-dock-status';
import Queue from '@/next/features/composer/queue';
import { inQueueOrder } from '@/next/features/composer/queue-order';
import type { ComposerMode, DockNotice, QueueRow } from '@/next/features/composer/types';
import QuestionCard from '@/next/features/questions/question-card';
import type { ConvId, MachineId, PromoteTo } from '@/next/protocol/v2';
import { reconnectNow, useConnection } from '@/next/stores/connection';
import { openGates, useFedClients, useGates, useQueue } from '@/next/stores/conversation';
import { useConversation } from '@/next/stores/conversations';
import { addDraftRef, clearDraft, draftsStore, removeDraftRef, setDraft } from '@/next/stores/drafts';
import { attachedOf, refKey } from '@/next/features/chat/attached';
import { designThumb } from '@/next/features/composer/attach-sources';
import type { IPlusAttach } from '@/next/features/composer/plus-menu';
import { openAttached } from '@/next/features/pane/open-attached';
import { REFS_MAX, type MessageRef } from '@/next/protocol/v2';
import { limitPlaceholder } from '@/next/features/chat/limit';
import { machineNow } from '@/next/core/clock';
import { useDraftField } from '@/next/bindings/use-draft';
import { useFieldStash } from '@/next/bindings/use-stash';
import { limitsOf, useMachine } from '@/next/stores/machines';
import { useVoice } from '@/next/bindings/use-voice';
import { messagesStore, usePendingMessages, waitsInQueue, type MessageView } from '@/next/stores/messages';
import { useWaitingAgents } from '@/next/bindings/waiting';
import { t } from '@/next/i18n';
import { bindLaunch } from '@/next/ui/flight';
import { toast } from '@/next/ui/toast';
import { useEnterSends } from '@/next/stores/me';

const rowOf = (m: MessageView): QueueRow => ({
  clientId: m.clientId,
  text: m.text,
  attachments: m.attachments,
  refs: m.refs,
  state: m.state,
  steer: m.mode === 'steer',
  ahead: m.mode === 'now',
  handed: isHanded(m),
});

/**
 * «Right now» to a working agent is typed into its turn (steer): «now» only puts a message at the head of the line, and
 * it would wait for the turn to end all the same. To an agent that does not work, «now» is the next thing it gets.
 */
const straightTo = (working: boolean): PromoteTo => (working ? 'steer' : 'now');

/** What a host adds to the dock (a design's notes on its blocks): chips that go with the message, buttons on the bar. */
export interface IDockExtra {
  quotes: readonly IComposerQuote[];
  onRemoveQuote: (id: string) => void;
  /** The message as it goes: the field's words with the host's own. */
  wrap: (text: string) => string;
  /** The message went: the host lets go of what it added. */
  onSent: () => void;
  tools?: ReactNode;
}

interface IChatDockProps {
  conv: ConvId;
  /** The machine of the chat: known from the cached feed before the conversation's record has come. */
  machine: MachineId;
  extra?: IDockExtra | null;
  /** The other agents the picker hands the chat's context to (a new chat); null where it cannot. */
  handoff?: IModelPickerProps['handoff'];
}

/**
 * Why the chat's model cannot change: only an agent that has no such command. A model asked while the agent works or
 * is not started is not refused — the server holds it and applies it at the first moment it can.
 */
const keepText = (text: string) => text;

const modelBlocked = (agent: IPickerAgent): string | null =>
  agent.canSwitch ? null : t('next.picker.cantSwitch', { agent: agent.name });

const ChatDock = ({ conv, machine, extra = null, handoff = null }: IChatDockProps) => {
  const enterSends = useEnterSends();
  const record = useConversation(conv);
  const machineView = useMachine(machine);
  const connection = useConnection(machine);
  const queue = useQueue(conv);
  const gates = useGates(conv);
  const pending = usePendingMessages(conv);
  const fed = useFedClients(conv);
  const { text, files, refs, setText: onChange, read, field } = useDraftField(conv);
  const uploads = useUploads(machine, conv, files);
  const stash = useFieldStash(conv);
  const quotes = useQuotes(conv);
  // The notes made on a design in the pane beside this chat ride in its field like quotes and go with the message.
  const designNotes = useDesignNotes(conv);
  // The notes of a queued message taken back into the field: chips before the new ones, numbered first.
  const carried = useCarriedNotes(conv);
  const open = useMemo(() => openGates(gates), [gates]);
  const question = open.questions[0] ?? null;
  const working = record?.state.k === 'working';
  const waitingAgents = useWaitingAgents(conv);
  const stopped = record?.state.k === 'stopped' && record.state.resumable;
  let mode: ComposerMode = 'idle';
  if (question) mode = 'answer';
  else if (working) mode = 'working';
  else if (stopped) mode = 'stopped';
  const maxText = limitsOf(machine).maxText;
  // A chat of a vendor whose key was removed cannot run: the bar says so and the field waits.
  const goneVendor = record
    ? removedKeyOf(machineView?.machine.agents ?? [], record.provider, record.model?.profile)
    : null;
  const keyNotice = useMemo<DockNotice | null>(
    () =>
      goneVendor === null
        ? null
        : {
            kind: 'key',
            vendor: goneVendor,
            onOpen: () => goTo({ kind: ROUTE.settings, section: 'accounts' }),
            onContinue: handoff ? () => requestContinue(conv) : null,
          },
    [goneVendor, handoff, conv],
  );

  // Held on the usage limit: the queue waits for the reset by itself, so it is not a pause to resume. The card in the
  // feed is the control; here the field and the rows only say what a message written now waits for.
  const limitHold = record?.limited ?? null;
  const heldOnLimit = limitHold !== null || queue?.paused?.reason === 'limited';
  const hint = goneVendor
    ? t('next.dockBar.keyGone', { vendor: goneVendor })
    : heldOnLimit
      ? limitPlaceholder(limitHold ?? { resetsAt: null }, machineNow(machine))
      : undefined;

  const agentState = record?.state.k;
  // The rows stand in the order of the machine's line; a runtime that cannot put them in another order has no grips.
  const line = queue?.items;
  const rows = useMemo(
    () =>
      inQueueOrder(
        pending.filter((m) => waitsInQueue(m, agentState) && !fed.has(m.clientId)).map(rowOf),
        line,
      ),
    [pending, agentState, fed, line],
  );
  const canMove = machineView?.features.includes('queue-move') ?? false;

  const voice = useVoice(machine, text, onChange, read);
  const window = useContextWindow(machine, conv, working);
  // The usage meter is Anthropic's (or Codex's) own: a chat on a vendor's profile has none.
  const limits = useAgentLimits(
    machine,
    limitsProviderOf(record ? { provider: record.provider, profile: record.model?.profile } : null),
  );
  const compacting = record?.state.k === 'working' && record.state.compacting;
  const idle = record?.state.k === 'idle' || record?.state.k === 'ready';
  const compact = useMemo(
    () =>
      idle
        ? () =>
            sendMessage(machine, { conversationId: conv, text: '/compact', attachments: [], mode: 'queue' })
        : undefined,
    [idle, machine, conv],
  );

  // What «/» can do on this screen beyond typing: the goal dialog (Claude's /goal), a new chat, the earlier sessions.
  const provider = record?.provider ?? null;
  const slashActions = useMemo<ISlashActions>(
    () => ({
      newChat: () => goTo(HOME),
      resume: () => requestUi(UI_REQUEST.resume),
      goal: provider === 'claude' ? () => requestUi(UI_REQUEST.goal) : undefined,
      compact,
    }),
    [provider, compact],
  );
  const onChat = useCallback((ref: MessageRef) => addDraftRef(conv, ref, REFS_MAX), [conv]);
  const slash = useSlashSource({
    machine,
    conv,
    provider,
    cwd: record?.cwd ?? null,
    actions: slashActions,
    onChat,
  });

  const onSubmit = useCallback(
    (sendMode: TSendMode) => {
      if (question) {
        toast(t('next.ask.useCard'), { tone: 'info' });
        return;
      }
      if (uploads.uploading) {
        toast(t('next.composer.uploadingWait'), { tone: 'info' });
        return;
      }
      const clientId = sendMessage(machine, {
        conversationId: conv,
        text: withAnnotations((extra?.wrap ?? keepText)(withQuotes(quotes, text.trim())), [
          ...carried.map((note, i) => renumberBlock(note.raw, i + 1)),
          ...designNotes.map((note, i) =>
            formatAnnotation({
              n: carried.length + i + 1,
              picked: note.picked,
              comment: note.text,
              style: note.style,
              url: note.page,
              port: note.port,
            }),
          ),
        ]),
        attachments: uploads.ready,
        refs,
        mode: sendMode === 'now' ? straightTo(working) : 'queue',
      });
      extra?.onSent();
      // The bubble of this message grows out of the place the text left (ui/flight.ts).
      bindLaunch(clientId);
      // The message is on its way as its own record (submitting), the field is free for the next one.
      clearDraft(conv);
      clearQuotes(conv);
      clearDesignNotes(conv);
      clearCarriedNotes(conv);
      uploads.reset();
    },
    [machine, conv, text, refs, quotes, designNotes, carried, uploads, question, working, extra],
  );

  // «Изменить»: the runtime lets the message go (pessimistic), and only then its text is back in the field.
  const onEdit = useCallback(
    async (clientId: string) => {
      const row = rows.find((r) => r.clientId === clientId);
      if (!row) return;
      await settled(cancelMessage(clientId));
      // A message that was delivered after all (the row was stale) only leaves the queue: its words do not come
      // back into the field to be sent a second time.
      const after = messagesStore.get(clientId);
      if (!after || after.state.k === 'done') return;
      const have = draftsStore.get(conv);
      // The words come back into the text, the design's notes and the attached places as chips: nobody edits the
      // `<annotation>` or `<attached>` markup by hand.
      const places = attachedOf(row.text, row.refs);
      const back = readAnnotated(places.text);
      carryNotes(conv, back.notes);
      const kept = have?.refs ?? [];
      const keys = new Set(kept.map(refKey));
      setDraft(conv, {
        text: [back.text, have?.text ?? ''].filter(Boolean).join('\n\n'),
        attachments: [...row.attachments, ...(have?.attachments ?? [])],
        refs: [
          ...(row.refs?.length ? row.refs : places.refs).filter((r) => !keys.has(refKey(r))),
          ...kept,
        ].slice(0, REFS_MAX),
      });
    },
    [rows, conv],
  );

  // What «+» attaches besides files: places of the machine and the team, kept in the draft like the files. A design
  // opens at once in this chat's pane, so the person sees what the agent is pointed at.
  const cwd = record?.cwd ?? null;
  const attachedKeys = useMemo(() => new Set(refs.map(refKey)), [refs]);
  const attach = useMemo<IPlusAttach | null>(
    () =>
      record
        ? {
            scope: { machine, cwd },
            attached: attachedKeys,
            onAttach: (row) => {
              addDraftRef(conv, row.ref, REFS_MAX);
              if (row.ref.kind === 'design') openAttached(row.ref);
            },
          }
        : null,
    [record, machine, cwd, attachedKeys, conv],
  );
  const refChips = useMemo(
    () => refs.map((r) => ({ ...r, thumb: r.kind === 'design' ? designThumb(machine, r.id) : null })),
    [refs, machine],
  );

  // The pill shows what the agent last said; a change waits for its answer and a refusal says why.
  const [modelBusy, setModelBusy] = useState(false);
  const onModel = async (choice: IModelChoice) => {
    if (choice.model === null) return;
    setModelBusy(true);
    try {
      const handle = setModel(machine, conv, choice.model, choice.effort, choice.ultracode);
      const st = handle ? await handle.done : null;
      // The agent kept its model, or the model has no such mode: said in words of the switch, the pill stays true.
      if (st?.k === 'failed') {
        const reason = st.failure.r === 'refused' ? refusalReason(st.failure.code) : failureText(st.failure);
        toast(t('next.picker.failed'), { tone: 'error', description: reason });
      }
    } finally {
      setModelBusy(false);
    }
  };

  // The field is there from the first frame, so the page does not jump when the chat's record arrives; the pill that
  // needs the record comes later inside it.
  const agent = record
    ? pickerAgentOf(machineView?.machine.agents ?? [], record.provider, record.model?.profile ?? null)
    : null;
  const status = useDockStatus(record, pending, agent);
  // The pill shows what the chat will run on: a change that waits is already the choice, the line over the field says
  // when it takes effect. Ultracode is the exception: the pill shows the mode the agent was last seen in, and an ask
  // that waits shows only on the switch, said as waiting (the record's pendingModel), until the agent confirms it.
  const wanted = record?.pendingModel ?? record?.model;
  const ultracodeAsked = record?.pendingModel ? record.pendingModel.ultracode === true : null;

  return (
    <>
      <ConnectionBanner
        connection={connection}
        machineName={machineView?.machine.name ?? machine}
        onRetryNow={() => reconnectNow(machine)}
      />
      <QuestionCard
        question={question}
        device={deviceId()}
        ownAnswer={text}
        onAnswer={async (id, answers) => {
          await settled(answerQuestion(machine, conv, id, answers));
          clearDraft(conv);
        }}
      />
      <DockBar notice={keyNotice} />
      <DockStatus pending={status.pending} steps={status.steps} />
      <GoalBar
        goal={provider === 'claude' ? (record?.goal ?? null) : null}
        onStop={() => settled(setGoal(machine, conv, ''))}
      />
      <Queue
        rows={rows}
        paused={queue?.paused?.reason === 'limited' ? null : (queue?.paused ?? null)}
        waitingLimit={heldOnLimit}
        onRemove={(id) => settled(cancelMessage(id))}
        onEdit={onEdit}
        onSendNow={(id) => settled(promoteMessage(id, straightTo(working)))}
        onMove={canMove ? (id, before) => settled(moveQueued(machine, conv, id, before)) : undefined}
        onResume={() => settled(resumeQueue(machine, conv))}
        onRetry={(id) => settled(retryMessage(id))}
      />
      <Composer
        enterSends={enterSends}
        value={text}
        onChange={onChange}
        inputRef={field}
        onSubmit={onSubmit}
        mode={mode}
        placeholder={hint}
        quiet={goneVendor !== null}
        onStop={() => settled(interrupt(machine, conv))}
        waiting={waitingAgents.length > 0}
        picker={
          record && agent
            ? {
                value: {
                  provider: record.provider,
                  ...(record.model?.profile ? { profile: record.model.profile } : {}),
                  model: wanted?.id ?? null,
                  effort: wanted?.effort ?? null,
                  ultracode: record.model?.ultracode === true,
                },
                ultracodeAsked,
                busy: modelBusy,
                blocked: modelBlocked(agent),
                agents: [agent],
                locked: true,
                handoff,
                onChange: (choice) => void onModel(choice),
              }
            : null
        }
        slash={slash}
        attachments={uploads.attachments}
        onFiles={uploads.onFiles}
        onRemoveAttachment={uploads.onRemove}
        onRetryAttachment={uploads.onRetry}
        refs={refChips}
        onRemoveRef={(ref) => removeDraftRef(conv, ref)}
        attach={attach}
        stash={stash}
        acceptsChats
        quotes={[
          ...quotes.map((q) => ({
            id: q.id,
            label: quoteLabel(q),
            title: quoteTitle(q),
            remark: q.note !== null,
          })),
          ...carried.map((note, i) => ({
            id: `${CARRIED_PREFIX}${i}`,
            label: [`#${i + 1}`, note.page, note.comment || note.element].filter(Boolean).join(' '),
            title: [note.comment, note.element].filter(Boolean).join('\n'),
            remark: true,
            icon: 'map-pin',
          })),
          ...designNotes.map((note, i) => noteQuote(note, carried.length + i + 1)),
          ...(extra?.quotes ?? []),
        ]}
        onRemoveQuote={(id) => {
          if (id.startsWith(CARRIED_PREFIX)) dropCarriedNote(conv, Number(id.slice(CARRIED_PREFIX.length)));
          else if (extra?.quotes.some((q) => q.id === id)) extra.onRemoveQuote(id);
          else if (designNotes.some((n) => n.id === id)) {
            patchDesignNotes(conv, (have) => have.filter((n) => n.id !== id));
          } else removeQuote(conv, id);
        }}
        tools={extra?.tools}
        {...voice}
        limits={limits}
        contextRing={
          window && {
            used: window.usedTokens,
            limit: window.limitTokens,
            compacting,
            onCompact: compact,
          }
        }
        maxText={maxText}
      />
    </>
  );
};

export default ChatDock;
