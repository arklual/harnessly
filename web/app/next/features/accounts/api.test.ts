import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  cancelLogin,
  loadLogin,
  logoutAgent,
  putAgentKey,
  sendLoginCode,
  startLogin,
} from '@/next/features/accounts/api';
import { setApiOrigin } from '@/next/core/net/machine';

// The answers are the wire fixtures the runtime's own tests check (internal/wire/testdata).
const fixture = (name: string): unknown =>
  (JSON.parse(readFileSync(`../internal/wire/testdata/http.${name}.json`, 'utf8')) as { response: unknown })
    .response;

const calls: { url: string; method: string; body: unknown }[] = [];
const answer = (name: string) =>
  vi.fn((url: string, init: RequestInit) => {
    calls.push({ url, method: init.method ?? 'GET', body: init.body ? JSON.parse(String(init.body)) : null });
    return Promise.resolve(
      new Response(JSON.stringify(fixture(name)), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );
  });

beforeEach(() => {
  calls.length = 0;
  setApiOrigin({ base: '', host: 'runtime.test' });
});
afterEach(() => vi.unstubAllGlobals());

const path = (i: number) => new URL(calls[i].url, 'http://runtime.test').pathname;

describe('the agent sign-in routes', () => {
  it('starts a Claude login in the subscription or the console mode', async () => {
    vi.stubGlobal('fetch', answer('login-awaiting-claude'));
    const res = await startLogin('local', 'claude', 'console');
    expect(path(0)).toBe('/api/v2/agents/claude/login');
    expect(calls[0].method).toBe('POST');
    expect(calls[0].body).toEqual({ mode: 'console' });
    expect('ok' in res && res.value.flow?.input).toBe('code');
  });

  it('starts a Codex login only in the device mode', async () => {
    vi.stubGlobal('fetch', answer('login-awaiting-codex'));
    const res = await startLogin('local', 'codex', 'subscription');
    expect(calls[0].body).toEqual({ mode: 'device' });
    expect('ok' in res && res.value.flow?.userCode).toBe('ABCD-EFGH1');
  });

  it('reads the login that was not started as no flow', async () => {
    vi.stubGlobal('fetch', answer('login-none'));
    const res = await loadLogin('local', 'codex');
    expect(path(0)).toBe('/api/v2/agents/codex/login');
    expect(calls[0].method).toBe('GET');
    expect('ok' in res && res.value.flow).toBeNull();
  });

  it('sends the pasted code with the flow id', async () => {
    vi.stubGlobal('fetch', answer('login-awaiting-claude'));
    await sendLoginCode('local', 'claude', 'f1', 'abc-123');
    expect(path(0)).toBe('/api/v2/agents/claude/login/code');
    expect(calls[0].method).toBe('POST');
    expect(calls[0].body).toEqual({ id: 'f1', code: 'abc-123' });
  });

  it('reads a failed login with its reason', async () => {
    vi.stubGlobal('fetch', answer('login-failed'));
    const res = await loadLogin('local', 'claude');
    expect('ok' in res && res.value.flow?.state).toBe('failed');
    expect('ok' in res && res.value.flow?.reason).not.toBeNull();
  });

  it('cancels the login', async () => {
    vi.stubGlobal('fetch', answer('login-awaiting-claude'));
    await cancelLogin('local', 'claude');
    expect(path(0)).toBe('/api/v2/agents/claude/login');
    expect(calls[0].method).toBe('DELETE');
  });

  it('puts the Codex key and gets only ok back', async () => {
    vi.stubGlobal('fetch', answer('loginOK'));
    const fakeKey = 'sk-' + 'test-1234567890'; // joined: no key-shaped literal for gitleaks
    const res = await putAgentKey('local', 'codex', fakeKey);
    expect(path(0)).toBe('/api/v2/agents/codex/key');
    expect(calls[0].method).toBe('PUT');
    expect(calls[0].body).toEqual({ key: fakeKey });
    expect('ok' in res && res.value).toEqual({ ok: true });
  });

  it('signs out', async () => {
    vi.stubGlobal('fetch', answer('loginOK'));
    await logoutAgent('local', 'claude');
    expect(path(0)).toBe('/api/v2/agents/claude/logout');
    expect(calls[0].method).toBe('POST');
  });
});
