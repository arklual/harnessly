import { describe, expect, it } from 'vitest';
import { activeApps, offeredApps, sidebarApps, withApp } from '@/next/features/app-main/chat-apps';
import type { IInstalledApp } from '@/next/features/app-views/api';

const app = (id: string, targets: string[] = ['chat', 'pane']): IInstalledApp => ({
  id,
  name: id,
  description: '',
  version: '1.0.0',
  trust: 'sandboxed',
  targets,
  permissions: [],
  views: [],
  mcpServers: [],
});

describe('withApp', () => {
  it('turns an app on once, last, and off without touching the others', () => {
    expect(withApp(['a.one'], 'b.two', true)).toEqual(['a.one', 'b.two']);
    expect(withApp(['a.one', 'b.two'], 'a.one', true)).toEqual(['b.two', 'a.one']);
    expect(withApp(['a.one', 'b.two'], 'a.one', false)).toEqual(['b.two']);
    expect(withApp([], 'a.one', false)).toEqual([]);
  });
});

describe('activeApps', () => {
  const installed = [app('a.one'), app('b.two')];

  it('lists each app once with the most recent chat it is on in', () => {
    const chats = [{ id: 'c1', apps: ['b.two'] }, { id: 'c2' }, { id: 'c3', apps: ['a.one', 'b.two'] }];
    expect(activeApps(chats, installed).map((x) => [x.app.id, x.chat])).toEqual([
      ['b.two', 'c1'],
      ['a.one', 'c3'],
    ]);
  });

  it('skips an app that is not installed any more', () => {
    expect(activeApps([{ id: 'c1', apps: ['gone.app'] }], installed)).toEqual([]);
  });
});

describe('offeredApps', () => {
  it('offers the installed apps that work in a chat, the ones on included', () => {
    const installed = [app('a.one'), app('b.two'), app('c.pane', ['pane'])];
    expect(offeredApps(installed).map((a) => a.id)).toEqual(['a.one', 'b.two']);
  });
});

describe('sidebarApps', () => {
  const env = (id: string, extra: Partial<IInstalledApp> = {}): IInstalledApp => ({
    ...app(id, ['app', 'chat']),
    env: true,
    ...extra,
  });

  it('adds the environment apps with a page of their own after the ones on in chats, without a chat', () => {
    const installed = [app('a.one'), env('org.boards'), env('org.docs')];
    const rows = sidebarApps([{ id: 'c1', apps: ['a.one', 'org.docs'] }], installed);
    expect(rows.map((x) => [x.app.id, x.chat])).toEqual([
      ['a.one', 'c1'],
      ['org.docs', 'c1'],
      ['org.boards', undefined],
    ]);
  });

  it('leaves out an environment app the person turned off, one that is disabled, and one without a page', () => {
    const installed = [
      env('org.muted', { muted: true }),
      env('org.off', { off: true }),
      env('org.safe', { state: 'disabled' }),
      env('org.plugin', { targets: ['chat'] }),
      app('own.app', ['app']),
    ];
    expect(sidebarApps([], installed)).toEqual([]);
  });
});
