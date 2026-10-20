import { create } from 'zustand';
import type { ISidebarView, TSidebarSection } from '@/lib/hl-sidebar-model';

/**
 * What is folded in the left panel of this device. Kept in localStorage: a phone and a laptop
 * fold different things, and nothing here has to follow the user across devices.
 * "Show more" inside a project is not saved at all.
 */
const STORAGE_KEY = 'hl:sidebar-view';

const SECTIONS = [
  'pinned',
  'projects',
  'conversations',
  'docs',
] as const satisfies readonly TSidebarSection[];

const EMPTY_VIEW: ISidebarView = { collapsedProjects: [], collapsedSections: [] };

const readView = (): ISidebarView => {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null');
    if (typeof saved !== 'object' || saved === null) return EMPTY_VIEW;
    const { collapsedProjects, collapsedSections } = saved as Record<string, unknown>;
    return {
      collapsedProjects: Array.isArray(collapsedProjects)
        ? collapsedProjects.filter((id): id is string => typeof id === 'string')
        : [],
      collapsedSections: Array.isArray(collapsedSections)
        ? collapsedSections.filter((id): id is TSidebarSection =>
            (SECTIONS as readonly unknown[]).includes(id),
          )
        : [],
    };
  } catch {
    return EMPTY_VIEW;
  }
};

const writeView = (view: ISidebarView) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(view));
  } catch {
    // Private mode or full storage: folding still works for this page.
  }
};

const toggle = <T>(list: readonly T[], item: T): T[] =>
  list.includes(item) ? list.filter((x) => x !== item) : [...list, item];

interface ISidebarViewStore {
  view: ISidebarView;
  hydrated: boolean;
  /** Projects where "Show more" is open; component state shared with the ⌘ numbering. */
  expandedLists: ReadonlySet<string>;
  /** A request to open and scroll to a project (crumb in the header, palette); the nonce repeats the same request. */
  reveal: { projectId: string; nonce: number } | null;
  hydrate: () => void;
  toggleProject: (id: string) => void;
  toggleSection: (section: TSidebarSection) => void;
  toggleList: (id: string) => void;
  revealProject: (id: string) => void;
}

const useSidebarView = create<ISidebarViewStore>((set, get) => ({
  view: EMPTY_VIEW,
  hydrated: false,
  expandedLists: new Set(),
  reveal: null,
  hydrate: () => {
    if (get().hydrated) return;
    set({ view: readView(), hydrated: true });
  },
  toggleProject: (id) => {
    const view = { ...get().view, collapsedProjects: toggle(get().view.collapsedProjects, id) };
    writeView(view);
    set({ view });
  },
  toggleSection: (section) => {
    const view = { ...get().view, collapsedSections: toggle(get().view.collapsedSections, section) };
    writeView(view);
    set({ view });
  },
  toggleList: (id) => set((state) => ({ expandedLists: new Set(toggle([...state.expandedLists], id)) })),
  revealProject: (id) => {
    const { view, reveal } = get();
    const next = {
      collapsedProjects: view.collapsedProjects.filter((x) => x !== id),
      collapsedSections: view.collapsedSections.filter((x) => x !== 'projects'),
    };
    writeView(next);
    set({ view: next, reveal: { projectId: id, nonce: (reveal?.nonce ?? 0) + 1 } });
  },
}));

export default useSidebarView;
