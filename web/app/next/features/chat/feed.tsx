// The conversation: the feed of turns over the dock (questions, queue, composer). It opens at the bottom and stays
// there while the person reads the bottom, even as a turn grows or the dock above the field does (the design's
// --dock-pad); scrolled away it shows «To new». Older turns load when the top comes near, and the turn the person
// looks at stays where it was. Long feeds draw only the turns around the viewport (feed-window.ts).
import {
  memo,
  startTransition,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  coversWindow,
  ESTIMATED_TURN_PX,
  feedWindow,
  OVERSCAN,
  VIRTUAL_FROM,
  type IFeedWindow,
} from '@/next/features/chat/feed-window';
import { shortModelName } from '@/next/features/composer/agents';
import { RefContext, type IRefContext } from '@/next/features/chat/ref-chip';
import { docPathsOf } from '@/next/features/chat/refs';
import {
  OutputsContext,
  SHOWN_BY_DEFAULT,
  type IOutputsContext,
  type TShownKinds,
} from '@/next/features/chat/outputs';
import type { TurnOutput } from '@/next/features/chat/types';
import NowBlock from '@/next/features/chat/now';
import WaitingLine from '@/next/features/chat/waiting';
import TurnView, { type ITurnHandlers } from '@/next/features/chat/turn';
import type { FeedDensity, FeedItem, Ms, RefInfo, RefKind } from '@/next/features/chat/types';
import AutoLine from '@/next/features/chat/auto-line';
import ClearedLine from '@/next/features/chat/cleared-line';
import ModelSwitchRow from '@/next/features/chat/model-switch';
import OriginRow from '@/next/features/chat/origin-row';
import PeerNotes from '@/next/features/chat/peer-note';
import { RewindablePrompt, RewoundRow } from '@/next/features/chat/rewind';
import SystemNote, { TerminalStopNote } from '@/next/features/chat/system-note';
import GoalNote from '@/next/features/chat/goal-note';
import UserMessage from '@/next/features/chat/user-message';
import type { BackgroundTask } from '@/next/protocol/v2';
import { boxOf, claimOrigin, flyCopy, hasOrigin, retargetFlights, slideFrom } from '@/next/ui/flight';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { CatchingUp, FeedLoading, FeedStart, OlderLoading } from '@/next/features/chat/feed-status';
import { t } from '@/next/i18n';
import useFeedAnchor from '@/next/features/chat/use-feed-anchor';
import '@/next/styles/ui.css';
import '@/next/styles/chat.css';

/** Within this many px of the bottom the person reads the bottom. */
const BOTTOM_SLACK_PX = 32;
/** Older turns start loading this far from the top, so they are there before the person reaches it. */
const OLDER_AHEAD_PX = 800;
/** A turn that grows at the bottom by less than this just follows; by more, the feed slides to its new place. */
const SLIDE_MIN_PX = 6;
/** How many of the last items may be new at once (a message, its turn, the live line). */
const NEW_TAIL = 4;
/** The turn a search result pointed at shows itself this long (hx-reveal in chat.css). */
const REVEAL_MS = 1800;
/** Turns drawn around the one revealed, when a long feed draws only a window. */
const REVEAL_AROUND = 3;

/** Measured heights of drawn turns, by key: what the spacers of a windowed feed are made of. */
class HeightCache {
  private heights = new Map<string, number>();
  get(key: string): number | undefined {
    return this.heights.get(key);
  }
  set(key: string, height: number): boolean {
    if (this.heights.get(key) === height) return false;
    this.heights.set(key, height);
    return true;
  }
  view(): ReadonlyMap<string, number> {
    return this.heights;
  }
}

type THandlers = Omit<ITurnHandlers, 'onToggle'>;

