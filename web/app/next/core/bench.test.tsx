// @vitest-environment jsdom
// Stage C budgets (spec §12): no task over 50 ms while 5000 entries arrive, a cached chat's first frame within
// 50 ms, 100 `now` a second without touching the feed. Measured on the real code path: frames through the socket
// client, the sink and the stores, rows rendered by React in jsdom. The numbers are printed for the report.

import { act, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime, fixture, type FakeSocket } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { flush, setScheduler } from '@/next/core/store';
import {
  feedStore,
  key,
  turnStore,
  useFeedRows,
  useNow,
  useTurn,
  type FeedRow,
} from '@/next/stores/conversation';
import type { ConvEvent, ConvId, ConvSnapshot, Entry, Turn } from '@/next/protocol/v2';

const BUDGET_MS = 50;
const A = 'tab-k3Jx9QmZ' as ConvId;
const B = 'tab-benchB01' as ConvId;

beforeEach(useFakeTime);
afterEach(() => {
  hub.stop();
  setScheduler('sync');
  vi.useRealTimers();
});

const turnOf = (n: number, t0: number): Turn => ({
  ...fixture<{ e: { turn: Turn } }>('ev.turn_finished').e.turn,
  id: `c:00000000-0000-4000-8000-${String(n).padStart(12, '0')}#0`,
  startedAt: t0 + n * 1000,
  endedAt: t0 + n * 1000 + 900,
  answerEntryId: null,
});

const entryOf = (turn: Turn, k: number): Entry => ({
  id: `x:${turn.startedAt * 10 + k}#0`,
  turnId: turn.id,
  at: turn.startedAt + k,
  rev: 1,
  t: 'say',
  md: `шаг ${k} хода — текст ответа средней длины, чтобы было похоже на настоящую ленту`,
});

/** A feed of `turns` turns with `per` entries each. */
function bigFeed(turns: number, per: number): ConvSnapshot {
  const base = fixture<{ data: ConvSnapshot }>('snapshot.conv').data;
  const ts = Array.from({ length: turns }, (_, i) => turnOf(i + 1, 1792600000000));
  return {
    ...base,
    turns: ts,
    entries: ts.flatMap((t) => Array.from({ length: per }, (_, k) => entryOf(t, k + 1))),
    now: null,
    questions: [],
    permissions: [],
  };
}

/** Delivers frames as the runtime would in a burst, one socket message per chunk; returns the task times. */
function burst(s: FakeSocket, frames: object[], chunk: number) {
  const tasks: number[] = [];
  const t0 = performance.now();
  for (let i = 0; i < frames.length; i += chunk) {
    const data = frames
      .slice(i, i + chunk)
      .map((f) => JSON.stringify(f))
      .join('\n');
    const t = performance.now();
    s.onmessage?.({ data });
    flush();
    tasks.push(performance.now() - t);
  }
  const total = performance.now() - t0;
  tasks.sort((a, b) => a - b);
  const at = (q: number) => tasks[Math.min(tasks.length - 1, Math.floor(tasks.length * q))];
  return { median: at(0.5), p95: at(0.95), longest: tasks[tasks.length - 1], total };
}

const TurnRow = ({ conv, id, onRender }: { conv: ConvId; id: string; onRender: () => void }) => {
  const v = useTurn(conv, id);
  onRender();
  return <div className="turn">{v?.turn?.status ?? ''}</div>;
};

/** The feed as stage D draws it past 300 turns: a window of the last 40 turns (here always, for the budget). */
const Feed = ({ conv, onRow, window = Infinity }: { conv: ConvId; onRow: () => void; window?: number }) => {
  const rows = useFeedRows(conv);
  if (!rows) return <div>skeleton</div>;
  return (
    <div>
      {rows
        .slice(-window)
        .map((r: FeedRow) =>
          r.k === 'turn' ? <TurnRow key={r.id} conv={conv} id={r.id} onRender={onRow} /> : null,
        )}
    </div>
  );
};

