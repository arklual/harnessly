import { describe, expect, it } from 'vitest';
import type { ICatalogEntry, ITeamInfo } from '@/next/features/app-catalog/api';
import { refusalOf, ROW_ACTION, rowsOf, secretsOf } from '@/next/features/app-catalog/model';

const entry = (over: Partial<ICatalogEntry> = {}): ICatalogEntry => ({
  id: 'acme.demo',
  channel: 'stable',
  version: '1.0.0',
  name: 'Демо',
  description: '',
  trust: 'sandboxed',
  permissions: [],
  by: 'ann@team.test',
  installed: false,
  ...over,
});

const team = (role: ITeamInfo['role'], catalog: ICatalogEntry[], muted: string[] = []): ITeamInfo => ({
  space: 'sp1',
  role,
  catalog,
  muted,
});

describe('catalog rows', () => {
  it('lets an owner install what is not installed, and take off what is', () => {
    expect(rowsOf(team('owner', [entry()]))[0]?.action).toBe(ROW_ACTION.install);
    expect(rowsOf(team('owner', [entry({ installed: true })]))[0]?.action).toBe(ROW_ACTION.uninstall);
  });

  it('never offers a trusted app for the team', () => {
    expect(rowsOf(team('owner', [entry({ trust: 'trusted' })]))[0]?.action).toBeNull();
  });

  it('gives a member nothing before an owner installs, and a switch for themselves after', () => {
    expect(rowsOf(team('member', [entry()]))[0]?.action).toBeNull();
    expect(rowsOf(team('member', [entry({ installed: true })]))[0]?.action).toBe(ROW_ACTION.mute);
    const muted = rowsOf(team('member', [entry({ installed: true })], ['acme.demo']))[0];
    expect(muted?.action).toBe(ROW_ACTION.unmute);
    expect(muted?.muted).toBe(true);
  });

  it('never lets a member install or remove for the team', () => {
    for (const installed of [false, true]) {
      const action = rowsOf(team('member', [entry({ installed })]))[0]?.action;
      expect(action === ROW_ACTION.install || action === ROW_ACTION.uninstall).toBe(false);
    }
  });
});

describe('the check before the download (HRN-700)', () => {
  it('says each refusal in its own words and offers no install', () => {
    const check = { refusals: [{ code: 'command' as const, detail: '/deck acme.deck' }], secrets: [] };
    const rows = rowsOf({
      space: 's',
      role: 'owner',
      catalog: [entry({ installed: false, check })],
      muted: [],
    });
    expect(rows[0]?.action).toBeNull();
    expect(refusalOf(check)).toContain('/deck');
    expect(refusalOf({ refusals: [{ code: 'host', detail: '>=0.99' }], secrets: [] })).toContain('>=0.99');
    expect(refusalOf({ refusals: [], secrets: [] })).toBeNull();
    expect(secretsOf({ refusals: [], secrets: ['token'] })).toContain('token');
  });
});
