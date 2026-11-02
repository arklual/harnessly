// What the host does for a view beyond storage and tools (api.ts): read the chat, post a card into it, ask the person,
// tell them. Each is checked against the manifest by the bridge first; these are the work behind the checks.
import { useMemo } from 'react';
import { ItemsBuilder } from '@/next/bindings/feed-items';
import { BridgeError, type IBridgeHandlers } from '@/next/features/app-views/bridge';
import { askConfirm } from '@/next/features/app-views/confirm';
import { postCard } from '@/next/features/app-views/posted-cards';
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { FeedItem } from '@/next/features/chat/types';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

export interface IChatLine {
  role: 'user' | 'assistant';
  text: string;
}

/** The latest `last` lines of a chat's feed, text only: the person's messages and the agent's answers. */
export const chatLines = (items: readonly FeedItem[] | null, last: number): IChatLine[] => {
  const lines: IChatLine[] = [];
  for (const item of items ?? []) {
    if (item.kind === 'outgoing') {
      if (item.message.text.trim() !== '') lines.push({ role: 'user', text: item.message.text });
    } else if (item.kind === 'turn') {
      if (item.prompt && item.prompt.text.trim() !== '') lines.push({ role: 'user', text: item.prompt.text });
      if (item.answer && item.answer.md.trim() !== '')
        lines.push({ role: 'assistant', text: item.answer.md });
    }
  }
  return lines.slice(-last);
};

/**
 * The chat a view may touch: an app works with a chat only where the person turned it on. No chat is `host_unavailable`
 * (there is none here); a chat the app is not on in is `permission_denied`.
 */
export const chatOf = (conversation: ConvId | null, on: boolean): ConvId => {
  if (conversation === null) throw new BridgeError('host_unavailable', 'no chat here');
  if (!on) throw new BridgeError('permission_denied', 'the app is not turned on in this chat');
  return conversation;
};

/** The handlers of one view: the chat it works beside (null: none) and the app it belongs to. */
export const useHostHandlers = (
  machine: MachineId,
  conversation: ConvId | null,
  app: IInstalledApp,
  /** Whether the app is on in that chat (the chat's `apps`). */
  on: boolean,
): IBridgeHandlers =>
  useMemo<IBridgeHandlers>(() => {
    const chat = (): ConvId => chatOf(conversation, on);
    return {
      chatRead: async (last) => chatLines(new ItemsBuilder(chat(), machine).snapshot(), last),
      chatCard: async (view, data) => {
        const target = chat();
        // Only a view of the app made for the chat is a card of it.
        if (!app.views.some((v) => v.id === view && v.targets.includes('chat')))
          throw new BridgeError('invalid_params', `${view} is not a view of ${app.id} for a chat`);
        if (!postCard(target, { app: app.id, view, data }))
          throw new BridgeError('invalid_params', 'data is too big for a card');
      },
      // The host speaks: the app's real name leads, a view cannot sign as another.
      notify: async (text) => {
        toast(app.name, { description: text });
      },
      confirm: (text) => askConfirm(app.name, text),
    };
  }, [machine, conversation, app, on]);
