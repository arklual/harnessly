import { describe, expect, it } from 'vitest';
import { withHubProjects, type IHubProject } from './hub-projects';

const row = (id: string, asleep: boolean, name = id): IHubProject => ({
  id,
  name,
  machineId: 'm',
  machineName: 'M',
  asleep,
  label: asleep ? 'машина спит' : undefined,
});

describe('withHubProjects', () => {
  it('adds a project only another machine has, by its name alone', () => {
    const got = withHubProjects([{ id: 'a', name: 'a' }], [row('b', true), row('c', false)]);
    expect(got.map((p) => p.name)).toEqual(['a', 'b', 'c']);
    expect(got[1]?.elsewhere).toBe(true);
  });
  it('does not repeat a project this list has under the same name', () => {
    const own = [{ id: 'a', name: 'PROD' }];
    expect(withHubProjects(own, [row('x', true, 'PROD'), row('y', false, 'prod ')])).toBe(own);
  });
  it('shows one row for a name several machines share', () => {
    const got = withHubProjects([], [row('x', true, 'CU AI'), row('y', false, 'CU AI')]);
    expect(got.map((p) => p.id)).toEqual(['x']);
  });
  it("leaves out a machine folder of one of the person's projects", () => {
    const own = [{ id: 'pp-1', name: 'Работа' }];
    expect(withHubProjects(own, [row('x', true, 'старое имя')], (m, p) => `${m}/${p}` === 'm/x')).toBe(own);
  });
  it("is the page's own list without the registry", () => {
    const own = [{ id: 'a', name: 'a' }];
    expect(withHubProjects(own, [])).toBe(own);
  });
});
