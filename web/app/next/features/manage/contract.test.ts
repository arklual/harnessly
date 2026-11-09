// The client against the hub's real answers: fixtures/ are written by the Go test
// (internal/hub/orgs/me_fixtures_test.go), so a change on either side breaks a test instead of the screen.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { liveHub, mapKpis, mapLimits, mapSessions, mapUsage } from '@/next/features/manage/api';
import { clockOf, dayOf, hubMoney, hubMoneyShort, stamp } from '@/next/features/manage/format';

afterEach(() => vi.unstubAllGlobals());

const dir = join(__dirname, 'fixtures');
const read = (set: string, name: string): unknown =>
  JSON.parse(readFileSync(join(dir, set, `${name}.json`), 'utf8'));

const serve = (set: string) =>
  vi.fn(async (url: string) => {
    const name = String(url).split('/api/me/')[1] ?? '';
    return Response.json(read(set, name));
  });

describe.each(['empty', 'rules'])('the %s answers of the hub', (set) => {
  it('pass the schemas and map into the screen shapes', async () => {
    vi.stubGlobal('fetch', serve(set));
    const me = await liveHub('https://hub.example').load();
    expect(me.limits.currency).toMatch(/^[A-Z]{3}$/);
    for (const l of me.limits.limits) {
      expect(Number.isFinite(l.used)).toBe(true);
      expect(l.amount).toBeGreaterThan(0);
    }
    for (const d of me.usage.days) expect(Number.isFinite(d.cost)).toBe(true);
    for (const k of me.kpis.kpis) expect(Number.isFinite(k.value)).toBe(true);
  });

  it('can be formatted without throwing', async () => {
    vi.stubGlobal('fetch', serve(set));
    const me = await liveHub('https://hub.example').load();
    expect(() => {
      hubMoney(1, me.limits.currency);
      hubMoneyShort(1500, me.limits.currency);
      stamp(me.limits.measuredUntil);
      for (const l of me.limits.limits) dayOf(l.resetsAt);
      for (const s of me.sessions.sessions) clockOf(s.endedAt);
    }).not.toThrow();
  });
});

describe('the mappers', () => {
  it('map the rule fixtures', () => {
    const limits = mapLimits(read('rules', 'limits') as never, read('rules', 'usage') as never);
    expect(Array.isArray(limits.limits)).toBe(true);
    expect(mapUsage(read('rules', 'usage') as never).days).toBeInstanceOf(Array);
    expect(mapSessions(read('rules', 'sessions') as never).sessions).toBeInstanceOf(Array);
    const usage = read('rules', 'usage') as { currency: never };
    expect(mapKpis(read('rules', 'kpis') as never, usage.currency).kpis).toBeInstanceOf(Array);
  });
  it('say nothing was counted when the hub has not counted', () => {
    const limits = mapLimits(read('empty', 'limits') as never, read('empty', 'usage') as never);
    expect(limits.measuredUntil).toBeNull();
  });
  it('rejects a body of another shape instead of drawing it', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ limits: [] })));
    await expect(liveHub('https://hub.example').load()).rejects.toMatchObject({ kind: 'failed' });
  });
});
