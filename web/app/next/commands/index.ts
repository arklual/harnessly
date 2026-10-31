// The commands of the new interface (spec §3). Each returns an OpHandle at once: opsStore has the op pending in the
// same frame, and useOp(handle.opId) follows it to done or failed{retryable}. Nothing here changes a store
// optimistically: what the person sees change comes from the socket.

import { pageClock } from '@/next/core/clock';
import { uuidv7 } from '@/next/core/ids';
import { hub } from '@/next/core/realtime/hub';
import { forgetIfUnknown, newView, settleSubmit } from '@/next/commands/messages';
import { runCommand, type Body, type OpHandle } from '@/next/commands/run';
import { machinesStore } from '@/next/stores/machines';
import { putLocal, setConversationOf } from '@/next/stores/messages';
import type {
  AttachmentRef,
  ConvId,
  Effort,
  HandoffMode,
  Launch,
  LimitAction,
  MachineId,
  Model,
  ProfileId,
  ProjectChats,
  Ms,
  Provider,
  TurnId,
} from '@/next/protocol/v2';

export { retryCommand, runCommand, type OpHandle } from '@/next/commands/run';
export {
  cancelMessage,
  flushOutbox,
  promoteMessage,
  resendRefused,
  retryMessage,
  sendMessage,
  startOutbox,
  type Outgoing,
} from '@/next/commands/messages';
export { uploadFile, type UploadOutcome } from '@/next/commands/uploads';
export {
  loadEntries,
  loadHandoffDraft,
  loadOlderTurns,
  loadOutput,
  loadRewindPlan,
  loadRewound,
} from '@/next/commands/pages';

/** Per item the chosen options, or the person's own text with choices [] — never both. */
export type Answers = { i: number; choices: number[]; text?: string }[];

export const answerQuestion = (
  m: MachineId,
  conversationId: ConvId,
  questionId: string,
  answers: Answers,
): OpHandle => runCommand(m, 'answer', { conversationId, questionId, answers });

/** «Обсудить в чате»: the question closes, the agent waits for words. */
export const chatAboutQuestion = (m: MachineId, conversationId: ConvId, questionId: string): OpHandle =>
  runCommand(m, 'answer', { conversationId, questionId, chatAbout: true });

export const decidePermission = (
  m: MachineId,
  conversationId: ConvId,
  permissionId: string,
  optionId: string,
) => runCommand(m, 'decide', { conversationId, permissionId, optionId });

/** Stop: one Escape; the queue pauses unless pauseQueue is false. */
export const interrupt = (m: MachineId, conversationId: ConvId, pauseQueue = true) =>
  runCommand(m, 'interrupt', { conversationId, pauseQueue });

/**
 * Goes on from before the message that opened turn `entryId` with `text` instead; `files` returns the folder to how it
 * was then too. The feed after it comes as feed_reset.
 */
export const rewind = (m: MachineId, id: ConvId, entryId: TurnId, text: string, files: boolean) =>
  runCommand(m, 'rewind', { id, entryId, text, files });

/** Takes back the last rewind while its undoUntil has not passed. */
export const undoRewind = (m: MachineId, id: ConvId) => runCommand(m, 'rewind.undo', { id });

export const resumeQueue = (m: MachineId, conversationId: ConvId) =>
  runCommand(m, 'queue.resume', { conversationId });

/** Puts a queued message before another of the queue (null: last); the order changes when the runtime has said so. */
export const moveQueued = (
  m: MachineId,
  conversationId: ConvId,
  clientId: string,
  beforeClientId: string | null,
) =>
  forgetIfUnknown(
    clientId,
    conversationId,
    runCommand(m, 'queue.move', { conversationId, clientId, beforeClientId }),
  );

export const markSeen = (m: MachineId, conversationId: ConvId | 'all', upTo: Ms) =>
  runCommand(m, 'seen', { conversationId, upTo });

