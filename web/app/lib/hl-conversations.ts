import { t } from '@/lib/i18n';
import type { IConversationMeta } from '@/types/hl-tab-meta';

export interface ChatConversation {
  id: string;
  title: string;
  updated_at: number;
}
/** Agent entries carry the tab's Hl metadata (launch, mode) as GET conversations returns it. */
export interface AgentConversation extends IConversationMeta {
  id: string;
  title: string;
  updatedAt: number;
  provider: string;
  workspaceId: string;
  workspaceName?: string;
  machineId?: string;
  machineName?: string;
  jsonlPath?: string;
}
export interface Conversation extends IConversationMeta {
  key: string;
  id: string;
  title: string;
  updatedAt: number;
  provider: string;
  kind: 'chat' | 'agent';
  workspaceId?: string;
  workspaceName?: string;
  machineId?: string;
  machineName?: string;
  jsonlPath?: string;
}
export const agentConversationKey = (id: string, machineId?: string): string =>
  machineId ? `agent:${machineId}:${id}` : `agent:${id}`;
export const chatConversationKey = (id: string): string => `chat:${id}`;

/** What a conversation key points at; tab ids repeat across machines, so an agent ref keeps its machine. */
export type TConversationRef =
  { kind: 'chat'; id: string } | { kind: 'agent'; id: string; machineId?: string };

const REF_PART_RE = /^[A-Za-z0-9._-]{1,128}$/;

export const parseConversationRef = (key: string): TConversationRef | null => {
  const [kind, ...rest] = key.split(':');
  if (!rest.every((part) => REF_PART_RE.test(part))) return null;
  if (kind === 'chat' && rest.length === 1) return { kind: 'chat', id: rest[0] };
  if (kind === 'agent' && rest.length === 1) return { kind: 'agent', id: rest[0] };
  if (kind === 'agent' && rest.length === 2) return { kind: 'agent', id: rest[1], machineId: rest[0] };
  return null;
};

export const withConversationMachine = (item: Conversation, defaultMachineId?: string): Conversation => {
  if (item.kind !== 'agent') return item;
  const machineId = item.machineId || defaultMachineId;
  return { ...item, machineId, key: agentConversationKey(item.id, machineId) };
};
export const mergeConversations = (chats: ChatConversation[], agents: AgentConversation[]): Conversation[] =>
  [
    ...chats.map((chat) => ({
      key: chatConversationKey(chat.id),
      id: chat.id,
      title: chat.title,
      updatedAt: chat.updated_at * 1000,
      provider: 'chat',
      kind: 'chat' as const,
    })),
    ...agents.map((agent) => ({
      ...agent,
      key: agentConversationKey(agent.id, agent.machineId),
      kind: 'agent' as const,
    })),
  ].sort((a, b) => b.updatedAt - a.updatedAt || a.key.localeCompare(b.key));
export const selectionFromChatPath = (
  path: string,
  current: Conversation,
  chats: ChatConversation[],
): Conversation => {
  if (current.kind !== 'chat') return current;
  const match = path.match(/^\/c\/([a-zA-Z0-9-]+)$/);
  if (match) {
    const chat = chats.find((c) => c.id === match[1]);
    const title =
      chat?.title || (current.id === match[1] ? current.title : t('lib.hlConversations.newConversation'));
    if (current.id === match[1] && current.title === title) return current;
    return {
      key: chatConversationKey(match[1]),
      id: match[1],
      title,
      kind: 'chat',
      provider: 'chat',
      updatedAt: (chat?.updated_at || Date.now() / 1000) * 1000,
    };
  }
  if (path === '/' && current.id !== 'new')
    return {
      key: chatConversationKey('new'),
      id: 'new',
      title: t('lib.hlConversations.newConversation'),
      kind: 'chat',
      provider: 'chat',
      updatedAt: Date.now(),
    };
  return current;
};

const WRAPPER_TAG =
  /<\/?(pasted_content|system-reminder|cc-memory|cross-session-message|command-[a-z-]+|local-command-[a-z-]+)[^>]*>/g;

/** Agent titles come from raw prompts: strip transport wrappers and collapse whitespace. */
export const cleanConversationTitle = (title: string): string => {
  const text = title.replace(WRAPPER_TAG, ' ').replace(/\s+/g, ' ').trim();
  return text || t('lib.hlConversations.untitled');
};

export type TConversationGroup = 'today' | 'yesterday' | 'week' | 'older';

export const CONVERSATION_GROUP_LABELS: Record<TConversationGroup, string> = {
  today: t('lib.hlConversations.groupToday'),
  yesterday: t('lib.hlConversations.groupYesterday'),
  week: t('lib.hlConversations.groupWeek'),
  older: t('lib.hlConversations.groupOlder'),
};

export const conversationGroup = (updatedAt: number, now = Date.now()): TConversationGroup => {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  const today = start.getTime();
  if (updatedAt >= today) return 'today';
  if (updatedAt >= today - 86_400_000) return 'yesterday';
  if (updatedAt >= today - 6 * 86_400_000) return 'week';
  return 'older';
};

export const groupConversations = (items: Conversation[], now = Date.now()) => {
  const groups: { group: TConversationGroup; items: Conversation[] }[] = [];
  for (const item of items) {
    const group = conversationGroup(item.updatedAt, now);
    const last = groups.at(-1);
    if (last?.group === group) last.items.push(item);
    else groups.push({ group, items: [item] });
  }
  return groups;
};
