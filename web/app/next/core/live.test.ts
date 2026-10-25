// The core against a real runtime with RUNTIME_V2_FAKE=1 (stage B), opt-in: HX_V2_RUNTIME=127.0.0.1:<port> and
// HX_V2_COOKIE=session-token=… (POST /api/auth/login with INIT_PASSWORD); HX_V2_PID adds the frozen-runtime case
// (SIGSTOP: silence, stale, a socket that does not open, offline; SIGCONT: live again on the same feed).

import { afterAll, describe, expect, it } from 'vitest';
import { setFetch } from '@/next/core/net/http';
import { setApiOrigin } from '@/next/core/net/machine';
import { memoryKV } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import type { SocketLike } from '@/next/core/realtime/client';
import { setScheduler } from '@/next/core/store';
import { sendMessage } from '@/next/commands';
import { bootNext } from '@/next/boot';
import { connectionStore } from '@/next/stores/connection';
import { entryStore, feedStore, key } from '@/next/stores/conversation';
import { messagesStore } from '@/next/stores/messages';
import type { ConvId } from '@/next/protocol/v2';

const HOST = process.env.HX_V2_RUNTIME;
const COOKIE = process.env.HX_V2_COOKIE ?? '';
const PID = Number(process.env.HX_V2_PID ?? 0);
const IDLE = 'tab-s5Qidl' as ConvId;

const until = async (what: string, ok: () => boolean, ms = 20000) => {
  for (const end = Date.now() + ms; !ok(); await new Promise((r) => setTimeout(r, 50))) {
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`);
  }
};

describe.skipIf(!HOST)('a live v2 runtime (RUNTIME_V2_FAKE=1)', () => {
  afterAll(() => hub.stop());

  it('boots, opens a chat and takes a message to its end', async () => {
    setScheduler('sync');
    setApiOrigin({ base: '/agents', host: HOST });
    setFetch((url, init) =>
      fetch(`http://${HOST}${url}`, { ...init, headers: { ...(init.headers as object), Cookie: COOKIE } }),
    );
    const socket = (url: string) =>
      new (WebSocket as unknown as new (u: string, o: object) => SocketLike)(url, {
        headers: { Cookie: COOKIE },
      });
    const st = await bootNext({ kv: () => memoryKV(), lastPerson: null, hub: { socket } });
    expect(st).toEqual({ k: 'ready', machines: ['local'] });
    await until('live', () => connectionStore.get('local')?.k === 'live');

    const release = hub.openConversation('local', IDLE);
    await until('the feed', () => feedStore.get(IDLE)?.source === 'live');
    const states: string[] = [];
    const id = sendMessage('local', { conversationId: IDLE, text: 'live check' });
    const un = messagesStore.subscribeKey(id, () => {
      const k = messagesStore.get(id)?.state.k;
      if (k && states[states.length - 1] !== k) states.push(k);
    });
    await until('the message done', () => messagesStore.get(id)?.state.k === 'done', 30000);
    un();
    console.log(`[live] message states: submitting → ${states.join(' → ')}`);
    release();
  }, 60000);

  it.skipIf(!PID)(
    'frozen runtime: live → stale → offline → live, the feed kept',
    async () => {
      const release = hub.openConversation('local', IDLE);
      await until('the feed', () => feedStore.get(IDLE)?.source === 'live');
      const rows = feedStore.get(IDLE)!.rows;
      const firstTurn = rows.find((r) => r.k === 'turn')!;
      const seen: string[] = [];
      const un = connectionStore.subscribeKey('local', () => {
        const k = connectionStore.get('local')!.k;
        if (seen[seen.length - 1] !== k) seen.push(k);
      });
      const t0 = Date.now();
      process.kill(PID, 'SIGSTOP');
      try {
        await until('stale', () => connectionStore.get('local')?.k === 'stale', 30000);
        const staleAt = Date.now() - t0;
        await until('offline', () => connectionStore.get('local')?.k === 'offline', 40000);
        const offlineAt = Date.now() - t0;
        console.log(`[live] frozen: stale after ${staleAt} ms, offline after ${offlineAt} ms`);
      } finally {
        process.kill(PID, 'SIGCONT');
      }
      await until('live again', () => connectionStore.get('local')?.k === 'live', 40000);
      un();
      console.log(`[live] connection: ${seen.join(' → ')}`);
      expect(feedStore.get(IDLE)!.rows.find((r) => r.id === firstTurn.id)).toBeDefined();
      expect(entryStore.size).toBeGreaterThan(0);
      expect(entryStore.has(key(IDLE, firstTurn.id))).toBe(true);
      release();
    },
    150000,
  );
});
