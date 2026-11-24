import { describe, expect, it } from 'vitest';

import { detect, BAD } from './detect';
import { blankDraft, fromItem, hasBrew, need, secretVar, toSource, valid } from './model';
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
    d.f.independent = true;
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
    expect(back?.f.independent).toBe(true);
    expect(back?.f.body).toBe('corepack enable pnpm');
  });

  it('does not accept an empty script', () => {
    expect(valid(blankDraft('script'))).toBe(false);
  });

  const itemOf = (
    kind: 'app' | 'plugin' | 'claude-settings' | 'provider',
    source: ReturnType<typeof toSource>,
  ) =>
    fromItem({
      id: 'i',
      kind,
      name: 'X',
      hint: '',
      source,
      builtIn: false,
      archived: false,
      added: false,
      createdAt: '2026-11-01T00:00:00Z',
      owner: null,
    });

  it('takes an app from the folder of a repository, never outside it', () => {
    const d = blankDraft('app', [{ id: 'r1', name: 'kit' }]);
    expect(valid(d)).toBe(false);
    d.f.path = '../elsewhere';
    expect(valid(d)).toBe(false);
    d.f.path = 'apps/design-kit';
    expect(valid(d)).toBe(true);
    expect(toSource(d)).toEqual({ from: 'repo', path: 'apps/design-kit', branch: undefined, repo: 'r1' });
    d.f.repoOther = true;
    d.f.repoUrl = 'gitlab.example.ru/design/kit';
    d.f.branch = 'main';
    const source = toSource(d);
    expect(source).toEqual({
      from: 'repo',
      path: 'apps/design-kit',
      branch: 'main',
      repoUrl: 'https://gitlab.example.ru/design/kit',
    });
    const back = itemOf('app', source);
    expect(back?.f.repoOther).toBe(true);
    expect(back?.f.path).toBe('apps/design-kit');
  });

  it("keeps the organization's off switch of an app, and leaves it out of the source while on", () => {
    const d = blankDraft('app', [{ id: 'r1', name: 'kit' }]);
    d.f.path = 'boards';
    expect(toSource(d).off).toBeUndefined();
    d.f.appOff = true;
    const source = toSource(d);
    expect(source.off).toBe(true);
    expect(itemOf('app', source)?.f.appOff).toBe(true);
    expect(itemOf('app', { from: 'repo', path: 'boards', repo: 'r1' })?.f.appOff).toBe(false);
  });

  it('wants a plugin as name@marketplace with the marketplace', () => {
    const d = blankDraft('plugin');
    d.f.plugin = 'review-kit';
    d.f.marketplace = 'gitlab.example.ru/ai/plugins';
    expect(valid(d)).toBe(false);
    d.f.plugin = 'review-kit@team';
    expect(valid(d)).toBe(true);
    expect(itemOf('plugin', toSource(d))?.f.plugin).toBe('review-kit@team');
  });

  it('takes a model provider with its secrets named, never typed', () => {
    const d = blankDraft('provider');
    expect(valid(d)).toBe(false);
    d.f.url = 'llm.corp.example/anthropic';
    d.f.models = 'corp-large\n\ncorp-small';
    d.f.secret = 'vault:shared/llm/gateway#key';
    d.f.env = 'ANTHROPIC_MODEL=corp-large';
    d.f.secretEnv = 'ANTHROPIC_AUTH_TOKEN=gateway-token';
    expect(valid(d)).toBe(true);
    const source = toSource(d);
    expect(source).toEqual({
      api: 'anthropic',
      baseUrl: 'https://llm.corp.example/anthropic',
      models: ['corp-large', 'corp-small'],
      key: 'vault:shared/llm/gateway#key',
      env: ['ANTHROPIC_MODEL=corp-large'],
      secretEnv: ['ANTHROPIC_AUTH_TOKEN=gateway-token'],
    });
    expect(itemOf('provider', source)?.f).toMatchObject({
      api: 'anthropic',
      models: 'corp-large\ncorp-small',
      secret: 'vault:shared/llm/gateway#key',
      secretEnv: 'ANTHROPIC_AUTH_TOKEN=gateway-token',
    });

    // A token is not typed into the environment: the form says where it goes.
    d.f.env = 'ANTHROPIC_AUTH_TOKEN=sk-live-123';
    expect(valid(d)).toBe(false);
    expect(need(d)).toContain('ANTHROPIC_AUTH_TOKEN — секрет');
    d.f.env = '';
    d.f.secretEnv = 'ANTHROPIC_AUTH_TOKEN=sk live 123';
    expect(valid(d)).toBe(false);
    d.f.secretEnv = '';
    d.f.secret = 'sk live 123';
    expect(valid(d)).toBe(false);
  });

  it('reads Claude settings as JSON of always, ifAbsent and claudeMd', () => {
    const d = blankDraft('claude-settings');
    d.f.json = '{"other": 1}';
    expect(valid(d)).toBe(false);
    d.f.json = '{"always": {}}';
    expect(valid(d)).toBe(false);
    d.f.json = '{"ifAbsent": {"model": "sonnet"}, "claudeMd": "Отвечай по-русски."}';
    expect(valid(d)).toBe(true);
    const source = toSource(d);
    expect(source).toEqual({
      always: undefined,
      ifAbsent: { model: 'sonnet' },
      claudeMd: 'Отвечай по-русски.',
    });
    expect(JSON.parse(itemOf('claude-settings', source)?.f.json ?? '')).toEqual({
      ifAbsent: { model: 'sonnet' },
      claudeMd: 'Отвечай по-русски.',
    });
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
  it('offers an update again after a setup item fell, not only a check', () => {
    expect(stateOfMachine({ ...m, failed: 2, errors: 1 }, false)).toBe('failed');
    expect(stateOfMachine({ ...m, current: false, failed: 1, errors: 1 }, false)).toBe('stale');
    expect(stateOfMachine({ ...m, failed: 1, errors: 0 }, false)).toBe('token');
  });
  it('is not stale without the setup feature', () => {
    expect(stateOfMachine({ ...m, setup: false, current: false }, false)).toBe('none');
  });
});
