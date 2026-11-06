import { describe, expect, it } from 'vitest';
import { readHubEnv } from '@/next/features/hub-env/api';

describe('readHubEnv', () => {
  it('reads rows, with or without a message', () => {
    const got = readHubEnv({
      enabled: true,
      canSwitch: false,
      items: [
        { itemId: 'a', kind: 'skill', name: 'Review', layer: 'org', pinned: true, status: 'ok' },
        {
          itemId: 'b',
          kind: 'mcp',
          name: 'Tracker',
          layer: 'team',
          pinned: false,
          status: 'error',
          message: 'нет секрета',
        },
      ],
    });
    expect(got.enabled).toBe(true);
    expect(got.canSwitch).toBe(false);
    expect(got.items.map((i) => [i.itemId, i.pinned, i.status, i.message])).toEqual([
      ['a', true, 'ok', ''],
      ['b', false, 'error', 'нет секрета'],
    ]);
  });
  it('refuses a status or layer it does not know', () => {
    const row = { itemId: 'a', kind: 'skill', name: 'x', layer: 'org', pinned: false, status: 'ok' };
    expect(() => readHubEnv({ items: [{ ...row, status: 'weird' }] })).toThrow();
    expect(() => readHubEnv({ items: [{ ...row, layer: 'galaxy' }] })).toThrow();
    expect(() => readHubEnv({})).toThrow();
  });
});