/** The cheap model names the chat anew from its content; the title follows on the record. */
export const retitleConversation = (m: MachineId, id: ConvId) =>
  runCommand(m, 'conversation.retitle', { id });

export const renameConversation = (m: MachineId, id: ConvId, title: string) =>
  runCommand(m, 'conversation.rename', { id, title });
export const pinConversation = (m: MachineId, id: ConvId, pinned: boolean) =>
  runCommand(m, 'conversation.pin', { id, pinned });
export const moveConversation = (m: MachineId, id: ConvId, projectId: string | null) =>
  runCommand(m, 'conversation.move', { id, projectId });
export const deleteConversation = (m: MachineId, id: ConvId, force = false) =>
  runCommand(m, 'conversation.delete', force ? { id, force } : { id });
export const restoreConversation = (m: MachineId, id: ConvId) =>
  runCommand(m, 'conversation.restore', { id });
export const resumeConversation = (m: MachineId, id: ConvId) => runCommand(m, 'conversation.resume', { id });
/** Ends the chat's terminal and all that runs in it; force stops an agent at work too. The chat stays. */
export const stopTerminal = (m: MachineId, id: ConvId, force = false) =>
  runCommand(m, 'terminal.stop', force ? { id, force } : { id });
/** A chat held on its usage limit: go on now, do not go on by itself, or go on by itself at the reset again. */
export const decideLimit = (m: MachineId, id: ConvId, action: LimitAction) =>
  runCommand(m, 'limit', { id, action });
/** The model, its depth and ultracode for the chat; it takes at the first safe moment (pendingModel until then). */
export const setModel = (
  m: MachineId,
  id: ConvId,
  model: string,
  effort: Effort | null,
  ultracode: boolean,
) => runCommand(m, 'model', { id, model, effort, ultracode });
export const stopBackground = (m: MachineId, conversationId: ConvId, taskId: string) =>
  runCommand(m, 'bg.stop', { conversationId, taskId });
/** The session's /goal (Claude only); an empty text clears it. */
export const setGoal = (m: MachineId, id: ConvId, text: string) => runCommand(m, 'goal', { id, text });
/** A project of a name alone: its chats start in the machine's default folder. */
export const createProject = (m: MachineId, name: string) => runCommand(m, 'project.create', { name });
/** The folder a project's next chats start in (the chats it has stay where they are), and its name. */
export const updateProject = (m: MachineId, id: string, change: { path?: string; name?: string }) =>
  runCommand(m, 'project.update', { id, ...change });
/** The project goes; its chats move out to the common list (keep) or are deleted with it (delete). */
export const deleteProject = (m: MachineId, id: string, chats: ProjectChats) =>
  runCommand(m, 'project.delete', { id, chats });

/** A group in the favorites; resolves with its id once the runtime answers. */
export const createFavGroup = (m: MachineId, name: string) =>
  runCommand(m, 'favorites.group.create', { name });
export const renameFavGroup = (m: MachineId, id: string, name: string) =>
  runCommand(m, 'favorites.group.rename', { id, name });
/** The group goes, its chats stay in the favorites, ungrouped. */
export const deleteFavGroup = (m: MachineId, id: string) => runCommand(m, 'favorites.group.delete', { id });
/** The group goes before another one, or last with null. */
export const moveFavGroup = (m: MachineId, id: string, beforeId: string | null) =>
  runCommand(m, 'favorites.group.move', { id, beforeId });
/** A chat into the favorites (pinned when it is not), in a group or with null ungrouped, before a chat or last. */
export const placeFavorite = (m: MachineId, id: ConvId, groupId: string | null, beforeId: ConvId | null) =>
  runCommand(m, 'favorites.place', { id, groupId, beforeId });

/** A session a terminal agent kept on the machine, opened as a new conversation; resolves with its id. */
export function resumeSession(
  m: MachineId,
  session: { provider: Provider; sessionId: string; cwd: string },
): OpHandle & { conversation: Promise<ConvId | null> } {
  return opening(m, runCommand(m, 'conversation.resume', session));
}

