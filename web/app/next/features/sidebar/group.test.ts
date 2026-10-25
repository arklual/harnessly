import { describe, expect, it } from 'vitest';
import { DAY_GROUP, childrenOf, dayGroupOf, sectionsOf } from '@/next/features/sidebar/group';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';

const NOW = new Date(2026, 9, 22, 15, 0).getTime();
const HOUR = 3_600_000;

const chat = (id: string, hoursAgo: number, extra: Partial<ISidebarChat> = {}): ISidebarChat => ({
  id,
  title: id,
  status: CHAT_STATUS.none,
  pinned: false,
  projectId: null,
  createdAt: NOW - hoursAgo * HOUR,
  lastPromptAt: NOW - hoursAgo * HOUR,
  lastActivityAt: NOW - hoursAgo * HOUR,
  terminal: true,
  canStopTerminal: true,
  ...extra,
});

describe('dayGroupOf', () => {
  it('splits by the local midnight, not by 24 hours', () => {
    expect(dayGroupOf(NOW - 14 * HOUR, NOW)).toBe(DAY_GROUP.today);
    expect(dayGroupOf(NOW - 16 * HOUR, NOW)).toBe(DAY_GROUP.yesterday);
    expect(dayGroupOf(NOW - 4 * 24 * HOUR, NOW)).toBe(DAY_GROUP.week);
    expect(dayGroupOf(NOW - 9 * 24 * HOUR, NOW)).toBe(DAY_GROUP.older);
  });
});

describe('sectionsOf', () => {
  const projects = [
    { id: 'p1', name: 'harnessly' },
    { id: 'p2', name: 'empty' },
  ];

  it('shows a delegated chat under its parent, not in the day groups', () => {
    const chats = [
      chat('parent', 3),
      chat('kid-b', 1, { parentId: 'parent' }),
      chat('kid-a', 2, { parentId: 'parent' }),
      chat('orphan', 1.5, { parentId: 'deleted' }),
    ];
    const sections = sectionsOf(chats, projects, NOW);
    expect(sections.days.flatMap((d) => d.chats.map((c) => c.id))).toEqual(['orphan', 'parent']);
    expect(childrenOf(sections, 'parent').map((c) => c.id)).toEqual(['kid-a', 'kid-b']);
    expect(childrenOf(sections, 'orphan')).toEqual([]);
  });
  it('puts a chat in exactly one section, newest first', () => {
    const chats = [
      chat('old', 30),
      chat('pin', 50, { pinned: true, projectId: 'p1' }),
      chat('proj', 2, { projectId: 'p1' }),
      chat('lost', 1, { projectId: 'gone' }),
      chat('new', 0.5),
    ];
    const { pinned, projects: inProjects, days } = sectionsOf(chats, projects, NOW);
    expect(pinned.map((c) => c.id)).toEqual(['pin']);
    expect(inProjects.map((p) => [p.project.id, p.chats.map((c) => c.id)])).toEqual([
      ['p1', ['proj']],
      ['p2', []],
    ]);
    expect(days.map((d) => [d.group, d.chats.map((c) => c.id)])).toEqual([
      [DAY_GROUP.today, ['new', 'lost']],
      [DAY_GROUP.yesterday, ['old']],
    ]);
  });
  it('keeps the order and the day group when an agent replies (lastActivityAt moves, lastPromptAt does not)', () => {
    const before = [chat('a', 30), chat('b', 20), chat('c', 2)];
    const after = before.map((c) => (c.id === 'a' ? { ...c, lastActivityAt: NOW } : c));
    const order = (list: readonly ISidebarChat[]) =>
      sectionsOf(list, projects, NOW).days.map((d) => [d.group, d.chats.map((c) => c.id)]);
    expect(order(after)).toEqual(order(before));
    expect(order(after)).toEqual([
      [DAY_GROUP.today, ['c']],
      [DAY_GROUP.yesterday, ['b', 'a']],
    ]);
  });

  it('puts a chat the person just wrote to above a newer one, and in the day of that message', () => {
    const oldButWritten = chat('old', 30, { lastPromptAt: NOW - HOUR / 2 });
    const { days } = sectionsOf([chat('new', 1), oldButWritten, chat('older', 20)], projects, NOW);
    expect(days.map((d) => [d.group, d.chats.map((c) => c.id)])).toEqual([
      [DAY_GROUP.today, ['old', 'new']],
      [DAY_GROUP.yesterday, ['older']],
    ]);
  });

  it('breaks a tie of messages by the creation, and does not depend on the status', () => {
    const at = NOW - HOUR;
    const chats = [
      chat('a', 30, { lastPromptAt: at, status: CHAT_STATUS.done }),
      chat('b', 20, { lastPromptAt: at, status: CHAT_STATUS.thinking }),
      chat('c', 40, { lastPromptAt: at, status: CHAT_STATUS.question }),
    ];
    const { days } = sectionsOf(chats, projects, NOW);
    expect(days[0]?.chats.map((c) => c.id)).toEqual(['b', 'a', 'c']);
  });

  it('groups by the day of the last message, not by the last activity or the creation', () => {
    const old = chat('old', 30, { lastActivityAt: NOW });
    const { days } = sectionsOf([old, chat('new', 1)], projects, NOW);
    expect(days.map((d) => [d.group, d.chats.map((c) => c.id)])).toEqual([
      [DAY_GROUP.today, ['new']],
      [DAY_GROUP.yesterday, ['old']],
    ]);
  });

  it('orders a project and the pinned by the last message too', () => {
    const chats = [
      chat('p-old', 40, { projectId: 'p1', lastActivityAt: NOW }),
      chat('p-new', 3, { projectId: 'p1' }),
      chat('f-old', 50, { pinned: true, lastActivityAt: NOW }),
      chat('f-new', 5, { pinned: true }),
    ];
    const { pinned, projects: inProjects } = sectionsOf(chats, projects, NOW);
    expect(inProjects[0]?.chats.map((c) => c.id)).toEqual(['p-new', 'p-old']);
    expect(pinned.map((c) => c.id)).toEqual(['f-new', 'f-old']);
  });
});
