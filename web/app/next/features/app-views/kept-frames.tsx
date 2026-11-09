// An app's view lives on while it is not on screen. A pane tab's view: another tab in front, another chat, the pane
// closed, another screen of the page. The app's own page (/apps/<id>): another screen of the page. A frame cannot be moved in the document without starting again, so the views of
// the pane's app tabs and of the app's page are not drawn in place: they stand in one layer of the page (KeptAppFrames,
// mounted once by the shell), each over its place (KeptSlot) while that place is on screen, and out of sight, still
// running, while it is not. So a view's music plays on, and what the person typed into it stays. The view is told it is
// hidden (`visibility`) as before. A view is let go when its tab is closed, its chat is forgotten, another view of the
// same app comes on screen (one player, not two), or more than MAX_HIDDEN wait out of sight (the least recent goes).
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react';
import { createPortal } from 'react-dom';
import AppFrame from '@/next/features/app-views/app-frame';
import { ConsentStage } from '@/next/features/app-consent/consent-ask';
import { useInstalledApps } from '@/next/features/app-views/api';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { chatPaneOf, chatPanes } from '@/next/stores/pane-state';

/** Views out of sight beyond this many are let go, the least recent first. */
export const MAX_HIDDEN = 4;

/** Where a kept view is shown: a tab of a chat's pane, or the app's own page (/apps/<id>). */
export type TKeptTarget = 'pane' | 'app';

export interface IKeptFrame {
  /** `pane:<conversation>:<tab>` or `app:<machine>:<app>`. */
  key: string;
  target: TKeptTarget;
  machine: MachineId;
  /** The chat the view's session is for; null: the app's page with no chat. */
  conversation: ConvId | null;
  /** The pane tab; null on the app's page. */
  tab: string | null;
  app: string;
  view: string | null;
  /** The inspector's «reload». */
  reload: number;
  /** Its place while on screen. */
  slot: HTMLElement | null;
  /** When it was last on screen. */
  at: number;
}

export type TKeptInput = Omit<IKeptFrame, 'key' | 'slot' | 'at'>;

export const keyOfKept = (
  x: Pick<IKeptFrame, 'target' | 'machine' | 'conversation' | 'tab' | 'app'>,
): string =>
  x.target === 'app' ? `app:${x.machine}:${x.app}` : `pane:${x.conversation ?? ''}:${x.tab ?? ''}`;

let kept: readonly IKeptFrame[] = [];
const listeners = new Set<() => void>();

const commit = (next: readonly IKeptFrame[]) => {
  if (next === kept) return;
  kept = next;
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** The hidden ones over the cap go, the least recent first. */
const capped = (list: readonly IKeptFrame[]): readonly IKeptFrame[] => {
  const hidden = list.filter((x) => x.slot === null).sort((a, b) => a.at - b.at);
  if (hidden.length <= MAX_HIDDEN) return list;
  const drop = new Set(hidden.slice(0, hidden.length - MAX_HIDDEN).map((x) => x.key));
  return list.filter((x) => !drop.has(x.key));
};

export const keptFrames = {
  list: (): readonly IKeptFrame[] => kept,
  /**
   * The place is on screen: its view stands there (made now, or the one kept). Another copy of the same app kept out of
   * sight is let go, so one view of an app runs at a time (two would play two sounds).
   */
  show(input: TKeptInput, slot: HTMLElement) {
    const key = keyOfKept(input);
    const entry: IKeptFrame = { ...input, key, slot, at: Date.now() };
    const rest = kept.filter((x) => x.key !== key && !(x.app === input.app && x.slot === null));
    commit(capped([...rest, entry]));
  },
  /** The place left the screen: the view goes out of sight and runs on. */
  hide(key: string, slot: HTMLElement) {
    if (!kept.some((x) => x.key === key && x.slot === slot)) return;
    commit(capped(kept.map((x) => (x.key === key ? { ...x, slot: null, at: Date.now() } : x))));
  },
  /** Lets go of a view (the app's page closed by the person). */
  drop(key: string) {
    if (kept.some((x) => x.key === key)) commit(kept.filter((x) => x.key !== key));
  },
  /** Lets go of the pane views whose tab is no longer in its chat's pane. */
  prune() {
    const next = kept.filter((x) => {
      if (x.target !== 'pane') return true;
      const tab = chatPaneOf(x.conversation).tabs.find((t) => t.id === x.tab);
      return tab?.kind === 'app' && tab.app === x.app && tab.view === x.view;
    });
    if (next.length !== kept.length) commit(next);
  },
  /** Forgets them all (tests). */
  reset: () => commit([]),
};

export const useKeptFrames = (): readonly IKeptFrame[] =>
  useSyncExternalStore(subscribe, keptFrames.list, keptFrames.list);

interface IKeptSlotProps extends TKeptInput {
  className?: string;
}

/** The place of a kept view: an empty box the layer puts the view over. */
export const KeptSlot = ({ className, ...input }: IKeptSlotProps) => {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null);
  const { target, machine, conversation, tab, app, view, reload } = input;
  const key = keyOfKept(input);
  useLayoutEffect(() => {
    if (!slot) return;
    keptFrames.show({ target, machine, conversation, tab, app, view, reload }, slot);
  }, [slot, target, machine, conversation, tab, app, view, reload]);
  useLayoutEffect(() => {
    if (!slot) return;
    return () => keptFrames.hide(key, slot);
  }, [slot, key]);
  return <div ref={setSlot} className={className ?? 'hx-keptslot'} />;
};

