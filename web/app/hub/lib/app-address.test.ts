import { describe, expect, it } from 'vitest';
import { appPathOn, machineForApp } from './app-address';

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
});
