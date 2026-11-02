import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Config, Me } from '../lib/types';
import { deferOrgStep, orgStepDue } from './step';

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
});
