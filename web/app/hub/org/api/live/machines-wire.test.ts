import { describe, expect, it } from 'vitest';

import { busyOf, gbOf, hourly, monthRub, rentOf, type WireOp, type WirePoint } from './machines-wire';

const op = (kind: WireOp['kind'], status: WireOp['status'], chatsNow = 0): WireOp => ({
  id: `${kind}-${status}`,
  kind,
  status,
  chatsNow,
});

describe('machines wire', () => {
  it('shows the newest unfinished operation as the busy state', () => {
    expect(busyOf(undefined)).toBeNull();
    expect(busyOf([op('resize', 'done'), op('power_off', 'failed')])).toBeNull();
    expect(busyOf([op('resize', 'done'), op('drain', 'running', 2)])).toEqual({
      kind: 'drain',
      chats: 2,
      opId: 'drain-running',
      cancellable: true,
    });
    expect(busyOf([op('power_on', 'pending')])?.kind).toBe('on');
    expect(busyOf([op('resize', 'running')])?.cancellable).toBe(false);
  });

  it('reads the rented server in the provider shape and the older one', () => {
    expect(
      rentOf({
        provider: 'p',
        plan: 'c8',
        cpu: 8,
        ramGB: 32,
        diskGB: 500,
        price: { amount: 3376000, currency: 'RUB' },
      }),
    ).toMatchObject({ plan: 'c8', cpu: 8, ramGb: 32, price: { amount: 3376000 } });
    expect(rentOf({ provider: 'p', flavor: 'old', vcpus: 4, ramGb: 16, diskGb: 100 })).toMatchObject({
      plan: 'old',
      cpu: 4,
      price: null,
    });
    expect(rentOf(null)).toBeNull();
  });

  it('converts units', () => {
    expect(gbOf(34359738368)).toBe(32);
    expect(gbOf(null)).toBe(0);
    expect(monthRub({ amount: 3376000, currency: 'RUB' })).toBe(33760);
    expect(monthRub({ amount: 100, currency: 'USD' })).toBeNull();
    expect(monthRub(null)).toBeNull();
  });

  it('squeezes a day of points into 24 hours', () => {
    const points: WirePoint[] = Array.from({ length: 96 }, (_, i) => ({
      at: String(i),
      cpuAvg: i < 48 ? 10 : 30,
      ramAvg: null,
      diskAvg: null,
    }));
    const cpu = hourly(points, 'cpuAvg');
    expect(cpu).toHaveLength(24);
    expect(cpu[0]).toBe(10);
    expect(cpu[23]).toBe(30);
    expect(hourly(points, 'ramAvg')).toEqual([]);
    expect(hourly([], 'cpuAvg')).toEqual([]);
  });
});
