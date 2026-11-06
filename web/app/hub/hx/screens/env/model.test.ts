import { describe, expect, it } from 'vitest';

import { detect, BAD } from './detect';
import { blankDraft, fromItem, hasBrew, secretVar, toSource, valid } from './model';
import { missingCommand } from './runs';
import { stateOfMachine } from './machine';
import type { MeMachine } from '../../../../hub-api/env';

describe('model', () => {
  it('names the variable of a safe key', () => {
    expect(secretVar('npm-token')).toBe('NPM_TOKEN');
    expect(secretVar('1pass')).toBe('_1PASS');
  });

  it('finds brew in a script but not in a word', () => {
    expect(hasBrew('brew install node')).toBe(true);
    expect(hasBrew('echo homebrew')).toBe(false);
  });

  it('turns a text script into a source and back', () => {
    const d = blankDraft('script');
    d.f.name = 'Node';
    d.f.src = 'text';
    d.f.body = 'corepack enable pnpm';
    expect(valid(d)).toBe(true);
    const source = toSource(d);
    expect(source.from).toBe('text');
    expect(source.body).toBe('corepack enable pnpm');
    const back = fromItem({
      id: 'i',
      kind: 'script',
      name: 'Node',
      hint: '',
      source,
      builtIn: false,
      archived: false,
      added: false,
      createdAt: '2026-11-01T00:00:00Z',
      owner: null,
    });
    expect(back?.k).toBe('script');
    expect(back?.f.body).toBe('corepack enable pnpm');
  });

  it('does not accept an empty script', () => {
    expect(valid(blankDraft('script'))).toBe(false);
  });
});

describe('detect', () => {
  it('knows nothing from an empty line and rejects noise', () => {
    expect(detect('  ')).toBeNull();
    expect(detect('привет')).toBe(BAD);
  });
  it('sees an MCP address, a repository and a script', () => {
    expect(detect('https://mcp.sentry.dev/mcp')).toMatchObject({ k: 'mcp' });
    expect(detect('https://gitlab.example.ru/frontend/web')).toMatchObject({ k: 'repo' });
    expect(detect('https://example.ru/setup.sh')).toMatchObject({ k: 'script' });
  });
});

describe('runs', () => {
  it('reads the missing command from a log', () => {
    expect(missingCommand('bash: pnpm: command not found\nexit 127')).toBe('pnpm');
    expect(missingCommand('all fine')).toBeNull();
  });
});

describe('machine state', () => {
  const m: MeMachine = {
    id: 'm',
    name: 'vm',
    online: true,
    team: { id: 't', name: 'Фронт' },
    setup: true,
    current: true,
    failed: 0,
    lastRun: null,
  };
  it('is ready, stale, waits for a token', () => {
    expect(stateOfMachine(m, false)).toBe('ok');
    expect(stateOfMachine({ ...m, current: false }, false)).toBe('stale');
    expect(stateOfMachine({ ...m, failed: 1 }, false)).toBe('token');
  });
  it('is not stale without the setup feature', () => {
    expect(stateOfMachine({ ...m, setup: false, current: false }, false)).toBe('none');
  });
});
