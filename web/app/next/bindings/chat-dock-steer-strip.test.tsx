// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));

import ChatDock from '@/next/bindings/chat-dock';
import { startOutbox } from '@/next/commands';
import { machineNow } from '@/next/core/clock';
import { hub } from '@/next/core/realtime/hub';
import type { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { ConvEvent, ConvId, Entry, MachineId, Message } from '@/next/protocol/v2';
import { connectionStore } from '@/next/stores/connection';
import { applyMessage } from '@/next/stores/messages';

// 26.10: «Да, и ещё важно интеграцию КиМИ…» stood in the strip over the field with «В очереди агента — прочитает после
// текущего шага» while the feed held it as the person's message and the agent had answered it. Claude had queued it
// cut short, the runtime did not take that entry for it and kept it waiting in-input. The runtime now does; the strip
// lets a message go only by the runtime's word — its state, the entry that carries its client id, the status page —
// never by matching words on the page.

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const TURN = 'c:561a51c8-47ce-4ce5-9197-c79fa5b0fb80#0';
const WHOLE =
  'Да, и ещё важно интеграцию КиМИ, ГЛМ и так далее. Ну, вот КиМИ— это самое важное. А и кодексы катить надо осаб прям Вот, как только будет доступно. Сразу раскатываем на прод.';
const TOOK =
  'Да, и ещё важно интеграцию КиМИ, ГЛМ и так далее. Ну, вот КиМИ— это самое важное. А и кодексы катить н';

let rt: FakeRuntime;
let machine: MachineId;
let stop: (() => void) | null = null;
// A client id of its own per message: the stores of the page outlive a test.
let n = 0;
let ID = '';

const record = (over: Partial<Message> = {}): Message => ({
  clientId: ID,
  conversationId: CONV,
  text: WHOLE,
  attachments: [],
  mode: 'steer',
  kind: 'prompt',
  createdAt: 1792969544071,
  attempts: 0,
  state: {
    k: 'in-input',
    wait: { slowAt: machineNow(machine) + 3000, deadlineAt: machineNow(machine) + 1_800_000 },
  },
  rev: 4,
  ...over,
});

beforeEach(async () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  n += 1;
  ID = `01a17550-1120-7afa-a3cf-${String(n).padStart(12, '0')}`;
  useFakeTime();
  ({ rt, m: machine } = await bootFake());
  hub.openConversation(machine, CONV);
  await settle();
});
afterEach(() => {
  stop?.();
  stop = null;
  cleanup();
  hub.stop();
  vi.useRealTimers();
});

/** Every row of the message in the strip, the one on its way out too. */
const rows = () =>
  screen.queryAllByRole('listitem').filter((el) => el.textContent?.includes('Да, и ещё важно'));
/** The row of the message, unless it is on its way out. */
const standing = () => rows().find((el) => el.dataset.leaving === undefined);

const userEntry = (text: string, clientId: string | null, rev = 1): Entry =>
  ({
    id: `c:642bc047-ca59-4ea4-8a52-${String(n).padStart(12, '0')}#0`,
    turnId: TURN,
    at: machineNow(machine),
    rev,
    t: 'user',
    text,
    attachments: [],
    clientId,
  }) as Entry;

const emit = (e: ConvEvent) => act(() => void rt.emit(CH, e));

/** The turn Claude opened for the queued message: its user entry comes inside it. */
const startTurn = () =>
  emit({
    type: 'turn_started',
    turn: {
      id: TURN,
      author: { provider: 'claude', role: 'lead' },
      startedAt: machineNow(machine),
      endedAt: null,
      durationMs: null,
      toolCount: 0,
      stepCount: 0,
      status: 'live',
      failure: null,
      answerEntryId: null,
      files: [],
    },
  } as ConvEvent);

async function steerInStrip() {
  act(() => applyMessage(machine, record()));
  render(<ChatDock conv={CONV} machine={machine} />);
  await act(settle);
  expect(standing()?.textContent).toContain(t('next.message.handed'));
}

/** The row leaves with its closing animation: first on its way out, then gone. */
async function leaves() {
  expect(standing()).toBeUndefined();
  expect(rows()).toHaveLength(1);
  await act(() => vi.advanceTimersByTimeAsync(1000));
  expect(rows()).toHaveLength(0);
  // Every frame was read: a row did not leave because the page dropped the socket on a frame it could not read.
  expect(connectionStore.get(machine)?.k).toBe('live');
}

