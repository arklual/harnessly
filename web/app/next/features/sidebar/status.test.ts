import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { chatStatusOf, statusNoteOf, waitsForOf, type IStatusSource } from '@/next/features/sidebar/status';
import { CHAT_STATUS } from '@/next/features/sidebar/types';

const base: IStatusSource = { state: { k: 'ready' }, gate: null, lastTurn: null, seenAt: 100 };

describe('chatStatusOf', () => {
  it('shows what waits for the person before anything else', () => {
    expect(chatStatusOf({ ...base, state: { k: 'waiting-question' } }, true)).toBe(CHAT_STATUS.question);
    expect(chatStatusOf({ ...base, state: { k: 'waiting-permission' }, gate: { kind: 'plan' } }, true)).toBe(
      CHAT_STATUS.plan,
    );
    expect(
      chatStatusOf({ ...base, state: { k: 'waiting-permission' }, gate: { kind: 'permission' } }, true),
    ).toBe(CHAT_STATUS.perm);
  });

  it('a working agent on a machine that does not answer waits for the machine', () => {
    expect(chatStatusOf({ ...base, state: { k: 'working' } }, true)).toBe(CHAT_STATUS.thinking);
    expect(chatStatusOf({ ...base, state: { k: 'working' } }, false)).toBe(CHAT_STATUS.waitingMachine);
  });

  it('a finished turn shows only until it is read', () => {
    const done = { ...base, lastTurn: { status: 'done' as const, endedAt: 200 } };
    expect(chatStatusOf(done, true)).toBe(CHAT_STATUS.done);
    expect(chatStatusOf({ ...done, seenAt: 200 }, true)).toBe(CHAT_STATUS.none);
    expect(chatStatusOf({ ...done, lastTurn: { status: 'failed', endedAt: 200 } }, true)).toBe(
      CHAT_STATUS.failed,
    );
    expect(chatStatusOf({ ...done, lastTurn: { status: 'interrupted', endedAt: 200 } }, true)).toBe(
      CHAT_STATUS.stopped,
    );
  });

  it('a chat that waits for its background agents is at work, not finished or read', () => {
    const waiting = {
      ...base,
      bgAgents: 2,
      lastTurn: { status: 'done' as const, endedAt: 200 },
    };
    expect(chatStatusOf(waiting, true)).toBe(CHAT_STATUS.background);
    expect(chatStatusOf({ ...waiting, state: { k: 'idle' } }, true)).toBe(CHAT_STATUS.background);
    // Even read: it does not turn into «quiet» while the agents work.
    expect(chatStatusOf({ ...waiting, seenAt: 300 }, true)).toBe(CHAT_STATUS.background);
    // What the person must answer comes first; a working agent is plainly working.
    expect(chatStatusOf({ ...waiting, state: { k: 'waiting-question' } }, true)).toBe(CHAT_STATUS.question);
    expect(chatStatusOf({ ...waiting, state: { k: 'working' } }, true)).toBe(CHAT_STATUS.thinking);
    // Once the agents have reported, the finished answer shows as unread again.
    expect(chatStatusOf({ ...waiting, bgAgents: 0 }, true)).toBe(CHAT_STATUS.done);
    expect(chatStatusOf({ ...waiting, bgAgents: undefined, seenAt: 300 }, true)).toBe(CHAT_STATUS.none);
  });
});

describe('a chat that waits for any background work', () => {
  const finished = { ...base, lastTurn: { status: 'done' as const, endedAt: 200 } };

  it('is at work for a command, a monitor or a wakeup alone, not finished', () => {
    const waits = [
      { bgAgents: 1, bgWait: { commands: 1, command: 'npm test' } },
      { bgAgents: 1, bgWait: { monitors: 1 } },
      { bgAgents: 1, bgWait: { wakeupAt: 5_000 } },
      { bgAgents: 3 }, // a server that sends only the total
    ];
    for (const wait of waits) {
      expect(chatStatusOf({ ...finished, ...wait }, true)).toBe(CHAT_STATUS.background);
    }
  });

  it('gives its «done» only when nothing is left to wait for', () => {
    expect(chatStatusOf({ ...finished, bgAgents: 1, bgWait: { commands: 1 } }, true)).toBe(
      CHAT_STATUS.background,
    );
    expect(chatStatusOf(finished, true)).toBe(CHAT_STATUS.done);
  });
});

describe('waitsForOf: what the hover says the chat waits for', () => {
  beforeAll(() => pinLocale('ru'));
  afterAll(() => pinLocale(null));
  const at = new Date(2026, 9, 25, 12, 30).getTime();
  const now = at - 600_000;

  it('lists every kind in words', () => {
    expect(waitsForOf({ bgWait: { agents: 2, commands: 1, command: 'npm test', wakeupAt: at } }, now)).toBe(
      'Ждёт: 2 сабагента, команда npm test, пробуждение в 12:30',
    );
    expect(waitsForOf({ bgWait: { agents: 1, commands: 3, monitors: 2 } }, now)).toBe(
      'Ждёт: 1 сабагент, 3 команды, 2 монитора',
    );
  });

  it('names the teammates at work apart from the subagents', () => {
    expect(waitsForOf({ bgWait: { teammates: 2 } }, now)).toBe('Ждёт: 2 напарника');
    expect(waitsForOf({ bgWait: { agents: 1, teammates: 5 } }, now)).toBe('Ждёт: 1 сабагент, 5 напарников');
    expect(waitsForOf({ bgWait: { workflows: 1 } }, now)).toBe('Ждёт: 1 workflow');
    expect(waitsForOf({ bgWait: { agents: 2, workflows: 2 } }, now)).toBe('Ждёт: 2 сабагента, 2 workflow');
  });

  it('forgets a wakeup whose time has come, and says nothing when the server did not say what', () => {
    expect(waitsForOf({ bgWait: { wakeupAt: at } }, at + 1)).toBeNull();
    expect(waitsForOf({}, now)).toBeNull();
  });

  it('speaks English too', () => {
    pinLocale('en');
    expect(waitsForOf({ bgWait: { agents: 1, commands: 2 } }, now)).toBe(
      'Waiting for: 1 subagent, 2 commands',
    );
    pinLocale('ru');
  });
});

describe('a chat held on its usage limit', () => {
  beforeAll(() => pinLocale('ru'));
  afterAll(() => pinLocale(null));
  const held = { ...base, limited: { resetsAt: new Date(2026, 9, 25, 18, 0).getTime() } };
  const morning = new Date(2026, 9, 25, 9, 0).getTime();

  it('shows the clock, though its last turn is unread', () => {
    const unread = { ...held, lastTurn: { status: 'failed' as const, endedAt: 200 } };
    expect(chatStatusOf(unread, true)).toBe(CHAT_STATUS.limited);
  });

  it('yields to what waits for the person and to an agent that works again', () => {
    expect(chatStatusOf({ ...held, state: { k: 'waiting-question' } }, true)).toBe(CHAT_STATUS.question);
    expect(chatStatusOf({ ...held, state: { k: 'working' } }, true)).toBe(CHAT_STATUS.thinking);
  });

  it('says until when, or only that it waits when no time is known', () => {
    expect(statusNoteOf(held, CHAT_STATUS.limited, morning)).toBe('Ждёт сброса лимита до 18:00');
    expect(statusNoteOf({ ...base, limited: { resetsAt: null } }, CHAT_STATUS.limited, morning)).toBe(
      'Ждёт сброса лимита',
    );
    expect(statusNoteOf(held, CHAT_STATUS.done, morning)).toBeNull();
  });
});
