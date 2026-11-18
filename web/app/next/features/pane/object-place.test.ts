// a document and a board open in one place of the pane, whatever way they are opened.
import { describe, expect, it } from 'vitest';
import { docSlugOf, objectPlaceOf } from '@/next/features/pane/doc-link';
import { handedTabs } from '@/next/legacy/handed';
import { placeAppObject, type IPaneTabs } from '@/next/features/pane/pane-tabs';
import type { IInstalledApp } from '@/next/features/app-views/api';

const ORIGIN = 'https://ai.example.com';
const config = { artifactsUrl: 'https://a.example.com', boardsUrl: 'https://a.example.com' };

const app = (id: string, view: string): IInstalledApp =>
  ({
    id,
    name: id,
    description: '',
    version: '1.0.0',
    trust: 'sandboxed',
    targets: ['app', 'pane'],
    permissions: [],
    views: [{ id: view, title: view, targets: ['app', 'pane'] }],
    mcpServers: [],
    state: 'ready',
  }) as IInstalledApp;
const APPS = [app('harnessly.docs', 'docs'), app('harnessly.boards', 'board')];
const DOC = { app: 'harnessly.docs', view: 'docs', slug: 'room-backstage' };
const BOARD = { app: 'harnessly.boards', view: 'board', slug: 'flow-abc' };

describe('objectPlaceOf: every address of a document opens the documents app', () => {
  it.each([
    ['the portal link of a chip, a card of the turn, «Документы»', 'https://a.example.com/a/room-backstage'],
    ['the portal link with a trailing slash', 'https://a.example.com/a/room-backstage/'],
    ['the reader path the runtime resolves `open` to', '/agents/docs/room-backstage'],
    ['the reader address of this page', `${ORIGIN}/agents/docs/room-backstage`],
  ])('%s', (_, target) => {
    expect(objectPlaceOf(target, APPS, ORIGIN, config)).toEqual(DOC);
  });

  it('a board, with or without its key, opens the boards app', () => {
    expect(objectPlaceOf('https://a.example.com/b/flow-abc', APPS, ORIGIN, config)).toEqual(BOARD);
    expect(objectPlaceOf('https://a.example.com/b/flow-abc?k=KEY', APPS, ORIGIN, config)).toEqual(BOARD);
  });

  it('leaves the old tab what the app does not show', () => {
    expect(objectPlaceOf('https://a.example.com/a/plan?v=3', APPS, ORIGIN, config)).toBeNull();
    expect(objectPlaceOf('https://a.example.com/a/x-abc', APPS, ORIGIN, config)).toBeNull();
    expect(objectPlaceOf('https://other.example.com/a/plan', APPS, ORIGIN, config)).toBeNull();
    expect(objectPlaceOf('https://a.example.com/a/plan', [], ORIGIN, config)).toBeNull();
    expect(objectPlaceOf('https://a.example.com/a/plan', null, ORIGIN, config)).toBeNull();
    expect(docSlugOf('https://a.example.com/a/plan', ORIGIN, { artifactsUrl: null })).toBeNull();
  });

  it('the portal and the reader name the same document: one tab, brought forward again', () => {
    const ctx = {
      busy: false,
      cwd: '/w',
      nextId: (() => {
        let n = 0;
        return () => `t${++n}`;
      })(),
    };
    let state: IPaneTabs = { tabs: [], active: '', unread: [] };
    for (const target of [
      'https://a.example.com/a/room-backstage',
      '/agents/docs/room-backstage',
      `${ORIGIN}/agents/docs/room-backstage`,
    ]) {
      const place = objectPlaceOf(target, APPS, ORIGIN, config);
      expect(place).not.toBeNull();
      if (place) state = placeAppObject(state, place.app, place.view, place.slug, ctx).next;
    }
    expect(state.tabs).toEqual([
      { id: 't1', kind: 'app', app: 'harnessly.docs', view: 'docs', object: 'room-backstage' },
    ]);
    expect(state.active).toBe('t1');
  });
});

describe('handedTabs: the old documents tab of a document an app shows is handed over', () => {
  const claims = (target: string) => objectPlaceOf(target, APPS, ORIGIN, config) !== null;
  const tabs = [
    { id: 'a', kind: 'artifacts' as const, target: '/agents/docs/room-backstage' },
    { id: 'b', kind: 'artifacts' as const, target: 'https://a.example.com/b/flow-abc?k=KEY' },
    { id: 'c', kind: 'artifacts' as const, target: null },
    { id: 'd', kind: 'artifacts' as const, target: '/agents/docs/plan?v=2' },
    { id: 'e', kind: 'task' as const, target: 'HRN-1' },
    { id: 'f', kind: 'browser' as const, target: 'http://localhost:3000' },
  ];
  it('hands the documents and boards, keeps the catalogue, a version and other tabs', () => {
    expect(handedTabs(tabs, claims).map((x) => x.id)).toEqual(['a', 'b', 'f']);
  });
  it('keeps them while the list of apps is not known yet', () => {
    expect(handedTabs(tabs, null).map((x) => x.id)).toEqual(['f']);
  });
});
