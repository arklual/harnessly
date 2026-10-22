import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  enqueuePrompt,
  isGoneFromQueue,
  onQueueHandoff,
  setPromptQueue,
  usePromptQueueStore,
} from '@/hooks/use-prompt-queue';
import {
  OWN_TEXT_MAX_AGE_MS,
  RECORDED_SLACK_MS,
  createHandoffTracker,
  pendingFromHandoff,
  type THandoffEvent,
} from '@/lib/hl-queue-handoff';
import { pendingUserEntry } from '@/lib/timeline-launch';
import { adoptPendingInto, isRecordedSince } from '@/lib/timeline-pending';
import type { IQueuedPrompt } from '@/types/server/hl-prompt-queue';
import type { ITimelineEntry, ITimelineUserMessage } from '@/types/timeline';

const TAB = 'tab-1';
const item = (id: string, text: string, extra: Partial<IQueuedPrompt> = {}): IQueuedPrompt => ({
  id,
  text,
  images: [],
  createdAt: 10_000,
  ...extra,
});
const local = (text: string) => item('local-abc', text);
const user = (id: string, text: string, extra: Partial<ITimelineUserMessage> = {}): ITimelineUserMessage => ({
  id,
  type: 'user-message',
  timestamp: 20_000,
  text,
  ...extra,
});

describe('a message leaving the queue for the agent', () => {
  it('becomes a row when this page queued it', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'write the tests');
    const events = tracker.observe(TAB, [item('q1', 'write the tests')], []);
    expect(events).toEqual([{ kind: 'handoff', tabId: TAB, item: item('q1', 'write the tests') }]);
  });

  it('is ignored when another page queued it', () => {
    const tracker = createHandoffTracker();
    expect(tracker.observe(TAB, [item('q1', 'not ours')], [])).toEqual([]);
  });

  it('is ignored while it is only the local chip of a send in flight', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'text');
    expect(tracker.observe(TAB, [local('text')], [])).toEqual([]);
    expect(tracker.observe(TAB, [], [local('text')])).toEqual([]);
  });

  it('is not a hand-over when the person removed or took it back', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'changed my mind');
    tracker.dismiss(TAB, 'q1');
    expect(tracker.observe(TAB, [item('q1', 'changed my mind')], [])).toEqual([]);
    tracker.own(TAB, 'changed my mind');
    expect(tracker.observe(TAB, [item('q2', 'changed my mind')], [])).toHaveLength(1);
  });

  it('forgets a dismissal that failed, so the leave counts', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'text');
    tracker.dismiss(TAB, 'q1');
    tracker.undismiss(TAB, 'q1');
    expect(tracker.observe(TAB, [item('q1', 'text')], [])).toHaveLength(1);
  });

  it('counts one queued message once, even when the same text is queued twice', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'yes');
    tracker.own(TAB, 'yes');
    expect(tracker.observe(TAB, [item('q1', 'yes'), item('q2', 'yes')], [item('q2', 'yes')])).toHaveLength(1);
    expect(tracker.observe(TAB, [item('q2', 'yes')], [])).toHaveLength(1);
    tracker.own(TAB, 'yes');
    expect(tracker.observe(TAB, [item('q3', 'yes')], [])).toHaveLength(1);
  });

  it('stops counting a text this page queued long ago', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'old', 0);
    expect(tracker.observe(TAB, [item('q1', 'old')], [], OWN_TEXT_MAX_AGE_MS + 1)).toEqual([]);
  });

  it('is not ours after the add failed', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'did not go');
    tracker.disown(TAB, 'did not go');
    expect(tracker.observe(TAB, [item('q1', 'did not go')], [])).toEqual([]);
  });

  it('keeps tabs apart', () => {
    const tracker = createHandoffTracker();
    tracker.own('tab-2', 'text');
    expect(tracker.observe(TAB, [item('q1', 'text')], [])).toEqual([]);
  });
});

describe('a steered message the page never saw in the queue', () => {
  it('becomes a row from the server answer', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'right now');
    expect(tracker.answered(TAB, item('q1', 'right now'), 'steered')).toHaveLength(1);
  });

  it('does not become a row when it stays queued', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'later');
    expect(tracker.answered(TAB, item('q1', 'later'), 'queued')).toEqual([]);
  });

  it('is not shown twice when the queue update came first', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'right now');
    expect(tracker.observe(TAB, [item('q1', 'right now')], [])).toHaveLength(1);
    expect(tracker.answered(TAB, item('q1', 'right now'), 'steered')).toEqual([]);
  });

  it('is not shown twice when the answer came first and the queue update follows', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'right now');
    expect(tracker.answered(TAB, item('q1', 'right now'), 'steered')).toHaveLength(1);
    expect(tracker.observe(TAB, [], [item('q1', 'right now')])).toEqual([]);
    expect(tracker.observe(TAB, [item('q1', 'right now')], [])).toEqual([]);
  });
});

