// An open design over the machine: the project from the library, its boards as the pages, tabs with their own
// history, and the page on screen shown in a preview slot. The slot is opened once per load; switching pages inside it
// only moves the frame to the page's plain address, which skips the ticket round trip and its redirect. Anyone taking
// the slot meanwhile (another preview, another window) or its cookie nearing its end opens it anew on the next switch.
import { useCallback, useEffect, useEffectEvent, useRef, useState } from 'react';
import SiteView, {
  type ISiteAccess,
  type ISiteMode,
  type ISitePage,
  type ISiteTab,
} from '@/next/features/designly/site-view';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';
import { ROLE, claimOwnership, fetchAccess } from '@/next/features/designly/access/api';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import ResourceShare from '@/next/features/designly/access/resource-share';
import { useResourceView } from '@/next/features/designly/access/use-resource';
import PublishSection from '@/next/features/designly/publish/publish-section';
import { usePublish } from '@/next/features/designly/publish/use-publish';
import NoAccess from '@/next/features/designly/access/no-access';
import ProjectAway from '@/next/features/designly/access/project-away';
import {
  boardHref,
  fetchLibrary,
  isPublicAddress,
  keptLibrary,
  type IDesign,
  type IDesignLibrary,
} from '@/next/features/designly/api';
import { openPreview, slotNonce } from '@/next/core/net/preview';
import { slotLeases } from '@/next/features/pane/slot-lease';
import type { IStageOptions } from '@/next/bindings/site-stage';
import { usePaneChatStage, useSiteOnlyStage, type IStageWithChat } from '@/next/bindings/pane-stage';
import { useTabState, useTabTitle } from '@/next/stores/tabs';
import { useChatPaneView } from '@/next/stores/pane-state';
import { modeKindOf, modeStateKey, useDesignMode } from '@/next/features/designly/design-mode';
import { goTo, ROUTE, routePath, type TAccessFocus } from '@/next/routes/routes';
import { useLinkRestored } from '@/next/stores/connection';
import { useConversationList } from '@/next/stores/conversations';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import { onOpenNotePage, takeNoteBoard } from '@/next/features/designly/open-note';
import DesignlyDeck from '@/next/bindings/designly-deck';
import DesignlyVideo from '@/next/bindings/designly-video';
import SinopiaStage from '@/next/features/app-views/sinopia/sinopia-stage';
import { useSinopia } from '@/next/features/app-views/sinopia/use-sinopia';

/** The slot the open design's page lives in; «open outside» takes the next one so the two do not fight. */
const PAGE_SLOT = 1;
const OUTSIDE_SLOT = 2;
/** In the pane the chat's own browser tabs take the first slots of ports (3 is theirs): the pane's design takes these. */
const PANE_PAGE_SLOT = 4;
const PANE_OUTSIDE_SLOT = 5;
/**
 * The slot's cookie lives 12 hours (previewSlotTTL), and an expired one answers with a bare 401 page the host cannot
 * read across origins. An hour short of the end the slot counts as stale at the next page switch.
 */
const SLOT_FRESH_MS = 11 * 60 * 60 * 1000;
/** The waits before each new read of the library for a project the gateway says is the person's (about 15 s in all). */
const RETRY_STEPS_MS = [500, 1000, 2000, 4000, 4000, 4000];

interface IProject {
  design: IDesign;
  port: number;
  /** Where the project's conversations work (the runtime lists the ones in it as the design's). */
  dir: string;
}

const pagesOf = (design: IDesign): ISitePage[] =>
  design.boards.map((b) => ({ path: b.path, name: b.title || b.path }));

/** An older runtime does not say which other chats worked on a design. */
const NO_TOUCHED: readonly string[] = [];

let tabSeq = 0;
/** Unique across reloads too: the window's tab keeps the design's tabs (useTabState). */
const newTab = (path: string): ISiteTab => ({
  id: `t${Date.now().toString(36)}${(++tabSeq).toString(36)}`,
  hist: [path],
  hi: 0,
});

/** The project of the design in a library read; null when the design is not in it. */
const projectIn = (library: IDesignLibrary, id: string): IProject | null => {
  const design = library.designs.find((d) => d.id === id);
  if (!design) return null;
  return { design, port: library.port, dir: `${library.root.replace(/\/+$/, '')}/${design.id}` };
};

