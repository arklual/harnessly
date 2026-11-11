import { describe, expect, it } from 'vitest';
import {
  DRAG,
  DROP,
  EDGE,
  PLAN,
  favoritesOf,
  groupOfChat,
  neighboursOf,
  resolveDrop,
  type IDragSource,
  type IDropTarget,
} from '@/next/features/sidebar/favorites';
import { CHAT_STATUS, NO_FAVORITES, type ISidebarChat } from '@/next/features/sidebar/types';

const chat = (id: string, at: number, extra: Partial<ISidebarChat> = {}): ISidebarChat => ({
  id,
  title: id,
  status: CHAT_STATUS.none,
  pinned: false,
  projectId: null,
  createdAt: at,
  lastPromptAt: at,
  lastActivityAt: at,
  terminal: true,
  canStopTerminal: true,
  ...extra,
});

const ids = (list: readonly ISidebarChat[]) => list.map((c) => c.id);

describe('favoritesOf', () => {
  const chats = [
    chat('a', 10, { pinned: true }),
    chat('b', 20, { pinned: true }),
    chat('c', 30, { pinned: true }),
    chat('d', 40, { pinned: true }),
    chat('x', 50),
  ];

  it('shows a pinned chat the layout does not name first among the loose, newest first', () => {
    const view = favoritesOf(chats, { groups: [], loose: ['a'] });
    expect(ids(view.loose)).toEqual(['d', 'c', 'b', 'a']);
  });

  it('keeps the manual order and the unnamed order when the activity moves', () => {
    const layout = { groups: [{ id: 'fg-1', name: 'Релизы', chats: ['b', 'a'] }], loose: ['d', 'c'] };
    const moved = chats.map((c) => (c.id === 'a' ? { ...c, lastActivityAt: 999 } : c));
    const view = favoritesOf(moved, layout);
    expect(ids(view.groups[0]?.chats ?? [])).toEqual(['b', 'a']);
    expect(ids(view.loose)).toEqual(['d', 'c']);
    const fresh = favoritesOf(moved, { groups: [], loose: [] });
    expect(ids(fresh.loose)).toEqual(['d', 'c', 'b', 'a']);
  });

  it('keeps the unnamed favorites still when a message or a reply lands in one', () => {
    const before = ids(favoritesOf(chats, NO_FAVORITES).loose);
    const written = chats.map((c) => (c.id === 'a' ? { ...c, lastPromptAt: 999, lastActivityAt: 999 } : c));
    expect(ids(favoritesOf(written, NO_FAVORITES).loose)).toEqual(before);
  });

  it('draws the same order whichever copy of the list came last (the machine or the cluster)', () => {
    // The two copies of one chat differ in their times (the cluster's lags, an agent's reminder counts as a message):
    // the order of the favorites does not follow them, so the 30 s sync does not shake the list.
    const machine = chats.map((c, i) => ({ ...c, lastPromptAt: 100 + i, lastActivityAt: 200 + i }));
    const cluster = chats.map((c, i) => ({ ...c, lastPromptAt: 100 - i, lastActivityAt: 50 - i }));
    const first = ids(favoritesOf(machine, NO_FAVORITES).loose);
    for (let n = 0; n < 5; n++) {
      expect(ids(favoritesOf(cluster, NO_FAVORITES).loose)).toEqual(first);
      expect(ids(favoritesOf(machine, NO_FAVORITES).loose)).toEqual(first);
    }
  });

  it('orders chats created at the same moment by id, not by where they stand in the list', () => {
    const same = [
      chat('z', 5, { pinned: true }),
      chat('m', 5, { pinned: true }),
      chat('b', 5, { pinned: true }),
    ];
    expect(ids(favoritesOf(same, NO_FAVORITES).loose)).toEqual(['b', 'm', 'z']);
    expect(ids(favoritesOf([...same].reverse(), NO_FAVORITES).loose)).toEqual(['b', 'm', 'z']);
  });

  it('puts a chat where the layout says and skips the names of chats that are not pinned', () => {
    const view = favoritesOf(chats, {
      groups: [
        { id: 'fg-1', name: 'Релизы', chats: ['b', 'x', 'gone', 'a'] },
        { id: 'fg-2', name: 'Идеи', chats: [] },
      ],
      loose: ['c', 'a'],
    });
    expect(view.groups.map((g) => [g.id, g.name, ids(g.chats)])).toEqual([
      ['fg-1', 'Релизы', ['b', 'a']],
      ['fg-2', 'Идеи', []],
    ]);
    // a stands in its group only; d is new to the layout, so it comes first
    expect(ids(view.loose)).toEqual(['d', 'c']);
  });

  it('is empty when nothing is pinned', () => {
    const view = favoritesOf([chat('x', 1)], NO_FAVORITES);
    expect(view.groups).toEqual([]);
    expect(view.loose).toEqual([]);
  });
});

