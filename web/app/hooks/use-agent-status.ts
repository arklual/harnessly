// The runtime's status socket (/api/status): every tab's state into the tab store, and the page's answers back.
import { useEffect } from 'react';
import { ReconnectingSocket } from '@/runtime/socket';
import { socketUrl } from '@/runtime/base';
import useTabStore from '@/hooks/use-tab-store';
import type { IStatusEntry } from '@/types/status';

let socket: ReconnectingSocket | null = null;
let users = 0;

const onMessage = (raw: string) => {
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
    default:
      window.dispatchEvent(new CustomEvent('hl:status-message', { detail: msg }));
  }
};

const ensureSocket = () => {
  if (socket) return;
  socket = new ReconnectingSocket({
    url: () => socketUrl('/api/status'),
    onMessage: (data) => onMessage(String(data)),
  });
};

/** Tells the runtime a finished agent tab has been looked at. */
export const dismissTab = (tabId: string): void => {
  socket?.send(JSON.stringify({ type: 'status:tab-dismissed', tabId }));
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
