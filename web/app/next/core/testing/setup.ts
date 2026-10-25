// Boots the core against a FakeRuntime on fake timers: the shared start of the stage C tests.

import { vi } from 'vitest';
import { setFetch } from '@/next/core/net/http';
import type { Env } from '@/next/core/realtime/client';
import { setLinkGrace } from '@/next/core/realtime/grace';
import { hub } from '@/next/core/realtime/hub';
import { setScheduler } from '@/next/core/store';
import { resetCore } from '@/next/core/testing/reset';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { putMachines } from '@/next/stores/machines';

export interface FakeEnv extends Env {
  online: boolean;
  fire(event: 'online' | 'visible'): void;
}

export function fakeEnv(): FakeEnv {
  const fns = { online: new Set<() => void>(), visible: new Set<() => void>() };
  return {
    online: true,
    isOnline() {
      return this.online;
    },
    on(event, fn) {
      fns[event].add(fn);
      return () => fns[event].delete(fn);
    },
    fire(event) {
      for (const fn of [...fns[event]]) fn();
    },
  };
}

export function useFakeTime(): void {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
  vi.setSystemTime(new Date('2026-10-22T12:00:00Z'));
}

/** Lets the sockets open and the frames flow: microtasks and timers due now. */
export const settle = () => vi.advanceTimersByTimeAsync(0);

export async function bootFake(o: { rt?: FakeRuntime; env?: FakeEnv; start?: boolean } = {}) {
  resetCore();
  setScheduler('sync');
  setLinkGrace(0);
  const rt = o.rt ?? new FakeRuntime();
  const env = o.env ?? fakeEnv();
  setFetch(rt.fetch);
  hub.configure({ socket: rt.socket, env, random: () => 0.5, probe: async () => 'network' });
  putMachines([rt.machine]);
  if (o.start !== false) {
    hub.start([rt.machineId], rt.machineId);
    await settle();
  }
  return { rt, env, m: rt.machineId };
}
