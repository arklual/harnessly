import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { forgetHome, HOME_AGAIN_MS, homeHas, primeHome, viaHome } from '@/next/features/designly/home';
import { readLibrary } from '@/next/features/designly/api';

const answer = (status: number, body: unknown) =>
  Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  );

describe('the home in one answer (HRN-1410)', () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    forgetHome();
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('gives each part from the one answer, once, and asks its own route the second time', async () => {
    fetchMock.mockReturnValueOnce(answer(200, { parts: { kinds: { kinds: [{ id: 'k' }] } } }));
    primeHome('m1');
    const own = vi.fn(() => Promise.resolve({ kinds: [] as { id: string }[] }));
    const read = (v: unknown) => v as { kinds: { id: string }[] };

    expect(await viaHome('m1', 'kinds', read, own)).toEqual({ kinds: [{ id: 'k' }] });
    expect(own).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toContain('/design/home');

    await viaHome('m1', 'kinds', read, own);
    expect(own).toHaveBeenCalledTimes(1);
  });

  it('falls back to the own route for a part the answer lacks or its reader refuses', async () => {
    fetchMock.mockReturnValueOnce(answer(200, { parts: { systems: 'not a page' } }));
    primeHome('m1');
    const own = vi.fn(() => Promise.resolve('own'));
    const strict = (v: unknown) => {
      if (typeof v !== 'object') throw new Error('not mine');
      return 'home';
    };
    expect(await viaHome('m1', 'systems', strict, own)).toBe('own');
    expect(await viaHome('m1', 'layout', strict, own)).toBe('own');
    expect(own).toHaveBeenCalledTimes(2);
  });

  it('stops asking a hub that said 404 (the switch is off) and reads at most every HOME_AGAIN_MS', async () => {
    fetchMock.mockReturnValueOnce(answer(404, { code: 'home_off' }));
    const t0 = Date.now();
    primeHome('m1', t0);
    expect(
      await viaHome(
        'm1',
        'library',
        (v) => v,
        () => Promise.resolve('own'),
      ),
    ).toBe('own');
    primeHome('m1', t0 + HOME_AGAIN_MS + 1);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    fetchMock.mockReturnValue(answer(200, { parts: {} }));
    primeHome('m2', t0);
    primeHome('m2', t0 + 1000);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    primeHome('m2', t0 + HOME_AGAIN_MS + 1);
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('asks its own route when the home was not primed', async () => {
    const own = vi.fn(() => Promise.resolve(1));
    expect(await viaHome('m3', 'kits', (v) => v, own)).toBe(1);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows the hub's copy at once when the hub answered the home from its store (HRN-1418)", async () => {
    fetchMock.mockReturnValueOnce(answer(200, { from: 'store', parts: { storeLibrary: { artifacts: [] } } }));
    primeHome('m1');
    expect(await homeHas('m1', 'storeLibrary')).toBe(true);
    expect(await homeHas('m1', 'library')).toBe(false);

    const shown: string[] = [];
    let machineDone: (v: { designs: [] }) => void = () => {};
    const machine = new Promise<{ designs: [] }>((resolve) => (machineDone = resolve));
    const read = {
      machine: () => machine.then((l) => ({ port: 1, root: '/', designs: l.designs })),
      store: () => Promise.resolve({ designs: [], layout: { folders: [], placed: {}, systems: {} } }),
    };
    // A wait far longer than the test: only the home's answer can show the copy before the machine.
    await readLibrary('m1', read, (view) => shown.push(view.from), 60_000);
    expect(shown).toEqual(['store']);
    machineDone({ designs: [] });
    await machine;
    await Promise.resolve();
    await Promise.resolve();
    expect(shown).toEqual(['store', 'machine']);
  });
});