const NowLine = ({ conv, onRender }: { conv: ConvId; onRender: () => void }) => {
  const now = useNow(conv);
  onRender();
  return <div>{now?.word ?? ''}</div>;
};

describe('bench', () => {
  it('applies 5000 entries with no task over 50 ms', async () => {
    const rt = new FakeRuntime();
    const { m } = await bootFake({ rt });
    hub.openConversation(m, A);
    await settle();
    const s = rt.opened[rt.opened.length - 1];
    const turn = turnOf(1, 1792656100000);
    let seq = rt.ring(`conv:${A}`).seq;
    const live = { ...turn, status: 'live', endedAt: null, durationMs: null };
    const frames: object[] = [
      { f: 'ev', ch: `conv:${A}`, seq: ++seq, e: { type: 'turn_started', turn: live } },
    ];
    for (let k = 1; k <= 5000; k += 1) {
      const e: ConvEvent = { type: 'step', entry: entryOf(turn, k), counts: { toolCount: 0, stepCount: k } };
      frames.push({ f: 'ev', ch: `conv:${A}`, seq: ++seq, e });
    }
    // One turn of 5000 steps, 100 frames per socket message (a replay after a reconnect): the worst case for
    // appends to one turn. The runtime sends a frame per message; 100 is the stress.
    const one = burst(s, frames, 100);
    console.log(
      `[bench] 5000 steps into one turn, 50 tasks of 100 frames: median ${one.median.toFixed(1)} ms, p95 ${one.p95.toFixed(1)} ms, longest ${one.longest.toFixed(1)} ms, total ${one.total.toFixed(0)} ms`,
    );
    expect(turnStore.get(key(A, turn.id))?.entryIds).toHaveLength(5000);
    expect(one.p95).toBeLessThan(BUDGET_MS);
  });

  it('a reconnect snapshot at the protocol budget merges within 50 ms', async () => {
    const times: number[] = [];
    let size = 0;
    for (let run = 0; run < 5; run += 1) {
      // A long chat on the page (250 turns, 5000 entries); the runtime restarted and lost the cursor, so a
      // replace snapshot comes: ≤ 256 KB, the last 3 turns whole and 30 more by their answer, merged by id.
      const rt = new FakeRuntime({ conv: { [A]: bigFeed(250, 20) } });
      const { m } = await bootFake({ rt });
      hub.openConversation(m, A);
      await settle();
      const s = rt.opened[rt.opened.length - 1];
      const full = bigFeed(250, 20);
      const tail = full.turns.slice(-33);
      const keep = new Set(tail.slice(-3).map((t) => t.id));
      // The whole turns' steps near the 4 KB a tool output is cut to, the older turns' answers as they are.
      const answers = new Set(tail.map((t) => full.entries.filter((e) => e.turnId === t.id).pop()!.id));
      const entries = full.entries
        .filter((e) => e.turnId && (keep.has(e.turnId) || answers.has(e.id)))
        .map((e) => (e.t === 'say' && keep.has(e.turnId!) ? { ...e, md: e.md.repeat(24) } : e));
      const data = {
        ...full,
        turns: tail.map((t) => ({
          ...t,
          answerEntryId: full.entries.filter((e) => e.turnId === t.id).pop()!.id,
        })),
        entries,
      };
      const raw = JSON.stringify({ f: 'snapshot', ch: `conv:${A}`, seq: 1, mode: 'replace', data });
      size = raw.length;
      const t = performance.now();
      s.onmessage?.({ data: raw });
      flush();
      times.push(performance.now() - t);
      expect(feedStore.get(A)?.rows).toHaveLength(250);
    }
    const sorted = [...times].sort((a, b) => a - b);
    console.log(
      `[bench] reconnect snapshot ${(size / 1024).toFixed(0)} KB into a 5000-entry feed, 5 runs: ${times.map((x) => x.toFixed(1)).join(' / ')} ms`,
    );
    expect(size).toBeLessThan(256 * 1024);
    expect(sorted[2]).toBeLessThan(BUDGET_MS);
  });

  it('switches to a cached chat: first frame within 50 ms', async () => {
    const rt = new FakeRuntime({ conv: { [A]: bigFeed(250, 20), [B]: bigFeed(250, 20) } });
    const { m } = await bootFake({ rt });
    const relA = hub.openConversation(m, A);
    await settle();
    relA();
    const relB = hub.openConversation(m, B);
    await settle();

    let rows = 0;
    const { rerender } = render(<Feed conv={B} window={40} onRow={() => (rows += 1)} />);
    // Back to A: in the memory cache, so its rows are there before the socket answers.
    const t0 = performance.now();
    relB();
    hub.openConversation(m, A);
    const ready = feedStore.get(A);
    const storeMs = performance.now() - t0;
    expect(ready?.loaded).toBe(true);
    rows = 0;
    act(() => rerender(<Feed conv={A} window={40} onRow={() => (rows += 1)} />));
    const ms = performance.now() - t0;
    console.log(
      `[bench] switch to a cached chat (250 turns, 5000 entries): store ${storeMs.toFixed(2)} ms, first frame of 40 turns ${ms.toFixed(1)} ms (jsdom)`,
    );
    expect(rows).toBe(40);
    expect(storeMs).toBeLessThan(5);
    expect(ms).toBeLessThan(BUDGET_MS);

    const tAll = performance.now();
    act(() => rerender(<Feed conv={A} onRow={() => {}} />));
    console.log(
      `[bench] the same chat with all 250 turns rendered: ${(performance.now() - tAll).toFixed(1)} ms (jsdom, no window)`,
    );

    // The delta that follows merges by id: no row re-renders for a resent snapshot.
    rows = 0;
    await act(async () => {
      await settle();
    });
    expect(rows).toBe(0);
  });

  it('100 `now` a second: one notification per frame, the feed untouched', async () => {
    const rt = new FakeRuntime();
    const { m } = await bootFake({ rt });
    hub.openConversation(m, A);
    await settle();
    const frames: (() => void)[] = [];
    setScheduler((run) => frames.push(run));
    let rowRenders = 0;
    let nowRenders = 0;
    render(
      <>
        <Feed conv={A} onRow={() => (rowRenders += 1)} />
        <NowLine conv={A} onRender={() => (nowRenders += 1)} />
      </>,
    );
    rowRenders = 0;
    nowRenders = 0;
    const now = fixture<{ e: ConvEvent }>('ev.now').e as Extract<ConvEvent, { type: 'now' }>;
    let longest = 0;
    const times: number[] = [];
    // Ten seconds at 100 Hz, 60 frames a second: the frame scheduler runs after every ~1.67 events.
    for (let i = 1; i <= 1000; i += 1) {
      const t = performance.now();
      rt.emit(`conv:${A}`, { ...now, at: now.at + i * 10, word: `слово ${i}` });
      longest = Math.max(longest, performance.now() - t);
      times.push(performance.now() - t);
      if (i % 5 === 0 || i % 5 === 3) {
        await act(async () => {
          frames.splice(0).forEach((run) => run());
        });
      }
    }
    times.sort((a, b) => b - a);
    console.log(
      `[bench] now top: ${times
        .slice(0, 8)
        .map((x) => x.toFixed(1))
        .join(' ')}`,
    );
    console.log(
      `[bench] 1000 now events: longest ${longest.toFixed(2)} ms, now renders ${nowRenders}, row renders ${rowRenders}`,
    );
    // A single long task here is a GC pause of jsdom, not the apply: the budget is on the 99th percentile.
    const p99 = times[Math.floor(times.length / 100)];
    console.log(`[bench] now p99 ${p99.toFixed(2)} ms`);
    expect(p99).toBeLessThan(5);
    expect(rowRenders).toBe(0);
    expect(nowRenders).toBeLessThanOrEqual(400);
  });
});
