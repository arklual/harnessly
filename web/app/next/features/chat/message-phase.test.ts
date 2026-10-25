import { describe, expect, it } from 'vitest';
import { MESSAGE_PHASE, messagePhase, nextPhaseIn, SEND_SLOW_MS } from '@/next/features/chat/message-phase';

const since = 1_000_000;

describe('messagePhase', () => {
  it("is pale «sending» at once, «longer than usual» after 3 s, and never red by the page's clock", () => {
    const state = { k: 'submitting' } as const;
    expect(messagePhase(state, since, since, false)).toBe(MESSAGE_PHASE.sending);
    expect(messagePhase(state, since, since + SEND_SLOW_MS - 1, false)).toBe(MESSAGE_PHASE.sending);
    expect(messagePhase(state, since, since + SEND_SLOW_MS, false)).toBe(MESSAGE_PHASE.slow);
    expect(messagePhase(state, since, since + 10 * 60_000, false)).toBe(MESSAGE_PHASE.slow);
  });

  it('waits for the machine, calmly, when the last try got no answer from it — however long it takes', () => {
    const state = { k: 'submitting' } as const;
    expect(messagePhase(state, since, since, false, false, true)).toBe(MESSAGE_PHASE.waitingMachine);
    expect(messagePhase(state, since, since + 10 * 60_000, false, false, true)).toBe(
      MESSAGE_PHASE.waitingMachine,
    );
    expect(nextPhaseIn(state, since, since, false, false, true)).toBeNull();
    // Without a network the network is the reason; once the runtime has the message the flag means nothing.
    expect(messagePhase(state, since, since, true, false, true)).toBe(MESSAGE_PHASE.waitingNet);
    expect(messagePhase({ k: 'typing' }, since, since, false, false, true)).toBe(MESSAGE_PHASE.sending);
  });

  it('waits for the network before the runtime has the message, not after', () => {
    expect(messagePhase({ k: 'submitting' }, since, since + 60_000, true)).toBe(MESSAGE_PHASE.waitingNet);
    expect(messagePhase({ k: 'typing' }, since, since, true)).toBe(MESSAGE_PHASE.sending);
  });

  it('keeps final states whatever the clock says', () => {
    expect(messagePhase({ k: 'draft' }, since, since + 10 * 60_000, false)).toBe(MESSAGE_PHASE.draft);
    expect(messagePhase({ k: 'queued' } as never, since, since + 10 * 60_000, false)).toBe(
      MESSAGE_PHASE.queued,
    );
    expect(messagePhase({ k: 'failed' } as never, since, since, false)).toBe(MESSAGE_PHASE.failed);
  });
});

describe('a steer handed to a working agent', () => {
  const inInput = { k: 'in-input' } as const;

  it("waits for the agent's step without going slow or stuck, however long it takes", () => {
    expect(messagePhase(inInput, since, since + 10 * 60_000, false, true)).toBe(MESSAGE_PHASE.handed);
    expect(nextPhaseIn(inInput, since, since, false, true)).toBeNull();
  });

  it('is only the state in-input: typing is still sending, and once read it is sent', () => {
    expect(messagePhase({ k: 'typing' }, since, since + SEND_SLOW_MS, false, true)).toBe(MESSAGE_PHASE.slow);
    expect(messagePhase({ k: 'accepted', entryId: 'c:1#0' }, since, since, false, true)).toBe(
      MESSAGE_PHASE.sent,
    );
  });

  it("an ordinary message in-input goes slow; «not confirmed» is the runtime's to say, by its own deadline", () => {
    expect(messagePhase(inInput, since, since + SEND_SLOW_MS, false)).toBe(MESSAGE_PHASE.slow);
    expect(messagePhase(inInput, since, since + 10 * 60_000, false)).toBe(MESSAGE_PHASE.slow);
    expect(nextPhaseIn(inInput, since, since + SEND_SLOW_MS, false)).toBeNull();
  });
});

describe('nextPhaseIn', () => {
  it('names the next threshold, and none once nothing changes by time', () => {
    const state = { k: 'submitting' } as const;
    expect(nextPhaseIn(state, since, since + 1000, false)).toBe(SEND_SLOW_MS - 1000);
    expect(nextPhaseIn(state, since, since + SEND_SLOW_MS + 500, false)).toBeNull();
    expect(nextPhaseIn(state, since, since, true)).toBeNull();
  });
});