interface IBox {
  left: number;
  top: number;
  width: number;
  height: number;
  z: string;
}

const OUT: IBox = { left: -100_000, top: 0, width: 0, height: 0, z: '' };

/** The pane's stacking order, so the view stands where the pane's own content would (menus of the page over it). */
const zOf = (slot: HTMLElement): string => {
  const pane = slot.closest<HTMLElement>('.hx-pane');
  const z = pane ? getComputedStyle(pane).zIndex : '';
  return z === 'auto' ? '' : z;
};

/** Follows the place every frame while it is on screen: the pane slides, widens and is dragged wider. */
const useBoxOf = (slot: HTMLElement | null): IBox => {
  // The last place it had, and whether the place is on screen now.
  const [seen, setSeen] = useState<{ box: IBox | null; on: boolean }>({ box: null, on: false });
  useEffect(() => {
    if (!slot) return;
    let raf = 0;
    const z = zOf(slot);
    const step = () => {
      const r = slot.getBoundingClientRect();
      const on = slot.isConnected && r.width > 0 && r.height > 0;
      setSeen((was) => {
        if (!on) return was.on ? { ...was, on: false } : was;
        const b = was.box;
        if (
          was.on &&
          b &&
          b.left === r.left &&
          b.top === r.top &&
          b.width === r.width &&
          b.height === r.height
        )
          return was;
        return { box: { left: r.left, top: r.top, width: r.width, height: r.height, z }, on: true };
      });
      raf = requestAnimationFrame(step);
    };
    step();
    return () => cancelAnimationFrame(raf);
  }, [slot]);
  // Out of sight it keeps the size it had, so the view is not laid out again for nothing.
  if (!slot || !seen.on || !seen.box)
    return { ...OUT, width: seen.box?.width ?? 0, height: seen.box?.height ?? 0 };
  return seen.box;
};

const KeptFrame = ({ frame }: { frame: IKeptFrame }) => {
  const { list } = useInstalledApps(frame.machine);
  const box = useBoxOf(frame.slot);
  const ref = useRef<HTMLDivElement>(null);
  const shown = frame.slot !== null && box.left !== OUT.left;
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.left = `${box.left}px`;
    el.style.top = `${box.top}px`;
    el.style.width = `${box.width}px`;
    el.style.height = `${box.height}px`;
    el.style.zIndex = box.z;
  }, [box]);
  const app = list?.apps.find((a) => a.id === frame.app);
  const view = app?.views.find(
    (v) => (frame.view === null || v.id === frame.view) && v.targets.includes(frame.target),
  );
  return (
    <div
      ref={ref}
      className="hx-keptframe"
      data-target={frame.target}
      data-shown={shown || undefined}
      aria-hidden={!shown || undefined}
      inert={!shown || undefined}
    >
      {list && app && view && (
        <ConsentStage machine={frame.machine} app={app}>
          <AppFrame
            machine={frame.machine}
            app={app}
            view={view}
            target={frame.target}
            viewPort={list.viewPort}
            conversation={frame.conversation}
            reloadToken={frame.reload}
          />
        </ConsentStage>
      )}
    </div>
  );
};

/** The layer of the kept views: mounted once, for the life of the page. */
const KeptAppFrames = () => {
  const frames = useKeptFrames();
  useEffect(() => {
    keptFrames.prune();
    return chatPanes.subscribe(() => keptFrames.prune());
  }, []);
  if (frames.length === 0) return null;
  // Sorted by key, so a view coming on screen is not moved in the document (that would start it again).
  const ordered = [...frames].sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
  return createPortal(
    <div className="hx-keptframes">
      {ordered.map((frame) => (
        <KeptFrame key={frame.key} frame={frame} />
      ))}
    </div>,
    document.body,
  );
};

export default KeptAppFrames;
