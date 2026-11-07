// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { useEffect } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { boards, cashback, lint, SLOT, stateOf } from '@/next/features/app-views/sinopia/fixtures';
import { useSinopia, type ISinopia } from '@/next/features/app-views/sinopia/use-sinopia';
import type { ISinopiaState } from '@/next/features/app-views/sinopia/types';

const api = vi.hoisted(() => ({
  syncSinopia: vi.fn(),
  runSinopiaChecks: vi.fn(),
  list: { apps: [] as unknown[], viewPort: 7000 },
}));

vi.mock('@/next/features/app-views/api', () => ({
  syncSinopia: api.syncSinopia,
  runSinopiaChecks: api.runSinopiaChecks,
  callTool: vi.fn(),
  createViewSession: vi.fn(),
  useInstalledApps: () => ({ list: api.list, error: null }),
}));
vi.mock('@/next/commands/messages', () => ({ sendMessage: vi.fn() }));
vi.mock('@/next/ui/toast', () => ({ toast: vi.fn() }));
vi.mock('@/next/features/designly/sdk/toast', () => ({ toast: vi.fn() }));

const MACHINE = 'm1' as never;

/** A board's window: it posts to the page the way a frame does, and the page answers it. */
const board = () => {
  const posted: { data: Record<string, any>; origin: string }[] = []; // eslint-disable-line @typescript-eslint/no-explicit-any
  return {
    posted,
    postMessage: (data: unknown, origin: string) =>
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      void posted.push({ data: data as Record<string, any>, origin }),
  };
};

const fromBoard = (source: unknown, data: unknown, origin = SLOT) =>
  act(() => {
    window.dispatchEvent(new MessageEvent('message', { data, source: source as Window, origin }));
  });

let current: ISinopia | null = null;
const Probe = ({ kind, viewing = false }: { kind: string | null; viewing?: boolean }) => {
  const value = useSinopia({
    machine: MACHINE,
    id: 'proj1',
    kind,
    conversation: null,
    slotOrigin: SLOT,
    viewing,
  });
  useEffect(() => {
    current = value;
  });
  return null;
};
const sinopia = () => {
  if (current === null) throw new Error('not rendered');
  return current;
};
const flush = () => act(async () => undefined);

beforeEach(() => {
  api.list.apps = [lint, cashback];
  api.syncSinopia.mockReset();
  api.runSinopiaChecks.mockReset();
  current = null;
});
afterEach(cleanup);

describe('the apps of a presentation', () => {
  it('turns «Валидатор презентаций» on in slides and marks the elements the checks found', async () => {
    const marks: ISinopiaState['marks'] = [
      {
        node: `${boards[0].id}:3`,
        level: 'error',
        note: 'Слишком много слов',
        source: 'app:harnessly.deck-lint',
      },
    ];
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id], marks }));
    render(<Probe kind="slides" />);
    await flush();
    expect(sinopia().active.map((a) => a.app.id)).toEqual([lint.id]);

    // A board of the presentation says hello and is sent its marks, at the slot's origin only.
    const f = board();
    fromBoard(f, { type: 'tk:hl-hello', url: `${SLOT}/a/proj1/p/index.dc.html` });
    const sent = f.posted.filter((p) => p.data.type === 'tk:hl-marks').at(-1);
    expect(sent?.origin).toBe(SLOT);
    expect(sent?.data.items).toEqual([{ tpl: 3, level: 'error', note: 'Слишком много слов' }]);
  });

  it('shows no app of the list in a site: the runtime says none is on, and the page agrees', async () => {
    // Even a runtime that listed the validator would not put it on a site: its target is slides only.
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'site', apps: [lint.id] }));
    render(<Probe kind="site" />);
    await flush();
    expect(sinopia().active).toEqual([]);
  });

  it('asks the runtime nothing about a document until its kind is known', async () => {
    render(<Probe kind={null} />);
    await flush();
    expect(api.syncSinopia).not.toHaveBeenCalled();
  });
});

