// The levels of a machine's access list (docs/hub/own-work.md, «Levels»): read from the hub's lines and written back.
import { describe, expect, it } from 'vitest';

import type { HubMachine } from '../../../../hub-api/analytics';
import { accessBody, accessOf } from './machines';

const machine = (access: HubMachine['access'], teamId: string | null = 't1') =>
  ({ access, teamId }) as HubMachine;

describe('access levels', () => {
  it('reads a watch line and leaves the rest as tasks', () => {
    const a = accessOf(
      machine([
        { kind: 'user', id: 'u1', label: 'Аня', level: 'view' },
        { kind: 'user', id: 'u2', label: 'Боря', level: 'tasks' },
        { kind: 'user', id: 'u3', label: 'Вера' },
      ]),
    );
    expect(a).toEqual({
      mode: 'pick',
      people: [
        { id: 'u1', name: 'Аня', level: 'view' },
        { id: 'u2', name: 'Боря' },
        { id: 'u3', name: 'Вера' },
      ],
    });
  });

  it('reads the team level', () => {
    expect(accessOf(machine([{ kind: 'team', id: 't1', label: 'Ядро', level: 'view' }]))).toEqual({
      mode: 'team',
      people: [],
      teamLevel: 'view',
    });
  });

  it('writes only the watch lines with a level, as an older hub reads the rest', () => {
    const m = machine([]);
    expect(accessBody(m, { mode: 'pick', personIds: ['u1', 'u2'], levels: { u1: 'view' } })).toEqual([
      { kind: 'user', id: 'u1', level: 'view' },
      { kind: 'user', id: 'u2' },
    ]);
    expect(accessBody(m, { mode: 'team', personIds: [], teamLevel: 'view' })).toEqual([
      { kind: 'team', id: 't1', level: 'view' },
    ]);
    expect(accessBody(m, { mode: 'team', personIds: [] })).toEqual([{ kind: 'team', id: 't1' }]);
  });
});
