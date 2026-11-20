import { describe, expect, it } from 'vitest';
import { BOARDS_APP, boardPlaceOf } from '@/next/features/pane/board-link';
import { placeAppObject, placeRequest, type IPaneTabs } from '@/next/features/pane/pane-tabs';
import type { IInstalledApp } from '@/next/features/app-views/api';

const config = { boardsUrl: 'https://portal.example.com' };

const boards = (over: Partial<IInstalledApp> = {}): IInstalledApp =>
  ({
    id: BOARDS_APP,
    name: 'Доски',
    description: '',
    version: '0.5.0',
    trust: 'sandboxed',
    targets: ['app', 'pane'],
    permissions: [],
    views: [{ id: 'board', title: 'Boards', targets: ['app', 'pane'] }],
    mcpServers: [],
    state: 'ready',
    ...over,
  }) as IInstalledApp;

describe('boardPlaceOf: a board link opens in the boards app (HRN-1478)', () => {
  it('names the app, its pane view and the board', () => {
    expect(boardPlaceOf('https://portal.example.com/b/plan-abc123def456', [boards()], config)).toEqual({
      app: BOARDS_APP,
      view: 'board',
      slug: 'plan-abc123def456',
    });
    expect(
      boardPlaceOf('https://portal.example.com/b/plan-abc123def456?k=KEY', [boards()], config)?.slug,
    ).toBe('plan-abc123def456');
  });

  it('leaves other links, the API and the app home alone', () => {
    for (const href of [
      'https://portal.example.com/a/doc-1',
      'https://portal.example.com/b/api',
      'https://portal.example.com/b/app',
      'https://other.example.com/b/plan-abc123def456',
      'not a link',
    ])
      expect(boardPlaceOf(href, [boards()], config)).toBeNull();
  });

  it('opens as before without the app, or with it turned off or not ready', () => {
    const href = 'https://portal.example.com/b/plan-abc123def456';
    expect(boardPlaceOf(href, null, config)).toBeNull();
    expect(boardPlaceOf(href, [], config)).toBeNull();
    expect(boardPlaceOf(href, [boards({ muted: true })], config)).toBeNull();
    expect(boardPlaceOf(href, [boards({ off: true })], config)).toBeNull();
    expect(boardPlaceOf(href, [boards({ state: 'waiting' })], config)).toBeNull();
    expect(
      boardPlaceOf(
        href,
        [boards({ views: [{ id: 'board', title: 'B', targets: ['app'] }] } as never)],
        config,
      ),
    ).toBeNull();
  });
});

describe('placeAppObject', () => {
  const empty: IPaneTabs = { tabs: [], active: '', unread: [] };
  let n = 0;
  const ctx = { busy: false, cwd: '/w/', nextId: () => `t${++n}` };

  it('places an object carried by an app target', () => {
    const placed = placeRequest(
      empty,
      { k: 'app', target: `${BOARDS_APP}/board/one`, reload: false },
      { ...ctx, nextId: () => 'object-1' },
    );
    expect(placed.next.tabs).toEqual([
      { id: 'object-1', kind: 'app', app: BOARDS_APP, view: 'board', object: 'one' },
    ]);
  });

  it('a new tab on the board, then the same tab on another board', () => {
    const first = placeAppObject(empty, BOARDS_APP, 'board', 'one', ctx);
    expect(first.next.tabs).toEqual([
      { id: 't1', kind: 'app', app: BOARDS_APP, view: 'board', object: 'one' },
    ]);
    expect(first.next.active).toBe('t1');
    const second = placeAppObject(first.next, BOARDS_APP, 'board', 'two', ctx);
    expect(second.next.tabs).toEqual([
      { id: 't1', kind: 'app', app: BOARDS_APP, view: 'board', object: 'two' },
    ]);
  });
});
