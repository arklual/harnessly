import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import type { EnvResponse } from '../../hub-api/env';
import { countsOf, recount } from './env-calc';

const admin = JSON.parse(
  readFileSync(new URL('../../hub-api/fixtures/env/env-admin.json', import.meta.url), 'utf8'),
) as EnvResponse;

describe('countsOf', () => {
  it('counts the switched-on rows by kind', () => {
    expect(
      countsOf([
        { kind: 'skill', enabled: true },
        { kind: 'skill', enabled: false },
        { kind: 'mcp', enabled: true },
      ]),
    ).toEqual({ skill: 1, mcp: 1, repo: 0, doc: 0 });
  });
});

describe('recount', () => {
  it('agrees with the hub on an answer that is already counted', () => {
    const again = recount(admin);
    expect(again.summary).toEqual(admin.summary);
    expect(again.layers.map((l) => l.counts)).toEqual(admin.layers.map((l) => l.counts));
  });

  it('drops a row that was switched off from the counts and the pinned tile', () => {
    const org = admin.layers[0];
    if (!org) throw new Error('fixture has no layers');
    const row = org.items[0];
    if (!row) throw new Error('fixture has no rows');
    const off = recount({
      ...admin,
      layers: [{ ...org, items: [{ ...row, enabled: false }] }, ...admin.layers.slice(1)],
    });
    expect(off.layers[0]?.counts.skill).toBe(0);
    expect(off.summary.total).toBe(admin.summary.total - 1);
    expect(off.summary.pinnedByOrg).toBe(0);
  });
});
