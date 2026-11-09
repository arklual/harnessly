import { beforeEach, describe, expect, it, vi } from 'vitest';

// HRN-879, spec v10: a message to a machine that is off waits in the cluster's queue too — the very send command the
// page sends the machine, so the machine takes it once whoever comes first — and another device of the person shows it.

const net = vi.hoisted(() => ({ getPage: vi.fn(), sendBody: vi.fn(), sendPage: vi.fn() }));
vi.mock('@/next/core/net/http', () => ({
  getPage: net.getPage,
  sendBody: net.sendBody,
  sendPage: net.sendPage,
  postCommand: vi.fn(),
  sendJson: vi.fn(),
  sendBlob: vi.fn(),
}));
vi.mock('@/next/core/net/machine', () => ({
  gatewayPath: (p: string) => `/gw${p}`,
  machineApi: (m: string) => ({ http: (p: string) => `/m/${m}${p}` }),
}));

import {
  adoptParked,
  parkInCluster,
  resetClusterQueue,
  sendOf,
  unparkInCluster,
} from '@/next/commands/cluster-queue';
import { newView } from '@/next/commands/messages';
import { centralStore } from '@/next/stores/central';
import { forgetMessage, messagesStore, putLocal } from '@/next/stores/messages';
import type { ClientId, ConvId } from '@/next/protocol/v2';

const CONV = 'tab-q' as ConvId;

const keep = (off: string[]) =>
  centralStore.set({
    enabled: true,
    off: new Set(off),
    moved: new Set(),
    names: new Map(),
    on: new Set(['a']),
  });

const message = (id: string, extra: Partial<Parameters<typeof newView>[3]> = {}) => {
  const view = newView('a', id as ClientId, CONV, { text: `hello ${id}`, ...extra });
  putLocal(view);
  return view;
};

describe('the cluster keeps a message to a machine that is off', () => {
  beforeEach(() => {
    resetClusterQueue();
    for (const id of [...messagesStore.keys()]) forgetMessage(id);
    net.getPage.mockReset();
    net.sendBody.mockReset();
    net.sendPage.mockReset();
  });

  it('as the very send command the page sends the machine', async () => {
    keep(['a']);
    const m = message('c1');
    net.sendBody.mockResolvedValue({
      ok: true,
      value: { id: 'q1', state: 'queued', createdAt: 1, send: null },
    });
    expect(await parkInCluster(m.clientId)).toBe(true);
    const [method, url, body] = net.sendBody.mock.calls[0] as [string, string, Record<string, unknown>];
    expect(method).toBe('POST');
    expect(url).toBe('/gw/offline-prompts?machine=a');
    expect(body).toEqual({
      tabId: CONV,
      text: 'hello c1',
      send: {
        opId: 'c1',
        clientId: 'c1',
        conversationId: CONV,
        text: 'hello c1',
        attachmentIds: [],
        mode: 'queue',
        kind: 'prompt',
      },
    });
    // Sent again by the outbox: kept once.
    expect(await parkInCluster(m.clientId)).toBe(true);
    expect(net.sendBody).toHaveBeenCalledTimes(1);
  });

  it('not while the machine is on, nor with files attached (they live on the machine)', async () => {
    keep([]);
    expect(await parkInCluster(message('c2').clientId)).toBe(false);
    keep(['a']);
    const withFile = message('c3', {
      attachments: [{ id: 'u1', name: 'a.png', size: 1, mime: 'image/png' } as never],
    });
    expect(await parkInCluster(withFile.clientId)).toBe(false);
    expect(net.sendBody).not.toHaveBeenCalled();
  });

  it('asks a gateway without the queue (a hub) once', async () => {
    keep(['a']);
    net.sendBody.mockResolvedValue({ r: 'failed', code: 'http', status: 404 });
    expect(await parkInCluster(message('c4').clientId)).toBe(false);
    expect(await parkInCluster(message('c5').clientId)).toBe(false);
    expect(net.sendBody).toHaveBeenCalledTimes(1);
  });

  it('takes the message back from the cluster when the person deletes it', async () => {
    keep(['a']);
    const m = message('c6');
    net.sendBody.mockResolvedValue({
      ok: true,
      value: { id: 'q6', state: 'queued', createdAt: 1, send: null },
    });
    await parkInCluster(m.clientId);
    net.sendPage.mockResolvedValue({ ok: true, value: null });
    await unparkInCluster(m);
    expect(net.sendPage.mock.calls[0]?.slice(0, 2)).toEqual([
      'DELETE',
      '/gw/offline-prompts?machine=a&client=c6',
    ]);
  });

  it('shows on another device what waits there, once, and nothing already gone', async () => {
    keep(['a']);
    const waiting = sendOf({
      ...newView('a', 'c7' as ClientId, CONV, { text: 'from the phone' }),
      conversationId: CONV,
    });
    const gone = { ...waiting, opId: 'c8', clientId: 'c8' as ClientId };
    net.getPage.mockResolvedValue({
      ok: true,
      value: [
        { id: 'q7', state: 'queued', createdAt: 1, send: waiting },
        { id: 'q8', state: 'delivered', createdAt: 2, send: gone },
        { id: 'q9', state: 'queued', createdAt: 3, send: null },
      ],
    });
    const view = vi.fn((s: typeof waiting) => newView('a', s.clientId, CONV, { text: s.text }));
    expect(await adoptParked('a', CONV, view)).toEqual(['c7']);
    expect(net.getPage.mock.calls[0]?.[0]).toBe('/gw/offline-prompts?machine=a&tab=tab-q');
    expect(messagesStore.get('c7' as ClientId)?.text).toBe('from the phone');
    expect(messagesStore.get('c8' as ClientId)).toBeUndefined();
    expect(await adoptParked('a', CONV, view)).toEqual([]);
  });
});
