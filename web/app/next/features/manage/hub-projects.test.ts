import { describe, expect, it } from 'vitest';
import { withHubProjects, type IHubProject } from './hub-projects';

const row = (id: string, asleep: boolean): IHubProject => ({
  id,
  name: id,
  machineId: 'm',
  machineName: 'M',
  asleep,
  label: asleep ? 'машина спит' : undefined,
});

describe('withHubProjects', () => {
  it('adds a project of a sleeping machine with its label', () => {
    const got = withHubProjects([{ id: 'a', name: 'a' }], [row('b', true)]);
    expect(got.map((p) => p.name)).toEqual(['a', 'b · машина спит']);
  });
  it('keeps what the page has and leaves out the machines that are on', () => {
    const own = [{ id: 'a', name: 'a' }];
    expect(withHubProjects(own, [row('a', true), row('c', false)])).toBe(own);
  });
  it("is the page's own list without the registry", () => {
    const own = [{ id: 'a', name: 'a' }];
    expect(withHubProjects(own, [])).toBe(own);
  });
});
