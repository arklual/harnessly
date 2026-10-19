// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { setThemeMode, startTheme } from './theme';

function fakeMedia(dark: boolean) {
  const listeners: Array<() => void> = [];
  const mq = {
    matches: dark,
    addEventListener: (_: string, fn: () => void) => listeners.push(fn),
  };
  vi.stubGlobal('matchMedia', () => mq);
  window.matchMedia = (() => mq) as unknown as typeof window.matchMedia;
  return {
    flip(next: boolean) {
      mq.matches = next;
      listeners.forEach((fn) => fn());
    },
  };
}

describe('theme', () => {
  beforeEach(() => localStorage.clear());

  it('follows the system until a mode is chosen, and remembers the choice', () => {
    const os = fakeMedia(true);
    startTheme({ storageKey: 't' });
    expect(document.documentElement.dataset.theme).toBe('dark');
    os.flip(false);
    expect(document.documentElement.dataset.theme).toBe('light');
    setThemeMode('dark');
    expect(localStorage.getItem('t')).toBe('dark');
    os.flip(false);
    expect(document.documentElement.dataset.theme).toBe('dark');
  });

  it('starts from the stored mode', () => {
    fakeMedia(false);
    localStorage.setItem('t', 'dark');
    startTheme({ storageKey: 't' });
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.style.colorScheme).toBe('dark');
  });
});
