// The deck's shell: bar, strip, stage, panel and the scenes of a deck (create, edit, show, record, done, montage,
// link), with the state attributes the board defines (data-screen, data-panel, data-pick, data-full, data-drag,
// data-lmode, data-es, data-embed, data-paused). The shell holds what the parts share — the screen, the slide on
// screen, the panel's mode and tab, the selected and hovered block, the open popover or sheet — and talks to the board
// on screen through the deck bridge: it turns the board's deck mode on, writes hand edits (text, style, move) into the
// board's source through the patch route, and shows the frame, grip and guides of the blocks.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import type { ISiteAccess, ISiteStage } from '@/next/features/designly/site-view';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';
import { useMedia } from '@/next/ui/use-media';
import { usePresence, PRESENCE } from '@/next/ui/presence';
import { goTo, ROUTE } from '@/next/routes/routes';
import { putDeckSettingsLocal, putDeckSlide, useDeck } from '@/next/stores/deck';
import { thumbPath } from '@/next/features/designly/api';
import { patchDeck, putDeckNotes, putDeckSettings } from '@/next/features/designly/deck/api';
import {
  DECK_PRESS,
  postDeck,
  type TDeckBoardMessage,
  type TDeckHostMessage,
  type TDeckMode,
} from '@/next/features/designly/deck/bridge';
import {
  DECK_OPENS,
  DECK_PANEL,
  DECK_SCREEN,
  DECK_TAB,
  type IDeckContext,
  type IDeckRecording,
  type IDeckSettings,
  type TDeckExport,
  type TDeckLayout,
  type TDeckOpens,
  type TDeckPanelMode,
  type TDeckPatch,
  type TDeckScreen,
  type TDeckTab,
} from '@/next/features/designly/deck/types';
import { DECK_BOARD_H, DECK_BOARD_W, deckIndexOf, deckStep } from '@/next/features/designly/deck/layout';
import {
  DECK_KEY,
  deckKeyAction,
  useDeckKeys,
  type TDeckKey,
} from '@/next/features/designly/deck/use-deck-keys';
import { useDeckBridge } from '@/next/features/designly/deck/use-deck-bridge';
import DeckBar from '@/next/features/designly/deck/deck-bar';
import DeckStrip, { DECK_PENDING_THUMBS } from '@/next/features/designly/deck/deck-strip';
import DeckStage from '@/next/features/designly/deck/deck-stage';
import { deckHold, deckPlayBlock, slideOfBoard } from '@/next/features/designly/deck/deck-making';
import DeckOverlay, { type IDeckDrag, type IDeckHover } from '@/next/features/designly/deck/deck-overlay';
import DeckToolbar, {
  type IDeckSelection,
  type IDeckTextLook,
} from '@/next/features/designly/deck/deck-toolbar';
import { DeckNotesLine, DeckPager, type IDeckSubsDictation } from '@/next/features/designly/deck/deck-subs';
import DeckStyle, { DECK_THEMES, deckTheme, useThemeVars } from '@/next/features/designly/deck/deck-style';
import DeckMenu, { exportDeck, lastRecording } from '@/next/features/designly/deck/deck-menu';
import DeckMdock from '@/next/features/designly/deck/deck-mdock';
import DeckShow from '@/next/features/designly/deck/deck-show';
import DeckRec from '@/next/features/designly/deck/deck-rec';
import DeckDone from '@/next/features/designly/deck/deck-done';
import DeckShare from '@/next/features/designly/deck/deck-share';
import DeckCut from '@/next/features/designly/deck/deck-cut';
import type { IDeckPlayHost, IDeckPointer } from '@/next/features/designly/deck/deck-play';
import DeckToast, { deckToast } from '@/next/features/designly/deck/deck-toast';
import { DECK_SHEET_EXIT_MS, EditSheet, NotesSheet } from '@/next/features/designly/deck/deck-sheets';
import { DeckPanel, focusDeckChat } from '@/next/features/designly/deck/deck-panel';
import { DeckCommentLayer } from '@/next/features/designly/deck/deck-comments';
import {
  closeDeckDraft,
  escapeDeckComment,
  feedDeckRing,
  openDeckDraft,
  startDeckPick,
  stopDeckPick,
  useDeckPick,
} from '@/next/features/designly/deck/comment-ui';
import { useDeckComments } from '@/next/features/designly/deck/use-deck-comments';
import { useDeckDictation } from '@/next/features/designly/deck/dictation';
import { DICT } from '@/next/features/designly/deck/dictation-state';

export interface IDeckBoard {
  path: string;
  title: string;
}

