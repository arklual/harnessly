import { describe, expect, it } from 'vitest';

import {
  loginActive,
  PROVIDER_INFO,
  providerStepFailed,
  readyProviders,
  rowView,
  safeLoginUrl,
} from './agent-view';
import type { AgentStatus, Job, Login, Machine } from './types';

const status = (patch: Partial<AgentStatus> = {}): AgentStatus => ({
  provider: 'claude',
  installed: true,
  version: '2.1.0',
  login: 'logged_in',
  authMode: 'subscription',
  overriddenBy: null,
  access: null,
  ...patch,
});

const job = (state: Job['state'], failedStep?: string): Job => ({
  id: 'j',
  kind: 'agent-install',
  provider: 'claude',
  state,
  steps: [
    { id: 'system', state: 'ok', detail: null },
    { id: 'provider', state: failedStep === 'provider' ? 'fail' : 'ok', detail: null },
  ],
  error: null,
  check: null,
});

describe('rowView', () => {
  it.each([
    ['gemini is soon whatever the status says', 'gemini' as const, null, null, false, 'soon'],
    ['no status: missing', 'claude' as const, null, null, false, 'missing'],
    ['not installed: missing', 'claude' as const, status({ installed: false }), null, false, 'missing'],
    ['an install just asked for: installing', 'claude' as const, null, null, true, 'installing'],
    [
      'a running job: installing',
      'claude' as const,
      status({ installed: false }),
      job('running'),
      false,
      'installing',
    ],
    [
      'a failed job and still not installed: failed',
      'claude' as const,
      status({ installed: false }),
      job('failed'),
      false,
      'failed',
    ],
    [
      'a failed job but installed meanwhile: by login',
      'claude' as const,
      status({ login: 'logged_out' }),
      job('failed'),
      false,
      'needsLogin',
    ],
    [
      'logged out: needs login',
      'claude' as const,
      status({ login: 'logged_out' }),
      null,
      false,
      'needsLogin',
    ],
    ['logged in: ready', 'codex' as const, status({ provider: 'codex' }), null, false, 'ready'],
  ])('%s', (_name, provider, st, j, starting, kind) => {
    expect(rowView(provider, st, j, starting).kind).toBe(kind);
  });

  it('marks an unreadable login', () => {
    const v = rowView('claude', status({ login: 'unknown' }), null, false);
    expect(v.kind).toBe('needsLogin');
    expect(v.unknown).toBe(true);
  });
});

describe('logins and jobs', () => {
  const login = (state: Login['state']): Login => ({
    id: 'l',
    provider: 'claude',
    method: 'link',
    state,
    url: null,
    userCode: null,
    needsCode: false,
    consoleUrl: null,
    expiresAt: null,
    error: null,
    status: null,
  });

  it.each([
    ['starting', true],
    ['url', true],
    ['waiting', true],
    ['verifying', true],
    ['done', false],
    ['failed', false],
    ['expired', false],
    ['cancelled', false],
  ] as const)('login %s is active: %s', (state, active) => {
    expect(loginActive(login(state))).toBe(active);
  });

  it('is not active without a login', () => {
    expect(loginActive(null)).toBe(false);
  });

  it('knows when the provider step is what failed', () => {
    expect(providerStepFailed(job('failed', 'provider'))).toBe(true);
    expect(providerStepFailed(job('failed'))).toBe(false);
    expect(providerStepFailed(job('running', 'provider'))).toBe(false);
    expect(providerStepFailed(null)).toBe(false);
  });

  it('lists the providers that can start a chat', () => {
    const m = {
      agents: [
        status(),
        status({ provider: 'codex', login: 'logged_out' }),
        { ...status({ provider: 'gemini' }), soon: true as const, installed: false },
      ],
    } as Machine;
    expect(readyProviders(m)).toEqual(['claude']);
  });
});

describe('safeLoginUrl', () => {
  it.each([
    ['claude', 'https://claude.ai/oauth/authorize?code=true&client_id=x'],
    ['claude', 'https://console.anthropic.com/oauth/authorize?x=1'],
    ['codex', 'https://auth.openai.com/codex/device'],
    ['gemini', 'https://accounts.google.com/o/oauth2/v2/auth?x=1'],
  ] as const)('lets a %s sign-in page through: %s', (provider, url) => {
    expect(safeLoginUrl(provider, url)).toBe(new URL(url).href);
  });

  it.each([
    ['nothing', null],
    ['an empty string', ''],
    ['not a URL', 'claude.ai/oauth'],
    ['a relative path', '/oauth/authorize'],
    ['http', 'http://claude.ai/oauth/authorize'],
    ['a javascript address', 'javascript:alert(1)'],
    ['a data address', 'data:text/html,<script>1</script>'],
    ['another host', 'https://evil.example.net/oauth/authorize'],
    ['a look-alike suffix', 'https://claude.ai.evil.example.net/oauth'],
    ['a look-alike prefix', 'https://evilclaude.ai/oauth'],
    ['a subdomain', 'https://x.claude.ai/oauth'],
    ['userinfo hiding the host', 'https://claude.ai@evil.example.net/oauth'],
    ['credentials on the right host', 'https://user:pw@claude.ai/oauth'],
    ['a custom port', 'https://claude.ai:8443/oauth'],
    ["another provider's host", 'https://auth.openai.com/codex/device'],
  ])('refuses %s for claude', (_name, url) => {
    expect(safeLoginUrl('claude', url)).toBeNull();
  });

  it('keeps a host list for every provider, all plain lowercase hosts', () => {
    for (const info of Object.values(PROVIDER_INFO)) {
      expect(info.loginHosts.length).toBeGreaterThan(0);
      for (const h of info.loginHosts) expect(h).toMatch(/^[a-z0-9.-]+$/);
    }
  });
});
