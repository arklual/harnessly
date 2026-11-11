import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { bootNext, bootStore, shutdownNext } from '@/next/boot';
import type { FleetSource } from '@/next/core/fleet-watch';
import { setFetch } from '@/next/core/net/http';
import { memoryKV } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, fakeEnv, settle, useFakeTime } from '@/next/core/testing/setup';
import { connectionStore } from '@/next/stores/connection';
import {
  activeMachineStore,
  machineOrderStore,
  machinesStore,
  type FleetMachine,
} from '@/next/stores/machines';
import type { MachineId } from '@/next/protocol/v2';

beforeEach(useFakeTime);
afterEach(() => {
  void shutdownNext();
  hub.stop();
  vi.useRealTimers();
});

class FakeStream implements FleetSource {
  readyState = 1;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  private fns: (() => void)[] = [];
  addEventListener(type: string, fn: () => void): void {
    if (type === 'fleet') this.fns.push(fn);
  }
  close(): void {
    this.closed = true;
  }
  fleet(): void {
    for (const fn of this.fns) fn();
  }
}

const json = (v: unknown) =>
  new Response(JSON.stringify(v), { status: 200, headers: { 'Content-Type': 'application/json' } });

/** Boots against a hub that lists `fleet()`: every other request is the fake runtime's. */
async function bootHub(rt: FakeRuntime, fleet: () => FleetMachine[]) {
  await bootFake({ rt, start: false });
  let asked = 0;
  setFetch(async (url, init) => {
    if (new URL(url, 'http://fake').pathname.endsWith('/api/v2/machines')) {
      asked += 1;
      return json(fleet());
    }
    return rt.fetch(url, init);
  });
  const streams: FakeStream[] = [];
  const booting = bootNext({
    kv: () => memoryKV(),
    lastPerson: null,
    hub: { socket: rt.socket, env: fakeEnv(), random: () => 0.5, probe: async () => 'network' },
    fleetSource: () => {
      const s = new FakeStream();
      streams.push(s);
      return s;
    },
  });
  await vi.advanceTimersByTimeAsync(400);
  await booting;
  await settle();
  return { streams, asked: () => asked };
}

const socketsOf = (rt: FakeRuntime, m: string) => rt.opened.filter((s) => s.url.includes(`machine=${m}`));

describe('the fleet of an open page', () => {
  it('a runtime on its own is one machine for good: no stream is asked for', async () => {
    const rt = new FakeRuntime();
    const { streams } = await bootHub(rt, () => [rt.machine]);
    expect(bootStore.get()).toEqual({ k: 'ready', machines: [rt.machineId] });
    expect(streams).toHaveLength(0);
  });

  it('a machine added to the hub gets its socket, a renamed one its name, without a reload', async () => {
    const rt = new FakeRuntime();
    const m1 = rt.machineId as MachineId;
    const m2 = 'second' as MachineId;
    let fleet: FleetMachine[] = [{ ...rt.machine, mine: true }];
    const { streams, asked } = await bootHub(rt, () => fleet);
    expect(streams).toHaveLength(1);
    expect(connectionStore.get(m1)?.k).toBe('live');
    const first = hub.client(m1);
    const epoch = machinesStore.get(m1)?.epoch;
    expect(epoch).not.toBeNull();
    const before = asked();

    fleet = [
      { ...rt.machine, name: 'Сборочная', mine: true },
      { ...rt.machine, id: m2, name: 'Вторая', reach: 'unreachable', mine: true },
    ];
    streams[0]!.fleet();
    await settle();

    expect(asked()).toBe(before + 1);
    expect(bootStore.get()).toEqual({ k: 'ready', machines: [m1, m2] });
    expect(machineOrderStore.get()).toEqual([m1, m2]);
    expect(machinesStore.get(m1)?.machine.name).toBe('Сборочная');
    expect(machinesStore.get(m2)).toBeDefined();
    expect(hub.client(m2)).toBeDefined();
    expect(socketsOf(rt, m2).length).toBeGreaterThan(0);
    // The machine the page already had keeps its socket and what that socket said.
    expect(hub.client(m1)).toBe(first);
    expect(socketsOf(rt, m1)).toHaveLength(1);
    expect(machinesStore.get(m1)?.epoch).toBe(epoch);
    expect(machinesStore.get(m1)?.runtime).toBe('v2');
    expect(activeMachineStore.get()).toBe(m1);
  });

  it('a machine that left the fleet has its socket closed and leaves the list', async () => {
    const rt = new FakeRuntime();
    const m1 = rt.machineId as MachineId;
    const m2 = 'second' as MachineId;
    const both: FleetMachine[] = [
      { ...rt.machine, mine: true },
      { ...rt.machine, id: m2, name: 'Вторая', reach: 'unreachable', mine: true },
    ];
    let fleet = both;
    const { streams } = await bootHub(rt, () => fleet);
    expect(bootStore.get()).toEqual({ k: 'ready', machines: [m1, m2] });
    hub.setActive(m2);

    fleet = [both[0]!];
    streams[0]!.fleet();
    await settle();

    expect(bootStore.get()).toEqual({ k: 'ready', machines: [m1] });
    expect(machineOrderStore.get()).toEqual([m1]);
    expect(machinesStore.get(m2)).toBeUndefined();
    expect(hub.client(m2)).toBeUndefined();
    // The page was on the machine that is gone: it moves to one that is there.
    expect(activeMachineStore.get()).toBe(m1);
    expect(hub.client(m1)).toBeDefined();
  });

  it('a reading that fails leaves the list the page has', async () => {
    const rt = new FakeRuntime();
    const m1 = rt.machineId as MachineId;
    let broken = false;
    const { streams } = await bootHub(rt, () => {
      if (broken) throw new TypeError('Failed to fetch');
      return [{ ...rt.machine, mine: true }];
    });
    broken = true;
    streams[0]!.fleet();
    await settle();
    expect(bootStore.get()).toEqual({ k: 'ready', machines: [m1] });
    expect(hub.client(m1)).toBeDefined();
  });

  it('letting the person go closes the stream', async () => {
    const rt = new FakeRuntime();
    const { streams } = await bootHub(rt, () => [{ ...rt.machine, mine: true }]);
    await shutdownNext();
    expect(streams[0]!.closed).toBe(true);
  });
});
