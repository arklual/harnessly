import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { OrgList } from '../../hub-api/orgs';
import { orgLanding, rememberOrg } from './step';

describe('organization step', () => {
  beforeEach(() => {
    const data = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
    });
  });

  const list = (...slugs: string[]): OrgList =>
    ({
      orgs: slugs.map((slug) => ({ slug, name: slug, role: 'admin' })),
      looseSpaces: [],
    }) as unknown as OrgList;

  it('lands a member of an organization in its hub, the last one opened first', () => {
    expect(orgLanding(null)).toBeNull();
    expect(orgLanding(list())).toBeNull();
    expect(orgLanding(list('acme', 'harnessly'))).toBe('/o/acme');
    rememberOrg('harnessly');
    expect(orgLanding(list('acme', 'harnessly'))).toBe('/o/harnessly');
    rememberOrg('gone');
    expect(orgLanding(list('acme', 'harnessly'))).toBe('/o/acme');
  });
});
