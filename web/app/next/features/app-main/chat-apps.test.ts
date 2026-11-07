import { describe, expect, it } from 'vitest';
import { activeApps, offeredApps, withApp } from '@/next/features/app-main/chat-apps';
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
