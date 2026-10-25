import { describe, expect, it } from 'vitest';
import { drawerState } from '@/next/features/shell/drawer-state';

describe('drawerState', () => {
  it('draws the drawer and shuts the window under it only on a phone', () => {
    expect(drawerState(true, true, false)).toEqual({ drawer: 'open', inert: true, rail: false });
  });

  it('never leaves a wide window inert after a drawer was left open', () => {
    expect(drawerState(true, false, false)).toEqual({ drawer: 'closed', inert: false, rail: false });
    expect(drawerState(true, false, true).inert).toBe(false);
  });

  it('keeps the rail for a wide window and the full sidebar for the phone drawer', () => {
    expect(drawerState(false, false, true).rail).toBe(true);
    expect(drawerState(true, true, true).rail).toBe(false);
  });

  it('closed stays closed and clickable', () => {
    expect(drawerState(false, true, false)).toEqual({ drawer: 'closed', inert: false, rail: false });
  });
});
