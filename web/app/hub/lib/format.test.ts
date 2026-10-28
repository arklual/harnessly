import { describe, expect, it } from 'vitest';

import {
  checkAnswer,
  checkedAgo,
  clock,
  consoleTarget,
  envNameProblem,
  isEmailLike,
  isSocksUrl,
  machineLine,
  validProxyUrl,
} from './format';
import type { PreflightReport, ProviderCheck } from './types';

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

describe('proxy URLs', () => {
  it.each([
    ['http://proxy.company.ru:3128', true],
    ['http://user:pa:ss@proxy.company.ru:3128', true],
    ['https://proxy.company.ru', true],
    ['socks5://127.0.0.1:1080', true],
    ['socks5h://proxy:1080', true],
    ['proxy.company.ru:3128', false],
    ['ftp://proxy:21', false],
    ['http://', false],
    ['http://proxy:3128/path', false],
    ['', false],
  ])('%s is valid: %s', (url, ok) => {
    expect(validProxyUrl(url)).toBe(ok);
  });

  it('knows a SOCKS proxy', () => {
    expect(isSocksUrl(' SOCKS5://h:1 ')).toBe(true);
    expect(isSocksUrl('http://h:1')).toBe(false);
  });
});

describe('consoleTarget', () => {
  it('stays a path in path mode', () => {
    expect(
      consoleTarget('/m/abc/', '/m/abc/console/0123456789abcdef', 'https://hub.example.com/s/x', 'abc'),
    ).toBe('/m/abc/console/0123456789abcdef');
  });

  it('keeps the runtime origin and the ticket in subdomain mode', () => {
    expect(
      consoleTarget(
        'https://abc.m.example.com/m/abc/?hl_ticket=tk',
        '/m/abc/console/0123456789abcdef',
        'https://hub.example.com/s/x',
        'abc',
      ),
    ).toBe('https://abc.m.example.com/m/abc/console/0123456789abcdef?hl_ticket=tk');
  });

  const open = 'https://abc.m.example.com/m/abc/?hl_ticket=tk';
  const here = 'https://hub.example.com/s/x';

  it.each([
    ['another host', 'https://evil.example.net/m/abc/console/1'],
    ['a protocol-relative address', '//evil.example.net/m/abc/console/1'],
    ['another scheme', 'http://abc.m.example.com/m/abc/console/1'],
    ['another port', 'https://abc.m.example.com:8443/m/abc/console/1'],
    ['a userinfo trick', 'https://abc.m.example.com@evil.example.net/m/abc/console/1'],
    ['a javascript address', 'javascript:alert(1)'],
    ['a path outside the machine', '/api/account'],
    ['another machine', '/m/other/console/1'],
    ['a dot-dot escape', '/m/abc/../../api/account'],
    ['an encoded dot-dot escape', '/m/abc/%2e%2e/%2e%2e/api'],
    ['a sibling prefix', '/m/abcd/console/1'],
    ['the machine root without a slash', '/m/abc'],
  ])('refuses %s without handing out the ticket', (_name, consoleUrl) => {
    expect(() => consoleTarget(open, consoleUrl, here, 'abc')).toThrow();
  });

  it('refuses a path of a machine of the same origin in path mode', () => {
    expect(() => consoleTarget('/m/abc/', '/m/other/console/1', here, 'abc')).toThrow();
  });
});

describe('small formats', () => {
  it('writes a count-down', () => {
    expect(clock(42)).toBe('0:42');
    expect(clock(61.2)).toBe('1:02');
    expect(clock(-3)).toBe('0:00');
  });

  it('says how long ago a check was', () => {
    expect(checkedAgo(1_000, 20_000)).toBe('только что');
    expect(checkedAgo(0, 3 * 60_000 + 5)).toBe('3 мин назад');
  });

  it('describes the answer of a failed check', () => {
    const base: ProviderCheck = {
      provider: 'claude',
      verdict: 'rejected',
      at: 0,
      via: 'direct',
      host: 'api.anthropic.com',
      status: 403,
      detail: null,
      hosts: [],
    };
    expect(checkAnswer(base)).toBe('403 · запрос отклонён');
    expect(checkAnswer({ ...base, verdict: 'network', status: null, detail: 'timeout' })).toBe('timeout');
  });

  it.each([
    ['you@company.ru', true],
    [' you@company.ru ', true],
    ['you@', false],
    ['you company.ru', false],
  ])('email %s: %s', (text, ok) => {
    expect(isEmailLike(text)).toBe(ok);
  });

  it.each([
    ['SENTRY_TOKEN', null],
    ['_X1', null],
    ['sentry', 'format'],
    ['1TOKEN', 'format'],
    ['PATH', 'reserved'],
    ['LD_PRELOAD', 'reserved'],
    ['ANTHROPIC_API_KEY', 'reserved'],
    ['HTTPS_PROXY', 'reserved'],
    ['RUNTIME_PORT', 'reserved'],
    ['A'.repeat(65), 'format'],
  ])('env name %s: %s', (name, problem) => {
    expect(envNameProblem(name)).toBe(problem);
  });
});
