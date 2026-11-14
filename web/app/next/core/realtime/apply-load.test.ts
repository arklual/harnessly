import { describe, expect, it } from 'vitest';
import { applyMachineEvent } from '@/next/core/realtime/apply';
import { flush } from '@/next/core/store';
import { machineLoadStore, machinesStore, splitLoad } from '@/next/stores/machines';
import type { Machine, MachineId, MachineLoad } from '@/next/protocol/v2';

const M = 'local' as MachineId;
const LOAD: MachineLoad = {
  at: 1,
  cpu: 0.4,
  memUsed: 1,
  memTotal: 4,
  diskFree: 9,
  diskTotal: 10,
  busyAgents: 0,
};

describe('the load of a machine (HRN-1449)', () => {
  it('a reading of the load leaves the record as it was', () => {
    const view = {
      machine: { id: M } as Machine,
      runtime: 'v2' as const,
      epoch: 'e',
      features: [],
      limits: null,
    };
    machinesStore.set(M, view);
    flush();
    applyMachineEvent(M, { type: 'machine_load', load: LOAD });
    flush();
    expect(machinesStore.get(M)).toBe(view);
    expect(machineLoadStore.get(M)).toBe(LOAD);
  });

  it('a record that brings its load gives it to the load store', () => {
    const record = splitLoad(M, { id: M, load: { ...LOAD, cpu: 0.9 } } as Machine);
    flush();
    expect(record).toEqual({ id: M });
    expect(machineLoadStore.get(M)?.cpu).toBe(0.9);
  });
});
