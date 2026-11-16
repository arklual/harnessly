import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { hub } from '@/next/core/realtime/hub';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { sendMessage } from '@/next/commands';
import { DEFAULT_SETTINGS, meStore } from '@/next/stores/me';
import { setWhoami } from '@/next/stores/whoami';
import type { ConvId } from '@/next/protocol/v2';

const CONV = 'tab-k3Jx9QmZ' as ConvId;

beforeEach(useFakeTime);
afterEach(() => {
  setWhoami(null);
  hub.stop();
  vi.useRealTimers();
});

const people = (list: string[]) =>
  meStore.set({
    k: 'ready',
    me: { email: null, name: null, settings: { ...DEFAULT_SETTINGS, people: list }, rev: 1 },
  });

describe('who wrote a message', () => {
  it('is the person this browser names, when the machine lists them', async () => {
    const { rt, m } = await bootFake();
    people(['Артём', 'Маша']);
    setWhoami('Маша');
    sendMessage(m, { conversationId: CONV, text: 'Глянь' });
    await settle();
    const sent = rt.commands.filter((c) => c.op === 'send');
    expect(sent.at(-1)?.body).toMatchObject({ by: 'Маша' });
  });

  it('is nobody while this browser names nobody, or someone the machine does not list', async () => {
    const { rt, m } = await bootFake();
    people(['Артём', 'Маша']);
    sendMessage(m, { conversationId: CONV, text: 'раз' });
    setWhoami('Петя');
    sendMessage(m, { conversationId: CONV, text: 'два' });
    await settle();
    for (const c of rt.commands.filter((c) => c.op === 'send')) expect(c.body).not.toHaveProperty('by');
  });
});