describe('a click on a bound element', () => {
  const click = (url: string) => ({
    type: 'tk:hl-click',
    field: 'amount',
    hl: { field: 'amount' },
    tpl: 4,
    fp: 'fp1',
    selector: '#amount',
    tag: 'span',
    text: '5%',
    rect: { x: 10, y: 20, width: 40, height: 16 },
    url,
  });

  it('opens the app’s inspector on that field, selects the element and tells the runtime', async () => {
    const state = stateOf({ apps: [cashback.id] });
    api.syncSinopia.mockResolvedValue(state);
    render(<Probe kind="site" />);
    await flush();
    const f = board();
    const url = `${SLOT}/a/proj1/p/cashback/card.dc.html`;
    fromBoard(f, { type: 'tk:hl-hello', url });
    // The apps with bindings turn the board's clicks on.
    expect(f.posted.some((p) => p.data.type === 'tk:hl' && p.data.on === true)).toBe(true);

    fromBoard(f, click(url));
    await flush();

    const now = sinopia();
    expect(now.focus?.app.id).toBe(cashback.id);
    expect(now.focus?.view.id).toBe('inspector');
    expect(now.focus?.field).toBe('amount');
    expect(now.selection).toHaveLength(1);
    expect(now.selection[0]).toMatchObject({ id: `${boards[1].id}:4`, field: 'amount', fp: 'fp1' });
    // The runtime keeps the selection, which is what the view reads with hl.sinopia.selection().
    const sent = api.syncSinopia.mock.calls.at(-1);
    expect(sent?.[3]).toEqual([expect.objectContaining({ id: `${boards[1].id}:4`, field: 'amount' })]);
    // A view that is open is told by a notification.
    expect(now.notice).toMatchObject({
      method: 'harnessly/notifications/selection',
      params: { document: 'proj1', field: 'amount' },
    });
  });

  it('does nothing for a click from a board the page does not know, or from another origin', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ apps: [cashback.id] }));
    render(<Probe kind="site" />);
    await flush();
    const unknown = board();
    fromBoard(unknown, click(`${SLOT}/a/proj1/p/index.dc.html`));
    const f = board();
    fromBoard(f, { type: 'tk:hl-hello', url: `${SLOT}/a/proj1/p/index.dc.html` });
    fromBoard(f, click(`${SLOT}/a/proj1/p/index.dc.html`), 'https://evil.example');
    await flush();
    expect(sinopia().focus).toBeNull();
    expect(sinopia().selection).toEqual([]);
  });

  it('opens nothing where no app binds elements', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id] }));
    render(<Probe kind="slides" />);
    await flush();
    const f = board();
    const url = `${SLOT}/a/proj1/p/index.dc.html`;
    fromBoard(f, { type: 'tk:hl-hello', url });
    fromBoard(f, click(url));
    await flush();
    expect(sinopia().focus).toBeNull();
    expect(f.posted.find((p) => p.data.type === 'tk:hl')?.data.on).toBe(false);
  });
});

describe('a document looked at («Макет»)', () => {
  const marks: ISinopiaState['marks'] = [
    {
      node: `${boards[0].id}:3`,
      level: 'error',
      note: 'Слишком много слов',
      source: 'app:harnessly.deck-lint',
    },
  ];
  const click = (url: string) => ({
    type: 'tk:hl-click',
    field: 'amount',
    hl: { field: 'amount' },
    tpl: 4,
    selector: '#amount',
    tag: 'span',
    rect: { x: 10, y: 20, width: 40, height: 16 },
    url,
  });

  it('sends its boards no marks, and they come back when it is edited again', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id], marks }));
    const view = render(<Probe kind="slides" viewing />);
    await flush();
    const f = board();
    fromBoard(f, { type: 'tk:hl-hello', url: `${SLOT}/a/proj1/p/index.dc.html` });
    expect(f.posted.filter((p) => p.data.type === 'tk:hl-marks').at(-1)?.data.items).toEqual([]);

    view.rerender(<Probe kind="slides" />);
    await flush();
    expect(f.posted.filter((p) => p.data.type === 'tk:hl-marks').at(-1)?.data.items).toEqual([
      { tpl: 3, level: 'error', note: 'Слишком много слов' },
    ]);
  });

  it('turns the bound elements off: a click opens no inspector and selects nothing', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ apps: [cashback.id] }));
    render(<Probe kind="site" viewing />);
    await flush();
    const f = board();
    const url = `${SLOT}/a/proj1/p/cashback/card.dc.html`;
    fromBoard(f, { type: 'tk:hl-hello', url });
    expect(f.posted.filter((p) => p.data.type === 'tk:hl').at(-1)?.data.on).toBe(false);
    fromBoard(f, click(url));
    await flush();
    expect(sinopia().focus).toBeNull();
    expect(sinopia().selection).toEqual([]);
  });
});

