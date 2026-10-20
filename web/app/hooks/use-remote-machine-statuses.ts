import { useEffect } from 'react';
import useFleetStatus from '@/hooks/use-fleet-status';
import { scopedAppPath } from '@/lib/app-path';
import {
  applyStatusMessage,
  noticeDetails,
  providerStatuses,
  type TRemoteTabs,
} from '@/lib/hl-fleet-statuses';
import type { TStatusServerMessage } from '@/types/status';
import { onNetworkWake } from '@/lib/ws-heartbeat';
import { openStatusSocket, type IStatusSocket } from '@/lib/status-stream-client';

const RECONNECT_BASE = 1_000;
const RECONNECT_MAX = 8_000;

/** Open status sockets by machine, so the bell can mark a remote turn as read the way opening it does. */
const sockets = new Map<string, IStatusSocket>();

/** "Mark read" for a conversation of another machine; false when its socket is not open right now. */
export const dismissRemoteTab = (machineId: string, tabId: string): boolean => {
  const ws = sockets.get(machineId);
  if (ws?.readyState !== WebSocket.OPEN) return false;
  ws.send(JSON.stringify({ type: 'status:tab-dismissed', tabId }));
  return true;
};

const watchMachine = (machineId: string) => {
  let ws: IStatusSocket | null = null;
  let tabs: TRemoteTabs = {};
  let retry = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;

  const connect = () => {
    if (stopped) return;
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = openStatusSocket(
      `${protocol}//${location.host}${scopedAppPath('/api/status', machineId)}`,
      machineId,
      `machine:${machineId}`,
    );
    ws.onopen = () => {
      retry = 0;
      if (ws) sockets.set(machineId, ws);
    };
    ws.onmessage = (event) => {
      try {
        const next = applyStatusMessage(tabs, JSON.parse(event.data) as TStatusServerMessage);
        if (next === tabs) return;
        tabs = next;
        useFleetStatus.getState().setMachine(machineId, providerStatuses(tabs), noticeDetails(tabs));
      } catch {
        // not a status message
      }
    };
    ws.onclose = () => {
      if (sockets.get(machineId) === ws) sockets.delete(machineId);
      ws = null;
      if (stopped) return;
      timer = setTimeout(
        () => {
          timer = undefined;
          connect();
        },
        Math.min(RECONNECT_BASE * 2 ** retry++, RECONNECT_MAX),
      );
    };
  };

  connect();
  // Waiting out a long backoff while the network is already back helps nobody.
  const offWake = onNetworkWake(() => {
    if (ws || timer === undefined) return;
    clearTimeout(timer);
    timer = undefined;
    retry = 0;
    connect();
  });
  return () => {
    stopped = true;
    offWake();
    clearTimeout(timer);
    ws?.close();
  };
};

/**
 * Status sockets of the person's other machines through the gateway, so the sidebar shows
 * their conversations working, waiting or finished without opening a machine frame.
 */
const useRemoteMachineStatuses = (machineIds: readonly string[]) => {
  const key = machineIds.join(',');
  useEffect(() => {
    if (!key) return;
    const stops = key.split(',').map(watchMachine);
    return () => stops.forEach((stop) => stop());
  }, [key]);
};

export default useRemoteMachineStatuses;