export interface IDesignlySiteProps {
  machine: MachineId;
  id: string;
  /** The chat the person came from: the design's chat column shows it. */
  chat?: string | null;
  maxed: boolean;
  /** Null where there is nothing to widen: the full Sinopia view opens its sidebar from the rail itself. */
  onToggleMax: (() => void) | null;
  onOpenDrawer: () => void;
  /** The project is not in the library (deleted, wrong address). */
  onMissing: () => void;
  /** `?access=` of the address: the share dialog opens on that block. */
  access?: TAccessFocus | null;
  /** The editing mode (`?edit=1`): the chat, the tools and the pictures' edits. Without it the design is looked at. */
  edit?: boolean;
  /** Turns the editing mode on or off (the address keeps it). */
  onMode?: (edit: boolean) => void;
  /** The right pane of the design's chat, as a chat has it: its button in the design's strip; null without a chat. */
  toolPane?: { open: boolean; onToggle: () => void } | null;
}

/** The design in the right pane: the chat it sits beside edits it; the way back is to the library. */
interface IPane {
  conv: ConvId;
  onBack: () => void;
}

interface IBodyProps extends IDesignlySiteProps {
  pane?: IPane;
}

/** The address a design is opened at on its own page: what «Поделиться» gives and the phone's code points to. */
const designAddress = (id: string) => new URL(routePath({ kind: ROUTE.site, id }), window.location.href).href;

/** The design's first pages are its tabs from the start, as in the design; the rest are opened with «+». */
/** How often the pages are read again while an agent works on the design, ms. */
const PAGES_POLL_MS = 4000;
const SEED_TABS = 6;
const seedTabs = (boards: readonly { path: string }[]): ISiteTab[] =>
  boards.slice(0, SEED_TABS).map((b) => newTab(b.path));

