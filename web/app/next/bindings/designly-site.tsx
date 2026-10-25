// An open design over the machine: the project from the library, its boards as the pages, tabs with their own
// history, and the page on screen shown in a preview slot. The slot is opened once per load; switching pages inside it
// only moves the frame to the page's plain address, which skips the ticket round trip and its redirect. Anyone taking
// the slot meanwhile (another preview, another window) or its cookie nearing its end opens it anew on the next switch.
import { useCallback, useEffect, useEffectEvent, useRef, useState } from 'react';
import SiteView, {
  type ISiteAccess,
  type ISitePage,
  type ISiteTab,
} from '@/next/features/designly/site-view';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';
import { atLeast, claimOwnership, fetchAccess, isUnowned } from '@/next/features/designly/access/api';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import NoAccess from '@/next/features/designly/access/no-access';
import {
  boardHref,
  fetchLibrary,
  isPublicAddress,
  keptLibrary,
  type IDesign,
  type IDesignLibrary,
} from '@/next/features/designly/api';
import { openPreview, slotNonce } from '@/next/core/net/preview';
import type { IStageOptions } from '@/next/bindings/site-stage';
import { usePaneChatStage, useSiteOnlyStage, type IStageWithChat } from '@/next/bindings/pane-stage';
import { useTabState, useTabTitle } from '@/next/stores/tabs';
import { useChatPaneView } from '@/next/stores/pane-state';
import { goTo, ROUTE, routePath, type TAccessFocus } from '@/next/routes/routes';
import { useLinkRestored } from '@/next/stores/connection';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import { onOpenNotePage, takeNoteBoard } from '@/next/features/designly/open-note';

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
  onToggleMax: () => void;
  onOpenDrawer: () => void;
  /** The project is not in the library (deleted, wrong address). */
  onMissing: () => void;
  /** `?access=` of the address: the share dialog opens on that block. */
  access?: TAccessFocus | null;
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
  const focusPending = useRef(accessFocus);
  const seeded = useRef(tabs.length > 0);
  // The link coming back reads the library again, quietly: the open design stays as it is.
  const restored = useLinkRestored(machine);
  // The window's tab is named by the page a design is open on; in a pane the chat names it.
  useTabTitle(routePath({ kind: ROUTE.site, id }), pane ? null : (project?.design.title ?? null));
  const pageSlot = pane ? PANE_PAGE_SLOT : PAGE_SLOT;
  const outsideSlot = pane ? PANE_OUTSIDE_SLOT : OUTSIDE_SLOT;
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
        setProject(read);
        const first = seedTabs(read.design.boards);
        if (!seeded.current && first.length > 0) {
          seeded.current = true;
          setTabs(first);
          setTab(first[0].id);
        }
      },
      (failure: Error) => {
        if (!abort.signal.aborted) setLoadError(failure.message);
      },
    );
    return () => abort.abort();
    // onMissing is the host's; a new one each render must not load the library again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [machine, id, attempt, restored]);

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
  useEffect(() => {
    if (missing && gate.state.k !== ACCESS.loading && !turnedAway && (!granted || exhausted)) onMissing();
    // onMissing is the host's; a new one each render must not run it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [missing, gate.state.k, turnedAway, granted, exhausted]);
  // Opening the dialog asks the gateway again, quietly: a request that came in meanwhile shows up.
  useEffect(() => {
    if (shareOpen) gate.reload();
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

  // The slot opens for the page on screen at the time; the pages after it reuse it (see the effect on the path).
  const pathRef = useRef(path);
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
    // The path is read once through the ref: a page switch inside an open slot must not open another.
  }, [machine, port, id, reloadKey, reopen, pageSlot]);

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
  const shareButton = ((): ISiteAccess | undefined => {
    if (gated === null) return undefined;
    if (gated.canManage) return { label: t('next.pane.designShare'), onOpen: () => setShareOpen(true) };
    if (gated.role === 'view' || gated.role === 'comment')
      return { label: t('next.access.askEdit'), onOpen: () => setShareOpen(true) };
    if (isUnowned(gated) && atLeast(gated.role, 'edit'))
      return { label: t('next.pane.designShare'), onOpen: () => setShareOpen(true) };
    return undefined;
  })();

  if (gated !== null && gated.role === null) {
    return (
      <NoAccess
        project={id}
        access={gated}
        onAsked={gate.apply}
        onBack={pane ? pane.onBack : () => goTo({ kind: ROUTE.designly })}
      />
    );
  }
  if (missing && project === null) return null;

  if (loadError !== null && project === null) {
    return (
      <SiteView
        title=""
        pages={[]}
        tabs={[]}
        tab=""
        src={null}
        error={loadError}
        reloadKey={0}
        onPickTab={() => undefined}
        onCloseTab={() => undefined}
        onNewTab={() => undefined}
        onNavigate={() => undefined}
        onHistory={() => undefined}
        onReload={() => undefined}
        onOpenOutside={() => undefined}
        onCopyLink={() => undefined}
        onRetry={() => {
          setLoadError(null);
          setAttempt((n) => n + 1);
        }}
        onOpenDrawer={onOpenDrawer}
        onToggleMax={onToggleMax}
        maxed={maxed}
      />
    );
  }

  return (
    <>
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
        onNavigate={(page) => {
          refreshSlot(page);
          updateTab((x) =>
            x.hist[x.hi] === page ? x : { ...x, hist: [...x.hist.slice(0, x.hi + 1), page], hi: x.hi + 1 },
          );
        }}
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
        stage={stage}
        pane={pane && { onShare: copyLink, chat: paneChat }}
        access={shareButton}
        devtools={{ machine, slot: slotOrigin, artifact: id }}
        memoryKey={id}
      />
      {gated !== null && (
        <ShareDialog
          open={shareOpen}
          onClose={() => setShareOpen(false)}
          project={id}
          title={project?.design.title ?? ''}
          access={gated}
          focus={accessFocus}
          onNext={gate.apply}
          onCopyLink={copyLink}
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
