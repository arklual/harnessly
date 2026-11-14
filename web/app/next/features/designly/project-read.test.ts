import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IDesign, IDesignLibrary, IStoreLibrary } from '@/next/features/designly/api';
import { WordedError } from '@/next/core/net/worded';
import {
  AWAY,
  awayIsFinal,
  PROJECT_FROM,
  PROJECT_READ,
  readProject,
  type TProjectRead,
  createProjectReads,
} from '@/next/features/designly/project-read';

vi.mock('@/next/core/net/runtime-json', () => ({ runtimeJson: vi.fn(), postJson: vi.fn() }));

const design = (id: string) => ({ id }) as unknown as IDesign;
const library = (...ids: string[]): IDesignLibrary => ({
  port: 4400,
  root: '/lib',
  designs: ids.map(design),
});
const copy = (...ids: string[]): IStoreLibrary => ({
  designs: ids.map(design),
  layout: { folders: [], placed: {}, systems: {} },
});
const WAITS = { store: 2500, machine: 20_000 };
const after = <T>(ms: number, value: T) => new Promise<T>((resolve) => setTimeout(() => resolve(value), ms));
const failAfter = (ms: number, message: string) =>
  new Promise<never>((_, reject) => setTimeout(() => reject(new Error(message)), ms));
const never = <T>() => new Promise<T>(() => {});

const run = (machine: () => Promise<IDesignLibrary>, store: () => Promise<IStoreLibrary>) => {
  const shown: TProjectRead[] = [];
  const storeRead = vi.fn(store);
  void readProject('p1', { machine, store: storeRead }, (state) => shown.push(state), WAITS);
  return { shown, storeRead };
};

describe('readProject (HRN-1308)', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('the store has no copy and the machine is slow but on the link: the project opens from the machine', async () => {
    const { shown } = run(
      () => after(6000, library('p1')),
      () => after(100, copy('other')),
    );
    await vi.advanceTimersByTimeAsync(5000);
    // The store's «no copy» is not the end: the machine is still waited for, and no card is shown.
    expect(shown).toEqual([]);
    await vi.advanceTimersByTimeAsync(1500);
    expect(shown).toHaveLength(1);
    expect(shown[0]).toMatchObject({ k: PROJECT_READ.open, from: PROJECT_FROM.machine });
  });

  it('the store cannot be read and the machine is on the link: the project opens from the machine', async () => {
    const { shown } = run(
      () => after(4000, library('p1')),
      () => failAfter(50, 'store 500'),
    );
    await vi.advanceTimersByTimeAsync(3500);
    expect(shown).toEqual([]);
    await vi.advanceTimersByTimeAsync(1000);
    expect(shown).toHaveLength(1);
    expect(shown[0]).toMatchObject({ k: PROJECT_READ.open, from: PROJECT_FROM.machine });
  });

  it('a machine that answers at once is not followed by a read of the store', async () => {
    const { shown, storeRead } = run(
      () => after(100, library('p1')),
      () => after(10, copy('p1')),
    );
    await vi.advanceTimersByTimeAsync(5000);
    expect(storeRead).not.toHaveBeenCalled();
    expect(shown.map((s) => s.k)).toEqual([PROJECT_READ.open]);
  });

  it('a machine without the project sends the read to the store at once', async () => {
    const { shown } = run(
      () => after(100, library('other')),
      () => after(100, copy('p1')),
    );
    await vi.advanceTimersByTimeAsync(300);
    expect(shown).toHaveLength(1);
    expect(shown[0]).toMatchObject({ k: PROJECT_READ.open, from: PROJECT_FROM.store });
  });

  it('the copy is shown while the machine is slow, and the machine replaces it when it answers', async () => {
    const { shown } = run(
      () => after(9000, library('p1')),
      () => after(100, copy('p1')),
    );
    await vi.advanceTimersByTimeAsync(3000);
    expect(shown.map((s) => (s.k === PROJECT_READ.open ? s.from : s.k))).toEqual([PROJECT_FROM.store]);
    await vi.advanceTimersByTimeAsync(7000);
    expect(shown.map((s) => (s.k === PROJECT_READ.open ? s.from : s.k))).toEqual([
      PROJECT_FROM.store,
      PROJECT_FROM.machine,
    ]);
  });

  it('no copy and a machine that is off: away, «the machine does not answer, no copy»', async () => {
    const { shown } = run(
      () => failAfter(200, 'machine offline'),
      () => after(100, copy()),
    );
    await vi.advanceTimersByTimeAsync(500);
    expect(shown).toEqual([
      { k: PROJECT_READ.away, why: AWAY.silentNoCopy, machineError: 'machine offline' },
    ]);
  });

  it('no copy and a machine that hangs: away only after the machine was waited for, and it still opens later', async () => {
    let answer: (l: IDesignLibrary) => void = () => {};
    const { shown } = run(
      () => new Promise<IDesignLibrary>((resolve) => (answer = resolve)),
      () => after(100, copy()),
    );
    await vi.advanceTimersByTimeAsync(19_000);
    expect(shown).toEqual([]);
    await vi.advanceTimersByTimeAsync(1500);
    expect(shown).toEqual([{ k: PROJECT_READ.away, why: AWAY.silentNoCopy, machineError: null }]);
    answer(library('p1'));
    await vi.advanceTimersByTimeAsync(10);
    expect(shown[1]).toMatchObject({ k: PROJECT_READ.open, from: PROJECT_FROM.machine });
  });

  it('names each reason: what the machine said and what the store said', async () => {
    const cases = [
      { machine: () => failAfter(10, 'off'), store: () => failAfter(10, 'down'), why: AWAY.silentUnreadable },
      { machine: () => after(10, library()), store: () => after(10, copy()), why: AWAY.absentNoCopy },
      { machine: () => after(10, library()), store: () => failAfter(10, 'down'), why: AWAY.absentUnreadable },
      {
        machine: () => never<IDesignLibrary>(),
        store: () => failAfter(10, 'down'),
        why: AWAY.silentUnreadable,
      },
    ];
    for (const c of cases) {
      const { shown } = run(c.machine, c.store);
      await vi.advanceTimersByTimeAsync(21_000);
      expect(shown.at(-1)).toMatchObject({ k: PROJECT_READ.away, why: c.why });
    }
  });

  // GitHub #82: a hub with no store and a machine closed to the person are not «on its way».
  it('names a hub without a store and a machine that refuses the person', async () => {
    const storeOff = () =>
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(Object.assign(new Error('off'), { body: { code: 'store_off' } })), 10),
      );
    const denied = () =>
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new WordedError('Forbidden', 'http', null, 403)), 10),
      );
    const cases = [
      { machine: () => after(10, library()), store: storeOff, why: AWAY.absentStoreOff },
      { machine: () => failAfter(10, 'off'), store: storeOff, why: AWAY.silentStoreOff },
      { machine: denied, store: () => after(10, copy()), why: AWAY.machineDenied },
      { machine: denied, store: storeOff, why: AWAY.machineDenied },
    ];
    for (const c of cases) {
      const { shown } = run(c.machine, c.store);
      await vi.advanceTimersByTimeAsync(WAITS.machine + 100);
      expect(shown.map((s) => (s.k === PROJECT_READ.away ? s.why : s.k))).toEqual([c.why]);
    }
    expect(awayIsFinal(AWAY.absentStoreOff)).toBe(true);
    expect(awayIsFinal(AWAY.machineDenied)).toBe(true);
    expect(awayIsFinal(AWAY.silentStoreOff)).toBe(false);
    expect(awayIsFinal(AWAY.absentNoCopy)).toBe(false);
  });

  it('a machine that refuses the person still opens the project from a store that has it', async () => {
    const denied = () => Promise.reject(new WordedError('Forbidden', 'http', null, 403));
    const { shown } = run(denied, () => after(10, copy('p1')));
    await vi.advanceTimersByTimeAsync(100);
    expect(shown).toEqual([{ k: PROJECT_READ.open, from: PROJECT_FROM.store, design: design('p1') }]);
  });
});

