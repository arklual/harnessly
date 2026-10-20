import type { TProviderStatus } from '@/components/agents/provider-icon';
import type { Conversation } from '@/lib/hl-conversations';
import type { ICurrentAction, ILastEvent } from '@/types/status';
import type { IPermissionRequest } from '@/types/codex-permission';
import { t } from '@/lib/i18n';

/** The parts of a tab's status the notifications list reads; the local tab store and a machine's status socket both have them. */
export interface INoticeSource {
  lastEvent?: ILastEvent | null;
  lastAssistantMessage?: string | null;
  permissionRequest?: IPermissionRequest | null;
  currentAction?: ICurrentAction | null;
  readyForReviewAt?: number | null;
  dismissedAt?: number | null;
}

export interface INoticeDetail {
  /** What the agent asks, or the head of its last answer. */
  question: string | null;
  answer: string | null;
  at: number | null;
  /** The finished turn was opened (or «Mark all read»); cleared by the next prompt. */
  seen: boolean;
}

export const NOTICE_GROUP = {
  waiting: 'waiting',
  finished: 'finished',
  failed: 'failed',
} as const;

export type TNoticeGroup = (typeof NOTICE_GROUP)[keyof typeof NOTICE_GROUP];

export const NOTICE_GROUP_LABELS: Record<TNoticeGroup, string> = {
  waiting: t('lib.hlNotices.groupWaiting'),
  finished: t('lib.hlNotices.groupFinished'),
  failed: t('lib.hlNotices.groupFailed'),
};

export interface INotice {
  key: string;
  conversation: Conversation;
  group: TNoticeGroup;
  status: TProviderStatus;
  text: string | null;
  at: number;
  unread: boolean;
}

export type TNoticeGroups = Record<TNoticeGroup, INotice[]>;

/** A seen finished turn stays in Finished this long, so the list is not empty right after reading it. */
export const SEEN_WINDOW_MS = 24 * 60 * 60 * 1000;
const GROUP_LIMIT = 20;

export const permissionText = (request: IPermissionRequest | null | undefined): string | null => {
  if (!request) return null;
  if (request.type === 'ExecApprovalRequest')
    return t('lib.hlNotices.allowCommand', { command: request.command });
  if (request.type === 'ApplyPatchApprovalRequest')
    return t('lib.hlNotices.allowEdit', {
      files: request.patches.map((p) => p.path.split('/').pop()).join(', '),
    });
  return t('lib.hlNotices.asksPermission');
};

export const noticeDetailOf = (tab: INoticeSource): INoticeDetail => ({
  question:
    permissionText(tab.permissionRequest) ??
    (tab.currentAction?.summary
      ? t('lib.hlNotices.allowAction', { action: tab.currentAction.summary })
      : null),
  answer: tab.lastAssistantMessage?.replace(/\s+/g, ' ').trim() || null,
  at: tab.readyForReviewAt ?? tab.lastEvent?.at ?? null,
  seen: Boolean(tab.dismissedAt),
});

const GROUP_OF: Partial<Record<TProviderStatus, TNoticeGroup>> = {
  'needs-input': NOTICE_GROUP.waiting,
  ready: NOTICE_GROUP.finished,
  done: NOTICE_GROUP.finished,
  failed: NOTICE_GROUP.failed,
};

/**
 * The bell's list: who waits for an answer, whose turn finished (unread first, seen ones for a day), whose turn failed.
 * Only agent conversations with a status take part; newest first inside a group.
 */
export const composeNotices = (
  conversations: readonly Conversation[],
  statuses: Readonly<Record<string, TProviderStatus>>,
  details: Readonly<Record<string, INoticeDetail>>,
  now: number,
): TNoticeGroups => {
  const groups: TNoticeGroups = { waiting: [], finished: [], failed: [] };
  for (const conversation of conversations) {
    const status = statuses[conversation.key];
    const group = status && GROUP_OF[status];
    if (!group) continue;
    const detail = details[conversation.key];
    const at = detail?.at ?? conversation.updatedAt;
    const unread = status === 'needs-input' || status === 'ready' || (status === 'failed' && !detail?.seen);
    if (!unread && now - at > SEEN_WINDOW_MS) continue;
    const text = group === NOTICE_GROUP.waiting ? (detail?.question ?? null) : (detail?.answer ?? null);
    groups[group].push({ key: conversation.key, conversation, group, status, text, at, unread });
  }
  for (const list of Object.values(groups))
    list.sort((a, b) => Number(b.unread) - Number(a.unread) || b.at - a.at);
  return {
    waiting: groups.waiting.slice(0, GROUP_LIMIT),
    finished: groups.finished.slice(0, GROUP_LIMIT),
    failed: groups.failed.slice(0, GROUP_LIMIT),
  };
};
