// The person's machines and their sockets. The active machine holds machine, convs and the open conversation; the
// others hold only convs (badges and toasts), and their sockets close after 10 minutes of a hidden tab.

import { useEffect } from 'react';
import { pageClock } from '@/next/core/clock';
import { deviceId } from '@/next/core/device';
import { getPage } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { storeSink } from '@/next/core/realtime/apply';
import {
  browserEnv,
  RealtimeClient,
  type Env,
  type OfflineCause,
  type Sink,
  type SocketLike,
} from '@/next/core/realtime/client';
import { batch } from '@/next/core/store';
import { holdConversation } from '@/next/stores/cache';
import { onReconnectNow } from '@/next/stores/connection';
import { ensureFeed, startEmptyFeed } from '@/next/stores/conversation';
import { activeMachineStore } from '@/next/stores/machines';
import { CAPS_QUERY, parsePage, type Channel, type ConvId, type MachineId } from '@/next/protocol/v2';

export const APP = 'next/0.1.0';
export const HIDDEN_CLOSE_MS = 10 * 60 * 1000;

export interface HubOptions {
  socket?: (url: string) => SocketLike;
  env?: Env;
  random?: () => number;
  sink?: Sink;
  probe?: (m: MachineId) => Promise<OfflineCause>;
}

/** Why a machine's socket does not open: asks its /api/v2/hello through the gateway. */
export async function probeMachine(m: MachineId): Promise<OfflineCause> {
  const res = await getPage(machineApi(m).http('/hello'), parsePage.hello, 5000);
  if ('ok' in res) return 'network';
  if (res.r === 'failed' && res.code === 'http') {
    if (res.status === 401 || res.status === 403) return 'unauthorized';
    if (res.status >= 502 && res.status <= 504) return 'machine-unreachable';
  }
  return 'network';
}

class Hub {
  private clients = new Map<MachineId, RealtimeClient>();
  private held = new Map<MachineId, (() => void)[]>();
  private o: HubOptions = {};
  private hiddenTimer: unknown = null;
  private unVisible: (() => void) | null = null;

  configure(o: HubOptions): void {
    this.o = o;
  }

  /** Opens a socket for each machine; each holds convs and its record, the active one its record with load. */
  start(machines: MachineId[], active: MachineId | null): void {
    batch(() => {
      for (const m of machines) this.ensure(m);
      this.setActive(active ?? machines[0] ?? null);
    });
    this.watchHidden();
  }

  client(m: MachineId): RealtimeClient | undefined {
    return this.clients.get(m);
  }

  ensure(m: MachineId): RealtimeClient {
    let c = this.clients.get(m);
    if (c) return c;
    const clock = pageClock();
    c = new RealtimeClient({
      machine: m,
      url: machineApi(m).ws(`/socket?${CAPS_QUERY}`),
      device: deviceId(),
      app: APP,
      sink: this.o.sink ?? storeSink,
      clock,
      socket: this.o.socket ?? ((url) => new WebSocket(url) as unknown as SocketLike),
      env: this.o.env ?? browserEnv,
      random: this.o.random,
      probe: () => (this.o.probe ?? probeMachine)(m),
    });
    this.clients.set(m, c);
    // Every machine holds its record too (without load): its health colours its dot among the other machines.
    this.held.set(m, [c.subscribe('convs'), c.subscribe('machine')]);
    c.start();
    return c;
  }

  setActive(m: MachineId | null): void {
    const prev = activeMachineStore.get();
    if (prev === m) return;
    if (prev) this.holdMachine(prev, false);
    activeMachineStore.set(m);
    // The machine card at the bottom of the sidebar shows its load: the active machine reports it every 2 s.
    if (m) this.holdMachine(m, true);
  }

  /** The machine keeps convs and holds its record anew, with load or without (a channel's options are its first
   *  holder's, so the record is let go before it is held the other way). */
  private holdMachine(m: MachineId, load: boolean): void {
    const c = this.ensure(m);
    const list = this.held.get(m) ?? [];
    list.slice(1).forEach((release) => release());
    this.held.set(m, [...list.slice(0, 1), c.subscribe('machine', load ? { load: true } : undefined)]);
  }

  /** Holds a conversation's channel and its place in the cache; the release lets both go. */
  openConversation(m: MachineId, conv: ConvId): () => void {
    ensureFeed(conv, m);
    const unhold = holdConversation(conv);
    const release = this.ensure(m).subscribe(`conv:${conv}` as Channel);
    return () => {
      release();
      unhold();
    };
  }

  /** A conversation that conversation.create just made: its channel from the first event. */
  openNewConversation(m: MachineId, conv: ConvId, epoch: string): () => void {
    startEmptyFeed(conv, m, pageClock().now());
    const unhold = holdConversation(conv);
    const release = this.ensure(m).subscribeNew(`conv:${conv}` as Channel, epoch);
    return () => {
      release();
      unhold();
    };
  }

  reconnectNow(m: MachineId): void {
    this.clients.get(m)?.reconnectNow();
  }

  stop(): void {
    for (const c of this.clients.values()) c.stop();
    this.clients.clear();
    this.held.clear();
    this.unVisible?.();
    this.unVisible = null;
    pageClock().clearTimeout(this.hiddenTimer);
    activeMachineStore.set(null);
  }

  /** A hidden tab lets the sockets of the inactive machines go after 10 minutes; visible brings them back. */
  private watchHidden(): void {
    if (this.unVisible || typeof document === 'undefined') return;
    const onChange = () => {
      const clock = pageClock();
      clock.clearTimeout(this.hiddenTimer);
      if (document.visibilityState === 'hidden') {
        this.hiddenTimer = clock.setTimeout(() => {
          const active = activeMachineStore.get();
          for (const [m, c] of this.clients) if (m !== active) c.stop();
        }, HIDDEN_CLOSE_MS);
      } else {
        for (const c of this.clients.values()) c.start();
      }
    };
    document.addEventListener('visibilitychange', onChange);
    this.unVisible = () => document.removeEventListener('visibilitychange', onChange);
  }
}

export const hub = new Hub();
onReconnectNow((m) => hub.reconnectNow(m));

/** Holds a conversation's channel while the component is mounted: the cached feed draws at once, the delta follows. */
export function useConversationChannel(m: MachineId | null, conv: ConvId | null): void {
  useEffect(() => {
    if (!m || !conv) return;
    return hub.openConversation(m, conv);
  }, [m, conv]);
}
