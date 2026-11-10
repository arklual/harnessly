// Which preview slot a browser tab uses. A slot is one cookie, so two tabs on one slot take each other's page over.
// Every tab holds a slot of its own (ports 2, 3, 5, 6, sites 7–8; 1 and 4 are the design library's); when all are held, the one used least recently is taken
// from its tab, which asks again when it is looked at. A page that keeps a fixed slot (a design's board) pins it: the
// leases go round it, or the next app view or tab would take the board's cookie and the board would turn into a 404.

export type TSlotKind = 'port' | 'site';

export const SLOTS: Readonly<Record<TSlotKind, readonly number[]>> = {
  port: [1, 2, 3, 4, 5, 6],
  site: [7, 8],
};

/**
 * The slots of the design library's server: the page of a design (and the kit page) in a window of its own,
 * and in the chat's pane. Only `pin` gives them out: no tab or app view leases them, even while no design is open. The
 * runtime wipes a slot's cache and storage when it shows another target than the cookie's last (preview_slots.go,
 * enter), so a slot that the library shares with a dev server's tab loses every board's cached engine and kit files at
 * each switch; a slot of its own only ever shows the library, and the wipe never comes.
 */
export const LIBRARY_PAGE_SLOT = 1;
export const LIBRARY_PANE_SLOT = 4;
const RESERVED: readonly number[] = [LIBRARY_PAGE_SLOT, LIBRARY_PANE_SLOT];

interface IHold {
  tab: string;
  kind: TSlotKind;
  at: number;
  /** Kept by `pin`: never handed to another tab. */
  pinned: boolean;
}

export interface ISlotLeases {
  /** The tab's slot for this kind of page: the one it holds, a free one, or the least recently used one of another tab. */
  lease: (kind: TSlotKind, tab: string) => number;
  /** The slot the tab holds now; null when it holds none (never asked, or it was taken). */
  slotOf: (tab: string) => number | null;
  /** Lets go of the tab's slot (a closed tab). */
  release: (tab: string) => void;
  /** Keeps this slot for the page whatever leases come; whoever held it is told it was taken. Returns the unpin. */
  pin: (slot: number, page: string) => () => void;
  /** Called with the tab that lost its slot to another. Returns the unsubscribe. */
  onTaken: (listener: (tab: string) => void) => () => void;
}

export const createSlotLeases = (clock: () => number = Date.now): ISlotLeases => {
  const holds = new Map<number, IHold>();
  const listeners = new Set<(tab: string) => void>();
  // A counter besides the clock: two leases in one millisecond still have an order.
  let tick = 0;
  const stamp = () => clock() * 1000 + (tick = (tick + 1) % 1000);

  const heldBy = (tab: string): [number, IHold] | undefined =>
    [...holds.entries()].find(([, hold]) => hold.tab === tab && !hold.pinned);
  const tell = (tab: string) => {
    for (const listener of [...listeners]) listener(tab);
  };

  const lease: ISlotLeases['lease'] = (kind, tab) => {
    const mine = heldBy(tab);
    if (mine && mine[1].kind === kind) {
      mine[1].at = stamp();
      return mine[0];
    }
    // The address moved from a port to a site (or back): the old slot is of no use to this tab.
    if (mine) holds.delete(mine[0]);
    const pool = SLOTS[kind].filter((slot) => !RESERVED.includes(slot));
    const free = pool.find((slot) => !holds.has(slot));
    if (free !== undefined) {
      holds.set(free, { tab, kind, at: stamp(), pinned: false });
      return free;
    }
    // Every slot of the pool pinned is no case of this window (a design pins two of six); the oldest of all then.
    const open = pool.filter((slot) => !holds.get(slot)?.pinned);
    const oldest = (open.length > 0 ? open : pool)
      .map((slot) => [slot, holds.get(slot)] as const)
      .reduce((best, now) => (now[1] && best[1] && now[1].at < best[1].at ? now : best));
    const [slot, taken] = oldest;
    holds.set(slot, { tab, kind, at: stamp(), pinned: false });
    if (taken) tell(taken.tab);
    return slot;
  };

  return {
    lease,
    slotOf: (tab) => heldBy(tab)?.[0] ?? null,
    release: (tab) => {
      const mine = heldBy(tab);
      if (mine) holds.delete(mine[0]);
    },
    pin: (slot, page) => {
      const held = holds.get(slot);
      if (held && held.tab !== page) tell(held.tab);
      const kind: TSlotKind = SLOTS.site.includes(slot) ? 'site' : 'port';
      holds.set(slot, { tab: page, kind, at: stamp(), pinned: true });
      return () => {
        const now = holds.get(slot);
        if (now?.tab === page && now.pinned) holds.delete(slot);
      };
    },
    onTaken: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
};

/** The leases of this window's tabs: every browser tab asks here. */
export const slotLeases = createSlotLeases();