describe('a block picked in the presentation editor', () => {
  const pick = {
    path: 'index.dc.html',
    tpl: 4,
    was: 'p:1sj5ar9',
    text: 'Слишком длинный текст слайда',
    tag: 'p',
    rect: [10, 20, 400, 60] as const,
  };

  it('selects the block with its words, tells the runtime, and clears it when none is picked', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id] }));
    render(<Probe kind="slides" />);
    await flush();
    await act(async () => sinopia().pickBlock(pick));
    expect(sinopia().selection).toEqual([
      expect.objectContaining({
        id: `${boards[0].id}:4`,
        page: boards[0].id,
        fp: 'p:1sj5ar9',
        tag: 'p',
        text: 'Слишком длинный текст слайда',
        rect: { x: 10, y: 20, width: 400, height: 60 },
      }),
    ]);
    expect(api.syncSinopia.mock.calls.at(-1)?.[3]).toEqual([
      expect.objectContaining({ id: `${boards[0].id}:4` }),
    ]);

    // The same block again says nothing to the runtime.
    const calls = api.syncSinopia.mock.calls.length;
    await act(async () => sinopia().pickBlock(pick));
    expect(api.syncSinopia.mock.calls.length).toBe(calls);

    await act(async () => sinopia().pickBlock(null));
    expect(sinopia().selection).toEqual([]);
    expect(api.syncSinopia.mock.calls.at(-1)?.[3]).toEqual([]);
  });

  it('ignores a board the document does not have', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id] }));
    render(<Probe kind="slides" />);
    await flush();
    await act(async () => sinopia().pickBlock({ ...pick, path: 'gone.dc.html' }));
    expect(sinopia().selection).toEqual([]);
  });
});

describe('the share gate', () => {
  const finding = {
    app: lint.id,
    appName: lint.name,
    check: 'rules',
    findings: [{ node: `${boards[0].id}:3`, level: 'error', message: 'Слишком много слов' }],
    blocks: true,
  };

  it('keeps a presentation from going out while a check blocks it, and says why', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id] }));
    api.runSinopiaChecks.mockResolvedValue({ results: [finding], blocked: true, marks: [] });
    render(<Probe kind="slides" />);
    await flush();
    const proceed = vi.fn();
    act(() => sinopia().gate(proceed));
    await flush();
    expect(api.runSinopiaChecks).toHaveBeenCalledWith(MACHINE, 'proj1', 'share');
    expect(proceed).not.toHaveBeenCalled();
    expect(sinopia().blocked).toEqual([finding]);
  });

  it('lets it go when the checks pass', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ kind: 'slides', apps: [lint.id] }));
    api.runSinopiaChecks.mockResolvedValue({
      results: [{ ...finding, findings: [], blocks: false }],
      blocked: false,
      marks: [],
    });
    render(<Probe kind="slides" />);
    await flush();
    const proceed = vi.fn();
    act(() => sinopia().gate(proceed));
    await flush();
    expect(proceed).toHaveBeenCalledOnce();
    expect(sinopia().blocked).toBeNull();
  });

  it('asks no check where no app on in the document has one', async () => {
    api.syncSinopia.mockResolvedValue(stateOf({ apps: [cashback.id] }));
    render(<Probe kind="site" />);
    await flush();
    const proceed = vi.fn();
    act(() => sinopia().gate(proceed));
    expect(proceed).toHaveBeenCalledOnce();
    expect(api.runSinopiaChecks).not.toHaveBeenCalled();
  });
});
