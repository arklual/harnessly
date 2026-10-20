/// <reference lib="webworker" />
import { createMuxHub, type IMuxHub } from '@/lib/mux-hub';

/**
 * One mux socket for every tab of the browser: tabs open their channels through this shared worker, so ten
 * tabs cost one WebSocket and none of the six HTTP/1.1 connections the host allows.
 */

export type TMuxPortRequest =
  | { t: 'open'; id: number; url: string; path: string; last: string | null }
  | { t: 'close'; id: number }
  | { t: 'beat' }
  | { t: 'bye' };

export type TMuxPortReply =
  | { id: number; k: 'head'; v: number }
  | { id: number; k: 'data'; v: string }
  | { id: number; k: 'end' | 'drop' | 'unavailable' };

// A background tab's timers run once a minute at most: a port is gone only after several missed beats.
const PORT_GONE_MS = 3 * 60_000;

const ctx = self as unknown as SharedWorkerGlobalScope;
const hubs = new Map<string, IMuxHub>();
const hubOf = (url: string) => {
  let hub = hubs.get(url);
  if (!hub) {
    hub = createMuxHub(url, (address) => new WebSocket(address));
    hubs.set(url, hub);
  }
  return hub;
};

interface IPortState {
  port: MessagePort;
  beat: number;
  channels: Map<number, { hub: IMuxHub; channel: number }>;
}
const ports = new Set<IPortState>();

const forget = (state: IPortState) => {
  ports.delete(state);
  for (const { hub, channel } of state.channels.values()) hub.close(channel);
  state.channels.clear();
};

ctx.onconnect = (event) => {
  const port = event.ports[0];
  const state: IPortState = { port, beat: Date.now(), channels: new Map() };
  ports.add(state);
  const reply = (message: TMuxPortReply) => port.postMessage(message);
  port.onmessage = ({ data }: MessageEvent<TMuxPortRequest>) => {
    state.beat = Date.now();
    if (!ports.has(state)) ports.add(state);
    if (data.t === 'bye') return forget(state);
    if (data.t === 'close') {
      const open = state.channels.get(data.id);
      state.channels.delete(data.id);
      if (open) open.hub.close(open.channel);
      return;
    }
    if (data.t !== 'open') return;
    const { id } = data;
    const hub = hubOf(data.url);
    let settled = false;
    const settle = () => {
      settled = true;
      state.channels.delete(id);
    };
    const channel = hub.open(data.path, data.last, {
      head: (status) => reply({ id, k: 'head', v: status }),
      data: (chunk) => reply({ id, k: 'data', v: chunk }),
      end: () => {
        settle();
        reply({ id, k: 'end' });
      },
      drop: () => {
        settle();
        reply({ id, k: 'drop' });
      },
      unavailable: () => {
        settle();
        reply({ id, k: 'unavailable' });
      },
    });
    if (channel === null) {
      if (!settled) reply({ id, k: 'unavailable' });
      return;
    }
    if (!settled) state.channels.set(id, { hub, channel });
  };
  port.start();
};

setInterval(() => {
  const now = Date.now();
  for (const state of ports) {
    if (now - state.beat < PORT_GONE_MS) continue;
    // A frozen tab that comes back gets its channels as broken and opens them again.
    for (const id of state.channels.keys()) state.port.postMessage({ id, k: 'drop' } satisfies TMuxPortReply);
    forget(state);
  }
}, 60_000);
