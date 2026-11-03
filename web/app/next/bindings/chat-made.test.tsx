// @vitest-environment jsdom
// The chat's made list: asked when the tools home is on screen, the pictures merged from the snapshot, asked again
// while the runtime has not finished and after a failure, and a runtime that does not announce it has nothing.
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import type { ChatMade, Conversation, MadeItem, Ms, Picture, Shot } from '@/next/protocol/v2';

const mocks = vi.hoisted(() => ({
  loadMade: vi.fn(),
  features: true,
  shots: null as unknown,
  pictures: null as unknown,
  /** What the meta store answers for a url: a title, or null with the look (wait, none). */
  metas: new Map<string, { title: string | null; look: string }>(),
  failed: 0,
  retryMetas: vi.fn(),
}));
vi.mock('@/next/commands/pages', () => ({ loadMade: mocks.loadMade }));
vi.mock('@/next/stores/machines', () => ({ hasFeature: () => mocks.features }));
vi.mock('@/next/stores/connection', () => ({ useLinkRestored: () => 0 }));
vi.mock('@/next/stores/conversation', () => ({
  useShots: () => mocks.shots,
  usePictures: () => mocks.pictures,
}));
vi.mock('@/next/features/artifacts/meta-store', () => ({
  retryMetas: mocks.retryMetas,
  useArtifactMetas: () => ({
    of: (x: { url: string }) => {
      const got = mocks.metas.get(x.url) ?? { title: null, look: 'wait' };
      return {
        meta: got.title ? { kind: 'artifact', title: got.title, description: null } : null,
        look: got.look,
      };
    },
    failed: mocks.failed,
    waiting: 0,
  }),
}));
vi.mock('@/next/core/net/machine', () => ({ machineApi: () => ({ http: (p: string) => `/api/v2${p}` }) }));

import {
  MADE_DEBOUNCE_MS,
  MADE_RETRY_MS,
  TITLE_TRIES,
  retryPause,
  useChatMade,
} from '@/next/bindings/chat-made';
import { resetMade } from '@/next/features/pane/made-model';

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  vi.useFakeTimers();
  mocks.loadMade.mockReset();
  mocks.features = true;
  mocks.shots = [];
  mocks.pictures = [];
  mocks.metas = new Map();
  mocks.failed = 0;
  mocks.retryMetas.mockReset();
  resetMade();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const conv = (over: Partial<Conversation> = {}): Conversation =>
  ({
    id: 'c1',
    machineId: 'm1',
    state: { k: 'idle', since: 1 },
    lastActivityAt: 1,
    ...over,
  }) as unknown as Conversation;
const item = (kind: MadeItem['kind'], ref: string, title = 'T'): MadeItem => ({
  kind,
  ref,
  title,
  op: 'created',
  at: 1 as Ms,
  cover: null,
  designKind: null,
});
const ok = (made: ChatMade) => Promise.resolve({ ok: true, value: made });
const flush = async (ms = 0) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};

