// One conversation on the stores: its feed over its dock. The feed is never replaced by a skeleton because of the
// connection or a slow machine (spec §12): it is rows from the store at any state of the socket, and the only thing
// that comes of a bad connection is the strip over the field and a live turn that says it waits.
import SelBar from '@/next/features/chat/selbar';
import { addQuote } from '@/next/stores/quotes';
import { useCallback, useEffect, useEffectEvent, useMemo, useState } from 'react';
import {
  cancelMessage,
  decideLimit,
  decidePermission,
  interrupt,
  loadEntries,
  loadOlderTurns,
  loadRewindPlan,
  loadRewound,
  resendRefused,
  retryMessage,
  rewind,
  sendMessage,
  setGoal,
  undoRewind,
} from '@/next/commands';
import { machineNow } from '@/next/core/clock';
import { deviceId } from '@/next/core/device';
import { pageText, settled } from '@/next/bindings/outcome';
import { useCentralFeed } from '@/next/bindings/use-central';
import ChatDock, { type IDockExtra } from '@/next/bindings/chat-dock';
import { uploadSrc } from '@/next/commands/uploads';
import { AttachmentSrc } from '@/next/ui/attachment-thumb';
import { useFeedItems, withOrigin } from '@/next/bindings/feed-items';
import { useChatHandoff } from '@/next/bindings/chat-handoff';
import { useChatMove } from '@/next/bindings/chat-move';
import MovedNote from '@/next/features/chat-move/moved-note';
import { ChatLinks } from '@/next/features/chat/handoff';
import { terminalStopText } from '@/next/bindings/shell-model';
import { modelName, pickerAgentOf } from '@/next/features/composer/agents';
import Feed from '@/next/features/chat/feed';
import FileShare from '@/next/features/filedoc/file-share';
import LimitCard from '@/next/features/chat/limit-card';
import TerminalFailureCard from '@/next/features/chat/terminal-failure';
import type { FeedItem } from '@/next/features/chat/types';
import GateCard from '@/next/features/questions/gate-card';
import type { PermissionScope } from '@/next/features/questions/types';
import { useConversationChannel } from '@/next/core/realtime/hub';
import type { ConvId, MachineId, TurnOutput } from '@/next/protocol/v2';
import { useFeedKinds } from '@/next/stores/feed-kinds';
import { downloadUrl } from '@/next/features/pane/delivered';
import { linkReason, useConnection, useLinkRestored } from '@/next/stores/connection';
import { useBackground, useFeed, useGates, usePictures } from '@/next/stores/conversation';
import { runsByCall, WorkflowRuns } from '@/next/features/workflows/run';
import { machineApi } from '@/next/core/net/machine';
import { MessagePictures, type IMessagePictures } from '@/next/features/chat/pictures';
import { conversationsStore, useConversation, useConversationList } from '@/next/stores/conversations';
import { convOfRef, parsePeer, PeerActions, type IPeerActions } from '@/next/features/chat/peer';
import { AskSource, type IAskSource } from '@/next/features/asks/parse';
import { RewindActions, type IRewindActions } from '@/next/features/chat/rewind';
import { DelegatedChats } from '@/next/features/chat/delegation';
import { sendPeer } from '@/next/features/peers/api';
import { goTo, ROUTE } from '@/next/routes/routes';
import { useMachine } from '@/next/stores/machines';
import { useWaitingAgents } from '@/next/bindings/waiting';
import { useMe } from '@/next/stores/me';
import { messagesStore } from '@/next/stores/messages';
import { doneReveal, useReveal } from '@/next/stores/reveal';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';
import { useDeadline } from '@/next/ui/use-deadline';
import { requestContinue } from '@/next/stores/ui-requests';

interface IChatViewProps {
  conv: ConvId;
  machine: MachineId;
  onRetryLoad: () => void;
  /** A file of a turn, in the right pane. */
  onOpenFile?: (path: string) => void;
  /** A thing a turn made (a page, a design, a document, a merge request, a stand), in the right pane. */
  onOpenOutput?: (output: TurnOutput) => void;
  /** A task, a merge request or a link of the text, in the right pane: the runtime says which of its tabs shows it. */
  onOpenRef?: (ref: string) => void;
  /** Brings up the right column, with the background agents the finished answer waits for. */
  onShowBackground: () => void;
  /** What the host adds to the dock (a design's notes). */
  dockExtra?: IDockExtra | null;
  /** Drawn while the feed is not read yet instead of its skeleton: a chat just created shows its first message. */
  pendingItems?: FeedItem[] | null;
  /** Opens the chat's terminal on the agent's pane (a gate the page could not decide); null while it has none. */
  onOpenTerminal?: (() => void) | null;
}

