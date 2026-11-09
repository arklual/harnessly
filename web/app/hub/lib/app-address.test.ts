import { describe, expect, it } from 'vitest';
import { appPathOn, appTarget, machineForApp } from './app-address';

describe('the address of an app on the hub (HRN-913)', () => {
  it('puts the app under its machine and keeps the chat', () => {
    expect(appPathOn('m-1', 'example.app')).toBe('/m/m-1/apps/example.app');
    expect(appPathOn('m-1', 'example.app', '?chat=tab-1')).toBe('/m/m-1/apps/example.app?chat=tab-1');
  });
  it('opens on the online machine first', () => {
    expect(
      machineForApp([
        { id: 'a', status: 'offline' },
        { id: 'b', status: 'online' },
      ]),
    ).toBe('b');
    expect(machineForApp([{ id: 'a', status: 'pending' }])).toBe('a');
    expect(machineForApp([])).toBeNull();
  });
  // with no machine online, an app installed for the space opens at the hub, from the store.
  it('opens at the hub an app of the space when no machine is online', () => {
    const page = '/apps/space/sp1/example.app/';
    const installed = [{ id: 'example.app', path: page }];
    expect(appTarget([{ id: 'a', status: 'online' }], 'example.app', installed)).toEqual({ machine: 'a' });
    expect(appTarget([{ id: 'a', status: 'offline' }], 'example.app', installed)).toEqual({ hub: page });
    expect(appTarget([], 'example.app', installed)).toEqual({ hub: page });
    expect(appTarget([{ id: 'a', status: 'offline' }], 'other.app', installed)).toEqual({ machine: 'a' });
    expect(appTarget([{ id: 'a', status: 'offline' }], 'example.app', [{ id: 'example.app' }])).toEqual({
      machine: 'a',
    });
    expect(appTarget([], 'other.app', installed)).toBeNull();
  });
});