export interface IDeckViewProps {
  machine: MachineId;
  /** The project's id: the deck routes' `artifact`. */
  id: string;
  title: string;
  /** The deck's boards in the order of its pages; the deck's own state (notes, comments) comes from the store. */
  boards: readonly IDeckBoard[];
  /** The board on screen; null before the project is read. */
  path: string | null;
  onGo: (path: string) => void;
  /** The frame address of the board on screen: on the first frame it is the slot's ticket address. Null while the slot opens. */
  src: string | null;
  /**
   * The plain address of any board in the open slot (a second frame for the crossfade, the next slide's mini), valid
   * once the first frame has loaded `src`; null before the slot is open.
   */
  frameSrc: (path: string) => string | null;
  slotError: string | null;
  onReload: () => void;
  /** The site's stage: its `chat` is the panel's Чат tab, `context` and `ask` talk to it. */
  stage?: ISiteStage;
  /** Set in the right pane beside a chat: the compact column (`data-embed`), its chat is the host's. */
  pane?: { onBack: () => void };
  access?: ISiteAccess;
  maxed: boolean;
  onToggleMax: () => void;
  onOpenDrawer: () => void;
  /** What shows until the shell is built: the site view of the same project. */
  fallback: ReactNode;
  /** The deck's state could not be read: why (the shell still shows the boards). */
  deckError?: string | null;
}

/** The board's breakpoints (not the app's): ≤560 is a phone. */
export const DECK_PHONE_QUERY = '(max-width: 560px)';

const POP = { style: 'style', menu: 'menu', share: 'share' } as const;
type TPop = (typeof POP)[keyof typeof POP];
const POP_EXIT_MS = 160;

/** The phone's sheets under the slide: a text's edit sheet, the notes sheet, the comment being written. */
export const DECK_SHEET = { edit: 'edit', notes: 'notes', comment: 'comment' } as const;
export type TDeckSheet = (typeof DECK_SHEET)[keyof typeof DECK_SHEET];

/**
 * Where a press keeps the selected block: its toolbar, its frame's grip and reset, the phone's edit sheet, and the
 * chat's field («Попросить…» asks about the block there).
 */
const DECK_SEL_TOOLS = '.hx-deck-tb, .hx-deck-overlay, .hx-deck-esheet, .hx-composer';

/** A press on the toolbar takes the focus out of the board: the deselect it causes within this time is ignored. */
const HOLD_MS = 400;
/** The host chat's field in the sidebar takes this long to be there, ms. */
const FOCUS_DELAY_MS = 60;
/** The board answers which block is at a point within this time; later the comment goes without a label. */
const HIT_MS = 800;

const MODE_OF: Partial<Record<TDeckScreen, TDeckMode>> = {
  edit: 'edit',
  show: 'show',
  rec: 'rec',
  link: 'guest',
};

const LAYOUT_KEYS = {
  cover: 'next.deck.style.layoutCover',
  statement: 'next.deck.style.layoutStatement',
  list: 'next.deck.style.layoutList',
  cards: 'next.deck.style.layoutCards',
} as const satisfies Record<TDeckLayout, string>;

/** Focuses the host chat's field (the deck in a chat's sidebar has no panel of its own). */
const focusComposer = (scope: ParentNode | null) =>
  setTimeout(
    () => scope?.querySelector<HTMLTextAreaElement>('.hx-composer__input')?.focus({ preventScroll: true }),
    FOCUS_DELAY_MS,
  );

