import { describe, expect, it } from 'vitest';

import { fleetWorkspace } from './flag';

describe('fleetWorkspace', () => {
  it('is the hub’s /agents/ only when the hub serves it and the organization has the fleet', () => {
    expect(fleetWorkspace(true, ['ui.hx', 'runtime.fleet'])).toBe(true);
    expect(fleetWorkspace(true, ['ui.hx'])).toBe(false);
    expect(fleetWorkspace(false, ['runtime.fleet'])).toBe(false);
    expect(fleetWorkspace(undefined, ['runtime.fleet'])).toBe(false);
    expect(fleetWorkspace(true, undefined)).toBe(false);
  });
});
