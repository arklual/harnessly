import { describe, expect, it } from 'vitest';
import { createSlotLeases, LIBRARY_PAGE_SLOT, LIBRARY_PANE_SLOT } from '@/next/features/pane/slot-lease';

describe('slot leases', () => {
  it('gives every tab a slot of its own and the same one again', () => {
    const leases = createSlotLeases(() => 1);
    expect(leases.lease('port', 'a')).toBe(2);
    expect(leases.lease('port', 'b')).toBe(3);
    expect(leases.lease('port', 'a')).toBe(2);
    expect(leases.slotOf('b')).toBe(3);
  });

  it('keeps ports in 2, 3, 5, 6 and sites in 7–8: 1 and 4 are the design library’s', () => {
    const leases = createSlotLeases(() => 1);
    expect(leases.lease('site', 'a')).toBe(7);
    expect(leases.lease('site', 'b')).toBe(8);
    expect(['c', 'd', 'e', 'f'].map((tab) => leases.lease('port', tab))).toEqual([2, 3, 5, 6]);
  });

  it('never leases the library’s slots to a tab or an app view, pinned or not, free or the oldest', () => {
    let now = 0;
    const leases = createSlotLeases(() => ++now);
    const tabs = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'];
    const got = tabs.map((tab) => leases.lease('port', tab));
    expect(got.filter((slot) => slot === LIBRARY_PAGE_SLOT || slot === LIBRARY_PANE_SLOT)).toEqual([]);
    // The library's own page takes its slot, and lets go of it, without any tab getting it in between.
    const unpin = leases.pin(LIBRARY_PAGE_SLOT, 'design:1');
    unpin();
    expect(leases.lease('port', 'i')).not.toBe(LIBRARY_PAGE_SLOT);
    expect(leases.lease('port', 'i')).not.toBe(LIBRARY_PANE_SLOT);
  });

  it('takes the least recently used slot when all are held, and says whose it was', () => {
    let now = 0;
    const leases = createSlotLeases(() => ++now);
    const taken: string[] = [];
    leases.onTaken((tab) => taken.push(tab));
    for (const tab of ['a', 'b', 'c', 'd']) leases.lease('port', tab);
    leases.lease('port', 'a');
    expect(leases.lease('port', 'g')).toBe(3);
    expect(taken).toEqual(['b']);
    expect(leases.slotOf('b')).toBeNull();
    expect(leases.slotOf('a')).toBe(2);
  });

  it('sites are only two: the third takes the older', () => {
    let now = 0;
    const leases = createSlotLeases(() => ++now);
    leases.lease('site', 'a');
    leases.lease('site', 'b');
    expect(leases.lease('site', 'c')).toBe(7);
    expect(leases.slotOf('a')).toBeNull();
  });

  it('moves a tab to the other pool when its address changes kind, freeing the old slot', () => {
    const leases = createSlotLeases(() => 1);
    expect(leases.lease('port', 'a')).toBe(2);
    expect(leases.lease('site', 'a')).toBe(7);
    expect(leases.lease('port', 'b')).toBe(2);
  });

  it('frees the slot of a released tab and stops telling the unsubscribed', () => {
    const leases = createSlotLeases(() => 1);
    const heard: string[] = [];
    const off = leases.onTaken((tab) => heard.push(tab));
    leases.lease('site', 'a');
    leases.release('a');
    expect(leases.slotOf('a')).toBeNull();
    expect(leases.lease('site', 'b')).toBe(7);
    off();
    leases.lease('site', 'c');
    leases.lease('site', 'd');
    expect(heard).toEqual([]);
  });

  it('goes round a pinned slot: the board of a design keeps its cookie while app views lease', () => {
    let now = 0;
    const leases = createSlotLeases(() => ++now);
    const heard: string[] = [];
    leases.onTaken((tab) => heard.push(tab));
    expect(leases.lease('port', 'app:a')).toBe(2);
    leases.pin(LIBRARY_PAGE_SLOT, 'design:1');
    const unpin = leases.pin(2, 'design:2');
    // The view that held 2 was told, and asks again: it gets a slot the board does not use.
    expect(heard).toEqual(['app:a']);
    expect(leases.lease('port', 'app:a')).toBe(3);
    for (const tab of ['b', 'c', 'd', 'e']) leases.lease('port', tab);
    expect([1, 2]).not.toContain(leases.slotOf('e'));
    unpin();
    expect(leases.lease('port', 'f')).toBe(2);
  });
});