export interface IFeedProps extends THandlers {
  /** The turns and outgoing messages, oldest first; null while the conversation loads. */
  items: FeedItem[] | null;
  /** The first load failed: the feed says so and offers to try again. */
  failed?: boolean;
  /** Why it did not load, in words. */
  failedReason?: string | null;
  onRetryLoad?: () => void;
  hasOlder: boolean;
  loadingOlder: boolean;
  /**
   * The feed on screen is a copy (the browser's, from before a reload) that the runtime has not brought up to date
   * yet: newer turns may still come. A thin bar over the feed says so until it has.
   */
  catchingUp?: boolean;
  /** The last page of older turns did not come: the top says why and offers to try again, nothing asks it by scroll. */
  olderFailed?: string | null;
  onLoadOlder: () => void;
  /**
   * A message a search result points at: the feed brings its turn into view (loading older turns until it is there)
   * and shows it for a moment, then says it is served. A turn that is not in the chat any more is given up on.
   */
  reveal?: { turnId: string | null; entryId: string; nonce: number } | null;
  onRevealed?: (nonce: number) => void;
  density: FeedDensity;
  offline: boolean;
  /** Turns and messages newer than this come in with an animation; history that loads does not. */
  enterAfter: Ms;
  machineName: string;
  /** Since when the agent works, by its state; null when it does not. Shown at the bottom while no live turn is. */
  workingSince?: Ms | null;
  /** The agent is done talking and waits for this many background agents; a tap shows them. Shown in place of the timer. */
  waiting?: { agents: readonly BackgroundTask[]; onOpen: () => void } | null;
  /** The chat's terminal was ended and nothing runs it since: what the quiet line at the bottom says. */
  terminalStop?: string | null;
  /** The chat's goal was reached: the divider after the last turn says what it was and can resume it. */
  goalReached?: { text: string; onContinue: () => Promise<unknown> } | null;
  /** The card of a chat held on its usage limit, under the last turn; draws nothing when the chat is not held. */
  limit?: ReactNode;
  /** A terminal provider failure which waits for a person's target choice. */
  terminalFailure?: ReactNode;
  refs?: Readonly<Record<string, RefInfo>>;
  /** A model's id as people say it, for the lines of changed models. */
  modelLabel?: (id: string) => string;
  onOpenRef?: (kind: RefKind, label: string, href: string | null) => void;
  /** A plain file of a turn, in the right pane. */
  onOpenFile?: (path: string) => void;
  /** A thing a turn made (a page, a design, a document, a merge request, a stand), in the right pane. */
  onOpenOutput?: (output: TurnOutput) => void;
  /** A document file of a turn, shared (the dialog is the host's). */
  onShareFile?: (path: string) => void;
  /** Which kinds of what a turn made get a card (Settings); everything but the plain files when not given. */
  shown?: TShownKinds;
  /** Where a file the agent handed over is saved from. */
  downloadUrl?: (path: string) => string;
  /** The chat the chips ask where a merge request opens; without it they ask nothing. */
  /** What stands over the bottom of the feed: questions, queue, composer. */
  dock?: ReactNode;
}

/**
 * A turn that one of the person's messages started keeps that message's row: the bubble that went out becomes the
 * turn's prompt in place, with no row leaving and another coming in.
 */
const rowKey = (item: FeedItem): string =>
  item.kind === 'turn' && item.prompt?.clientId ? item.prompt.clientId : item.key;

/** The client id of the message a row shows: the bubble that went out is the turn's prompt later. */
const clientOf = (item: FeedItem): string | null => {
  if (item.kind === 'outgoing') return item.message.clientId;
  if (item.kind === 'turn') return item.prompt?.clientId ?? null;
  return null;
};

interface IRowProps {
  item: FeedItem;
  open: boolean;
  enter: boolean;
  offline: boolean;
  machineName: string;
  handlers: ITurnHandlers;
  modelLabel: (id: string) => string;
  measure: (element: HTMLDivElement | null) => void;
}

