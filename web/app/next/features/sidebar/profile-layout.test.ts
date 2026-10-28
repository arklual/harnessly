import { describe, expect, it } from 'vitest';
import {
  chatProjectOf,
  EMPTY_PROFILE,
  favGroupDeleted,
  favGroupMoved,
  favPlaced,
  memberOn,
  mergeMachine,
  profileProjectOf,
  withMember,
  withProject,
} from '@/next/features/sidebar/profile-layout';
import type { Favorites, Project } from '@/next/protocol/v2';

const project = (machineId: string, id: string, name: string, createdAt = 1): Project => ({
  id,
  machineId,
  path: `/w/${name}`,
  name,
  git: false,
  createdAt,
});
const fav = (groups: [string, string, `tab-${string}`[]][], loose: `tab-${string}`[] = []): Favorites => ({
  groups: groups.map(([id, name, chats]) => ({ id, name, chats })),
  loose,
});
let n = 0;
const ids = { project: () => `pp-${++n}`, group: () => `fg-new${++n}` };

describe('the person sidebar', () => {
  it('merges the projects of two machines by name, nothing lost', () => {
    const a = mergeMachine(
      EMPTY_PROFILE,
      'runner-2',
      [project('runner-2', 'prj-1', 'admin'), project('runner-2', 'prj-2', 'Хакатон')],
      null,
      ids,
    );
    const b = mergeMachine(
      a,
      'dev',
      [project('dev', 'prj-1', 'Admin'), project('dev', 'prj-9', 'llm platform')],
      null,
      ids,
    );
    expect(b.projects.map((p) => p.name)).toEqual(['admin', 'Хакатон', 'llm platform']);
    const index = profileProjectOf(b);
    // The same project id on two machines stays two members of the one project.
    expect(chatProjectOf(index, 'runner-2', 'prj-1')).toBe(chatProjectOf(index, 'dev', 'prj-1'));
    expect(chatProjectOf(index, 'dev', 'prj-9')).not.toBeNull();
    expect(chatProjectOf(index, 'dev', null)).toBeNull();
    expect(chatProjectOf(index, 'dev', 'prj-unknown')).toBeNull();
  });

  it('changes nothing for what is merged already', () => {
    const projects = [project('m', 'prj-1', 'admin')];
    const once = mergeMachine(EMPTY_PROFILE, 'm', projects, fav([['fg-aaaa', 'Срочно', ['tab-1']]]), ids);
    expect(mergeMachine(once, 'm', projects, fav([['fg-aaaa', 'Срочно', ['tab-1', 'tab-2']]]), ids)).toBe(
      once,
    );
  });

  it('merges a machine favorites once: groups by name, chats where missing, loose after loose', () => {
    const a = mergeMachine(EMPTY_PROFILE, 'a', [], fav([['fg-aaaa', 'Срочно', ['tab-1']]], ['tab-2']), ids);
    const b = mergeMachine(
      a,
      'b',
      [],
      fav(
        [
          ['fg-bbbb', 'срочно', ['tab-3', 'tab-1']],
          ['fg-aaaa', 'Потом', ['tab-4']],
        ],
        ['tab-5', 'tab-2'],
      ),
      ids,
    );
    expect(b.favorites.groups.map((g) => [g.name, g.chats])).toEqual([
      ['Срочно', ['tab-1', 'tab-3']],
      ['Потом', ['tab-4']],
    ]);
    // A group id taken already gets a new one.
    expect(b.favorites.groups[1].id).not.toBe('fg-aaaa');
    expect(b.favorites.loose).toEqual(['tab-2', 'tab-5']);
    expect(b.merged).toEqual(['a', 'b']);
  });

  it('gives a project of the person a folder on a machine once', () => {
    const doc = withProject(EMPTY_PROFILE, ' Новый ', 'pp-x', 5);
    expect(doc.projects[0]).toEqual({ id: 'pp-x', name: 'Новый', createdAt: 5, members: [] });
    expect(memberOn(doc, 'pp-x', 'm')).toBeNull();
    const placed = withMember(doc, 'pp-x', { machineId: 'm', projectId: 'prj-7' });
    expect(memberOn(placed, 'pp-x', 'm')).toBe('prj-7');
    expect(withMember(placed, 'pp-x', { machineId: 'm', projectId: 'prj-7' })).toBe(placed);
  });

  it('places, groups and ungroups favorites as a machine does', () => {
    const doc = {
      ...EMPTY_PROFILE,
      favorites: fav(
        [
          ['fg-aaaa', 'A', ['tab-1']],
          ['fg-bbbb', 'B', []],
        ],
        ['tab-2'],
      ),
    };
    expect(favPlaced(doc, 'tab-2', 'fg-aaaa', 'tab-1')?.favorites).toEqual(
      fav([
        ['fg-aaaa', 'A', ['tab-2', 'tab-1']],
        ['fg-bbbb', 'B', []],
      ]),
    );
    expect(favPlaced(doc, 'tab-1', null, null)?.favorites.loose).toEqual(['tab-2', 'tab-1']);
    expect(favPlaced(doc, 'tab-1', 'fg-gone', null)).toBeNull();
    expect(favGroupDeleted(doc, 'fg-aaaa')?.favorites).toEqual(
      fav([['fg-bbbb', 'B', []]], ['tab-1', 'tab-2']),
    );
    expect(favGroupMoved(doc, 'fg-bbbb', 'fg-aaaa')?.favorites.groups.map((g) => g.id)).toEqual([
      'fg-bbbb',
      'fg-aaaa',
    ]);
    expect(favGroupMoved(doc, 'fg-aaaa', null)?.favorites.groups.map((g) => g.id)).toEqual([
      'fg-bbbb',
      'fg-aaaa',
    ]);
  });
});
