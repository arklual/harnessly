import { afterEach, describe, expect, it } from 'vitest';
import {
  answersNow,
  LINK,
  startCandidateOf,
  startMachineOf,
  type IStartCandidate,
} from '@/next/bindings/start-machine';
import { storeSink } from '@/next/core/realtime/apply';
import { DEFAULT_LIMITS, machinesStore, putMachines, type FleetMachine } from '@/next/stores/machines';
import type { MachineAgent, Provider } from '@/next/protocol/v2';

const agent = (provider: Provider): MachineAgent => ({
  provider,
  version: '1.0.0',
  status: 'ready',
  models: [{ id: `${provider}-model`, label: provider, hint: '', efforts: [], defaultEffort: null }],
  canSwitchModel: true,
});

const fleet = (id: string, over: Partial<FleetMachine> = {}): FleetMachine => ({
  id,
  name: id,
  host: id,
  reach: 'online',
  lastSeenAt: null,
  runtime: null,
  agents: [agent('claude')],
  ...over,
});

const cand = (id: string, over: Partial<IStartCandidate> = {}): IStartCandidate => ({
  id,
  off: false,
  answers: true,
  providers: ['claude'],
  mine: true,
  ...over,
});

afterEach(() => {
  for (const id of [...machinesStore.keys()]) machinesStore.delete(id);
});

// runner-2 on top hanged (heartbeat on, runtime silent) and the start page drew no model pill: a new chat
// went to the machine on top whatever it did. A new chat goes to a machine of the person's that answers now.
describe('the machine a new chat starts on', () => {
  it('passes over the machine on top while it hangs, for one of the person’s that answers', () => {
    const top = cand('runner-2', { answers: false });
    expect(startMachineOf([top, cand('dev')], null, 'claude')).toBe('dev');
  });

  it('keeps the machine on top while it answers', () => {
    expect(startMachineOf([cand('runner-2'), cand('dev')], null, 'claude')).toBe('runner-2');
  });

  it('takes a machine that has the agent of the last choice over one that has not', () => {
    const codexOnly = cand('a', { providers: ['codex'] });
    expect(startMachineOf([codexOnly, cand('b')], null, 'claude')).toBe('b');
    expect(startMachineOf([codexOnly], null, 'claude')).toBe('a');
  });

  it('keeps a machine picked in the pill, even one that does not answer, but not one that is off', () => {
    expect(startMachineOf([cand('top'), cand('picked', { answers: false })], 'picked', 'claude')).toBe(
      'picked',
    );
    expect(
      startMachineOf([cand('top'), cand('picked', { off: true, answers: false })], 'picked', 'claude'),
    ).toBe('top');
  });

  it('does not start on a machine the person only administers unless it is on top', () => {
    const top = cand('top', { answers: false });
    expect(startMachineOf([top, cand('admin', { mine: false }), cand('own')], null, 'claude')).toBe('own');
    expect(startMachineOf([cand('admin', { mine: false }), cand('own')], null, 'claude')).toBe('admin');
  });

  it('with no machine answering, stays on one whose agents are known, so the pill is drawn', () => {
    const none = cand('none', { answers: false, providers: [] });
    const known = cand('known', { answers: false, off: true });
    expect(startMachineOf([none, known], null, 'claude')).toBe('known');
    expect(startMachineOf([none], null, 'claude')).toBe('none');
    expect(startMachineOf([], null, 'claude')).toBeNull();
  });
});

describe('a machine that answers now', () => {
  it('is one whose socket is live, or, before it is, one the hub lists on and not hanging', () => {
    putMachines([fleet('live'), fleet('hangs', { hangs: true }), fleet('away', { reach: 'unreachable' })]);
    const view = (id: string) => machinesStore.get(id)!;
    expect(answersNow(view('live'), LINK.live)).toBe(true);
    expect(answersNow(view('live'), LINK.unknown)).toBe(true);
    expect(answersNow(view('live'), LINK.down)).toBe(false);
    expect(answersNow(view('hangs'), LINK.unknown)).toBe(false);
    expect(answersNow(view('hangs'), LINK.live)).toBe(true);
    expect(answersNow(view('away'), LINK.unknown)).toBe(false);
  });

  it('lists the ready agents the hub remembers, and the machine’s own welcome clears the hub’s «hangs»', () => {
    putMachines([
      fleet('m', { hangs: true, agents: [agent('codex'), { ...agent('claude'), status: 'missing' }] }),
    ]);
    const c = startCandidateOf(machinesStore.get('m')!, LINK.unknown, false);
    expect(c).toEqual({ id: 'm', off: false, answers: false, providers: ['codex'], mine: true });
    storeSink.welcome('m', {
      f: 'welcome',
      proto: 2,
      epoch: 'e1',
      serverTime: 1,
      machine: fleet('m', { agents: [agent('claude')] }),
      features: [],
      limits: DEFAULT_LIMITS,
    });
    expect(machinesStore.get('m')?.hangs).toBeUndefined();
    expect(machinesStore.get('m')?.machine.agents.map((a) => a.provider)).toEqual(['claude']);
  });

  it('keeps the hub’s list fields out of the machine record, and its features on the view', () => {
    putMachines([fleet('m', { hangs: true, features: ['settings'], usage: { claude: null } })]);
    const v = machinesStore.get('m')!;
    expect(v.features).toEqual(['settings']);
    expect(v.hangs).toBe(true);
    expect(v.machine).not.toHaveProperty('hangs');
    expect(v.machine).not.toHaveProperty('usage');
    expect(v.machine).not.toHaveProperty('features');
  });
});
