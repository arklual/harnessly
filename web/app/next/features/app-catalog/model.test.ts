import { describe, expect, it } from 'vitest';
import type { ICatalogEntry, ITeamInfo } from '@/next/features/app-catalog/api';
import { ROW_ACTION, rowsOf } from '@/next/features/app-catalog/model';

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