describe('useChatMade', () => {
  it('asks at once when the tools home is on and builds the cards and what the chat holds', async () => {
    mocks.loadMade.mockReturnValue(
      ok({
        items: [item('doc', 'https://d/a/spec', 'Spec'), item('design', 'landing', 'Landing')],
        complete: true,
      }),
    );
    const { result } = renderHook(() => useChatMade(conv(), true));
    expect(result.current.cards).toBeNull();
    await flush();
    expect(result.current.cards?.map((x) => x.title)).toEqual(['Spec', 'Landing']);
    expect(result.current.holds).toEqual({ pictures: false, documents: true, designs: true });
    expect(result.current.open(result.current.cards![1])?.ref).toBe('landing');
  });

  it('lists a tracker task by its own name, among the cards, and it does not make the chat hold documents', async () => {
    mocks.loadMade.mockReturnValue(
      ok({ items: [item('task', 'https://tracker.example.com/issue/HRN-7', 'Fix it')], complete: true }),
    );
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.cards?.map((x) => [x.title, x.icon])).toEqual([['Fix it', 'square-check-big']]);
    expect(result.current.holds?.documents).toBe(false);
    expect(result.current.open(result.current.cards![0])).toMatchObject({
      kind: 'task',
      ref: 'https://tracker.example.com/issue/HRN-7',
    });
    expect(mocks.retryMetas).not.toHaveBeenCalled();
  });

  it('asks nothing while the tools home is not on screen', async () => {
    renderHook(() => useChatMade(conv(), false));
    await flush(5000);
    expect(mocks.loadMade).not.toHaveBeenCalled();
  });

  it('knows the Gallery has content from the snapshot pictures', async () => {
    mocks.shots = [{ id: 's1', name: 'a.png', at: Date.now() as Ms }] as Shot[];
    mocks.pictures = [{ id: 'p1', text: '/tmp/b.png' }] as Picture[];
    mocks.loadMade.mockReturnValue(ok({ items: [], complete: true }));
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.holds?.pictures).toBe(true);
  });

  it('waits for the snapshot: no pictures yet means not known', async () => {
    mocks.shots = null;
    mocks.loadMade.mockReturnValue(ok({ items: [], complete: true }));
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.holds).toBeNull();
  });

  it('asks again while the answer is not complete, with a growing pause', async () => {
    mocks.loadMade
      .mockReturnValueOnce(ok({ items: [], complete: false }))
      .mockReturnValueOnce(ok({ items: [item('doc', 'https://d/a/x')], complete: true }));
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(mocks.loadMade).toHaveBeenCalledTimes(1);
    await flush(MADE_RETRY_MS);
    await flush(MADE_DEBOUNCE_MS);
    expect(mocks.loadMade).toHaveBeenCalledTimes(2);
    expect(result.current.cards).toHaveLength(1);
    await flush(60_000);
    expect(mocks.loadMade).toHaveBeenCalledTimes(2);
  });

  it('is quiet after a failure with nothing known, and asks again', async () => {
    mocks.loadMade
      .mockReturnValueOnce(Promise.resolve({ r: 'failed' }))
      .mockReturnValueOnce(ok({ items: [item('board', 'https://d/b/x?k=K', 'Flow')], complete: true }));
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.cards).toEqual([]);
    await flush(MADE_RETRY_MS);
    await flush(MADE_DEBOUNCE_MS);
    expect(result.current.cards?.map((x) => x.title)).toEqual(['Flow']);
  });

  it('asks again a second after the chat moves', async () => {
    mocks.loadMade.mockReturnValue(ok({ items: [], complete: true }));
    const { rerender } = renderHook(({ c }) => useChatMade(c, true), { initialProps: { c: conv() } });
    await flush();
    rerender({ c: conv({ lastActivityAt: 2 }) });
    await flush(MADE_DEBOUNCE_MS - 1);
    expect(mocks.loadMade).toHaveBeenCalledTimes(1);
    await flush(2);
    expect(mocks.loadMade).toHaveBeenCalledTimes(2);
  });

  it('has nothing to show on a runtime that does not announce it, and asks nothing', async () => {
    mocks.features = false;
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(mocks.loadMade).not.toHaveBeenCalled();
    expect(result.current.cards).toEqual([]);
  });

  it('draws the kept list at once on the next visit', async () => {
    mocks.loadMade.mockReturnValue(ok({ items: [item('doc', 'https://d/a/s', 'Spec')], complete: true }));
    const first = renderHook(() => useChatMade(conv(), true));
    await flush();
    first.unmount();
    mocks.loadMade.mockReturnValue(new Promise(() => undefined));
    const second = renderHook(() => useChatMade(conv(), true));
    expect(second.result.current.cards?.map((x) => x.title)).toEqual(['Spec']);
  });
});

describe('the names of what the chat made', () => {
  const unnamed = (ref: string) => item('doc', ref, '');

  it('shows a page once however many addresses named it', async () => {
    mocks.loadMade.mockReturnValue(
      ok({
        items: [unnamed('https://ai.example.com/agents/docs/spec'), unnamed('https://a.example.com/a/spec')],
        complete: true,
      }),
    );
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.cards).toHaveLength(1);
  });

  it('keeps a bar for a name that is being read, then the title that was read', async () => {
    const ref = 'https://a.example.com/a/spec';
    mocks.loadMade.mockReturnValue(ok({ items: [unnamed(ref)], complete: true }));
    const { result, rerender } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.cards?.[0]).toMatchObject({ pending: true });
    mocks.metas.set(ref, { title: 'Spec of the clip', look: 'got' });
    rerender();
    expect(result.current.cards?.[0]).toMatchObject({ title: 'Spec of the clip', pending: false });
  });

  it('says the kind and where it lives, apart from its neighbour, when nothing names it', async () => {
    const one = 'https://ai.example.com/agents/docs/one';
    const two = 'https://ai.example.com/agents/docs/two';
    mocks.metas.set(one, { title: null, look: 'none' });
    mocks.metas.set(two, { title: null, look: 'none' });
    mocks.loadMade.mockReturnValue(ok({ items: [unnamed(one), unnamed(two)], complete: true }));
    const { result } = renderHook(() => useChatMade(conv(), true));
    await flush();
    expect(result.current.cards?.map((x) => [x.title, x.pending, x.about])).toEqual([
      ['Document', false, 'ai.example.com/one'],
      ['Document', false, 'ai.example.com/two'],
    ]);
  });

  it('asks again for the names that did not come, a few times and no more', async () => {
    mocks.failed = 1;
    mocks.loadMade.mockReturnValue(ok({ items: [unnamed('https://a.example.com/a/spec')], complete: true }));
    renderHook(() => useChatMade(conv(), true));
    await flush();
    await flush(retryPause(1));
    expect(mocks.retryMetas).toHaveBeenCalledTimes(1);
    for (let i = 0; i < TITLE_TRIES + 3; i += 1) await flush(60_000);
    expect(mocks.retryMetas).toHaveBeenCalledTimes(TITLE_TRIES);
  });
});

describe('retryPause', () => {
  it('doubles up to a cap', () => {
    expect([1, 2, 3, 4, 5, 9].map(retryPause)).toEqual([2000, 4000, 8000, 16000, 16000, 16000]);
  });
});