describe('a steered message in the strip over the field', () => {
  it('leaves once the feed holds its entry with its client id — the entry cut short as Claude queued it', async () => {
    await steerInStrip();
    startTurn();
    emit({ type: 'step', entry: userEntry(TOOK, ID), counts: null } as ConvEvent);
    await leaves();
  });

  it('an entry first read without its client id and marked later takes the row then', async () => {
    await steerInStrip();
    startTurn();
    emit({ type: 'step', entry: userEntry(TOOK, null), counts: null } as ConvEvent);
    expect(standing()).toBeDefined();
    emit({ type: 'entry_update', entry: userEntry(TOOK, ID, 2) } as ConvEvent);
    await leaves();
  });

  it('the same words in the feed without its client id are not taken for it: the row waits for the runtime', async () => {
    await steerInStrip();
    startTurn();
    for (const text of [WHOLE, TOOK, WHOLE.replaceAll(' ', '\n')]) {
      emit({ type: 'step', entry: userEntry(text, null), counts: null } as ConvEvent);
      expect(standing()).toBeDefined();
    }
    emit({ type: 'message_upsert', message: record({ state: { k: 'in-turn', turnId: TURN }, rev: 6 }) });
    await leaves();
  });

  it('leaves when the runtime says the agent took it: accepted, in the turn, done, cancelled', async () => {
    for (const state of [
      { k: 'accepted', entryId: null },
      { k: 'accepted', entryId: 'c:642bc047-ca59-4ea4-8a52-7f52d346d861#0' },
      { k: 'in-turn', turnId: TURN },
      { k: 'done', turnId: TURN },
      { k: 'cancelled', by: 'user' },
    ] as Message['state'][]) {
      await steerInStrip();
      emit({ type: 'message_upsert', message: record({ state, rev: 6 }) });
      await leaves();
      cleanup();
      n += 1;
      ID = `01a17550-1120-7afa-a3cf-${String(n).padStart(12, '0')}`;
    }
  });

  it('a frame the page missed: the runtime’s word on the status page takes the row out', async () => {
    stop = startOutbox();
    await steerInStrip();
    rt.convs.get(CONV)!.messages = [record({ state: { k: 'in-turn', turnId: TURN }, rev: 9 })];
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(standing()).toBeDefined();
    await act(() => vi.advanceTimersByTimeAsync(5_050));
    expect(rt.statusAsks).toEqual([[ID]]);
    await leaves();
  });
});

describe('what stands in the strip, by the runtime’s state of the message', () => {
  const wait = () => ({ slowAt: machineNow(machine) + 3000, deadlineAt: machineNow(machine) + 30_000 });
  const cases: [string, () => Partial<Message>, string | null][] = [
    [
      'a steer behind another',
      () => ({ state: { k: 'queued', position: 2, reason: 'behind', wait: null } }),
      '',
    ],
    ['a steer being typed', () => ({ state: { k: 'typing', wait: wait() } }), 'next.queue.sending'],
    [
      'a steer in the agent’s queue',
      () => ({ state: { k: 'in-input', wait: wait() } }),
      'next.message.handed',
    ],
    [
      'a steer that did not reach the agent',
      () => ({ state: { k: 'queued', position: 1, reason: 'stalled', wait: null } }),
      'next.queue.stalled',
    ],
    ['a steer the agent took', () => ({ state: { k: 'accepted', entryId: null } }), null],
    ['a steer in the turn', () => ({ state: { k: 'in-turn', turnId: TURN } }), null],
    ['a steer answered', () => ({ state: { k: 'done', turnId: TURN } }), null],
    ['a steer taken back', () => ({ state: { k: 'cancelled', by: 'user' } }), null],
    [
      'a steer pulled back into the agent’s box (its bubble offers Повторить)',
      () => ({ state: { k: 'failed', code: 'returned-to-input', retryable: true, at: machineNow(machine) } }),
      null,
    ],
    [
      'a message behind another',
      () => ({ mode: 'queue', state: { k: 'queued', position: 2, reason: 'behind', wait: null } }),
      '',
    ],
    [
      'a message being typed (its bubble says so)',
      () => ({ mode: 'queue', state: { k: 'typing', wait: wait() } }),
      null,
    ],
    ['a message in the agent’s box', () => ({ mode: 'queue', state: { k: 'in-input', wait: wait() } }), null],
  ];
  for (const [name, over, note] of cases) {
    it(`${name}: ${note === null ? 'not in the strip' : 'in the strip'}`, async () => {
      act(() => applyMessage(machine, record(over())));
      render(<ChatDock conv={CONV} machine={machine} />);
      await act(settle);
      const row = standing();
      if (note === null) {
        expect(row).toBeUndefined();
        return;
      }
      expect(row).toBeDefined();
      if (note) expect(row!.textContent).toContain(t(note));
    });
  }
});
