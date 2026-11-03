import { describe, expect, it } from 'vitest';

import type { Person, TeamBlock, TeamsView } from '../../../org/api/teams';
import {
  groupOf,
  groupsOf,
  patchMove,
  patchNewTeam,
  patchPersonLimit,
  patchRemove,
  patchRole,
  patchTeamLimit,
  rowOf,
  seenOf,
  sliderOf,
  snapStop,
  stepStop,
  stopsOf,
  teamCeiling,
  tilesOf,
  toneOf,
} from './model';

const NOW = Date.parse('2026-11-03T12:00:00Z');

const person = (id: string, o: Partial<Person> = {}): Person => ({
  id,
  name: `Имя ${id}`,
  role: 'member',
  teamId: 't1',
  invited: false,
  lastActiveAt: '2026-11-03T10:00:00Z',
  spentRub: 10_000,
  limitRub: null,
  limitSetBy: null,
  limitSetAt: null,
  effective: { amountRub: 20_000, source: 'team', setBy: null, setAt: null },
  chatsWeek: 3,
  mrsWeek: 1,
  self: false,
  can: { limit: true, role: true, team: true, remove: true },
  needs: { limit: null, role: null, team: null },
  rules: [],
  ...o,
});

const block = (id: string | null, people: Person[], o: Partial<TeamBlock> = {}): TeamBlock => ({
  id,
  name: id ? `Команда ${id}` : 'Без команды',
  leadName: null,
  spentRub: people.reduce((n, p) => n + p.spentRub, 0),
  limitRub: id ? 100_000 : null,
  limitSetBy: null,
  handedOutPct: null,
  siblingsLimitRub: 0,
  can: { limit: true },
  needs: { limit: null },
  people,
  rules: [],
  ...o,
});

const view = (): TeamsView => ({
  scope: { label: 'Тенант', limitRub: 300_000, spentRub: 60_000 } as TeamsView['scope'],
  teams: [
    block('t1', [person('a'), person('b', { role: 'lead' })], { leadName: 'Имя b' }),
    block('t2', [person('c', { teamId: 't2' })], { limitRub: 50_000 }),
    block(null, [person('d', { teamId: null })]),
  ],
  activeWeek: 3,
  total: 4,
  activeSeries: [1, 2, 3],
  notAccepted: 0,
  notVisiting: 0,
  can: { createTeam: true, invite: true },
  spendKnown: true,
});

describe('tones', () => {
  it('warns from 75 % and goes red from 90 %', () => {
    expect([toneOf(10, null), toneOf(74, 100), toneOf(75, 100), toneOf(90, 100)]).toEqual([
      'none',
      'ok',
      'warn',
      'risk',
    ]);
  });
});

describe('rows', () => {
  it('says when a person was last seen', () => {
    expect(seenOf(person('a'), NOW)).toEqual({ text: '2 ч назад', idle: false });
    expect(seenOf(person('a', { lastActiveAt: '2026-10-20T10:00:00Z' }), NOW).idle).toBe(true);
    expect(seenOf(person('a', { invited: true, lastActiveAt: null }), NOW).text).toBe(
      'приглашение отправлено',
    );
  });
  it('writes the row of a person', () => {
    const r = rowOf(person('a', { spentRub: 18_000 }), NOW, 'Фронт');
    expect(r.hot).toBe(true);
    expect(r.sub).toBe('Фронт · 2 ч назад');
    expect(r.w).toBe('90%');
    expect(rowOf(person('a', { invited: true, lastActiveAt: null }), NOW, null).limTxt).toBe('— из 20 тыс ₽');
  });
});

describe('groups', () => {
  it('describes a team and filters by a query', () => {
    const v = view();
    const g = groupOf(v.teams[0]!, v.teams);
    expect(g.sub).toBe('лид Имя b · 2 человека');
    expect(g.hasLimit).toBe(true);
    expect(groupOf(v.teams[2]!, v.teams).hasLimit).toBe(false);
    expect(groupsOf(v, 'имя c').map((x) => x.teamId)).toEqual(['t2']);
    expect(groupsOf(v, 'команда t1')[0]!.people).toHaveLength(2);
  });
});

describe('tiles', () => {
  it('counts the people near their limit', () => {
    const v = view();
    v.teams[0]!.people[0] = person('a', { spentRub: 19_000 });
    expect(tilesOf(v, false).near.value).toContain('1 человек за 80%');
  });
});

describe('slider', () => {
  const steps = stopsOf([5_000, 10_000, 20_000, 30_000], 15_000);
  it('adds a value that is set off the presets, in order', () => {
    expect(steps).toEqual([5_000, 10_000, 15_000, 20_000, 30_000]);
  });
  it('locks what is below the spent and above the room left', () => {
    const s = sliderOf(steps, 20_000, 20_000, 12_000, 25_000);
    expect(s.lo).toBe(2);
    expect(s.hi).toBe(3);
    expect(snapStop(s, 0)).toBe(15_000);
    expect(snapStop(s, 1)).toBe(20_000);
    expect(stepStop(s, 1)).toBe(20_000);
    expect(stepStop(s, -1)).toBe(15_000);
  });
  it('never locks the value that is set now', () => {
    expect(sliderOf(steps, 30_000, 30_000, 0, 15_000).hi).toBe(4);
  });
  it('leaves the ceiling of a team to the tenant', () => {
    const v = view();
    expect(teamCeiling(v, v.teams[0]!)).toBe(300_000);
    v.teams[0]!.siblingsLimitRub = 50_000;
    expect(teamCeiling(v, v.teams[0]!)).toBe(250_000);
  });
});

describe('patches', () => {
  it('sets a personal limit and who it binds', () => {
    const v = patchPersonLimit(view(), 'a', 5_000);
    const a = v.teams[0]!.people[0]!;
    expect(a.limitRub).toBe(5_000);
    expect(a.effective.amountRub).toBe(5_000);
    expect(v.teams[0]!.handedOutPct).toBe(5);
  });
  it('changes a role and the lead of the team', () => {
    const v = patchRole(view(), 'a', 'lead');
    expect(v.teams[0]!.leadName).toBe('Имя a');
  });
  it('changes the limit of a team and the room of the others', () => {
    const v = patchTeamLimit(view(), 't2', 80_000);
    expect(v.teams[0]!.siblingsLimitRub).toBe(80_000);
    expect(v.teams[1]!.limitRub).toBe(80_000);
  });
  it('moves a person with their spend and keeps nobody in two places', () => {
    const v = patchMove(view(), 'a', 't2');
    expect(v.teams[0]!.people.map((p) => p.id)).toEqual(['b']);
    expect(v.teams[1]!.people.map((p) => p.id)).toEqual(['c', 'a']);
    expect(v.teams[1]!.spentRub).toBe(20_000);
    const none = patchMove(view(), 'c', null);
    expect(none.teams.find((t) => t.id === null)!.people.map((p) => p.id)).toEqual(['d', 'c']);
  });
  it('removes a person and counts the rest', () => {
    const v = patchRemove(view(), 'd');
    expect(v.total).toBe(3);
    expect(v.teams[2]!.people).toHaveLength(0);
  });
  it('adds a team before «без команды»', () => {
    const v = patchNewTeam(view(), ' Новая ');
    expect(v.teams.map((t) => t.name)).toEqual(['Команда t1', 'Команда t2', 'Новая', 'Без команды']);
  });
  it('does not change the view it was given', () => {
    const v = view();
    const before = JSON.stringify(v);
    patchMove(v, 'a', 't2');
    patchPersonLimit(v, 'a', 1);
    expect(JSON.stringify(v)).toBe(before);
  });
});
