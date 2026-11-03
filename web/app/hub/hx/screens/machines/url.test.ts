import { describe, expect, it } from 'vitest';

import { readDrawer, withDrawer } from './url';

describe('drawer url', () => {
  it('reads the card keys and ignores what it does not know', () => {
    expect(readDrawer('?m=abc&tab=access&dlg=off&hubdata=mock')).toEqual({
      m: 'abc',
      tab: 'access',
      dlg: 'off',
    });
    expect(readDrawer('?tab=weird&dlg=nope&m=')).toEqual({ m: null, tab: null, dlg: null });
    expect(readDrawer('')).toEqual({ m: null, tab: null, dlg: null });
  });

  it('opens a card and keeps the other keys', () => {
    expect(withDrawer('?hubdata=mock&as=admin', { m: 'a1' })).toBe('?hubdata=mock&as=admin&m=a1');
  });

  it('changes the tab and the dialog without touching the machine', () => {
    expect(withDrawer('?m=a1', { tab: 'resources' })).toBe('?m=a1&tab=resources');
    expect(withDrawer('?m=a1&tab=resources', { tab: null, dlg: 'restart' })).toBe('?m=a1&dlg=restart');
  });

  it('drops the tab and the dialog together with the machine', () => {
    expect(withDrawer('?x=1&m=a1&tab=access&dlg=off', { m: null })).toBe('?x=1');
    expect(withDrawer('?m=a1', { m: null })).toBe('');
  });

  it('does not set a tab on a closed card', () => {
    expect(withDrawer('?x=1', { tab: 'access' })).toBe('?x=1');
  });

  it('switches to another machine and clears its tab when asked', () => {
    expect(withDrawer('?m=a1&tab=access', { m: 'b2', tab: null })).toBe('?m=b2');
  });
});