const Row = memo(({ item, open, enter, offline, machineName, handlers, modelLabel, measure }: IRowProps) => {
  const client = clientOf(item);
  // A bubble that flies in from the field or the queue brings itself; the row's own entrance would play over it. Fixed
  // when the row is made: the row does not play again when the message is confirmed.
  const [flies] = useState(() => enter && client !== null && hasOrigin(client));
  return (
    <div
      ref={measure}
      className="hx-turnwrap"
      data-key={item.key}
      data-client={client ?? undefined}
      data-enter={(enter && !flies) || undefined}
      data-flight={flies || undefined}
    >
      {item.kind === 'switch' && <ModelSwitchRow item={item} label={modelLabel} />}
      {item.kind === 'rewound' && <RewoundRow item={item} />}
      {item.kind === 'origin' && <OriginRow item={item} />}
      {/* The runtime went on by itself with this message: a quiet line, in place of the bubble when it wrote it. */}
      {(item.kind === 'turn' || item.kind === 'outgoing') && item.auto && (
        <AutoLine why={item.auto.why} pending={item.kind === 'outgoing'} />
      )}
      {(item.kind === 'turn' || item.kind === 'outgoing') && item.cleared && (
        <ClearedLine what={item.cleared} />
      )}
      {/* The bubble is the same element going out and as the turn's prompt: it settles in place. */}
      {item.kind === 'outgoing'
        ? !item.auto?.own && (
            <UserMessage
              text={item.message.text}
              attachments={item.message.attachments}
              outgoing={item.message}
              offline={offline}
              onRetry={handlers.onRetryMessage}
              onResend={handlers.onResendMessage}
              onDelete={handlers.onDeleteMessage}
            />
          )
        : item.kind === 'turn' &&
          item.prompt &&
          !item.auto?.own && (
            <RewindablePrompt turnId={item.key} text={item.prompt.text}>
              <UserMessage
                text={item.prompt.text}
                attachments={item.prompt.attachments}
                outgoing={null}
                own={item.prompt.clientId !== null}
                offline={offline}
                onRetry={handlers.onRetryMessage}
                onResend={handlers.onResendMessage}
                onDelete={handlers.onDeleteMessage}
              />
            </RewindablePrompt>
          )}
      {item.kind === 'turn' && item.peer && <PeerNotes entry={item.peer} />}
      {item.kind === 'turn' && item.note && <SystemNote entry={item.note} />}
      {item.kind === 'turn' && (
        <TurnView item={item} open={open} machineName={machineName} handlers={handlers} />
      )}
    </div>
  );
});

/** Room for the turns a long feed does not draw; its height is geometry, set on the element like a gesture's. */
const Spacer = ({ px }: { px: number }) => (
  <div
    className="hx-feed__spacer"
    aria-hidden="true"
    ref={(element) => element?.style.setProperty('--hx-spacer', `${Math.round(px)}px`)}
  />
);

const startOf = (item: FeedItem): Ms => {
  if (item.kind === 'turn') return item.turn.startedAt;
  if (item.kind === 'switch' || item.kind === 'rewound' || item.kind === 'origin') return item.at;
  return item.message.createdAt;
};

const isNew = (item: FeedItem, after: Ms): boolean => startOf(item) > after;

/** The drawn range by keys, so turns that load above do not shift what is drawn; null: everything. */
interface IKeyWindow {
  startKey: string;
  /** null: up to the newest turn, so new turns join the drawn range. */
  endKey: string | null;
}

/**
 * The client ids of the last items that the feed has not seen before; it remembers them. The first call only learns:
 * what the feed opens with is not new.
 */
const freshClients = (items: FeedItem[] | null, seen: { current: Set<string> | null }): string[] => {
  const tail = (items ?? []).slice(-NEW_TAIL).flatMap((item) => clientOf(item) ?? []);
  if (seen.current === null) {
    seen.current = new Set(tail);
    return [];
  }
  const known = seen.current;
  const fresh = tail.filter((client) => !known.has(client));
  tail.forEach((client) => known.add(client));
  return fresh;
};