interface IGateOfProps {
  machine: MachineId;
  conv: ConvId;
  permissionId: string;
  onOpenTerminal: (() => void) | null;
}

/** A permission's card in the steps of its turn: the answer it was given stays on it as the person's choice. */
const GateOf = ({ machine, conv, permissionId, onOpenTerminal }: IGateOfProps) => {
  const gates = useGates(conv);
  const [decided, setDecided] = useState<PermissionScope | null>(null);
  const permission = gates?.permissions.find((p) => p.id === permissionId);
  if (!permission) return null;
  return (
    <GateCard
      permission={permission}
      device={deviceId()}
      decided={decided}
      onOpenTerminal={onOpenTerminal}
      onDecide={async (id, optionId) => {
        await settled(decidePermission(machine, conv, id, optionId));
        const scope = permission.options.find((o) => o.id === optionId)?.scope;
        if (scope) setDecided(scope);
      }}
    />
  );
};

/** The states the agent is at work in, or waits for the person's answer in: a rewind stops it first. */
const BUSY: ReadonlySet<string> = new Set(['starting', 'working', 'waiting-question', 'waiting-permission']);

const ChatView = ({
  conv,
  machine,
  onRetryLoad,
  onOpenFile,
  onOpenOutput,
  onOpenRef,
  onShowBackground,
  dockExtra = null,
  pendingItems = null,
  onOpenTerminal = null,
}: IChatViewProps) => {
  useConversationChannel(machine, conv);
  useCentralFeed(machine, conv);
  const read = useFeedItems(conv, machine);
  const feed = useFeed(conv);
  const connection = useConnection(machine);
  const machineView = useMachine(machine);
  const record = useConversation(conv);
  const handoff = useChatHandoff(machine, conv, record);
  const move = useChatMove(machine, conv);
  const hasOlderTurns = (feed?.beforeTurnId ?? null) !== null;
  const items = useMemo(
    () => (read ? [...withOrigin(read, handoff.origin, handoff.originTurn, hasOlderTurns)] : pendingItems),
    [read, pendingItems, handoff.origin, handoff.originTurn, hasOlderTurns],
  );
  const waitingAgents = useWaitingAgents(conv);
  // The chat's workflows by the Workflow call that started each: the feed's card of a call draws its run.
  const background = useBackground(conv);
  const workflowRuns = useMemo(() => runsByCall(background), [background]);
  const workingSince = record?.state.k === 'working' ? record.state.since : null;
  const terminalStop = record ? terminalStopText(record) : null;
  const limited = record?.limited ?? null;
  const machineClock = useCallback(() => machineNow(machine), [machine]);
  const me = useMe();
  const reveal = useReveal(conv);
  const shown = useFeedKinds();
  // A document file of a turn that is being shared: its card's «Поделиться» opens the dialog here.
  const [sharing, setSharing] = useState<{ path: string; open: boolean } | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [olderFailed, setOlderFailed] = useState<string | null>(null);
  const [openedAt] = useState(() => Date.now());

  const density = me.k === 'ready' ? me.me.settings.density : 'compact';
  const before = feed?.beforeTurnId ?? null;

  const onLoadOlder = useCallback(() => {
    if (!before || loadingOlder) return;
    setLoadingOlder(true);
    void loadOlderTurns(machine, conv, before)
      .then((res) => setOlderFailed('ok' in res ? null : pageText(res)))
      .finally(() => setLoadingOlder(false));
  }, [machine, conv, before, loadingOlder]);

  // The link came back after an older page failed: it is asked again quietly, the feed on screen stays as it is.
  const restored = useLinkRestored(machine);
  const askOlderAgain = useEffectEvent(() => {
    if (olderFailed !== null) onLoadOlder();
  });
  useEffect(() => {
    if (restored === 0) return;
    const timer = setTimeout(askOlderAgain, 0);
    return () => clearTimeout(timer);
  }, [restored]);

  // A conversation that does not come within the deadline says so with a retry, not a skeleton for ever. Once it is
  // on screen nothing here replaces it, whatever the connection does.
  const notFound = feed?.error === 'not-found';
  const wait = useDeadline(items === null && !notFound);
  const failedReason = linkReason(connection) ?? t('next.load.noAnswer');
  // The feed drawn from the browser's copy until the runtime's snapshot (or the end of its replay) comes; while the
  // machine is offline the strip over the field says why nothing comes, the bar does not run for ever.
  const catchingUp = feed?.source === 'cached' && !feed.error && connection?.k !== 'offline';

  const onLoadSteps = useCallback(
    (turnId: string) => {
      const attempt = (): Promise<boolean> =>
        loadEntries(machine, conv, turnId).then((res) => {
          if (!('ok' in res))
            toast(t('next.feed.stepsFailed'), {
              tone: 'error',
              description: pageText(res),
              action: { label: t('next.pending.retry'), onClick: () => void attempt() },
            });
          return 'ok' in res;
        });
      return attempt();
    },
    [machine, conv],
  );

  /** «Продолжить» and «Повторить» of a turn that ended badly send the person's words again. */
  const resend = useCallback(
    (text: string) => {
      sendMessage(machine, { conversationId: conv, text });
      return Promise.resolve();
    },
    [machine, conv],
  );

  // The feed calls this from a row's render through a stable function, so the card reads its gate itself.
  const renderGate = useCallback(
    (permissionId: string) => (
      <GateOf
        key={permissionId}
        machine={machine}
        conv={conv}
        permissionId={permissionId}
        onOpenTerminal={onOpenTerminal}
      />
    ),
    [machine, conv, onOpenTerminal],
  );

  const turnPrompt = useMemo(() => {
    const byTurn = new Map<string, string>();
    for (const item of items ?? [])
      if (item.kind === 'turn' && item.prompt) byTurn.set(item.key, item.prompt.text);
    return byTurn;
  }, [items]);

  const srcOf = useCallback((item: { id: string }) => uploadSrc(machine, item), [machine]);
  // The pictures the messages name, by their id on the machine (never by their path).
  const listedPictures = usePictures(conv);
  const pictures = useMemo<IMessagePictures | null>(
    () =>
      listedPictures && listedPictures.length > 0
        ? {
            pictures: listedPictures,
            srcOf: (picture) =>
              machineApi(machine).http(`/conversations/${conv}/shot?id=${encodeURIComponent(picture.id)}`),
          }
        : null,
    [listedPictures, machine, conv],
  );

  // A model as the machine's agent names it («Sonnet 5.5»), for the lines of changed models in the feed.
  const agents = machineView?.machine.agents;
  const provider = record?.provider;
  const profile = record?.model?.profile;
  const modelLabel = useCallback(
    (id: string) => {
      const agent = provider ? pickerAgentOf(agents ?? [], provider, profile ?? null) : null;
      return agent ? modelName(agent, id) : id;
    },
    [agents, provider, profile],
  );

  // Going on from an earlier message: any of the person's messages but the first of the transcript (nothing before it
  // to go on from), while the machine can.
  const canRewind = !!record && !!machineView?.features.includes('rewind');
  const firstTurn = before === null ? (items ?? []).find((item) => item.kind === 'turn')?.key : undefined;
  const agentBusy = BUSY.has(record?.state.k ?? 'unknown');
  const rewindActions = useMemo<IRewindActions | null>(
    () =>
      canRewind
        ? {
            can: (turnId) => {
              const text = turnPrompt.get(turnId);
              return turnId !== firstTurn && text !== undefined && parsePeer(text) === null;
            },
            busy: agentBusy,
            plan: async (turnId) => {
              const res = await loadRewindPlan(machine, conv, turnId);
              if (!('ok' in res)) throw new Error(pageText(res));
              return res.value;
            },
            rewind: (turnId, text, files) => settled(rewind(machine, conv, turnId, text, files)),
            stop: () => settled(interrupt(machine, conv)),
            undo: () => settled(undoRewind(machine, conv)),
            rewound: async (n) => {
              const res = await loadRewound(machine, conv, n);
              if (!('ok' in res)) throw new Error(pageText(res));
              return res.value;
            },
          }
        : null,
    [canRewind, turnPrompt, firstTurn, agentBusy, machine, conv],
  );

  // The chats this chat's agent delegated: each card stands under the turn that started it.
  const machineChats = useConversationList(machine);
  const delegated = useMemo(
    () => (machineChats ?? []).filter((c) => c.parentId === conv).sort((a, b) => a.createdAt - b.createdAt),
    [machineChats, conv],
  );

  // Another agent's card: its chat opens here when it is one of the person's, an answer goes back from this chat.
  const peerActions = useMemo<IPeerActions>(
    () => ({
      chatOf: (ref) => {
        const id = convOfRef(ref);
        return id && conversationsStore.get(id) ? id : null;
      },
      onOpen: (id) => goTo({ kind: ROUTE.chat, id }),
      onReply: (ref, text) => sendPeer(machine, conv, ref, text),
    }),
    [machine, conv],
  );

  // The agent's questions to people (`harnessly ask`) read from this machine's list of asks.
  const askSource = useMemo<IAskSource>(() => ({ machine, conversation: conv }), [machine, conv]);

  const metGoal = record?.goal?.status === 'met' ? record.goal : null;
  const goalReached =
    metGoal === null
      ? null
      : {
          text: metGoal.text,
          onContinue: () => settled(setGoal(machine, conv, metGoal.text)),
        };

  return (
    <AttachmentSrc.Provider value={srcOf}>
      <ChatLinks.Provider value={handoff.links}>
        <MessagePictures.Provider value={pictures}>
          <PeerActions.Provider value={peerActions}>
            <AskSource.Provider value={askSource}>
              <RewindActions value={rewindActions}>
                <DelegatedChats.Provider value={delegated}>
                  <WorkflowRuns value={workflowRuns}>
                    <Feed
                      items={items}
                      failed={notFound || wait.expired}
                      failedReason={notFound ? null : failedReason}
                      onRetryLoad={() => {
                        onRetryLoad();
                        wait.restart();
                      }}
                      olderFailed={olderFailed}
                      hasOlder={before !== null}
                      loadingOlder={loadingOlder}
                      catchingUp={catchingUp}
                      onLoadOlder={onLoadOlder}
                      reveal={reveal}
                      onRevealed={doneReveal}
                      density={density}
                      offline={connection !== undefined && connection.k !== 'live'}
                      enterAfter={feed?.subscribedAt ?? openedAt}
                      machineName={machineView?.machine.name ?? machine}
                      workingSince={workingSince}
                      terminalStop={terminalStop}
                      goalReached={goalReached}
                      limit={
                        <LimitCard
                          limited={limited}
                          clock={machineClock}
                          controls={machineView?.features.includes('auto-continue') ?? false}
                          onDecide={(action) => settled(decideLimit(machine, conv, action))}
                        />
                      }
                      terminalFailure={
                        <TerminalFailureCard
                          failure={record?.terminalFailure ?? null}
                          onChooseTarget={handoff.handoff ? () => requestContinue(conv) : null}
                        />
                      }
                      waiting={
                        waitingAgents.length > 0 ? { agents: waitingAgents, onOpen: onShowBackground } : null
                      }
                      modelLabel={modelLabel}
                      onOpenFile={onOpenFile}
                      onOpenOutput={onOpenOutput}
                      onShareFile={(path) => setSharing({ path, open: true })}
                      shown={shown}
                      downloadUrl={(path) => downloadUrl(machine, path)}
                      onOpenRef={(kind, label, href) => {
                        if (kind === 'file' && !href) onOpenFile?.(label.replace(/:\d+$/, ''));
                        else onOpenRef?.(href ?? label);
                      }}
                      onLoadSteps={onLoadSteps}
                      onContinueTurn={() => resend(t('next.turnEnd.continueText'))}
                      onRetryTurn={(turnId) =>
                        resend(turnPrompt.get(turnId) ?? t('next.turnEnd.continueText'))
                      }
                      onRetryMessage={(clientId) => settled(retryMessage(clientId))}
                      onResendMessage={(clientId) => {
                        resendRefused(clientId);
                        return Promise.resolve();
                      }}
                      onDeleteMessage={(clientId) => {
                        const m = messagesStore.get(clientId);
                        if (!m) return Promise.resolve();
                        return settled(cancelMessage(clientId));
                      }}
                      renderGate={renderGate}
                      onForkTurn={handoff.onForkTurn}
                      dock={
                        move.moved ? (
                          <MovedNote moved={move.moved} onOpen={move.openMoved} />
                        ) : (
                          <>
                            {move.here}
                            <ChatDock
                              conv={conv}
                              machine={machine}
                              extra={dockExtra}
                              handoff={handoff.handoff}
                            />
                          </>
                        )
                      }
                    />
                  </WorkflowRuns>
                </DelegatedChats.Provider>
              </RewindActions>
            </AskSource.Provider>
          </PeerActions.Provider>
        </MessagePictures.Provider>
      </ChatLinks.Provider>
      {handoff.sheets}
      {move.sheet}
      {sharing && (
        <FileShare
          machine={machine}
          conv={conv}
          path={sharing.path}
          open={sharing.open}
          onClose={() => setSharing({ path: sharing.path, open: false })}
        />
      )}
      <SelBar onAdd={(text, note) => addQuote(conv, text, note)} />
    </AttachmentSrc.Provider>
  );
};

export default ChatView;