describe('createProjectReads (HRN-1405)', () => {
  const later = () => {
    let done!: (v: string) => void;
    const p = new Promise<string>((resolve) => {
      done = resolve;
    });
    return { p, done };
  };

  it('a poll while the machine still answers leaves that read be and starts none', async () => {
    const reads = createProjectReads();
    const first = reads.start('m|harnessly|0|0');
    expect(first).not.toBeNull();
    const slow = later();
    const answer = reads.machine(() => slow.p);
    expect(reads.start('m|harnessly|0|0')).toBeNull();
    expect(first?.aborted).toBe(false);
    slow.done('library');
    await expect(answer).resolves.toBe('library');
    const next = reads.start('m|harnessly|0|0');
    expect(next).not.toBeNull();
    expect(first?.aborted).toBe(false);
  });

  it('a poll after the machine answered reads again without aborting the read before', () => {
    const reads = createProjectReads();
    const first = reads.start('k');
    const second = reads.start('k');
    expect(second).not.toBeNull();
    expect(first?.aborted).toBe(false);
  });

  it('another project or attempt aborts the reads on their way', () => {
    const reads = createProjectReads();
    const first = reads.start('m|a|0|0');
    void reads.machine(() => new Promise(() => undefined));
    const other = reads.start('m|b|0|0');
    expect(first?.aborted).toBe(true);
    expect(other).not.toBeNull();
  });

  it('stop aborts everything, and the same project starts again after it (a remount)', () => {
    const reads = createProjectReads();
    const first = reads.start('k');
    void reads.machine(() => new Promise(() => undefined));
    reads.stop();
    expect(first?.aborted).toBe(true);
    expect(reads.start('k')).not.toBeNull();
  });
});
