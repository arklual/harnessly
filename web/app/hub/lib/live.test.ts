import { describe, expect, it } from 'vitest';

import { initialLive, installJobOf, loginOf, parseSpaceEvent, reduceLive } from './live';
import type { Job, Login, Machine } from './types';

const machine = (id: string, patch: Partial<Machine> = {}): Machine => ({
  id,
  spaceId: 's',
  name: id,
  hostname: id,
  os: 'linux',
  arch: 'amd64',
  unixUser: 'harnessly',
  profile: 'dir',
  via: 'command',
  status: 'online',
  connectedAt: null,
  lastSeenAt: null,
  agentVersion: '1.0.0',
  encKey: '',
  policy: null,
  preflight: null,
  agents: [],
  proxy: { all: null, claude: null, codex: null },
  env: [],
  createdAt: '2026-10-26T09:00:00Z',
  ...patch,
});

const job = (id: string, state: Job['state'], provider: Job['provider'] = 'claude'): Job => ({
  id,
  kind: 'agent-install',
  provider,
  state,
  steps: [],
  error: null,
  check: null,
});

const login = (id: string, state: Login['state']): Login => ({
  id,
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

describe('reduceLive', () => {
  it('starts with machines unknown, not empty', () => {
    expect(initialLive.machines).toBeNull();
    const s = reduceLive(initialLive, { type: 'loaded', machines: [] });
    expect(s.machines).toEqual([]);
  });

  it('adds a machine from an event, replaces it in place, and drops it when removed', () => {
    let s = reduceLive(initialLive, {
      type: 'event',
      event: { type: 'machine', data: { machine: machine('a') } },
    });
    s = reduceLive(s, { type: 'event', event: { type: 'machine', data: { machine: machine('b') } } });
    s = reduceLive(s, {
      type: 'event',
      event: { type: 'machine', data: { machine: machine('a', { status: 'offline' }) } },
    });
    expect(s.machines?.map((m) => [m.id, m.status])).toEqual([
      ['a', 'offline'],
      ['b', 'online'],
    ]);
    s = reduceLive(s, { type: 'event', event: { type: 'machineRemoved', data: { machineId: 'a' } } });
    expect(s.machines?.map((m) => m.id)).toEqual(['b']);
  });

  it('keeps the latest job and does not step a finished one back to running', () => {
    let s = reduceLive(initialLive, { type: 'job', machineId: 'a', job: job('j1', 'running') });
    s = reduceLive(s, { type: 'job', machineId: 'a', job: job('j1', 'failed') });
    s = reduceLive(s, { type: 'job', machineId: 'a', job: job('j1', 'running') });
    expect(s.jobs.j1?.job.state).toBe('failed');
    expect(installJobOf(s, 'a', 'claude')?.id).toBe('j1');
    expect(installJobOf(s, 'a', 'codex')).toBeNull();
    expect(installJobOf(s, 'b', 'claude')).toBeNull();
  });

  it('does not reopen a finished login', () => {
    let s = reduceLive(initialLive, { type: 'login', machineId: 'a', login: login('l1', 'url') });
    s = reduceLive(s, { type: 'login', machineId: 'a', login: login('l1', 'done') });
    s = reduceLive(s, { type: 'login', machineId: 'a', login: login('l1', 'waiting') });
    expect(loginOf(s, 'a', 'claude')?.state).toBe('done');
  });

  it('counts member changes', () => {
    const s = reduceLive(initialLive, { type: 'event', event: { type: 'members', data: {} } });
    expect(s.membersTick).toBe(1);
  });
});

describe('parseSpaceEvent', () => {
  it('reads known events and ignores the rest', () => {
    expect(parseSpaceEvent('machineRemoved', '{"machineId":"a"}')).toEqual({
      type: 'machineRemoved',
      data: { machineId: 'a' },
    });
    expect(parseSpaceEvent('nope', '{}')).toBeNull();
    expect(parseSpaceEvent('machine', 'not json')).toBeNull();
    expect(parseSpaceEvent('machine', 'null')).toBeNull();
  });
});