/** A command that opens a new conversation: resolves with its id, its channel held from its first event meanwhile. */
function opening(m: MachineId, handle: OpHandle): OpHandle & { conversation: Promise<ConvId | null> } {
  const conversation = handle.done.then((st) => {
    if (st.k !== 'done' || !st.result.id) return null;
    const id = st.result.id as ConvId;
    const epoch = machinesStore.get(m)?.epoch;
    if (epoch) {
      const release = hub.openNewConversation(m, id, epoch);
      pageClock().setTimeout(release, HOLD_NEW_MS);
    }
    return id;
  });
  return { ...handle, conversation };
}

/** A branch of the conversation after a finished turn (the last one without `atTurn`), as a new conversation. */
export const forkConversation = (m: MachineId, id: ConvId, atTurn?: TurnId) =>
  opening(m, runCommand(m, 'conversation.fork', atTurn ? { id, atTurn } : { id }));

type HandoffContext =
  { mode: 'history'; artifactId: string } | { mode: Exclude<HandoffMode, 'history'>; text: string };

/** A new conversation on another agent or model that starts from approved text or a private history artifact. */
export const handoffConversation = (
  m: MachineId,
  id: ConvId,
  to: { provider: Provider; profile?: ProfileId; model: Model | null },
  context: HandoffContext,
) =>
  opening(
    m,
    runCommand(m, 'conversation.handoff', {
      id,
      provider: to.provider,
      model: to.model,
      ...(to.profile ? { profile: to.profile } : {}),
      ...context,
    }),
  );

/** A fork's result into the queue of the conversation it came from; the result's id is that conversation. */
export const mergeConversation = (m: MachineId, id: ConvId, text: string) =>
  runCommand(m, 'conversation.merge', { id, text });

export interface NewConversation {
  where: { projectId: string } | { cwd: string };
  provider: Provider;
  /** The backend profile (Kimi, GLM, Grok) the chat starts on; absent on the CLI's own account. */
  profile?: ProfileId;
  model: Model | null;
  first?: { text: string; attachments?: AttachmentRef[] };
  /** Several chats of one prompt on different models: each works in a git worktree of its own and they are grouped. */
  launch?: Launch;
  worktree?: boolean;
}

/** How long the create flow itself holds the new chat's channel, for the page to open it in the meantime. */
export const HOLD_NEW_MS = 60000;

/** conversation.create, with the first message submitting at once. The new chat's channel is subscribed from its
 *  first event (afterSeq 0), so the first message's queued{launching} and what follows are not lost. */
export function createConversation(
  m: MachineId,
  req: NewConversation,
): OpHandle & { firstClientId: string | null; conversation: Promise<ConvId | null> } {
  const firstClientId = req.first ? uuidv7() : null;
  if (req.first && firstClientId) putLocal(newView(m, firstClientId, null, req.first));
  const body: Body<'conversation.create'> = {
    ...req.where,
    provider: req.provider,
    model: req.model,
    ...(req.profile ? { profile: req.profile } : {}),
    ...(req.launch ? { launch: req.launch } : {}),
    ...(req.worktree ? { worktree: true } : {}),
    ...(req.first && firstClientId
      ? {
          first: {
            clientId: firstClientId,
            text: req.first.text,
            attachmentIds: (req.first.attachments ?? []).map((a) => a.id),
          },
        }
      : {}),
  };
  const handle = runCommand(m, 'conversation.create', body);
  const conversation = handle.done.then((st) => {
    if (firstClientId) settleSubmit(firstClientId, st);
    if (st.k !== 'done' || !st.result.id) return null;
    const id = st.result.id as ConvId;
    if (firstClientId) setConversationOf(firstClientId, id);
    const epoch = machinesStore.get(m)?.epoch;
    if (epoch) {
      const release = hub.openNewConversation(m, id, epoch);
      pageClock().setTimeout(release, HOLD_NEW_MS);
    }
    return id;
  });
  return { ...handle, firstClientId, conversation };
}
