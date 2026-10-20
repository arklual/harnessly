import { useCallback, useSyncExternalStore } from 'react';
import type { IOpenedPreview, TSlotTarget } from '@/lib/hl-preview-client';
import type { IPreviewPort } from '@/lib/hl-preview/ports-shared';
import type { TPreviewBrowserId, TPreviewDeviceId } from '@/lib/hl-preview-devices';
import { SLOT_RANGES } from '@/lib/preview-slots';

export type TPreviewState =
  | { kind: 'idle' }
  | { kind: 'opening'; target: TSlotTarget }
  | {
      kind: 'open';
      target: TSlotTarget;
      path: string;
      preview: IOpenedPreview;
      load: number;
      openedAt: number;
      entered: boolean;
    }
  | { kind: 'error'; message: string }
  /** Another opening on this machine reused a slot of this window: it would show someone else's port. */
  | { kind: 'taken'; target: TSlotTarget; path: string };

export type { TSlotTarget };

/** A target keeps its slot (and so its origin, cookies and storage) until a newer opening of its kind needs the slot. */
export interface ISlotLease {
  /** `port:<n>` or the external origin. */
  target: string;
  slot: number;
}

const leaseKey = (target: TSlotTarget): string =>
  target.kind === 'port' ? `port:${target.port}` : target.origin;

export interface IPreviewSnapshot {
  input: string;
  state: TPreviewState;
  /** Last answer of the runtime; null until the first one. */
  ports: IPreviewPort[] | null;
  /**
   * Whether the island has made its first-show decision for the tab, the newest chat address it has acted on, and the
   * port it opened by itself (cleared when the person opens one).
   */
  auto: { shown: boolean; seenAt: number; opened: { port: number; at: number } | null };
  /** Ports seen answering as sites while they keep listening (see `trackSeenSites`); null before the first answer. */
  seenSites: ReadonlySet<number> | null;
  /** The preview of the tab floats above the chat instead of sitting in the right panel. */
  detached: boolean;
  /** Device frame of the tab's preview; it stays when the frame moves between the panel and the floating window. */
  device: TPreviewDeviceId;
  /** Browser chrome chosen per device; a device without a choice shows its own browser. */
  browsers: Partial<Record<TPreviewDeviceId, TPreviewBrowserId>>;
}

let loads = 0;
/** Identifies one «open» so a reload remounts the frame while expanding does not. */
export const nextPreviewLoad = (): number => ++loads;

type TOpenPreview = Extract<TPreviewState, { kind: 'open' }>;

const spentTickets = new Set<number>();
/** A frame of this load was given the enter URL: a frame mounted later (detached, docked, expanded) must not reuse it. */
export const spendTicket = (load: number): void => {
  spentTickets.add(load);
};

/** The gateway keeps an enter ticket 60 s; a frame mounted later than this after the request asks for a fresh one. */
export const TICKET_FRESH_MS = 45_000;

/**
 * The enter URL works once and only for a minute; after the first load the slot cookie authorises plain slot URLs. null
 * when the enter URL went to a frame that was unmounted before it loaded (whether its cookie arrived is unknown) or has
 * been waiting too long, for example in a window the island opened by itself: a fresh ticket is needed.
 */
export const frameSrc = (state: TOpenPreview, now: number = Date.now()): string | null => {
  if (state.entered) return `${state.preview.origin}${state.path}`;
  return spentTickets.has(state.load) || now - state.openedAt >= TICKET_FRESH_MS
    ? null
    : state.preview.enterUrl;
};

const INITIAL: IPreviewSnapshot = {
  input: '',
  state: { kind: 'idle' },
  ports: null,
  auto: { shown: false, seenAt: 0, opened: null },
  seenSites: null,
  detached: false,
  device: 'fluid',
  browsers: {},
};

/**
 * The slot for one opening, within the slots of its kind (dev servers and external sites never share one). Neighbours
 * share the main slot, so only main ports hold leases: a target with a lease keeps its slot, another takes a free slot
 * of its kind or the least recently used one. The opening becomes the most recent lease.
 */
export const assignSlot = (
  leases: readonly ISlotLease[],
  target: TSlotTarget,
): { slot: number; leases: ISlotLease[] } => {
  const key = leaseKey(target);
  const [first, last] = SLOT_RANGES[target.kind];
  const ofKind = (slot: number) => slot >= first && slot <= last;
  const known = leases.find((lease) => lease.target === key);
  const others = leases.filter((lease) => lease.target !== key);
  const used = new Set(others.map((lease) => lease.slot));
  const free = Array.from({ length: last - first + 1 }, (_, i) => first + i).find(
    (candidate) => !used.has(candidate),
  );
  const slot = known?.slot ?? free ?? (others.find((lease) => ofKind(lease.slot)) as ISlotLease).slot;
  return { slot, leases: [...others.filter((lease) => lease.slot !== slot), { target: key, slot }] };
};

const holdsLease = (state: TPreviewState, lease: ISlotLease): boolean =>
  state.kind === 'open' && leaseKey(state.target) === lease.target && state.preview.slot === lease.slot;

/**
 * Preview state per conversation tab, outside React: the small island and the expanded dialog share it.
 * Slot leases are per machine — slot origins and their cookies belong to the person on that machine, not to a tab.
 */
export const createPreviewStore = () => {
  const snapshots = new Map<string, IPreviewSnapshot>();
  const leasesByMachine = new Map<string, ISlotLease[]>();
  const listeners = new Set<() => void>();
  const notify = () => {
    for (const listener of listeners) listener();
  };
  return {
    get: (key: string): IPreviewSnapshot => snapshots.get(key) ?? INITIAL,
    patch: (key: string, patch: Partial<IPreviewSnapshot>): void => {
      snapshots.set(key, { ...(snapshots.get(key) ?? INITIAL), ...patch });
      notify();
    },
    /** The slot for `target`; a window of any tab that loses its slot becomes «taken». */
    claimSlot: (machine: string, target: TSlotTarget): number => {
      const before = leasesByMachine.get(machine) ?? [];
      const { slot, leases } = assignSlot(before, target);
      leasesByMachine.set(machine, leases);
      const lost = before.filter(
        (lease) => !leases.some((kept) => kept.target === lease.target && kept.slot === lease.slot),
      );
      for (const [key, snapshot] of snapshots) {
        const { state } = snapshot;
        if (state.kind === 'open' && lost.some((lease) => holdsLease(state, lease))) {
          snapshots.set(key, {
            ...snapshot,
            state: { kind: 'taken', target: state.target, path: state.path },
          });
        }
      }
      notify();
      return slot;
    },
    subscribe: (listener: () => void): (() => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

const store = createPreviewStore();

/** The current snapshot outside render: an async opening must not act on the one it closed over. */
export const readPreview = (key: string): IPreviewSnapshot => store.get(key);

export const patchPreview = (key: string, patch: Partial<IPreviewSnapshot>): void => store.patch(key, patch);

export const claimPreviewSlot = (machine: string, target: TSlotTarget): number =>
  store.claimSlot(machine, target);

export const usePreviewState = (
  key: string,
): [IPreviewSnapshot, (patch: Partial<IPreviewSnapshot>) => void] => {
  const snapshot = useSyncExternalStore(
    store.subscribe,
    () => store.get(key),
    () => INITIAL,
  );
  const patch = useCallback((next: Partial<IPreviewSnapshot>) => store.patch(key, next), [key]);
  return [snapshot, patch];
};
