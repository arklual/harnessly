import { describe, expect, it, vi } from 'vitest';

import { boardOf, screenContext, screenTools, MAX_NODES } from '@/next/features/app-views/sinopia/page-tools';

vi.mock('@/next/features/app-views/sinopia/host', () => ({ connectSinopia: vi.fn() }));

const boards = [
  { path: 'a.html', title: 'Обложка' },
  { path: 'b.html', title: 'План' },
];
const screen = {
  id: 'p1',
  kind: 'slides',
  title: 'Питч',
  boards,
  path: 'b.html',
  selection: [{ id: 'n1', page: 'b.html', tag: 'h1', text: 'x'.repeat(500) }],
  viewing: false,
};

// What the agent reads of the editor (HRN-808): the document, the board on screen, the selection, held small.
describe('screenContext', () => {
  it('says what is open and selected', () => {
    const c = screenContext(screen);
    expect(c.document).toEqual({ id: 'p1', kind: 'slides', title: 'Питч' });
    expect(c.mode).toBe('editing');
    expect(c.board).toEqual({ number: 2, path: 'b.html', title: 'План' });
    expect(c.boards.count).toBe(2);
    expect(c.selection[0]).toMatchObject({ id: 'n1', board: 'b.html', tag: 'h1' });
    expect(c.selection[0].text?.length).toBeLessThan(250);
  });

  it('has no board when none is on screen, and cuts a long selection', () => {
    const many = Array.from({ length: 50 }, (_, i) => ({ tag: 'p', text: String(i) }));
    const c = screenContext({ ...screen, path: null, selection: many, viewing: true });
    expect(c.board).toBeNull();
    expect(c.mode).toBe('viewing');
    expect(c.selection).toHaveLength(MAX_NODES);
  });
});

describe('boardOf', () => {
  it('takes a path or a number from 1', () => {
    expect(boardOf(boards, 'b.html')).toBe(boards[1]);
    expect(boardOf(boards, 1)).toBe(boards[0]);
    expect(boardOf(boards, '2')).toBe(boards[1]);
  });

  it('refuses a board that is not there, in words', () => {
    expect(() => boardOf(boards, 3)).toThrow(/from 1 to 2/);
    expect(() => boardOf(boards, 'z.html')).toThrow(/no board/);
  });
});

describe('screenTools', () => {
  const actions = () => ({ onGo: vi.fn(), onReload: vi.fn() });

  it('are WebMCP tools: a name, words, a schema and execute', () => {
    const tools = screenTools(() => ({ ...screen, ...actions() }));
    expect(tools.map((t) => t.name)).toEqual(['show_board', 'reload_board']);
    for (const t of tools) {
      expect(t.description.length).toBeGreaterThan(10);
      expect(t.inputSchema?.type).toBe('object');
    }
  });

  it('show_board turns the screen to the board the agent named', async () => {
    const a = actions();
    const [show] = screenTools(() => ({ ...screen, ...a }));
    expect(await show.execute({ board: 1 })).toEqual({ shown: 'a.html', number: 1 });
    expect(a.onGo).toHaveBeenCalledWith('a.html');
  });

  it('reload_board needs a board on screen', async () => {
    const a = actions();
    const [, reload] = screenTools(() => ({ ...screen, path: null, ...a }));
    expect(() => reload.execute({})).toThrow(/no board/);
    const [, again] = screenTools(() => ({ ...screen, ...a }));
    expect(await again.execute({})).toEqual({ reloaded: 'b.html' });
    expect(a.onReload).toHaveBeenCalled();
  });
});
