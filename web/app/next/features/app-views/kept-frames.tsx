// An app's view and a browser tab of the pane live on while their tab is not on screen: another tab in front, another
// chat, the pane closed, another screen of the page. A frame cannot be moved in the document without starting again, so
// they are not drawn in the pane: they stand in one layer of the page (KeptAppFrames, mounted once by the shell), each
// over its place in the pane (KeptSlot, KeptBrowserSlot) while that place is on screen, and out of sight while it is
// not. An app's view runs on out of sight, so its music plays on and what the person typed into it stays; it is told
// it is hidden (`visibility`) as before. A browser tab waits under Activity, its effects stopped, and shows the page as
// it was when it comes back, with no new load. One is let go when its tab is closed, its chat is forgotten, the app's
// own page opens it (one player, not two), or the page's budget of frames out of sight is spent (features/shell/
// kept-budget: shared with the kept screens, the least recent goes).
import {
  Activity,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import { createPortal } from 'react-dom';
import AppFrame from '@/next/features/app-views/app-frame';
import { ConsentStage } from '@/next/features/app-consent/consent-ask';
import { useInstalledApps } from '@/next/features/app-views/api';
import { spaceCleared, spaceContextOf } from '@/next/features/app-views/space-context';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { chatPaneOf, chatPanes } from '@/next/stores/pane-state';
import { keptBudget } from '@/next/features/shell/kept-budget';
import BrowserTab from '@/next/bindings/browser-tab';
import type { IPlace } from '@/next/features/pane/place';
import type { TTab } from '@/next/features/pane/pane-tabs';

type TBrowserTab = Extract<TTab, { kind: 'browser' }>;

/** What a kept browser tab tells its pane: the place on screen gives the current ones. */
export interface IBrowserCalls {
  onChange: (tab: TBrowserTab) => void;
  onNewTab: () => void;
}

export interface IKeptFrame {
  /** `<conversation>:<pane tab id>`. */
  key: string;
  machine: MachineId;
  conversation: ConvId;
  tab: string;
  app: string;
  view: string | null;
  /** The app's object the view was asked to open (a board's slug); null or absent: its home. */
  object?: string | null;
  /** The inspector's «reload». */
  reload: number;
  /** Its place in the pane while on screen. */
  slot: HTMLElement | null;
  /** When it was last on screen. */
  at: number;
  /** A browser tab of the pane, not an app's view: its place, its tab and its calls. */
  browser?: { place: IPlace; tab: TBrowserTab; calls: IBrowserCalls };
}

type TKeptInput = Omit<IKeptFrame, 'key' | 'slot' | 'at'>;

const budgetKey = (key: string) => `pane:${key}`;

const keyOf = (conversation: string, tab: string) => `${conversation}:${tab}`;

let kept: readonly IKeptFrame[] = [];
const listeners = new Set<() => void>();

const commit = (next: readonly IKeptFrame[]) => {
  if (next === kept) return;
  kept = next;
  keptBudget.report(
    'pane',
    kept.map((x) => ({ key: budgetKey(x.key), shown: x.slot !== null, at: x.at })),
  );
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** The hidden ones over the page's budget go (features/shell/kept-budget). */
const capped = (list: readonly IKeptFrame[]): readonly IKeptFrame[] => {
  const keep = (x: IKeptFrame) => x.slot !== null || keptBudget.allows(budgetKey(x.key));
  return list.every(keep) ? list : list.filter(keep);
};
// The budget moved (a screen came on view): what went over it goes.
keptBudget.subscribe(() => commit(capped(kept)));

export const keptFrames = {
  list: (): readonly IKeptFrame[] => kept,
  /** The tab's place is on screen: its view stands there (made now, or the one kept). */
  show(input: TKeptInput, slot: HTMLElement) {
    const key = keyOf(input.conversation, input.tab);
    const entry: IKeptFrame = { ...input, key, slot, at: Date.now() };
    commit(capped([...kept.filter((x) => x.key !== key), entry]));
  },
  /** The place left the screen: the view goes out of sight and runs on. */
  hide(conversation: string, tab: string, slot: HTMLElement) {
    const key = keyOf(conversation, tab);
    if (!kept.some((x) => x.key === key && x.slot === slot)) return;
    commit(capped(kept.map((x) => (x.key === key ? { ...x, slot: null, at: Date.now() } : x))));
  },
  /** Lets go of the views of an app (its own page opens it now). */
  dropApp(app: string) {
    const its = (x: IKeptFrame) => !x.browser && x.app === app && x.slot === null;
    if (kept.some(its)) commit(kept.filter((x) => !its(x)));
  },
  /** Lets go of the views and browsers out of sight whose tab is no longer in its chat's pane. */
  prune() {
    const next = kept.filter((x) => {
      if (x.slot !== null) return true;
      const tab = chatPaneOf(x.conversation).tabs.find((t) => t.id === x.tab);
      if (x.browser) return tab?.kind === 'browser';
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

/** The place of a kept view in the pane: an empty box the layer puts the view over. */
export const KeptSlot = ({
  className,
  machine,
  conversation,
  tab,
  app,
  view,
  object = null,
  reload,
}: IKeptSlotProps) => {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    if (!slot) return;
    keptFrames.show({ machine, conversation, tab, app, view, object, reload }, slot);
  }, [slot, machine, conversation, tab, app, view, object, reload]);
  useLayoutEffect(() => {
    if (!slot) return;
    return () => keptFrames.hide(conversation, tab, slot);
  }, [slot, conversation, tab]);
  return <div ref={setSlot} className={className ?? 'hx-keptslot'} />;
};

interface IKeptBrowserSlotProps {
  place: IPlace;
  tab: TBrowserTab;
  onChange: IBrowserCalls['onChange'];
  onNewTab: IBrowserCalls['onNewTab'];
}

/** The place of a kept browser tab in the pane: the layer puts the tab (its bar and its page) over it. */
export const KeptBrowserSlot = ({ place, tab, onChange, onNewTab }: IKeptBrowserSlotProps) => {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null);
  // The calls of this render, read when called: the layer is not drawn again for new functions.
  const calls = useRef<IBrowserCalls>({ onChange, onNewTab });
  useLayoutEffect(() => {
    calls.current = { onChange, onNewTab };
  });
  const [stable] = useState<IBrowserCalls>(() => ({
    onChange: (next) => calls.current.onChange(next),
    onNewTab: () => calls.current.onNewTab(),
  }));
  useLayoutEffect(() => {
    if (!slot) return;
    keptFrames.show(
      {
        machine: place.machineId,
        conversation: place.id,
        tab: tab.id,
        app: '',
        view: null,
        reload: 0,
        browser: { place, tab, calls: stable },
      },
      slot,
    );
  }, [slot, place, tab, stable]);
  useLayoutEffect(() => {
    if (!slot) return;
    return () => keptFrames.hide(place.id, tab.id, slot);
  }, [slot, place.id, tab.id]);
  return <div ref={setSlot} className="hx-keptslot hx-keptslot--browser" />;
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

/** A kept browser tab: on screen over its place, under Activity out of sight (no effects, no load when back). */
const KeptBrowser = ({ frame }: { frame: IKeptFrame }) => {
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
  const browser = frame.browser;
  if (!browser) return null;
  return (
    <div
      ref={ref}
      className="hx-keptframe hx-keptframe--browser"
      data-shown={shown || undefined}
      aria-hidden={!shown || undefined}
      inert={!shown || undefined}
    >
      <Activity mode={frame.slot !== null ? 'visible' : 'hidden'}>
        <BrowserTab
          conv={browser.place}
          tab={browser.tab}
          onChange={browser.calls.onChange}
          onNewTab={browser.calls.onNewTab}
        />
      </Activity>
    </div>
  );
};

// What each kept view has open inside itself: a board the person went to in the view, so the view opens it
// again when its frame starts anew (a new version of the app, the slot taken back) rather than its home.
const openInFrame = new Map<string, string>();

const KeptFrame = ({ frame }: { frame: IKeptFrame }) => {
  const { list } = useInstalledApps(frame.machine);
  const asked = frame.object ?? null;
  const [inView, setInView] = useState<{ asked: string | null; slug: string | null }>(() => ({
    asked,
    slug: openInFrame.get(frame.key) ?? null,
  }));
  // A new object asked from outside (a link clicked in the chat) wins over what the view had open.
  if (inView.asked !== asked) setInView({ asked, slug: null });
  const opens = inView.asked !== asked ? asked : (inView.slug ?? asked);
  const onPublish = useCallback(
    (data: unknown) => {
      const ctx = spaceContextOf(data);
      const slug = ctx?.object ?? (spaceCleared(data) ? null : undefined);
      if (slug === undefined) return;
      if (slug === null) openInFrame.delete(frame.key);
      else openInFrame.set(frame.key, slug);
      setInView((was) => (was.slug === slug ? was : { ...was, slug }));
    },
    [frame.key],
  );
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
    (v) => (frame.view === null || v.id === frame.view) && v.targets.includes('pane'),
  );
  return (
    <div
      ref={ref}
      className="hx-keptframe"
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
            target="pane"
            viewPort={list.viewPort}
            conversation={frame.conversation}
            reloadToken={frame.reload}
            key={asked ?? ''}
            data={opens ? { object: opens } : undefined}
            onPublish={onPublish}
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
      {ordered.map((frame) =>
        frame.browser ? (
          <KeptBrowser key={frame.key} frame={frame} />
        ) : (
          <KeptFrame key={frame.key} frame={frame} />
        ),
      )}
    </div>,
    document.body,
  );
};

export default KeptAppFrames;
