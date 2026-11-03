import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Config, Me } from '../lib/types';
import type { OrgList } from '../../hub-api/orgs';
import { deferOrgStep, orgLanding, orgStepDue, rememberOrg } from './step';

const config = (orgs: string) => ({ orgs }) as unknown as Config;
const me = (role: 'owner' | 'member') =>
  ({ spaces: [{ id: 's', slug: 's', name: 'S', role }] }) as unknown as Me;

describe('organization step', () => {
  beforeEach(() => {
    const data = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
    });
  });

  it('is due only with HUB_ORGS=on, an owned space and no postponement', () => {
    expect(orgStepDue(config('on'), me('owner'))).toBe(true);
    expect(orgStepDue(config('shadow'), me('owner'))).toBe(false);
    expect(orgStepDue(config('off'), me('owner'))).toBe(false);
    expect(orgStepDue(config('on'), me('member'))).toBe(false);
  });

  it('leaves a person who put it off alone', () => {
    deferOrgStep();
    expect(orgStepDue(config('on'), me('owner'))).toBe(false);
  });

  const list = (...slugs: string[]): OrgList =>
    ({
      orgs: slugs.map((slug) => ({ slug, name: slug, role: 'admin' })),
      looseSpaces: [],
    }) as unknown as OrgList;

  it('is not due for a person already in an organization', () => {
    expect(orgStepDue(config('on'), me('owner'), list('acme'))).toBe(false);
    expect(orgStepDue(config('on'), me('owner'), list())).toBe(true);
  });

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