describe('a hand-over that fell through', () => {
  it('takes the row back when the message returns to the queue, and shows it again on the next leave', () => {
    const tracker = createHandoffTracker();
    tracker.own(TAB, 'came back');
    expect(tracker.observe(TAB, [item('q1', 'came back')], [])).toHaveLength(1);
    expect(tracker.observe(TAB, [], [item('q1', 'came back')])).toEqual([
      { kind: 'retract', tabId: TAB, id: 'q1' },
    ]);
    expect(tracker.observe(TAB, [item('q1', 'came back')], [])).toHaveLength(1);
  });
});

describe('the row of a handed-over message', () => {
  it('carries the text, the pictures by url and a floor for the transcript check', () => {
    const { text, options } = pendingFromHandoff(
      item('q1', 'look', { images: ['/uploads/a.png', '/etc/passwd'], createdAt: 50_000 }),
      (path) => (path.startsWith('/uploads/') ? `/api${path}` : null),
    );
    expect(text).toBe('look');
    expect(options).toEqual({ images: ['/api/uploads/a.png'], recordedSince: 50_000 - RECORDED_SLACK_MS });
  });

  it('is replaced by the transcript record with the same text, picture tokens and spacing aside', () => {
    const { text, options } = pendingFromHandoff(item('q1', 'look   here'), () => null);
    const row = pendingUserEntry(text, options);
    expect(row).not.toBeNull();
    const { merged, rest } = adoptPendingInto(
      [user('t1', '[Image #1] look here')],
      [row as ITimelineUserMessage],
      false,
    );
    expect(rest).toEqual([]);
    expect(merged[0]).toMatchObject({ id: (row as ITimelineUserMessage).id, text: '[Image #1] look here' });
  });
});

describe('isRecordedSince', () => {
  const row = user('row', 'done', { pending: true, timestamp: 30_000 });

  it('sees the transcript record that already arrived', () => {
    expect(isRecordedSince([user('t1', 'done', { timestamp: 25_000 })], row, 20_000)).toBe(true);
  });

  it('sees the record the agent keeps in its own queue', () => {
    expect(
      isRecordedSince([user('t1', 'done', { timestamp: 25_000, pending: true, queued: true })], row, 20_000),
    ).toBe(true);
  });

  it('ignores an earlier message with the same words', () => {
    expect(isRecordedSince([user('t1', 'done', { timestamp: 5_000 })], row, 20_000)).toBe(false);
  });

  it('ignores another row of the composer and other text', () => {
    const entries: ITimelineEntry[] = [
      user('p1', 'done', { timestamp: 25_000, pending: true }),
      user('t2', 'other', { timestamp: 25_000 }),
    ];
    expect(isRecordedSince(entries, row, 20_000)).toBe(false);
  });
});

describe('the prompt queue hands a message over to the timeline', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    usePromptQueueStore.setState({ server: {}, local: {}, queues: {} });
  });

  const steeringServer = (id: string) =>
    vi.fn(async () => {
      const queued = { id, text: 'Hello', images: [], createdAt: 2 };
      setPromptQueue('t1', [queued]);
      setPromptQueue('t1', []);
      return new Response(JSON.stringify({ item: queued, delivery: 'steered' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
  const send = (options?: { handoff?: boolean }) =>
    enqueuePrompt(undefined, 't1', 'Hello', [], async () => ({ text: 'Hello', images: [] }), options);

  it('gives a steered message a row', async () => {
    const events: THandoffEvent[] = [];
    const off = onQueueHandoff('t1', (event) => events.push(event));
    vi.stubGlobal('fetch', steeringServer('q-steer'));
    const answer = await send();
    off();
    expect(answer.delivery).toBe('steered');
    expect(events).toMatchObject([{ kind: 'handoff', tabId: 't1', item: { id: 'q-steer', text: 'Hello' } }]);
  });

  it('gives a launch message none, since the launch preview already shows it', async () => {
    const events: THandoffEvent[] = [];
    const off = onQueueHandoff('t1', (event) => events.push(event));
    vi.stubGlobal('fetch', steeringServer('q-launch'));
    await send({ handoff: false });
    off();
    expect(events).toEqual([]);
  });

  it('leaves nothing behind when the runtime refuses', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify({ error: 'too many' }), {
            status: 409,
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    await expect(send()).rejects.toThrow('too many');
    expect(usePromptQueueStore.getState().queues.t1).toBeUndefined();
  });

  it('knows the runtime words for a message already taken, in both languages', () => {
    expect(isGoneFromQueue(new Error('The message is no longer in the queue'))).toBe(true);
    expect(isGoneFromQueue(new Error('Сообщения уже нет в очереди'))).toBe(true);
    expect(isGoneFromQueue(new Error('HTTP 500'))).toBe(false);
  });
});
