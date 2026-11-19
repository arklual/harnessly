import { describe, expect, it } from 'vitest';

import { clock, isEmailLike, machineLine } from './format';
import type { PreflightReport } from './types';

const GIB = 1024 ** 3;

describe('machineLine', () => {
  const report = (cpus: number): PreflightReport => ({
    at: 0,
    osName: 'Ubuntu 22.04',
    os: 'linux',
    arch: 'amd64',
    cpus,
    memTotal: 64 * GIB,
    memAvailable: 50 * GIB,
    disk: { path: '/srv', total: 500 * GIB, free: 412 * GIB },
    tools: {
      tmux: { installed: true, version: '3.4', compatible: true },
      git: { installed: true, version: '2.43' },
      node: { installed: true, version: '22.1' },
      claude: { installed: false, version: null },
      codex: { installed: false, version: null },
    },
    checks: [],
  });

  it.each([
    [1, '1 ядро'],
    [2, '2 ядра'],
    [4, '4 ядра'],
    [5, '5 ядер'],
    [11, '11 ядер'],
    [21, '21 ядро'],
    [32, '32 ядра'],
  ])('writes %i cpus with the right form', (cpus, text) => {
    expect(machineLine(report(cpus))).toBe(`Ubuntu 22.04 · ${text} · 64 ГБ памяти · 412 ГБ свободно`);
  });
});

describe('small formats', () => {
  it('writes a count-down', () => {
    expect(clock(42)).toBe('0:42');
    expect(clock(61.2)).toBe('1:02');
    expect(clock(-3)).toBe('0:00');
  });

  it.each([
    ['you@company.ru', true],
    [' you@company.ru ', true],
    ['you@', false],
    ['you company.ru', false],
  ])('email %s: %s', (text, ok) => {
    expect(isEmailLike(text)).toBe(ok);
  });
});