const DesignlySiteBody = ({
  machine,
  id,
  chat = null,
  maxed,
  onToggleMax,
  onOpenDrawer,
  onMissing,
  access: accessFocus = null,
  edit = false,
  onMode,
  toolPane = null,
  pane,
}: IBodyProps) => {
  // The library the home has just read starts the design: its page is asked for at once, the library is read anew behind.
  const [start] = useState(() => {
    const kept = keptLibrary(machine);
    const known = kept ? projectIn(kept, id) : null;
    const seeded = known ? seedTabs(known.design.boards) : [];
    return { project: known, tabs: seeded, tab: seeded[0] ?? null };
  });
  const [project, setProject] = useState<IProject | null>(start.project);
  const [loadError, setLoadError] = useState<string | null>(null);
  // The design's own tabs and their history belong to the window's tab, in a pane to the chat beside it: switching
  // back finds the same page.
  const paneConv = pane?.conv ?? null;
  const windowTabs = useTabState<ISiteTab[]>(`site:${id}:tabs`, start.tabs);
  const windowTab = useTabState(`site:${id}:tab`, start.tab?.id ?? '');
  const chatTabs = useChatPaneView<ISiteTab[]>(paneConv, `site:${id}:tabs`, start.tabs);
  const chatTab = useChatPaneView(paneConv, `site:${id}:tab`, start.tab?.id ?? '');
  const [tabs, setTabs] = paneConv === null ? windowTabs : chatTabs;
  const [tab, setTab] = paneConv === null ? windowTab : chatTab;
  // Opened from a chat, the mode is that chat's; otherwise the address's (the pane has no switch: it edits).
  const [asked, setMode] = useDesignMode({ conv: pane ? null : chat, id, edit, onMode });
  /** The slot's answer for the page it was asked for; a stale one (another page, a reload) does not count. */
  const [slot, setSlot] = useState<{
    key: string;
    /** The opening's stamp (core/net/preview): another opening of the slot number changes the stored one. */
    nonce: string | null;
    openedAt: number;
    /** The page the slot was opened for: its ticket address is good for the first frame only. */
    path: string;
    enterUrl: string | null;
    /** The slot's origin: the key of the runtime's DevTools record of the page. */
    origin: string | null;
    error: string | null;
  } | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  /** Bumped when a stale slot is to be opened anew on a page switch. */
  const [reopen, setReopen] = useState(0);
  /** Set once the frame has left the ticket address: that address is spent, the slot's plain ones are used. */
  const [entered, setEntered] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // The library has no such project: for a person the gateway turns away that is the card, otherwise the way back.
  const [missing, setMissing] = useState(false);
  // Who may do what with the project, from the gateway; where there is none (standalone) the design is as it was.
  const gate = useProjectAccess(id);
  const [shareOpen, setShareOpen] = useState(false);
  // the one «Поделиться» of every kind reads the gateway's common access API; an older gateway answers nothing
  // there and the older dialog stays.
  const shared = useResourceView('design', id, gate.state.k === ACCESS.ready);
  const focusPending = useRef(accessFocus);
  const seeded = useRef(tabs.length > 0);
  // The link coming back reads the library again, quietly: the open design stays as it is.
  const restored = useLinkRestored(machine);
  // The pages' names and order change while an agent works on the design (a page renamed, added, removed): the library
  // is read again every few seconds while a chat of this design is in a turn, once more as the turn ends, and when the
  // person comes back to the window, so the bar follows without a reload.
  const [fresh, setFresh] = useState(0);
  const convs = useConversationList(machine);
  const drawing =
    project !== null &&
    (convs ?? []).some(
      (c) =>
        (c.id === chat || project.design.convs.includes(c.id)) &&
        (c.state.k === 'starting' || c.state.k === 'working'),
    );
  useEffect(() => {
    if (!drawing) return;
    const timer = setInterval(() => setFresh((n) => n + 1), PAGES_POLL_MS);
    return () => {
      clearInterval(timer);
      setFresh((n) => n + 1);
    };
  }, [drawing]);
  useEffect(() => {
    const onShow = () => {
      if (document.visibilityState === 'visible') setFresh((n) => n + 1);
    };
    document.addEventListener('visibilitychange', onShow);
    return () => document.removeEventListener('visibilitychange', onShow);
  }, []);
  const loaded = useRef(false);
  // The window's tab is named by the page a design is open on; in a pane the chat names it.
  useTabTitle(routePath({ kind: ROUTE.site, id }), pane ? null : (project?.design.title ?? null));
  const pageSlot = pane ? PANE_PAGE_SLOT : PAGE_SLOT;
  const outsideSlot = pane ? PANE_OUTSIDE_SLOT : OUTSIDE_SLOT;
  // The board's slots are this page's: an app view or a browser tab leasing one would take the board's cookie over.
  useEffect(() => {
    const unpinPage = slotLeases.pin(pageSlot, `design:${pageSlot}`);
    const unpinOutside = slotLeases.pin(outsideSlot, `design:${outsideSlot}`);
    return () => {
      unpinPage();
      unpinOutside();
    };
  }, [pageSlot, outsideSlot]);
  // What a share and the phone's code give: the design's own page, not the chat the pane is in.
  const shareUrl = pane ? designAddress(id) : window.location.href;

  useEffect(() => {
    const abort = new AbortController();
    fetchLibrary(machine, abort.signal).then(
      (library) => {
        const read = projectIn(library, id);
        if (!read) {
          setMissing(true);
          return;
        }
        setMissing(false);
        loaded.current = true;
        setProject(read);
        const first = seedTabs(read.design.boards);
        if (!seeded.current && first.length > 0) {
          seeded.current = true;
          setTabs(first);
          setTab(first[0].id);
        }
      },
      (failure: Error) => {
        // A failed quiet re-read keeps the pages on screen; only the first read turns into the error.
        if (!abort.signal.aborted && !loaded.current) setLoadError(failure.message);
      },
    );
    return () => abort.abort();
    // onMissing is the host's; a new one each render must not load the library again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [machine, id, attempt, restored, fresh]);

  const turnedAway = gate.state.k === ACCESS.ready && gate.state.access.role === null;
  // A role the gateway has just given reaches the runtime a moment later (its snapshot): the library is read again
  // for a while before the project counts as gone.
  const granted = gate.state.k === ACCESS.ready && gate.state.access.role !== null;
  const retries = useRef(0);
  const [exhausted, setExhausted] = useState(false);
  useEffect(() => {
    if (!missing || !granted) return;
    const step = RETRY_STEPS_MS[retries.current];
    if (step === undefined) {
      setExhausted(true);
      return;
    }
    const timer = setTimeout(() => {
      retries.current += 1;
      setAttempt((n) => n + 1);
    }, step);
    return () => clearTimeout(timer);
  }, [missing, granted, attempt]);
  // A project the gateway gives the person no place in, and no gateway at all, lead back to the library. One they have a
  // place in but their library does not have after the waits stays on its own card (ProjectAway): it is somewhere else.
  useEffect(() => {
    if (missing && gate.state.k !== ACCESS.loading && !turnedAway && !granted) onMissing();
    // onMissing is the host's; a new one each render must not run it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [missing, gate.state.k, turnedAway, granted]);
  /** Looks for the project again from the start: the waits run anew. */
  const lookAgain = () => {
    retries.current = 0;
    setExhausted(false);
    setLoadError(null);
    setAttempt((n) => n + 1);
  };
  // Opening the dialog asks the gateway again, quietly: a request that came in meanwhile shows up.
  useEffect(() => {
    if (shareOpen) {
      gate.reload();
      shared.reload();
    }
    // reload is stable; only the opening matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shareOpen]);
  // A push to `?access=requests` opens the dialog on that block once, for a person who manages.
  useEffect(() => {
    if (focusPending.current && gate.state.k === ACCESS.ready && gate.state.access.canManage) {
      focusPending.current = null;
      setShareOpen(true);
    }
  }, [gate.state]);

  const shownTab = tabs.find((x) => x.id === tab) ?? tabs[0];
  const shownPath = shownTab?.hist[shownTab.hi] ?? '';
  // A design is on its own page or in a pane for as long as it is open: which stage it has never changes.
  const useStage: (options: IStageOptions) => IStageWithChat = pane ? usePaneChatStage : useSiteOnlyStage;
  const { stage, chat: paneChat } = useStage({
    machine,
    design: id,
    fromChat: chat,
    dir: project?.dir ?? null,
    touched: project?.design.touchedBy ?? NO_TOUCHED,
    title: project?.design.title ?? '',
    pageUrl: boardHref(id, shownPath),
    port: project?.port ?? null,
    onOpenDrawer,
  });

  const current = tabs.find((x) => x.id === tab) ?? tabs[0];
  const path = current?.hist[current.hi] ?? null;
  const port = project?.port ?? null;

  const slotKey = `${port}|${reloadKey}|${reopen}`;
  const opened = slot?.key === slotKey ? slot : null;
  // The frame has left the ticket address: it is spent, the pages after it use the slot's plain ones. Set while
  // rendering, once: the next render has `entered` true and the condition false.
  if (opened !== null && path !== opened.path && !entered) setEntered(true);
  const onFirstPage = opened !== null && path === opened.path && !entered;
  const mayShow = opened !== null;
  const plainSrc =
    mayShow && opened.origin !== null && path !== null
      ? new URL(boardHref(id, path), opened.origin).href
      : null;
  const src = mayShow ? (onFirstPage ? opened.enterUrl : plainSrc) : null;
  const slotOrigin = opened?.origin ?? null;
  const slotError = opened?.error ?? null;
  // The apps on in this document (their panels, selection menu, marks on the boards, the gate before a share). The
  // chat they send tasks to is the one the pane sits beside, or the one the person came from.
  // The design's own chat, when it has no pane or chat it was opened from, is the column's.
  const appsChat =
    paneConv ?? (chat !== null && chat.startsWith('tab-') ? (chat as ConvId) : null) ?? stage?.conv ?? null;
  const sinopia = useSinopia({
    machine,
    id,
    kind: project?.design.kind ?? null,
    conversation: appsChat,
    slotOrigin,
  });

  // The slot opens for the page on screen at the time; the pages after it reuse it (see the effect on the path).
  const pathRef = useRef(path);
  const hasPath = path !== null;
  useEffect(() => {
    pathRef.current = path;
  }, [path]);
  useEffect(() => {
    const first = pathRef.current;
    if (port === null || first === null) return;
    let live = true;
    const key = `${port}|${reloadKey}|${reopen}`;
    setEntered(false);
    openPreview(machine, { kind: 'port', port }, pageSlot, boardHref(id, first)).then(
      (slotOpened) => {
        if (!live) return;
        setSlot({
          key,
          nonce: slotOpened.nonce,
          openedAt: Date.now(),
          path: first,
          enterUrl: slotOpened.enterUrl,
          origin: slotOpened.origin,
          error: null,
        });
      },
      (failure: Error) =>
        live &&
        setSlot({
          key,
          nonce: null,
          openedAt: Date.now(),
          path: first,
          enterUrl: null,
          origin: null,
          error: failure.message,
        }),
    );
    return () => {
      live = false;
    };
    // The path is read once through the ref: a page switch inside an open slot must not open another. A project that
    // had no page yet (a deck whose agent is still making it) opens the slot when its first page comes.
  }, [machine, port, id, reloadKey, reopen, pageSlot, hasPath]);

  /**
   * Called where the page on screen may change, with the page it will be (nothing changes when it is the same one).
   * The slot's cookie is one per slot number, so a page switch inside an open slot is only safe while nobody opened
   * that number since ours (another preview, another window: the stamp in core/net/preview-stamp.ts differs) and the
   * cookie is not nearly out; otherwise the slot is opened anew for the page.
   */
  const refreshSlot = (next: string | undefined) => {
    if (opened === null || opened.error !== null || next === path) return;
    if (slotNonce(pageSlot) !== opened.nonce || Date.now() - opened.openedAt > SLOT_FRESH_MS) {
      setReopen((n) => n + 1);
    }
  };

  const copyLink = () => {
    navigator.clipboard
      ?.writeText(shareUrl)
      .then(() => toast(t('next.site.linkCopied')))
      .catch((failure: Error) =>
        toast(t('next.site.copyFailed'), { tone: 'error', description: failure.message }),
      );
  };

  const updateTab = useCallback(
    (change: (x: ISiteTab) => ISiteTab) =>
      setTabs((list) => list.map((x) => (x.id === (current?.id ?? '') ? change(x) : x))),
    [current?.id, setTabs],
  );

  // A sent note's chip asked for one of this design's pages (open-note.ts): the open tab turns to it, or the first tab
  // is that page.
  const turnTo = useEffectEvent((board: string) => {
    seeded.current = true;
    if (!current) {
      const first = newTab(board);
      setTabs([first]);
      setTab(first.id);
      return;
    }
    updateTab((x) =>
      x.hist[x.hi] === board ? x : { ...x, hist: [...x.hist.slice(0, x.hi + 1), board], hi: x.hi + 1 },
    );
  });
  useEffect(() => {
    const take = () => {
      const board = takeNoteBoard(id);
      if (board !== null) turnTo(board);
    };
    take();
    return onOpenNotePage((place) => {
      if (place.design === id) take();
    });
  }, [id]);

  const gated = gate.state.k === ACCESS.ready ? gate.state.access : null;
  // The state of «Опубликовать как сайт» stays with the design while the share dialog opens and closes.
  const boards = project?.design.boards.map((b) => ({ path: b.path, title: b.title || b.path }));
  const sitePublish = usePublish({
    machine,
    project: id,
    title: project?.design.title ?? '',
    pages: boards ?? [],
  });
  // The one dialog for a design page (decks and videos move in stage «д»), once the gateway answers its API and the
  // project has an owner; a project without one is still claimed in the older dialog.
  const oneDialog =
    project !== null &&
    project.design.kind !== 'slides' &&
    project.design.kind !== 'video' &&
    shared.state !== null &&
    shared.state !== 'absent' &&
    shared.state.stored &&
    shared.state.owner !== null
      ? shared.state
      : null;
  // Wherever the gateway keeps the project's access, «Поделиться» opens its dialog, in the pane as on the page: who
  // manages shares there, anyone else sees their role, asks for more or copies the link from it.
  const shareButton = ((): ISiteAccess | undefined => {
    if (gated === null) return undefined;
    if (gated.canManage)
      return {
        label: t('next.pane.designShare'),
        onOpen: () => sinopia.gate(() => setShareOpen(true)),
        manage: true,
      };
    if (gated.role === 'view' || gated.role === 'comment')
      return { label: t('next.access.askEdit'), onOpen: () => setShareOpen(true) };
    return { label: t('next.pane.designShare'), onOpen: () => sinopia.gate(() => setShareOpen(true)) };
  })();

  // Who may change pictures: where there is no gateway the design is as it was (its person edits); otherwise the owner and editors.
  const mayEdit =
    gate.state.k === ACCESS.absent ||
    (gated !== null && (gated.role === ROLE.owner || gated.role === ROLE.manage || gated.role === ROLE.edit));

  const back = pane ? pane.onBack : () => goTo({ kind: ROUTE.designly });
  if (gated !== null && gated.role === null) {
    return <NoAccess project={id} access={gated} onAsked={gate.apply} onBack={back} />;
  }
  if (missing && project === null && granted && exhausted) {
    return <ProjectAway ownerName={gated?.ownerName ?? ''} onRetry={lookAgain} onBack={back} />;
  }

  // A design is looked at unless the editing mode is asked for (and may be): the pane of a chat always edits, one
  // opened from a chat edits as that chat last had it (design-mode).
  const editing = pane !== undefined || (asked && mayEdit) || onMode === undefined;
  const viewing = !editing;
  // «Опубликовать как сайт» lives in «Поделиться», where a manager edits a site design.
  const mayPublish =
    !viewing &&
    gated?.canManage === true &&
    !!project &&
    project.design.kind !== 'slides' &&
    project.design.kind !== 'video';
  const modeButton: ISiteMode | undefined = (() => {
    if (pane || !onMode) return undefined;
    const state = t(modeStateKey(modeKindOf(project?.design.kind), editing));
    if (editing)
      return {
        state,
        editing,
        action: { label: t('next.site.modeView'), icon: 'eye', onClick: () => setMode(false) },
      };
    if (mayEdit)
      return {
        state,
        editing,
        action: { label: t('next.site.modeEdit'), icon: 'pencil', onClick: () => setMode(true) },
      };
    return {
      state,
      editing,
      action: shareButton
        ? { label: t('next.site.modeAsk'), icon: 'lock-keyhole', onClick: shareButton.onOpen }
        : null,
    };
  })();

  // Before the project is known (the library, the gateway's answer, the waits for a role just given) the design's
  // frame stands with its loader, and a failed read says why with a retry: the screen is never empty.
  if (project === null && (missing || loadError !== null)) {
    return (
      <SiteView
        title=""
        pages={[]}
        tabs={[]}
        tab=""
        src={null}
        error={missing ? null : loadError}
        reloadKey={0}
        onPickTab={() => undefined}
        onCloseTab={() => undefined}
        onNewTab={() => undefined}
        onNavigate={() => undefined}
        onHistory={() => undefined}
        onReload={() => undefined}
        onOpenOutside={() => undefined}
        onCopyLink={() => undefined}
        onRetry={lookAgain}
        onOpenDrawer={onOpenDrawer}
        onToggleMax={onToggleMax}
        maxed={maxed}
      />
    );
  }

  const navigate = (page: string) => {
    refreshSlot(page);
    updateTab((x) =>
      x.hist[x.hi] === page ? x : { ...x, hist: [...x.hist.slice(0, x.hi + 1), page], hi: x.hi + 1 },
    );
  };

  const site = (
    <SiteView
      key={id}
      title={project?.design.title ?? ''}
      pages={project ? pagesOf(project.design) : []}
      tabs={tabs}
      tab={current?.id ?? ''}
      src={src}
      plainSrc={plainSrc}
      phoneUrl={isPublicAddress(shareUrl) ? shareUrl : null}
      error={slotError}
      reloadKey={reloadKey}
      onPickTab={(picked) => {
        const target = tabs.find((x) => x.id === picked);
        refreshSlot(target?.hist[target.hi]);
        setTab(picked);
      }}
      onCloseTab={(closing) => {
        const at = tabs.findIndex((x) => x.id === closing);
        const next = tabs.filter((x) => x.id !== closing);
        setTabs(next);
        if (closing === current?.id && next.length > 0) setTab(next[Math.max(0, at - 1)].id);
      }}
      onNewTab={(page) => {
        refreshSlot(page);
        const added = newTab(page);
        setTabs((list) => [...list, added]);
        setTab(added.id);
      }}
      onNavigate={navigate}
      onHistory={(step) => {
        refreshSlot(
          current && current.hist[Math.max(0, Math.min(current.hist.length - 1, current.hi + step))],
        );
        updateTab((x) => ({ ...x, hi: Math.max(0, Math.min(x.hist.length - 1, x.hi + step)) }));
      }}
      onReload={() => setReloadKey((n) => n + 1)}
      onOpenOutside={() => {
        if (port === null || path === null) return;
        openPreview(machine, { kind: 'port', port }, outsideSlot, boardHref(id, path)).then(
          (slot) => window.open(slot.enterUrl, '_blank', 'noopener'),
          (failure: Error) =>
            toast(t('next.site.openFailed'), { tone: 'error', description: failure.message }),
        );
      }}
      onCopyLink={copyLink}
      onRetry={() => setReloadKey((n) => n + 1)}
      onOpenDrawer={onOpenDrawer}
      onToggleMax={onToggleMax}
      maxed={maxed}
      stage={viewing ? undefined : stage}
      pane={pane && { onShare: copyLink, chat: paneChat }}
      toolPane={viewing ? null : toolPane}
      mode={modeButton}
      access={shareButton}
      devtools={viewing ? undefined : { machine, slot: slotOrigin, artifact: id }}
      images={!viewing && mayEdit && project ? { machine, artifact: id } : undefined}
      downloads={project ? { machine, artifact: id, archives: mayEdit } : undefined}
      memoryKey={id}
    />
  );

  // A deck and a video take the same host props: the boards, the board on screen, the slot's frames and the site view
  // as their fallback. Anything else, and a project not read yet, is the site view.
  const kindView = () => {
    if (!project) return site;
    const host = {
      machine,
      id,
      title: project.design.title,
      boards: project.design.boards.map((b) => ({ path: b.path, title: b.title || b.path })),
      path,
      onGo: navigate,
      src,
      frameSrc: (page: string) =>
        opened !== null && opened.origin !== null ? new URL(boardHref(id, page), opened.origin).href : null,
      slotError,
      onReload: () => setReloadKey((n) => n + 1),
      stage: viewing ? undefined : stage,
      pane: pane && { onBack: pane.onBack },
      access: shareButton,
      viewing,
      mode: modeButton,
      mayEdit,
      maxed,
      onToggleMax,
      onOpenDrawer,
      fallback: site,
    };
    if (project.design.kind === 'video') return <DesignlyVideo {...host} />;
    if (project.design.kind === 'slides')
      return <DesignlyDeck {...host} onPick={sinopia.pickBlock} onStale={() => setAttempt((n) => n + 1)} />;
    return site;
  };

  return (
    <>
      <SinopiaStage
        machine={machine}
        kind={project?.design.kind ?? null}
        id={id}
        conversation={appsChat}
        sinopia={sinopia}
      >
        {kindView()}
      </SinopiaStage>
      {gated !== null && oneDialog !== null && (
        <ResourceShare
          open={shareOpen}
          onClose={() => setShareOpen(false)}
          target={{ kind: 'design', id }}
          title={project?.design.title ?? ''}
          view={oneDialog}
          onNext={(next) => {
            shared.apply(next);
            // The page's own gate (who may edit pictures, the share button) follows what changed.
            gate.reload();
          }}
          address={shareUrl}
          onCopyLink={copyLink}
          focus={accessFocus}
          links={{
            project: id,
            access: gated,
            onNext: gate.apply,
            pages: boards,
          }}
          publish={mayPublish ? sitePublish : undefined}
        />
      )}
      {gated !== null && oneDialog === null && (
        <ShareDialog
          open={shareOpen}
          onClose={() => setShareOpen(false)}
          project={id}
          title={project?.design.title ?? ''}
          access={gated}
          focus={accessFocus}
          onNext={gate.apply}
          pages={boards}
          onCopyLink={copyLink}
          extra={mayPublish ? <PublishSection state={sitePublish} /> : undefined}
          onClaim={async () => {
            await claimOwnership(machine, id);
            const next = await fetchAccess(id);
            if (next) gate.apply(next);
          }}
        />
      )}
    </>
  );
};

const DesignlySite = (props: IDesignlySiteProps) => <DesignlySiteBody {...props} />;

export interface IDesignlyPaneSiteProps {
  machine: MachineId;
  id: string;
  /** The chat the pane is beside: it edits the design, its field gets the notes. */
  conv: ConvId;
  onBack: () => void;
}

/** A design opened in the right pane: the full viewer, with the chat on its left as the one that edits it. */
export const DesignlyPaneSite = ({ machine, id, conv, onBack }: IDesignlyPaneSiteProps) => (
  <DesignlySiteBody
    machine={machine}
    id={id}
    chat={conv}
    maxed={false}
    onToggleMax={() => undefined}
    onOpenDrawer={() => undefined}
    onMissing={onBack}
    pane={{ conv, onBack }}
  />
);

export default DesignlySite;
