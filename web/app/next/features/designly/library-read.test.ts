// the home's library is the machine's when it answers in time, else the hub's copy until it does.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  LIBRARY_FROM,
  readLibrary,
  type IDesign,
  type IDesignLibrary,
  type ILibraryView,
  type IStoreLibrary,
} from '@/next/features/designly/api';
import type { MachineId } from '@/next/protocol/v2';

const design = (id: string): IDesign =>
  ({
    id,
    title: id,
    boards: [],
    convs: [],
    kind: 'site',
    pinned: false,
    cover: null,
    projectPath: '',
    updatedAt: 1,
  }) as IDesign;
const machineLib: IDesignLibrary = { port: 1, root: '/lib', designs: [design('live')] };
const storeLib: IStoreLibrary = {
  designs: [design('copy')],
  layout: { folders: [], placed: {}, systems: {} },
};

let n = 0;
const m = () => `m${++n}` as MachineId;

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('readLibrary', () => {
  it('shows the machine when it answers in time and never asks the hub', async () => {
    const store = vi.fn(async () => storeLib);
    const shown: ILibraryView[] = [];
    await readLibrary(m(), { machine: async () => machineLib, store }, (v) => shown.push(v));
    expect(shown.map((v) => v.from)).toEqual([LIBRARY_FROM.machine]);
    expect(store).not.toHaveBeenCalled();
  });

  it('shows the hub copy at once when the machine answers 504', async () => {
    const shown: ILibraryView[] = [];
    await readLibrary(
      m(),
      { machine: () => Promise.reject(new Error('504')), store: async () => storeLib },
      (v) => shown.push(v),
    );
    expect(shown).toHaveLength(1);
    expect(shown[0].from).toBe(LIBRARY_FROM.store);
    expect(shown[0].designs[0].id).toBe('copy');
  });

  it('shows the hub copy after the wait, then the machine when it comes', async () => {
    let answer: (l: IDesignLibrary) => void = () => undefined;
    const shown: ILibraryView[] = [];
    const done = readLibrary(
      m(),
      { machine: () => new Promise<IDesignLibrary>((ok) => (answer = ok)), store: async () => storeLib },
      (v) => shown.push(v),
    );
    await vi.advanceTimersByTimeAsync(1500);
    await done;
    expect(shown.map((v) => v.from)).toEqual([LIBRARY_FROM.store]);
    answer(machineLib);
    await vi.advanceTimersByTimeAsync(0);
    expect(shown.map((v) => v.from)).toEqual([LIBRARY_FROM.store, LIBRARY_FROM.machine]);
  });

  it('fails only when the hub fails too', async () => {
    const shown: ILibraryView[] = [];
    await expect(
      readLibrary(
        m(),
        { machine: () => Promise.reject(new Error('504')), store: () => Promise.reject(new Error('404')) },
        (v) => shown.push(v),
      ),
    ).rejects.toThrow('504');
    expect(shown).toHaveLength(0);
  });
});