const Feed = (props: IFeedProps) => {
  const {
    items,
    failed = false,
    failedReason = null,
    onRetryLoad,
    hasOlder,
    loadingOlder,
    catchingUp = false,
    density,
    offline,
    enterAfter,
    machineName,
    workingSince = null,
    waiting = null,
    terminalStop = null,
    goalReached = null,
    modelLabel = shortModelName,
  } = props;
  const scrollerRef = useRef<HTMLDivElement>(null);
  const innerRef = useRef<HTMLDivElement>(null);
  const dockRef = useRef<HTMLDivElement>(null);
  const [heights] = useState(() => new HeightCache());
  const [toggled, setToggled] = useState<ReadonlySet<string>>(() => new Set());
  const [range, setRange] = useState<IKeyWindow | null>(null);
  const [away, setAway] = useState(false);
  const atBottom = useRef(true);
  const lastScrollHeight = useRef(0);
  /** The scroll position the last scroll event saw: the reader leaves the bottom only by moving up from it. */
  const lastTop = useRef(0);
  /** The messages whose rows the feed has seen: one that is not here yet is new, and may fly in. */
  const seenClients = useRef<Set<string> | null>(null);
  const firstKey = useRef<string | null>(null);
  const latest = useRef(props);
  const frame = useRef(0);

  useLayoutEffect(() => {
    latest.current = props;
  });

  const keys = useMemo(() => (items ?? []).map((item) => item.key), [items]);
  const virtual = keys.length >= VIRTUAL_FROM;

  // The drawn slice: indices from the key range, spacers from the measured heights of what is not drawn.
  let win: Pick<IFeedWindow, 'start' | 'end' | 'top' | 'bottom'> = {
    start: 0,
    end: keys.length,
    top: 0,
    bottom: 0,
  };
  if (virtual && range) {
    const start = Math.max(0, keys.indexOf(range.startKey));
    const endIndex = range.endKey === null ? keys.length : keys.indexOf(range.endKey) + 1;
    const end = endIndex <= start ? keys.length : endIndex;
    const view = heights.view();
    let top = 0;
    for (let index = 0; index < start; index += 1) top += view.get(keys[index]) ?? ESTIMATED_TURN_PX;
    let bottom = 0;
    for (let index = end; index < keys.length; index += 1)
      bottom += view.get(keys[index]) ?? ESTIMATED_TURN_PX;
    win = { start, end, top, bottom };
  } else if (virtual) {
    win = { start: Math.max(0, keys.length - OVERSCAN * 2), end: keys.length, top: 0, bottom: 0 };
    const view = heights.view();
    for (let index = 0; index < win.start; index += 1) win.top += view.get(keys[index]) ?? ESTIMATED_TURN_PX;
  }

  const noteAnchor = useFeedAnchor(scrollerRef, atBottom);

  // A message a search result pointed at: the turn it is in is drawn (the window moves to it) and brought into view;
  // the older turns are loaded until it is there.
  const { reveal = null, onRevealed, hasOlder: moreOlder, onLoadOlder: loadOlder } = props;
  const olderError = props.olderFailed ?? null;
  const [served, setServed] = useState<number | null>(null);
  const revealAt = reveal ? keys.indexOf(reveal.turnId ?? reveal.entryId) : -1;
  if (reveal && revealAt >= 0 && served !== reveal.nonce) {
    setServed(reveal.nonce);
    if (virtual)
      setRange({
        startKey: keys[Math.max(0, revealAt - REVEAL_AROUND)],
        endKey: keys[Math.min(keys.length - 1, revealAt + REVEAL_AROUND)],
      });
  }
  const revealTimer = useRef(0);
  useEffect(() => {
    if (!reveal || items === null || revealAt >= 0 || loadingOlder) return;
    if (moreOlder && olderError === null) loadOlder();
    else onRevealed?.(reveal.nonce);
  }, [reveal, items, revealAt, loadingOlder, moreOlder, olderError, loadOlder, onRevealed]);
  useLayoutEffect(() => {
    if (!reveal || served !== reveal.nonce || revealAt < 0) return;
    const scroller = scrollerRef.current;
    const row = scroller?.querySelector<HTMLElement>(`[data-key="${CSS.escape(keys[revealAt])}"]`);
    if (!scroller || !row) return;
    atBottom.current = false;
    row.scrollIntoView?.({ block: 'center' });
    row.setAttribute('data-reveal', 'true');
    window.clearTimeout(revealTimer.current);
    revealTimer.current = window.setTimeout(() => row.removeAttribute('data-reveal'), REVEAL_MS);
    onRevealed?.(reveal.nonce);
  }, [reveal, served, revealAt, keys, range, onRevealed]);
  useEffect(() => () => window.clearTimeout(revealTimer.current), []);

  const stick = useCallback(() => {
    const scroller = scrollerRef.current;
    if (scroller && atBottom.current) scroller.scrollTop = scroller.scrollHeight;
  }, []);

  /** Re-reads which turns to draw for the scroll position; renders only when the viewport nears the drawn edge. */
  const rewindow = useCallback(() => {
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(() => {
      const scroller = scrollerRef.current;
      const list = (latest.current.items ?? []).map((item) => item.key);
      if (!scroller || list.length < VIRTUAL_FROM) return;
      const next = feedWindow(list, heights.view(), scroller.scrollTop, scroller.clientHeight);
      const nextRange: IKeyWindow = {
        startKey: list[next.start],
        endKey: next.end >= list.length ? null : list[next.end - 1],
      };
      // A transition: the new turns render in slices between frames; they are RESERVE turns away from the viewport.
      startTransition(() =>
        setRange((current) => {
          if (current) {
            const start = list.indexOf(current.startKey);
            const end = current.endKey === null ? list.length : list.indexOf(current.endKey) + 1;
            if (start >= 0 && end > start && coversWindow(start, end, next, list.length)) return current;
          }
          return nextRange;
        }),
      );
    });
  }, [heights]);

  // Turns drawn for the first time above the viewport take their real height in place of the spacer's estimate; the
  // difference goes to the scroll position, so what the reader sees stays put (Safari has no scroll anchoring).
  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller || atBottom.current) return;
    const top = scroller.getBoundingClientRect().top;
    let delta = 0;
    for (const row of scroller.querySelectorAll<HTMLElement>('.hx-turnwrap')) {
      const key = row.dataset.key ?? '';
      if (heights.get(key) !== undefined) continue;
      const rect = row.getBoundingClientRect();
      if (rect.top >= top) break;
      heights.set(key, rect.height);
      delta += rect.height - ESTIMATED_TURN_PX;
    }
    if (Math.abs(delta) >= 1) scroller.scrollTop += delta;
  }, [range, heights]);

  // One observer measures every drawn turn and the dock; a change keeps the bottom in view when it was. Made on first
  // use, outside render (refs are read in its callback).
  const observerRef = useRef<ResizeObserver | null>(null);
  const observe = useCallback(
    (element: Element): (() => void) => {
      if (typeof ResizeObserver === 'undefined') return () => {};
      observerRef.current ??= new ResizeObserver((entries) => {
        const scroller = scrollerRef.current;
        // A turn that was drawn and grew (the live line came, a step was added) moves what is under it at once; the
        // dock's own growth follows frame by frame and needs no help.
        let grew = false;
        for (const entry of entries) {
          const target = entry.target as HTMLElement;
          const height = entry.borderBoxSize?.[0]?.blockSize ?? target.offsetHeight;
          if (target === dockRef.current) {
            scroller?.style.setProperty('--dock-pad', `${Math.ceil(height) + 24}px`);
            continue;
          }
          const key = target.dataset.key;
          if (!key) continue;
          const before = heights.get(key);
          if (before !== undefined && height > before) grew = true;
          heights.set(key, height);
        }
        const wasBottom = atBottom.current;
        const from = lastScrollHeight.current;
        stick();
        // A copy on its way aims at where its bubble stands after this change, in this frame.
        retargetFlights();
        if (scroller) {
          const moved = scroller.scrollHeight - from;
          const inner = innerRef.current;
          if (inner && grew && wasBottom && moved >= SLIDE_MIN_PX && moved <= scroller.clientHeight)
            slideFrom(inner, moved);
          lastScrollHeight.current = scroller.scrollHeight;
        }
        noteAnchor();
      });
      const observer = observerRef.current;
      observer.observe(element);
      return () => observer.unobserve(element);
    },
    [heights, stick, noteAnchor],
  );

  useEffect(() => () => observerRef.current?.disconnect(), []);

  const measure = useCallback(
    (element: HTMLDivElement | null) => {
      if (!element) return;
      return observe(element);
    },
    [observe],
  );

  useLayoutEffect(() => {
    const dock = dockRef.current;
    if (!dock) return;
    return observe(dock);
  }, [observe]);

  // Open at the bottom; turns that load above keep the one in view in place; new ones below keep the bottom.
  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller || items === null) return;
    const head = keys[0] ?? null;
    const opened = firstKey.current === null;
    const wasBottom = atBottom.current;
    const from = lastScrollHeight.current;
    if (opened) {
      scroller.scrollTop = scroller.scrollHeight;
      rewindow();
    } else if (head !== firstKey.current && !wasBottom) {
      scroller.scrollTop += scroller.scrollHeight - lastScrollHeight.current;
    } else {
      stick();
    }
    firstKey.current = head;
    // The messages that came in just now: a bubble the person sent flies from the field (or from the queue's row it
    // left) to its place, and the feed slides up to it instead of jumping. History that loads does neither.
    const learning = seenClients.current === null;
    const fresh = freshClients(items, seenClients);
    const flying = learning ? [] : fresh.filter(hasOrigin);
    const inner = innerRef.current;
    if (flying.length > 0 && !wasBottom) {
      // The person sent from up in the history: the feed goes down to the bubble, smoothly.
      atBottom.current = true;
      scroller.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' });
    } else if (!learning && wasBottom) {
      const moved = scroller.scrollHeight - from;
      const rows = [...scroller.querySelectorAll<HTMLElement>('[data-client]')];
      const lands = flying.flatMap((client) => {
        const bubble = rows
          .find((row) => row.dataset.client === client)
          ?.querySelector<HTMLElement>('.hx-msg-user');
        const origin = claimOrigin(client);
        return bubble && origin ? [{ bubble, origin }] : [];
      });
      if (inner && moved >= SLIDE_MIN_PX && moved <= scroller.clientHeight) slideFrom(inner, moved);
      // The copy flies to where the bubble is drawn, read again while it flies: the feed slides, the field shrinks after
      // its text has left and the feed keeps its bottom, so the place moves under the copy and it lands on the bubble.
      for (const { bubble, origin } of lands) flyCopy(bubble, origin, () => boxOf(bubble));
    }
    lastScrollHeight.current = scroller.scrollHeight;
  }, [items, keys, stick, rewindow]);

  // The first page holds only the newest turns: a feed that does not reach OLDER_AHEAD_PX above the viewport never
  // scrolls there, so older turns are asked without a scroll, page after page, until it does or there are none.
  const olderFailed = props.olderFailed ?? null;
  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller || items === null || !hasOlder || loadingOlder || olderFailed) return;
    if (scroller.scrollTop < OLDER_AHEAD_PX) latest.current.onLoadOlder();
  }, [items, hasOlder, loadingOlder, olderFailed]);

  const onScroll = useCallback(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const top = scroller.scrollTop;
    const near = scroller.scrollHeight - top - scroller.clientHeight < BOTTOM_SLACK_PX;
    // The event of a scroll the feed made to its bottom can come after the rows grew under it (a picture loaded, a
    // turn was drawn): it reads short of the bottom though the reader never moved. Only a move up leaves the bottom.
    const held = !near && atBottom.current && top >= lastTop.current - 1;
    const bottom = near || held;
    lastTop.current = top;
    if (held) scroller.scrollTop = scroller.scrollHeight;
    if (bottom !== atBottom.current) {
      atBottom.current = bottom;
      setAway(!bottom);
    }
    lastScrollHeight.current = scroller.scrollHeight;
    noteAnchor();
    const current = latest.current;
    if (
      scroller.scrollTop < OLDER_AHEAD_PX &&
      current.hasOlder &&
      !current.loadingOlder &&
      !current.olderFailed &&
      current.items
    )
      current.onLoadOlder();
    rewindow();
  }, [rewindow, noteAnchor]);

  const toNew = () => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    atBottom.current = true;
    setAway(false);
    setRange(null);
    scroller.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' });
  };

  const toggle = useCallback((turnId: string) => {
    setToggled((current) => {
      const next = new Set(current);
      if (next.has(turnId)) next.delete(turnId);
      else next.add(turnId);
      return next;
    });
  }, []);

  const canFork = props.onForkTurn !== undefined;
  // Stable handlers for memoized rows: they call the latest props.
  const handlers = useMemo<ITurnHandlers>(
    () => ({
      onToggle: toggle,
      onLoadSteps: (turnId) => latest.current.onLoadSteps?.(turnId) ?? Promise.resolve(false),
      onContinueTurn: (turnId) => latest.current.onContinueTurn(turnId),
      onRetryTurn: (turnId) => latest.current.onRetryTurn(turnId),
      onRetryMessage: (clientId) => latest.current.onRetryMessage(clientId),
      onDeleteMessage: (clientId) => latest.current.onDeleteMessage(clientId),
      onResendMessage: (clientId) => latest.current.onResendMessage?.(clientId) ?? Promise.resolve(),
      renderGate: (permissionId) => latest.current.renderGate?.(permissionId) ?? null,
      onForkTurn: canFork ? (turnId) => latest.current.onForkTurn?.(turnId) ?? Promise.resolve() : undefined,
    }),
    [toggle, canFork],
  );

  const shown = props.shown ?? SHOWN_BY_DEFAULT;
  const canShare = props.onShareFile !== undefined;
  const outputsContext = useMemo<IOutputsContext>(
    () => ({
      shown,
      onOpen: (output) => latest.current.onOpenOutput?.(output),
      onOpenFile: (path) => latest.current.onOpenFile?.(path),
      downloadUrl: (path) => latest.current.downloadUrl?.(path) ?? '',
      onShareFile: canShare ? (path) => latest.current.onShareFile?.(path) : undefined,
    }),
    [shown, canShare],
  );

  const docs = useMemo(() => docPathsOf(items ?? []), [items]);
  const refContext = useMemo<IRefContext>(
    () => ({
      refs: props.refs ?? {},
      onOpen: (kind, label, href) => latest.current.onOpenRef?.(kind, label, href),
      docs,
    }),
    [props.refs, docs],
  );

  const drawn = items === null ? [] : items.slice(win.start, win.end);

  return (
    <div className="hx-chat">
      <CatchingUp on={catchingUp && items !== null} />
      <div ref={scrollerRef} className="hx-feed" onScroll={onScroll} data-virtual={virtual || undefined}>
        <div ref={innerRef} className="hx-feed__inner">
          {items === null && !failed && <FeedLoading />}
          {items === null && failed && onRetryLoad && (
            <LoadFailed
              size={LOAD_FAILED_SIZE.page}
              title={t('next.load.feed')}
              reason={failedReason ?? t('next.feed.failed')}
              onRetry={onRetryLoad}
            />
          )}
          {items !== null && (
            <RefContext value={refContext}>
              <OutputsContext value={outputsContext}>
                <div className="hx-thread" data-density={density}>
                  {!hasOlder && items.length > 0 && <FeedStart />}
                  {hasOlder && (
                    <div className="hx-feed__older" aria-label={t('next.feed.older')}>
                      {loadingOlder && <OlderLoading />}
                      {props.olderFailed && !loadingOlder && (
                        <LoadFailed
                          size={LOAD_FAILED_SIZE.inline}
                          title={t('next.load.older')}
                          reason={props.olderFailed}
                          onRetry={props.onLoadOlder}
                        />
                      )}
                    </div>
                  )}
                  {win.top > 0 && <Spacer px={win.top} />}
                  {drawn.map((item) => (
                    <Row
                      key={rowKey(item)}
                      item={item}
                      open={item.kind === 'turn' && (density === 'full') !== toggled.has(item.key)}
                      enter={isNew(item, enterAfter)}
                      offline={offline}
                      machineName={machineName}
                      handlers={handlers}
                      modelLabel={modelLabel}
                      measure={measure}
                    />
                  ))}
                  {win.bottom > 0 && <Spacer px={win.bottom} />}
                  {!items.some((x) => x.kind === 'turn' && x.turn.status === 'live') && waiting && (
                    <div className="hx-msg" data-kind="turn">
                      <WaitingLine agents={waiting.agents} onOpen={waiting.onOpen} />
                    </div>
                  )}
                  {!items.some((x) => x.kind === 'turn' && x.turn.status === 'live') &&
                    !waiting &&
                    workingSince !== null && (
                      <div className="hx-msg" data-kind="turn">
                        <NowBlock now={null} startedAt={workingSince} wait={null} machineName={machineName} />
                      </div>
                    )}
                  {goalReached !== null && (
                    <div className="hx-turnwrap">
                      <GoalNote text={goalReached.text} onContinue={goalReached.onContinue} />
                    </div>
                  )}
                  {props.limit}
                  {props.terminalFailure}
                  {terminalStop !== null && (
                    <div className="hx-turnwrap">
                      <TerminalStopNote text={terminalStop} />
                    </div>
                  )}
                </div>
              </OutputsContext>
            </RefContext>
          )}
        </div>
      </div>
      <div ref={dockRef} className="hx-dock">
        <button
          type="button"
          className="hx-tonew"
          data-shown={away || undefined}
          tabIndex={away ? 0 : -1}
          aria-hidden={!away}
          onClick={toNew}
        >
          <Icon name="chevron-down" size="sm" />
          {t('next.feed.toNew')}
        </button>
        <div className="hx-enter" data-enter="up" data-eb="520" data-ed="700">
          {props.dock}
        </div>
      </div>
    </div>
  );
};

export default Feed;
