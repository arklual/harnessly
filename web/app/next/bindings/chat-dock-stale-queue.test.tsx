// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));

import ChatDock from '@/next/bindings/chat-dock';
import { hub } from '@/next/core/realtime/hub';
import type { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { ConvId, MachineId, Message } from '@/next/protocol/v2';
import { draftText } from '@/next/stores/drafts';
import { applyMessage, messagesStore } from '@/next/stores/messages';

// the queue over the field showed messages that were delivered hours ago (a chat cached in the browser as
// queued{agent-busy}); Изменить and Delete on them said «не получилось». The runtime now answers a command on a
// message that is over with the message as it is; the row leaves and nothing comes back into the field to be sent again.

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const ID = '01a170dc-1403-7834-b13f-05cf994a9033';
const TEXT = 'Смотри, отправил небольшой текст. Точнее, отправил большой текст наговаренный.';

const record = (over: Partial<Message> = {}): Message => ({
  clientId: ID,
  conversationId: CONV,
  text: TEXT,
  attachments: [{ id: 'up_5GURGK5YLFFX2MQ4', name: 'CleanShot.png', kind: 'image', mime: 'image/png' }],
  mode: 'queue',
  kind: 'prompt',
  createdAt: 1792894833946,
  attempts: 0,
  // Second in the line: it stands in the queue whatever the agent is doing.
  state: { k: 'queued', position: 2, reason: 'behind', wait: null },
  rev: 4,
  ...over,
});

let rt: FakeRuntime;
let machine: MachineId;

beforeEach(async () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  useFakeTime();
  ({ rt, m: machine } = await bootFake());
  hub.openConversation(machine, CONV);
  await settle();
});
afterEach(() => {
  cleanup();
  hub.stop();
  vi.useRealTimers();
});

/** The row of this message in the queue, unless it is on its way out. */
const myRow = () =>
  screen
    .queryAllByRole('listitem')
    .find((el) => el.textContent?.includes('Смотри, отправил') && el.dataset.leaving === undefined);
const edit = () => fireEvent.click(within(myRow()!).getByRole('button', { name: t('next.queue.edit') }));

describe('a stale row in the queue', () => {
  it('Изменить of a delivered message clears the row and leaves the field empty', async () => {
    act(() => applyMessage(machine, record()));
    render(<ChatDock conv={CONV} machine={machine} />);
    expect(myRow()).toBeDefined();
    rt.handlers.set('cancel', (_, r) => ({
      r: 'accepted',
      ch: CH,
      seq: r.emit(CH, {
        type: 'message_upsert',
        message: record({ state: { k: 'done', turnId: 'c:dc4de923-f69f-4b40-b34b-b98cc7e00140#0' }, rev: 8 }),
      }),
    }));
    edit();
    await act(settle);
    await act(settle);
    expect(messagesStore.get(ID)?.state.k).toBe('done');
    expect(myRow()).toBeUndefined();
    expect(draftText(CONV)).toBe('');
  });

  it('Изменить of a message still in the line takes it back into the field, as before', async () => {
    act(() => applyMessage(machine, record()));
    render(<ChatDock conv={CONV} machine={machine} />);
    rt.handlers.set('cancel', (_, r) => ({
      r: 'accepted',
      ch: CH,
      seq: r.emit(CH, { type: 'prompt_state', clientId: ID, state: { k: 'cancelled', by: 'user' }, rev: 5 }),
    }));
    edit();
    await act(settle);
    await act(settle);
    expect(myRow()).toBeUndefined();
    expect(draftText(CONV)).toBe(TEXT);
  });
});