describe('where a chat stands', () => {
  const chats = [
    chat('a', 1, { pinned: true }),
    chat('b', 2, { pinned: true }),
    chat('c', 3, { pinned: true }),
  ];
  const view = favoritesOf(chats, { groups: [{ id: 'fg-1', name: 'G', chats: ['a', 'b'] }], loose: ['c'] });

  it('names its group, null for the loose, undefined for a stranger', () => {
    expect(groupOfChat(view, 'a')).toBe('fg-1');
    expect(groupOfChat(view, 'c')).toBeNull();
    expect(groupOfChat(view, 'zzz')).toBeUndefined();
  });

  it('knows its neighbours', () => {
    expect(neighboursOf(view, 'a')).toEqual({ prev: false, next: true });
    expect(neighboursOf(view, 'b')).toEqual({ prev: true, next: false });
    expect(neighboursOf(view, 'c')).toEqual({ prev: false, next: false });
  });
});

describe('resolveDrop', () => {
  const chats = [
    chat('a', 1, { pinned: true }),
    chat('b', 2, { pinned: true }),
    chat('c', 3, { pinned: true }),
    chat('p', 4, { projectId: 'prj' }),
    chat('q', 5),
    chat('r', 6, { pinned: true, projectId: 'prj' }),
  ];
  const view = favoritesOf(chats, {
    groups: [
      { id: 'fg-1', name: 'One', chats: ['a', 'b'] },
      { id: 'fg-2', name: 'Two', chats: [] },
      { id: 'fg-3', name: 'Three', chats: ['r'] },
    ],
    loose: ['c'],
  });
  const chatSrc = (id: string): IDragSource => ({ kind: DRAG.chat, id });
  const groupSrc = (id: string): IDragSource => ({ kind: DRAG.group, id });
  const on = (kind: IDropTarget['kind'], id?: string, edge?: IDropTarget['edge']): IDropTarget => ({
    kind,
    id,
    edge,
  });
  const resolve = (source: IDragSource, target: IDropTarget) => resolveDrop(source, target, view, chats);

  it('pins an unpinned chat into the favorites, last among the loose', () => {
    expect(resolve(chatSrc('q'), on(DROP.favorites))).toEqual({
      op: PLAN.place,
      id: 'q',
      groupId: null,
      beforeId: null,
    });
  });

  it('puts a chat into a group, last, and takes a drop that changes nothing for nothing', () => {
    expect(resolve(chatSrc('c'), on(DROP.group, 'fg-2'))).toEqual({
      op: PLAN.place,
      id: 'c',
      groupId: 'fg-2',
      beforeId: null,
    });
    expect(resolve(chatSrc('b'), on(DROP.group, 'fg-1'))).toBeNull();
    expect(resolve(chatSrc('c'), on(DROP.favorites))).toBeNull();
  });

  it('puts a chat before or after a row by the half it is dropped on', () => {
    expect(resolve(chatSrc('c'), on(DROP.chat, 'a', EDGE.before))).toEqual({
      op: PLAN.place,
      id: 'c',
      groupId: 'fg-1',
      beforeId: 'a',
    });
    expect(resolve(chatSrc('c'), on(DROP.chat, 'a', EDGE.after))).toEqual({
      op: PLAN.place,
      id: 'c',
      groupId: 'fg-1',
      beforeId: 'b',
    });
    expect(resolve(chatSrc('c'), on(DROP.chat, 'b', EDGE.after))).toEqual({
      op: PLAN.place,
      id: 'c',
      groupId: 'fg-1',
      beforeId: null,
    });
  });

  it('does nothing for a chat dropped on itself or just where it already stands', () => {
    expect(resolve(chatSrc('a'), on(DROP.chat, 'a', EDGE.before))).toBeNull();
    expect(resolve(chatSrc('a'), on(DROP.chat, 'b', EDGE.before))).toBeNull();
    expect(resolve(chatSrc('b'), on(DROP.chat, 'a', EDGE.after))).toBeNull();
  });

  it('moves a chat into a project; a pinned one leaves the favorites with it', () => {
    expect(resolve(chatSrc('q'), on(DROP.project, 'prj'))).toEqual({
      op: PLAN.move,
      id: 'q',
      projectId: 'prj',
      unpin: false,
    });
    expect(resolve(chatSrc('a'), on(DROP.project, 'prj'))).toEqual({
      op: PLAN.move,
      id: 'a',
      projectId: 'prj',
      unpin: true,
    });
    expect(resolve(chatSrc('p'), on(DROP.project, 'prj'))).toBeNull();
    // pinned and already in the project: it only has to leave the favorites
    expect(resolve(chatSrc('r'), on(DROP.project, 'prj'))).toEqual({
      op: PLAN.move,
      id: 'r',
      projectId: 'prj',
      unpin: true,
    });
  });

  it('takes a chat out of the favorites or of its project when it is dropped on the recent', () => {
    expect(resolve(chatSrc('a'), on(DROP.recent))).toEqual({ op: PLAN.unpin, id: 'a' });
    expect(resolve(chatSrc('p'), on(DROP.recent))).toEqual({
      op: PLAN.move,
      id: 'p',
      projectId: null,
      unpin: false,
    });
    expect(resolve(chatSrc('q'), on(DROP.recent))).toBeNull();
  });

  it('reorders the groups', () => {
    expect(resolve(groupSrc('fg-3'), on(DROP.group, 'fg-1', EDGE.before))).toEqual({
      op: PLAN.moveGroup,
      id: 'fg-3',
      beforeId: 'fg-1',
    });
    expect(resolve(groupSrc('fg-1'), on(DROP.group, 'fg-2', EDGE.after))).toEqual({
      op: PLAN.moveGroup,
      id: 'fg-1',
      beforeId: 'fg-3',
    });
    expect(resolve(groupSrc('fg-1'), on(DROP.favorites))).toEqual({
      op: PLAN.moveGroup,
      id: 'fg-1',
      beforeId: null,
    });
    expect(resolve(groupSrc('fg-1'), on(DROP.group, 'fg-2', EDGE.before))).toBeNull();
    expect(resolve(groupSrc('fg-3'), on(DROP.favorites))).toBeNull();
    expect(resolve(groupSrc('fg-1'), on(DROP.group, 'fg-1', EDGE.after))).toBeNull();
  });

  it('puts a group after the group a chat of which it is dropped over, and nowhere over a loose chat', () => {
    expect(resolve(groupSrc('fg-3'), on(DROP.chat, 'a', EDGE.before))).toEqual({
      op: PLAN.moveGroup,
      id: 'fg-3',
      beforeId: 'fg-2',
    });
    expect(resolve(groupSrc('fg-1'), on(DROP.chat, 'c', EDGE.before))).toBeNull();
  });

  it('does not drop a group on a project or the recent', () => {
    expect(resolve(groupSrc('fg-1'), on(DROP.project, 'prj'))).toBeNull();
    expect(resolve(groupSrc('fg-1'), on(DROP.recent))).toBeNull();
  });
});
