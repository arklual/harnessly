import { plural, t } from '@/next/i18n';
import { clock } from '@/next/i18n/format';
import { limitWaitText } from '@/next/features/chat/limit';
import { CHAT_STATUS, type TChatStatus } from '@/next/features/sidebar/types';

interface IStatusLook {
  icon: string;
  label: string;
}

/** The icon and the words of each status, as the design has them; `none` has neither. */
export const STATUS_LOOK: Record<Exclude<TChatStatus, 'none'>, IStatusLook> = {
  thinking: { icon: 'loader-circle', label: t('next.chatStatus.thinking') },
  background: { icon: 'loader-circle', label: t('next.chatStatus.background') },
  question: { icon: 'circle-question-mark', label: t('next.chatStatus.question') },
  perm: { icon: 'shield-question', label: t('next.chatStatus.perm') },
  plan: { icon: 'list-checks', label: t('next.chatStatus.plan') },
  done: { icon: 'circle', label: t('next.chatStatus.done') },
  failed: { icon: 'circle-alert', label: t('next.chatStatus.failed') },
  stopped: { icon: 'circle-stop', label: t('next.chatStatus.stopped') },
  'waiting-machine': { icon: 'pause', label: t('next.chatStatus.waitingMachine') },
  limited: { icon: 'clock', label: t('next.chatStatus.limited') },
};

// The parts of a v2 Conversation (protocol/v2.ts) the status reads; structural, so the store's type fits as it is.
type TAgentState =
  | { k: 'starting' | 'working' | 'unknown' | 'ready' | 'idle' | 'gone' }
  | { k: 'waiting-question' | 'waiting-permission' }
  | { k: 'stopped'; resumable?: boolean }
  | { k: 'launch-failed' };

export interface IStatusSource {
  state: TAgentState;
  gate: { kind: 'question' | 'permission' | 'plan' } | null;
  lastTurn: { status: 'live' | 'done' | 'interrupted' | 'failed'; endedAt: number | null } | null;
  seenAt: number;
  /** The chat is held on its agent's usage limit; absent when it is not. */
  limited?: { resetsAt: number | null };
  /** Background work the agent waits for (subagents, commands, monitors, a wakeup together); absent or 0 when none. */
  bgAgents?: number;
  /** What that is made of; absent from an older server, which sends only the total. */
  bgWait?: {
    agents?: number;
    teammates?: number;
    commands?: number;
    monitors?: number;
    workflows?: number;
    command?: string;
    wakeupAt?: number;
  };
}

/**
 * The row's status from a v2 conversation and its machine's reach. What waits for the person comes first; a finished
 * turn shows only until it is read (`seenAt`), so the list does not fill with checkmarks.
 */
export const chatStatusOf = (conv: IStatusSource, machineOnline: boolean): TChatStatus => {
  const { state, gate, lastTurn, seenAt, bgAgents = 0 } = conv;
  const busy = state.k === 'working' || state.k === 'starting' || state.k === 'unknown';
  if (!machineOnline && busy) return CHAT_STATUS.waitingMachine;
  if (state.k === 'waiting-question') return CHAT_STATUS.question;
  if (state.k === 'waiting-permission') return gate?.kind === 'plan' ? CHAT_STATUS.plan : CHAT_STATUS.perm;
  if (state.k === 'working' || state.k === 'starting') return CHAT_STATUS.thinking;
  if (state.k === 'launch-failed') return CHAT_STATUS.failed;
  // Held on the limit: nothing to read and nothing to answer, the agent is not at work — the clock says it will be.
  if (conv.limited) return CHAT_STATUS.limited;
  // Free at its prompt but waiting for background work (subagents, commands, monitors, its wakeup): still at work, not
  // finished and not to be read yet.
  if (bgAgents > 0 && (state.k === 'ready' || state.k === 'idle')) return CHAT_STATUS.background;
  const unread = lastTurn !== null && lastTurn.endedAt !== null && lastTurn.endedAt > seenAt;
  if (!unread) return CHAT_STATUS.none;
  if (lastTurn.status === 'failed') return CHAT_STATUS.failed;
  if (lastTurn.status === 'interrupted') return CHAT_STATUS.stopped;
  if (lastTurn.status === 'done') return CHAT_STATUS.done;
  return CHAT_STATUS.none;
};

/**
 * What a chat at work in the background waits for, in words: «Ждёт: 2 сабагента, команда npm test, пробуждение в
 * 12:30». With `now`, a wakeup whose time has come is left out. Null when the server did not say what (an older one sends only
 * the total).
 */
export const waitsForOf = (conv: Pick<IStatusSource, 'bgWait'>, now?: number): string | null => {
  const wait = conv.bgWait;
  if (!wait) return null;
  const parts: string[] = [];
  if (wait.agents) parts.push(plural(wait.agents, 'next.chatStatus.wait.agents'));
  if (wait.teammates) parts.push(plural(wait.teammates, 'next.chatStatus.wait.teammates'));
  if (wait.workflows) parts.push(plural(wait.workflows, 'next.chatStatus.wait.workflows'));
  if (wait.command) parts.push(t('next.chatStatus.wait.command', { title: wait.command }));
  else if (wait.commands) parts.push(plural(wait.commands, 'next.chatStatus.wait.commands'));
  if (wait.monitors) parts.push(plural(wait.monitors, 'next.chatStatus.wait.monitors'));
  if (wait.wakeupAt && (now === undefined || wait.wakeupAt > now)) {
    parts.push(t('next.chatStatus.wait.wakeup', { time: clock(wait.wakeupAt) }));
  }
  return parts.length > 0 ? t('next.chatStatus.waitsFor', { list: parts.join(', ') }) : null;
};

/** What the row says beside its status when the status has more to tell: what the chat waits for, or until when. */
export const statusNoteOf = (conv: IStatusSource, status: TChatStatus, now?: number): string | null => {
  if (status === CHAT_STATUS.background) return waitsForOf(conv, now);
  if (status === CHAT_STATUS.limited && conv.limited) return limitWaitText(conv.limited, now ?? Date.now());
  return null;
};
