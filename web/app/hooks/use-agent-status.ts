// The runtime's status socket (/api/status): every tab's state into the tab store, and the page's answers back.
import { useEffect } from 'react';
import { ReconnectingSocket } from '@/runtime/socket';
import { homeSocketUrl } from '@/runtime/base';
import useTabStore from '@/hooks/use-tab-store';
import { setPromptQueue, syncPromptQueues } from '@/hooks/use-prompt-queue';
import { setBackgroundTasks, syncBackgroundTasks } from '@/hooks/use-background-tasks';
import type { IStatusEntry } from '@/types/status';
import type { IQueuedPrompt } from '@/types/server/hl-prompt-queue';
import type { IBackgroundTask } from '@/types/server/hl-background';

let socket: ReconnectingSocket | null = null;
let users = 0;

/** One message of the status socket, into the store it belongs to. */
export const onStatusMessage = (raw: string) => {
  let msg: { type?: string; [k: string]: unknown };
  try {
    msg = JSON.parse(raw) as typeof msg;
  } catch {
    return;
  }
  const store = useTabStore.getState();
  switch (msg.type) {
    case 'status:sync':
      store.replaceAll((msg.tabs ?? {}) as Record<string, IStatusEntry>);
      break;
    case 'status:update': {
      const { type: _type, tabId, ...entry } = msg;
      if (typeof tabId !== 'string') break;
      const gone = entry.cliState === null && entry.workspaceId === '' && entry.tabName === '';
      store.upsert(tabId, gone ? null : (entry as unknown as IStatusEntry));
      break;
    }
    case 'status:hook-event': {
      const event = msg.event as IStatusEntry['lastEvent'];
      if (typeof msg.tabId === 'string' && event)
        store.patch(msg.tabId, { lastEvent: event, eventSeq: event.seq });
      break;
    }
    case 'prompt-queue:sync':
      syncPromptQueues((msg.queues ?? {}) as Record<string, IQueuedPrompt[]>);
      break;
    case 'prompt-queue:update':
      if (typeof msg.tabId === 'string') setPromptQueue(msg.tabId, (msg.items ?? []) as IQueuedPrompt[]);
      break;
    case 'background:sync':
      syncBackgroundTasks((msg.tabs ?? {}) as Record<string, IBackgroundTask[]>);
      break;
    case 'background:update':
      if (typeof msg.tabId === 'string')
        setBackgroundTasks(msg.tabId, (msg.tasks ?? []) as IBackgroundTask[]);
      break;
    default:
      window.dispatchEvent(new CustomEvent('hl:status-message', { detail: msg }));
  }
};

const ensureSocket = () => {
  if (socket) return;
  socket = new ReconnectingSocket({
    url: () => homeSocketUrl('/api/status'),
    onMessage: (data) => onStatusMessage(String(data)),
  });
};

/** Tells the runtime a finished agent tab has been looked at; false when the socket is not open (nothing changes). */
export const dismissTab = (tabId: string): boolean => {
  if (socket?.send(JSON.stringify({ type: 'status:tab-dismissed', tabId })) !== true) return false;
  // The runtime answers every page but the one that said it, so this page marks the tab itself.
  useTabStore.getState().markSeen(tabId, Date.now());
  return true;
};

/** Confirms the page applied the runtime's pane intents; false when the socket is not open (the caller posts instead). */
export const sendPaneIntentsApplied = (tabId: string, ids: string[], device: string): boolean => {
  if (!socket) return false;
  return socket.send(JSON.stringify({ type: 'pane-intents:applied', tabId, ids, device })) !== false;
};

export default function useAgentStatus(): void {
  useEffect(() => {
    users += 1;
    ensureSocket();
    const ping = window.setInterval(() => socket?.send('{"type":"ping"}'), 20_000);
    return () => {
      window.clearInterval(ping);
      users -= 1;
      if (users === 0) {
        socket?.close();
        socket = null;
      }
    };
  }, []);
}
