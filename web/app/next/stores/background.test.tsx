// @vitest-environment jsdom
// The subagents of a chat are on screen when the chat is opened again: after switching away and back (the events that
// came meanwhile are replayed over what the page kept), after a new runtime (the snapshot has them) and after F5 (the
// cache kept them with the cursor the runtime replays from).
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { memoryKV, startPersistence, type Persistence } from '@/next/core/persist';
import { setFetch } from '@/next/core/net/http';
import { hub } from '@/next/core/realtime/hub';
import { resetCore } from '@/next/core/testing/reset';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import BackgroundPanel from '@/next/features/background/background-panel';
import type { BackgroundTask, ConvId } from '@/next/protocol/v2';
import { backgroundStore, useBackground } from '@/next/stores/conversation';
import { putMachines } from '@/next/stores/machines';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;

let persistence: Persistence | null = null;

beforeEach(() => {
  // The panel asks whether it is on a phone; jsdom has no matchMedia.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  useFakeTime();
});
afterEach(() => {
  cleanup();
  persistence?.stop();
  persistence = null;
  hub.stop();
  vi.useRealTimers();
});

const agent = (id: string, title: string, over: Partial<BackgroundTask> = {}): BackgroundTask => ({
  id,
  conversationId: CONV,
  kind: 'subagent',
  title,
  startedAt: 1_000,
  endedAt: null,
  status: 'running',
  devServer: false,
  stoppable: false,
  outputPath: null,
  ...over,
});

const finished = (id: string, title: string): BackgroundTask =>
  agent(id, title, { status: 'done', endedAt: 61_000 });

const idsOf = () => backgroundStore.get(CONV)?.map((t) => t.id) ?? [];

/** The right column as the chat draws it: its tasks are the store's, read at mount. */
const Rail = () => {
  const tasks = useBackground(CONV);
  if (tasks.length === 0) return null;
  return (
    <BackgroundPanel
      tasks={tasks}
      recap={null}
      shots={[]}
      shotSrc={() => ''}
      shown
      now={70_000}
      stopping={new Set()}
      onStop={vi.fn()}
    />
  );
};

describe('the subagents of a chat', () => {
  it('are in the right column again after switching away and back, with what happened meanwhile', async () => {
    const { rt, m } = await bootFake();
    const first = hub.openConversation(m, CONV);
    await settle();
    await act(async () => {
      rt.emit(CH, { type: 'background_upsert', task: agent('a1', 'Audit backend') });
      rt.emit(CH, { type: 'background_upsert', task: agent('a2', 'Audit shell') });
      await settle();
    });
    const view = render(<Rail />);
    expect(screen.getByText('Audit backend')).toBeTruthy();
    expect(screen.getByText('Audit shell')).toBeTruthy();

    // Away: the chat is closed, the column unmounted; one agent finishes and one starts while nobody listens.
    view.unmount();
    first();
    await settle();
    rt.emit(CH, { type: 'background_upsert', task: finished('a1', 'Audit backend') });
    rt.emit(CH, { type: 'background_upsert', task: agent('a3', 'Audit states') });

    // Back: the column is there at once from what the page kept, and the runtime's replay brings the rest.
    expect(idsOf()).toEqual(expect.arrayContaining(['a1', 'a2']));
    expect(idsOf()).not.toContain('a3');
    const again = hub.openConversation(m, CONV);
    render(<Rail />);
    expect(screen.getByText('Audit backend')).toBeTruthy();
    await act(async () => {
      await settle();
    });
    expect(screen.getByText('Audit states')).toBeTruthy();
    const done = screen.getByText('Audit backend').closest('.hx-bg__row');
    expect(done?.getAttribute('data-status')).toBe('done');
    expect(done?.textContent).toContain('done · 1 min');
    expect(screen.getByText('Audit shell').closest('.hx-bg__row')?.getAttribute('data-status')).toBe('run');
    again();
  });

  it('come with the snapshot of a runtime that started over', async () => {
    const { rt, m } = await bootFake();
    const first = hub.openConversation(m, CONV);
    await settle();
    rt.emit(CH, { type: 'background_upsert', task: finished('a1', 'Audit backend') });
    await settle();
    first();
    await settle();
    backgroundStore.delete(CONV);
    rt.restart();
    await vi.advanceTimersByTimeAsync(5000);
    const again = hub.openConversation(m, CONV);
    await vi.advanceTimersByTimeAsync(5000);
    expect(idsOf()).toContain('a1');
    expect(backgroundStore.get(CONV)?.find((t) => t.id === 'a1')?.status).toBe('done');
    again();
  });

  it('survive F5: the cache keeps them with the cursor the runtime replays from', async () => {
    const kv = memoryKV();
    const { rt, m } = await bootFake();
    persistence = startPersistence(kv);
    await persistence.ready;
    const first = hub.openConversation(m, CONV);
    await settle();
    rt.emit(CH, { type: 'background_upsert', task: agent('a1', 'Audit backend') });
    rt.emit(CH, { type: 'background_upsert', task: finished('a2', 'Audit shell') });
    await settle();
    await vi.advanceTimersByTimeAsync(1500);
    await persistence.flushNow();
    const kept = kv.dump().feeds.get(CONV) as { state?: { background: BackgroundTask[] } } | undefined;
    expect(kept?.state?.background.map((t) => t.id)).toEqual(expect.arrayContaining(['a1', 'a2']));
    first();
    await settle();

    // The reload: every store is empty; the runtime is the same one, so it replays from the cursor and sends no snapshot.
    persistence.stop();
    resetCore();
    expect(backgroundStore.get(CONV)).toBeUndefined();
    persistence = startPersistence(kv);
    await persistence.ready;
    expect(idsOf()).toEqual(expect.arrayContaining(['a1', 'a2']));
    setFetch(rt.fetch);
    putMachines([rt.machine]);
    hub.start([m], m);
    const again = hub.openConversation(m, CONV);
    await settle();
    await settle();
    expect(idsOf()).toEqual(expect.arrayContaining(['a1', 'a2']));
    again();
  });
});