const DeckView = (props: IDeckViewProps) => {
  const { machine, id, title, boards, path, onGo, src, stage, pane, access, deckError = null } = props;
  const deck = useDeck(id) ?? null;
  const phone = useMedia(DECK_PHONE_QUERY);
  const embed = pane !== undefined;
  const frameRef = useRef<HTMLIFrameElement>(null);

  const [screen, setScreen] = useState<TDeckScreen>(DECK_SCREEN.edit);
  const [panel, setPanel] = useState<TDeckPanelMode>(phone ? DECK_PANEL.off : DECK_PANEL.dock);
  const [tab, setTab] = useState<TDeckTab>(DECK_TAB.chat);
  // A phone hides the panel, a wide window brings it back docked (the board's resize rule).
  const [wasPhone, setWasPhone] = useState(phone);
  if (wasPhone !== phone) {
    setWasPhone(phone);
    setPanel(phone ? DECK_PANEL.off : DECK_PANEL.dock);
  }
  const [pop, setPop] = useState<TPop | null>(null);
  const shownPop = usePresence(pop, POP_EXIT_MS);
  const [sheet, setSheet] = useState<TDeckSheet | null>(null);
  const shownSheet = usePresence(sheet, DECK_SHEET_EXIT_MS);
  /** Sends to the board on screen (to its frame's origin only). */
  const post = (message: TDeckHostMessage) => postDeck(frameRef.current, src, message);
  const pick = useDeckPick(id);
  const picking = pick.picking;
  const comments = useDeckComments(machine, id);
  const dict = useDeckDictation(machine, id);
  const [sel, setSel] = useState<IDeckSelection | null>(null);
  const [hover, setHover] = useState<IDeckHover | null>(null);
  const [drag, setDrag] = useState<IDeckDrag | null>(null);
  const [grip, setGrip] = useState(false);
  const [subsOn, setSubsOn] = useState(true);
  const [lmode, setLmode] = useState<TDeckOpens>(DECK_OPENS.slides);
  const [lfull, setLfull] = useState(false);
  const [board, setBoard] = useState<{ w: number; h: number } | null>(null);
  /** What the person set on blocks this session, by `<page>:<tpl>`. */
  const [looks, setLooks] = useState<Record<string, IDeckTextLook>>({});
  /** The layout asked of the agent per slide. */
  const [layouts, setLayouts] = useState<Record<string, TDeckLayout>>({});
  /** Bumped by every own write to a board: the strip's pictures are drawn anew. */
  const [edits, setEdits] = useState(0);
  /** The steps of the slide on screen (the board's ready) and the one the show or the recording stands at. */
  const [steps, setSteps] = useState(0);
  /** Moves with each `ready` of the board on screen: the stage fades a new slide in once it is drawn. */
  const [painted, setPainted] = useState(0);
  const [playStep, setPlayStep] = useState(0);
  /** The recording just made: «Готово» and the montage show it. */
  const [rec, setRec] = useState<IDeckRecording | null>(null);
  const [paused, setPaused] = useState(false);
  const pointerListeners = useRef(new Set<(pointer: IDeckPointer) => void>());
  const heldAt = useRef(0);
  /** The board's answers to «which block is here», by request. */
  const hits = useRef(new Map<number, (label: string) => void>());
  const hitReq = useRef(0);
  /** The pointer is on the block frame's buttons; the board said «over nothing» meanwhile. */
  const hot = useRef(false);
  const lostHover = useRef(false);

  const index = deckIndexOf(boards, path);
  const total = boards.length;
  const current = boards[index] ?? null;
  const slide = deck?.slides.find((s) => s.path === current?.path) ?? deck?.slides[index] ?? null;
  const n = index + 1;
  // The agent of the deck's chat at work: blank slides are being made, not left empty.
  const busy = stage?.busy === true;
  const slides = deck?.slides ?? null;
  const hold = deckHold({ total, slide, busy });
  const playBlock = deckPlayBlock({ total, slides, busy });
  /** A slide to talk about: the chip «Слайд N» goes with it, not with a placeholder. */
  const drawn = hold === null && current !== null;
  const w = board?.w ?? slide?.w ?? DECK_BOARD_W;
  const h = board?.h ?? slide?.h ?? DECK_BOARD_H;
  const edit = screen === DECK_SCREEN.edit;
  const ctx: IDeckContext | null = drawn ? { slide: n, label: sel?.label ?? null } : null;
  const ctxLabel = sel
    ? `${t('next.deck.stage.slide', { n })} · ${sel.label}`
    : t('next.deck.stage.slide', { n });
  const themeRef = useThemeVars<HTMLDivElement>(
    deck ? deckTheme(deck.settings).vars : DECK_THEMES[0].vars,
    deck?.settings.font,
  );

  // A new slide starts with nothing selected or hovered; the board says its size again when it is ready.
  const [shownPath, setShownPath] = useState(path);
  if (shownPath !== path) {
    setShownPath(path);
    setSel(null);
    setHover(null);
    setDrag(null);
    setSteps(0);
    setPlayStep(0);
    if (sheet === DECK_SHEET.edit) setSheet(null);
  }
  // Leaving the editor drops what belongs to it.
  const [shownScreen, setShownScreen] = useState(screen);
  if (shownScreen !== screen) {
    setShownScreen(screen);
    setSel(null);
    setHover(null);
    setPop(null);
    setSheet(null);
    setPlayStep(0);
  }
  // A comment is left in the editor and on the guest's link; the other screens drop the one being started.
  const commentable = edit || screen === DECK_SCREEN.link;
  useEffect(() => {
    if (commentable) return;
    stopDeckPick(id);
    closeDeckDraft(id);
  }, [commentable, id]);

  // A press anywhere in the app outside the selection's own tools ends the edit in place (the board saves the text): the
  // board hears only presses inside its frame, and its focus leaving the frame keeps the edit for those tools.
  const hasSel = sel !== null;
  const postRef = useRef(post);
  useLayoutEffect(() => {
    postRef.current = post;
  });
  useEffect(() => {
    if (!hasSel) return;
    const down = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (target?.closest(DECK_SEL_TOOLS)) return;
      postRef.current({ type: 'tk:deck-done' });
      setSel(null);
    };
    document.addEventListener('pointerdown', down, true);
    return () => document.removeEventListener('pointerdown', down, true);
  }, [hasSel]);

  const go = (to: number) => {
    const target = boards[to];
    if (target && target.path !== path) onGo(target.path);
  };
  const step = (by: number) => {
    const to = deckStep(index, by, total);
    if (to !== null) go(to);
  };
  const togglePop = (which: TPop) => setPop((now) => (now === which ? null : which));
  const openPanel = (on: TDeckTab) => {
    setTab(on);
    setPanel((now) => (now === DECK_PANEL.off ? DECK_PANEL.dock : now));
  };
  const back = () => {
    if (!edit) {
      setScreen(DECK_SCREEN.edit);
      setLfull(false);
      return;
    }
    if (pane) pane.onBack();
    else goTo({ kind: ROUTE.designly });
  };
  const startPick = () => {
    setPop(null);
    setSel(null);
    startDeckPick(id);
  };
  const ask = (words: string) => stage?.ask?.(words);
  /** «Попросить…»: the chat about the block, its field in focus. */
  const askAgent = () => {
    if (embed) {
      stage?.context?.({ label: ctxLabel, text: sel?.label ?? '' });
      focusComposer(document);
      return;
    }
    openPanel(DECK_TAB.chat);
    focusDeckChat();
  };
  /** A point of the board (its px) in percent of the slide, and on the screen. */
  const percentOf = (x: number, y: number) => ({ px: (x / w) * 100, py: (y / h) * 100 });
  const screenOf = (x: number, y: number) => {
    const box = frameRef.current?.getBoundingClientRect();
    if (!box) return { left: 0, top: 0 };
    return { left: box.left + (x / w) * box.width, top: box.top + (y / h) * box.height };
  };
  /** Which block the board has at a point (percent of the slide); '' when it does not answer in time. */
  const hit = (x: number, y: number) =>
    new Promise<string>((resolve) => {
      const req = (hitReq.current += 1);
      hits.current.set(req, (label) => {
        hits.current.delete(req);
        resolve(label);
      });
      post({ type: 'tk:deck-hit', req, x: (x / 100) * w, y: (y / 100) * h });
      window.setTimeout(() => hits.current.get(req)?.(''), HIT_MS);
    });
  const shorter = () => {
    if (!sel) return;
    deckToast(t('next.deck.toolbar.shorterToast'));
    ask(t('next.deck.toolbar.shorterAsk', { n, label: sel.label }));
  };

  /** The board shows its source again: a hand edit's live preview the source did not take is gone. */
  const reloadBoard = () => {
    // The plain address of the open slot (the first frame's ticket address is spent).
    const plain = path === null ? null : props.frameSrc(path);
    if (frameRef.current && plain !== null) frameRef.current.src = plain;
  };
  // The slide on screen the agent has just drawn (blank a moment ago): its frame is read again, the drawing is not left
  // to the board's own live update.
  const drawnAt = useRef<{ id: string; blank: boolean } | null>(null);
  useEffect(() => {
    const was = drawnAt.current;
    drawnAt.current = slide ? { id: slide.id, blank: slide.blank } : null;
    if (slide && was?.id === slide.id && was.blank && !slide.blank) reloadBoard();
    // reloadBoard reads the frame through a ref; only the slide turning from blank to drawn matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slide?.id, slide?.blank]);

  /**
   * Writes a hand edit into the board on screen; the board redraws itself on success. A refusal or a failure is said
   * and the board is read anew, so the live preview of the text, style or move does not stay as if it were written.
   */
  const patch = (change: TDeckPatch) => {
    if (!slide) return;
    patchDeck(machine, id, slide.id, change).then(
      (result) => {
        if (result.ok) {
          setEdits((v) => v + 1);
          return;
        }
        reloadBoard();
        toast(t(result.code === 'dynamic' ? 'next.deck.edit.dynamic' : 'next.deck.edit.changed'), {
          tone: 'warning',
        });
      },
      (failure: Error) => {
        reloadBoard();
        toast(t('next.deck.edit.failed'), { tone: 'error', description: failure.message });
      },
    );
  };

  /** A block's move as written: board px, in px where the block sits in a container of its own. */
  const movePatch = (d: IDeckDrag): TDeckPatch =>
    d.px
      ? { op: 'move', tpl: d.tpl, was: d.was, x: d.x, y: d.y, unit: 'px' }
      : { op: 'move', tpl: d.tpl, was: d.was, x: d.x, y: d.y };

  const styleBlock = (prop: string, value: string | null) => {
    if (!sel || !slide) return;
    const key = `${slide.id}:${sel.tpl}`;
    post({ type: 'tk:deck-style', tpl: sel.tpl, prop, value });
    const look = looks[key] ?? {};
    const next: IDeckTextLook = { ...look };
    if (prop === 'font-weight') next.bold = value !== null;
    if (prop === 'font-style') next.italic = value !== null;
    if (prop === 'color') next.color = value;
    if (prop === 'font-size' && value) next.size = Number.parseFloat(value);
    if (prop === 'font-family') {
      const font = Object.entries({ inter: 'Inter', golos: 'Golos', serif: 'Georgia' }).find(([, head]) =>
        value?.includes(head),
      );
      if (font) next.font = font[0] as IDeckTextLook['font'];
    }
    setLooks({ ...looks, [key]: next });
    patch({ op: 'style', tpl: sel.tpl, was: sel.was, style: { [prop]: value } });
  };

  const onBoard = (message: TDeckBoardMessage) => {
    switch (message.k) {
      case 'ready':
        setBoard({ w: message.w, h: message.h });
        setSteps(message.steps);
        setPainted((v) => v + 1);
        return;
      case 'hover':
        if (drag !== null || !edit || picking) return;
        if (message.tpl === null || message.rect === null) {
          lostHover.current = true;
          if (!hot.current) setHover(null);
          return;
        }
        lostHover.current = false;
        setHover({
          tpl: message.tpl,
          name: message.name,
          rect: message.rect,
          moved: message.moved,
          movable: message.movable,
          tx: message.tx,
          ty: message.ty,
          was: message.was,
          px: message.px,
        });
        return;
      case 'select':
        if (!edit || picking) return;
        setSel({
          tpl: message.tpl,
          label: message.label,
          kind: message.kind,
          size: message.size,
          rect: message.rect,
          was: message.was,
        });
        if (phone) setSheet(DECK_SHEET.edit);
        return;
      case 'tap':
        if (edit && phone && !picking) setSheet(DECK_SHEET.edit);
        return;
      case 'deselect':
        if (Date.now() - heldAt.current < HOLD_MS || sheet === DECK_SHEET.edit) return;
        setSel(null);
        return;
      case 'text':
        if (message.html !== message.was)
          patch({ op: 'text', tpl: message.tpl, was: message.was, html: message.html });
        return;
      case 'dynamic':
        toast(t('next.deck.edit.dynamic'));
        return;
      case 'drag': {
        const moved: IDeckDrag = {
          tpl: message.tpl,
          x: message.x,
          y: message.y,
          guides: message.guides,
          was: message.was,
          px: message.px,
        };
        if (!message.end) {
          setDrag(moved);
          return;
        }
        setDrag(null);
        patch(movePatch(moved));
        return;
      }
      case 'hit':
        hits.current.get(message.req)?.(message.label);
        return;
      case 'pointer':
        pointerListeners.current.forEach((fn) => fn({ x: message.x, y: message.y, down: message.down }));
        return;
      case 'press':
        feedDeckRing(id, message.phase === DECK_PRESS.ring ? percentOf(message.x, message.y) : null);
        return;
      case 'held': {
        feedDeckRing(id, null);
        if (!commentable) return;
        const at = percentOf(message.x, message.y);
        const near = screenOf(message.x, message.y);
        void hit(at.px, at.py).then((label) => openDeckDraft(id, { x: at.px, y: at.py, label, at: near }));
        return;
      }
      default:
        return;
    }
  };

  const onKey = (key: TDeckKey) => {
    switch (key) {
      case DECK_KEY.next:
        step(1);
        return;
      case DECK_KEY.prev:
        step(-1);
        return;
      case DECK_KEY.comment:
        startPick();
        return;
      case DECK_KEY.full:
        setLmode(DECK_OPENS.slides);
        setLfull(true);
        return;
      case DECK_KEY.show:
        if (playBlock !== null) return;
        setScreen(DECK_SCREEN.show);
        return;
      case DECK_KEY.rec:
        if (playBlock !== null) return;
        setScreen(DECK_SCREEN.rec);
        go(0);
        return;
      case DECK_KEY.escape:
        if (escapeDeckComment(id)) return;
        if (lfull) {
          setLfull(false);
          return;
        }
        if (screen === DECK_SCREEN.show || screen === DECK_SCREEN.rec || screen === DECK_SCREEN.cut) {
          setScreen(DECK_SCREEN.edit);
          return;
        }
        if (sheet !== null) {
          setSheet(null);
          return;
        }
        if (sel) post({ type: 'tk:deck-done' });
        setPop(null);
        setSel(null);
    }
  };

  // The show and the recording take their keys themselves (on the window): a key pressed over the board goes there too.
  const playing = screen === DECK_SCREEN.show || screen === DECK_SCREEN.rec;
  useDeckBridge(frameRef, src, onBoard, (key) => {
    if (playing) {
      window.dispatchEvent(new KeyboardEvent('keydown', { key }));
      return;
    }
    const action = deckKeyAction(key, screen);
    if (action !== null) onKey(action);
  });
  useDeckKeys(!playing, screen, onKey);

  // The board on screen is in the deck's mode of the screen; a board loaded later is told when it loads.
  const mode = MODE_OF[screen] ?? null;
  // The show and the recording uncover a slide's steps one by one; the editor shows them all.
  const tellMode = () => {
    post({ type: 'tk:tweaks-host', on: true, open: false });
    if (mode !== null) post({ type: 'tk:deck', on: true, mode });
    if (playing) post({ type: 'tk:deck-step', n: playStep });
  };
  useEffect(() => {
    if (mode !== null) post({ type: 'tk:deck', on: true, mode });
    if (playing) post({ type: 'tk:deck-step', n: 0 });
    // post reads the frame on screen through a ref; only the mode changing must send it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  const host: IDeckPlayHost = {
    slides: deck?.slides ?? [],
    index,
    onGo: go,
    steps,
    step: playStep,
    onStep: (n) => {
      setPlayStep(n);
      post({ type: 'tk:deck-step', n });
    },
    slideRect: () => frameRef.current?.getBoundingClientRect() ?? null,
    onPointer: (listener) => {
      pointerListeners.current.add(listener);
      return () => pointerListeners.current.delete(listener);
    },
    setPointer: (on) => post({ type: 'tk:deck-pointer', on }),
    frameSrc: props.frameSrc,
  };
  const toEditor = () => setScreen(DECK_SCREEN.edit);

  const saveNotes = async (text: string) => {
    if (!slide) return;
    putDeckSlide(id, await putDeckNotes(machine, id, slide.id, text));
  };
  const saveSettings = async (change: Partial<IDeckSettings>) => {
    putDeckSettingsLocal(id, await putDeckSettings(machine, id, change));
  };
  const pickLayout = (layout: TDeckLayout) => {
    if (!slide) return;
    setLayouts({ ...layouts, [slide.id]: layout });
    ask(t('next.deck.style.layoutAsk', { n, layout: t(LAYOUT_KEYS[layout]) }));
  };
  /** An export; an MP4 is of `recId`, else of the last finished recording. */
  const runExport = (format: TDeckExport, recId?: string) => {
    setPop(null);
    const of = format === 'mp4' && deck ? (recId ?? lastRecording(deck) ?? undefined) : undefined;
    void exportDeck(machine, id, format, of);
  };
  const listensTo = dict.state.phase === DICT.listening ? dict.state.target : null;
  const dictation: IDeckSubsDictation = {
    page: listensTo?.kind === 'slide' ? listensTo.page : null,
    said: dict.said,
    available: dict.available,
    onMic: (page) => dict.toggle({ kind: 'slide', page }, t('next.deck.subs.doneToast')),
  };

  const crumbs: Record<TDeckScreen, string> = {
    create: t('next.deck.bar.crumbCreate'),
    edit: current?.title ?? '',
    show: current?.title ?? '',
    rec: current?.title ?? '',
    done: t('next.deck.bar.crumbDone'),
    cut: t('next.deck.bar.crumbCut'),
    link: t('next.deck.bar.crumbLink', { n, total }),
  };
  const panelMode = edit && !embed ? panel : DECK_PANEL.off;
  const look = sel && slide ? (looks[`${slide.id}:${sel.tpl}`] ?? {}) : {};
  // The sheet the slide rises above (`--esh` by data-sheet); it comes down as the sheet starts to leave.
  const sheetUp = ((): TDeckSheet | null => {
    if (!phone) return null;
    if (pick.draft) return DECK_SHEET.comment;
    if (edit && sheet !== null && shownSheet.state === PRESENCE.open) return sheet;
    return null;
  })();
  const mdock = phone && edit && panel === DECK_PANEL.off && sheet === null && !pick.draft;
  // A person who manages the project gets the deck's share popover; anyone else the access dialog (ask for edit).
  const onShare = access ? () => (access.manage ? togglePop(POP.share) : access.onOpen()) : undefined;

  return (
    <div
      ref={themeRef}
      className="hx-deck"
      data-screen={screen}
      data-panel={panelMode}
      data-pick={picking ? '1' : '0'}
      data-full={screen === DECK_SCREEN.link && lfull ? '1' : '0'}
      data-drag={drag !== null ? '1' : '0'}
      data-grip={grip ? '1' : undefined}
      data-lmode={screen === DECK_SCREEN.link ? lmode : DECK_OPENS.slides}
      data-es={sheetUp !== null ? '1' : '0'}
      data-sheet={sheetUp ?? undefined}
      data-embed={embed ? '1' : '0'}
      data-paused={screen === DECK_SCREEN.rec && paused ? '1' : '0'}
    >
      <DeckBar
        screen={screen}
        title={title}
        crumb={crumbs[screen]}
        onBack={back}
        onPanel={() => setPanel((now) => (now === DECK_PANEL.off ? DECK_PANEL.dock : DECK_PANEL.off))}
        panelOn={panel !== DECK_PANEL.off}
        onComment={startPick}
        onStyle={() => togglePop(POP.style)}
        styleOn={pop === POP.style}
        onShow={() => onKey(DECK_KEY.show)}
        onRec={() => onKey(DECK_KEY.rec)}
        playBlocked={playBlock === null ? null : t(playBlock)}
        onShare={onShare}
        shareOn={pop === POP.share}
        onMore={() => togglePop(POP.menu)}
        moreOn={pop === POP.menu}
        lmode={lmode}
        onLmode={(m) => {
          setLmode(m);
          if (m === DECK_OPENS.video) setLfull(false);
        }}
        onFull={() => onKey(DECK_KEY.full)}
        onVideoLink={() => onShare?.()}
        onDownloadMp4={() => runExport('mp4', rec?.summary.id)}
      />

      {!embed && (
        <DeckPanel
          mode={panelMode}
          tab={tab}
          onMode={setPanel}
          onTab={setTab}
          stage={stage?.deckChat ?? null}
          slideIndex={index}
          onGo={go}
          ctx={ctx}
          phone={phone}
          machine={machine}
          artifact={id}
          dict={dict}
          comments={comments}
          site={stage}
        />
      )}

      <DeckStrip
        boards={boards}
        index={index}
        onGo={go}
        thumb={(board) => {
          const of = slideOfBoard(slides, board);
          // A slide's picture is keyed on its file's version: a picture of the empty board is never kept.
          if (of === null || of.blank) return null;
          return thumbPath(machine, id, board, `${of.version}.${edits}`, true);
        }}
        making={(board) => busy && slideOfBoard(slides, board)?.blank === true}
        pending={busy && total === 0 ? DECK_PENDING_THUMBS : 0}
        onAdd={stage?.ask ? () => ask(t('next.deck.strip.addAsk', { n })) : undefined}
      />

      <DeckStage
        src={src}
        w={w}
        h={h}
        n={n}
        hold={hold}
        frameRef={frameRef}
        onLoad={tellMode}
        painted={painted}
        error={props.slotError}
        onReload={props.onReload}
        overlay={
          <>
            {commentable && hold === null && (
              <DeckCommentLayer
                artifact={id}
                slideIndex={index}
                comments={comments}
                hit={hit}
                phone={phone}
                onAdded={() => {
                  if (edit) openPanel(DECK_TAB.comments);
                }}
              />
            )}
            {edit && !picking && hold === null && (
              <DeckOverlay
                hover={hover}
                drag={drag}
                w={w}
                h={h}
                onDrag={(d) => {
                  setGrip(true);
                  setDrag(d);
                  post({ type: 'tk:deck-move', tpl: d.tpl, x: d.x, y: d.y });
                }}
                onDrop={(d) => {
                  setGrip(false);
                  setDrag(null);
                  setHover(null);
                  patch(movePatch(d));
                }}
                onHot={(on) => {
                  hot.current = on;
                  if (!on && lostHover.current && drag === null) setHover(null);
                }}
                onReset={(at) => {
                  post({ type: 'tk:deck-move', tpl: at.tpl, x: 0, y: 0 });
                  setHover(null);
                  patch({ op: 'reset', tpl: at.tpl, was: at.was });
                }}
              />
            )}
            {edit && sel && !picking && drag === null && !phone && (
              <DeckToolbar
                sel={sel}
                look={look}
                onStyle={styleBlock}
                onShorter={shorter}
                askLabel={embed ? t('next.deck.toolbar.toChat') : t('next.deck.toolbar.ask')}
                onAsk={askAgent}
                onHold={() => {
                  heldAt.current = Date.now();
                }}
              />
            )}
          </>
        }
        below={
          <>
            {edit && subsOn && drawn && (
              <DeckNotesLine
                slide={slide}
                n={n}
                phone={phone}
                dictation={dictation}
                onSave={saveNotes}
                onAll={() => openPanel(DECK_TAB.notes)}
                onSheet={() => {
                  setSel(null);
                  setSheet(DECK_SHEET.notes);
                }}
              />
            )}
            {edit && deck === null && deckError !== null && (
              <div className="hx-deck-subs__failed" role="alert">
                {t('next.deck.stage.deckFailed')}
                <small>{deckError}</small>
              </div>
            )}
            {(edit || screen === DECK_SCREEN.link) && total > 0 && (
              <DeckPager
                n={n}
                total={total}
                onPrev={() => step(-1)}
                onNext={() => step(1)}
                notes={edit ? { on: subsOn, onToggle: () => setSubsOn(!subsOn) } : undefined}
              />
            )}
          </>
        }
      />

      {screen === DECK_SCREEN.show && (
        <DeckShow
          host={host}
          minutes={deck?.settings.minutes ?? 0}
          onExit={toEditor}
          onRecord={() => onKey(DECK_KEY.rec)}
        />
      )}
      {screen === DECK_SCREEN.rec && (
        <DeckRec
          host={host}
          machine={machine}
          artifact={id}
          onFinished={(made) => {
            setRec(made);
            setPaused(false);
            setScreen(DECK_SCREEN.done);
          }}
          onExit={() => {
            setPaused(false);
            toEditor();
          }}
          onPaused={setPaused}
        />
      )}
      {screen === DECK_SCREEN.done && rec && (
        <DeckDone
          machine={machine}
          artifact={id}
          recId={rec.summary.id}
          initial={rec}
          slides={deck?.slides ?? []}
          frameSrc={props.frameSrc}
          onClose={toEditor}
          onMontage={() => setScreen(DECK_SCREEN.cut)}
          onDownload={(recId) => {
            runExport('mp4', recId);
            toEditor();
          }}
        />
      )}
      {screen === DECK_SCREEN.cut && rec && (
        <DeckCut
          machine={machine}
          artifact={id}
          recId={rec.summary.id}
          initial={rec}
          slides={deck?.slides ?? []}
          frameSrc={props.frameSrc}
        />
      )}

      {shownPop.value === POP.style && (
        <DeckStyle
          state={shownPop.state}
          settings={deck?.settings ?? null}
          onSettings={saveSettings}
          layout={slide ? (layouts[slide.id] ?? null) : null}
          onLayout={pickLayout}
        />
      )}
      {shownPop.value === POP.share && (
        <DeckShare
          machine={machine}
          project={id}
          title={title}
          closing={shownPop.state === PRESENCE.closing}
          onAsGuest={(flags) => {
            setPop(null);
            setLmode(flags.opens === DECK_OPENS.video ? DECK_OPENS.video : DECK_OPENS.slides);
            setLfull(flags.opens === DECK_OPENS.full);
            setScreen(DECK_SCREEN.link);
          }}
        />
      )}
      {shownPop.value === POP.menu && (
        <DeckMenu
          state={shownPop.state}
          deck={deck}
          edit={edit}
          onExport={runExport}
          onGuest={() => {
            setPop(null);
            setLmode(DECK_OPENS.slides);
            setScreen(DECK_SCREEN.link);
          }}
          onPanel={() => {
            setPop(null);
            setPanel((now) => (now === DECK_PANEL.off ? DECK_PANEL.dock : DECK_PANEL.off));
          }}
          onStyle={() => setPop(POP.style)}
          onShare={
            onShare &&
            (() => {
              setPop(null);
              onShare();
            })
          }
        />
      )}

      {mdock && (
        <DeckMdock onAsk={askAgent} onComments={() => openPanel(DECK_TAB.comments)} count={comments.open} />
      )}

      {phone && edit && shownSheet.value === DECK_SHEET.edit && sel && (
        <EditSheet
          state={shownSheet.state}
          machine={machine}
          n={n}
          sel={sel}
          look={look}
          onStyle={styleBlock}
          onShorter={shorter}
          onDone={() => {
            post({ type: 'tk:deck-done' });
            setSheet(null);
            setSel(null);
          }}
          onAsk={
            stage?.ask
              ? (words) => {
                  stage.context?.({ label: ctxLabel, text: sel.label });
                  ask(words);
                }
              : undefined
          }
        />
      )}
      <NotesSheet
        machine={machine}
        artifact={id}
        slide={slide}
        slideIndex={index}
        open={phone && edit && sheet === DECK_SHEET.notes}
        onClose={() => setSheet(null)}
        dict={dict}
        ask={stage?.ask}
      />

      <DeckToast />
    </div>
  );
};

export default DeckView;
