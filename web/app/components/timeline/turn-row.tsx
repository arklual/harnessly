import { memo, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { SiOpenai } from 'react-icons/si';
import ClaudeSpark from '@/components/icons/claude-spark';
import AssistantMessageItem from '@/components/timeline/assistant-message-item';
import ElapsedTime from '@/components/timeline/elapsed-time';
import ShimmerText from '@/components/timeline/shimmer-text';
import ToolGroupItem from '@/components/timeline/tool-group-item';
import TurnCheckpoint from '@/components/timeline/turn-checkpoint';
import { TimelineItemBody } from '@/components/timeline/timeline-entry';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { prettyModelName } from '@/lib/hl-models';
import { runningStepOfTurn, STEP_AGE_AFTER_MS } from '@/lib/running-step';
import { formatSpan, workedLabel } from '@/lib/turn-format';
import { actionsLabel, turnWorkMs, type ITurn } from '@/lib/timeline-turns';
import type { ITurnStatus } from '@/lib/turn-phase';
import type { TGroupedItem } from '@/lib/timeline-grouping';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineAssistantMessage, ITimelineThinking } from '@/types/timeline';

type TProvider = 'claude' | 'codex';

/** Same as the collapse transition in hl.css (`.tw-turn-fold`). */
const FOLD_MS = 220;

/** Keeps a folding part mounted until its collapse has played. */
const useLingering = (open: boolean) => {
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  useEffect(() => {
    if (open) return;
    const timer = setTimeout(() => setMounted(false), FOLD_MS);
    return () => clearTimeout(timer);
  }, [open]);
  return mounted;
};

/** Height animates through grid rows, without measuring; `enter` also unfolds it when it mounts. */
export const Fold = ({
  open,
  enter = false,
  className,
  children,
}: {
  open: boolean;
  enter?: boolean;
  className?: string;
  children: ReactNode;
}) => {
  const mounted = useLingering(open);
  if (!mounted) return null;
  return (
    <div
      className={cn('tw-turn-fold', enter && 'tw-turn-fold-enter', className)}
      data-open={open || undefined}
    >
      <div>{children}</div>
    </div>
  );
};

const thoughtMessage = (entry: ITimelineThinking): ITimelineAssistantMessage => ({
  id: entry.id,
  type: 'assistant-message',
  timestamp: entry.timestamp,
  markdown: entry.thinking,
});

/** A finished thought: one quiet line, its text (markdown) only once opened. */
export const ThoughtRow = memo(({ entry, ms }: { entry: ITimelineThinking; ms: number }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const message = useMemo(() => thoughtMessage(entry), [entry]);
  return (
    <div className="tw-turn-thought">
      <button
        type="button"
        className="tw-turn-toggle"
        aria-expanded={open}
        onClick={() => {
          holdPosition();
          setOpen((v) => !v);
        }}
      >
        <span>{t('timeline.turnRow.thoughtFor', { span: formatSpan(ms) })}</span>
        <ChevronRight size={13} className="tw-turn-chevron" />
      </button>
      <Fold open={open} enter>
        <AssistantMessageItem entry={message} muted showActions={false} />
      </Fold>
    </div>
  );
});
ThoughtRow.displayName = 'ThoughtRow';

interface IWorkItemProps {
  item: TGroupedItem;
  /** How long a thought took; unused by other rows. */
  ms: number;
  provider: TProvider;
  sessionName?: string;
  /** The turn is live: its summary line is the only live line. */
  quiet: boolean;
  /** Opened by a jump to an entry inside it. */
  revealed: boolean;
}

const WorkItem = memo(({ item, ms, provider, sessionName, quiet, revealed }: IWorkItemProps) => {
  let body: ReactNode;
  if (item.type === 'entry' && item.entry.type === 'thinking') {
    // Claude Code redacts some thinking to an empty block; there is nothing to show for it.
    if (!item.entry.thinking.trim()) return null;
    body = <ThoughtRow entry={item.entry} ms={ms} />;
  } else if (item.type === 'entry' && item.entry.type === 'assistant-message') {
    body = <AssistantMessageItem entry={item.entry} muted showActions={false} />;
  } else if (item.type === 'tool-group') {
    body = (
      <ToolGroupItem
        toolCalls={item.toolCalls}
        toolResults={item.toolResults}
        quiet={quiet}
        defaultExpanded={revealed}
      />
    );
  } else {
    body = <TimelineItemBody item={item} sessionName={sessionName} provider={provider} />;
  }
  return (
    <div className="tw-turn-item" data-entry-id={item.id}>
      {body}
    </div>
  );
});
WorkItem.displayName = 'WorkItem';

const thoughtMs = (turn: ITurn, id: string) => turn.thinking.find((t) => t.id === id)?.ms ?? 0;

/** The last thought as plain text: it changes while the agent works, so it is not parsed as markdown. */
const LiveThought = ({ text }: { text: string }) => {
  const ref = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    // The text is pinned to the bottom, so what overflows sits above the box, outside scrollHeight.
    const check = () => setOverflowing((el.firstElementChild?.clientHeight ?? 0) - el.clientHeight > 1);
    check();
    const observer = new ResizeObserver(check);
    observer.observe(el);
    return () => observer.disconnect();
  }, [text]);
  return (
    <div ref={ref} className="tw-turn-thought-live" data-overflow={overflowing || undefined}>
      <p>{text}</p>
    </div>
  );
};

interface IActionsLineProps {
  turn: ITurn;
  items: TGroupedItem[];
  /** The last line of a running turn: says what runs now, and the live thought shows above it. */
  live: boolean;
  /** Opened from the turn's summary line (or by a jump into the turn), together with every other line. */
  forced: boolean;
  revealItemId: string | null;
  quiet: boolean;
  provider: TProvider;
  sessionName?: string;
}

/** The work between two messages as one quiet line, «Edit in timeline-turns.ts · 1 command ›»; opens to its rows. */
const ActionsLine = ({
  turn,
  items,
  live,
  forced,
  revealItemId,
  quiet,
  provider,
  sessionName,
}: IActionsLineProps) => {
  const holdPosition = useHoldTimelinePosition();
  const [opened, setOpened] = useState(false);
  // A jump opens the whole turn through `forced`; closing the turn's summary closes every line again.
  const open = opened || forced;
  const thinkingMs = items.reduce((sum, item) => sum + thoughtMs(turn, item.id), 0);
  const label = actionsLabel(items, { live, thinkingMs });
  const thought = live
    ? items.findLast(
        (item) =>
          item.type === 'entry' && item.entry.type === 'thinking' && item.entry.thinking.trim() !== '',
      )
    : undefined;
  const thoughtText =
    thought?.type === 'entry' && thought.entry.type === 'thinking' ? thought.entry.thinking : '';
  // The thought folds away as the turn ends instead of vanishing, with its last text while it folds.
  const [shownThought, setShownThought] = useState(thoughtText);
  if (thoughtText && thoughtText !== shownThought) setShownThought(thoughtText);
  return (
    <div className="tw-turn-actions" data-live={live || undefined}>
      {shownThought && (
        <Fold open={!!thoughtText && !open} className="tw-turn-thought-fold">
          <LiveThought text={shownThought} />
        </Fold>
      )}
      <button
        type="button"
        className="tw-turn-actions-head"
        data-testid="turn-actions-toggle"
        aria-expanded={open}
        onClick={() => {
          holdPosition();
          setOpened((v) => !v);
        }}
      >
        <span className="tw-turn-actions-label">{label}</span>
        <ChevronRight size={13} className="tw-turn-chevron" />
      </button>
      <Fold open={open} enter className="tw-turn-body">
        {items.map((item) => {
          const revealed = revealItemId === item.id;
          // A revealed tool run remounts so it opens; nothing else changes on a jump.
          return (
            <WorkItem
              key={revealed ? `${item.id}:revealed` : item.id}
              item={item}
              ms={thoughtMs(turn, item.id)}
              provider={provider}
              sessionName={sessionName}
              quiet={quiet}
              revealed={revealed}
            />
          );
        })}
      </Fold>
    </div>
  );
};

/** How long the running tool call has taken, once it is worth showing; ticks on its own. */
const StepTime = ({ since }: { since: number }) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [since]);
  const ms = now - since;
  return ms > STEP_AGE_AFTER_MS ? <span className="tw-turn-step">· {formatSpan(ms)}</span> : null;
};

/** The live verb changes in place, a short fade instead of a remount. */
const LiveLabel = ({ text }: { text: string }) => {
  // No fade until the first change: opening a conversation must not play it.
  const [swap, setSwap] = useState<0 | 1 | null>(null);
  const [prevText, setPrevText] = useState(text);
  if (text !== prevText) {
    setPrevText(text);
    setSwap((v) => (v === 0 ? 1 : 0));
  }
  return (
    <span className="tw-turn-label" data-swap={swap ?? undefined}>
      <ShimmerText>{text}</ShimmerText>
    </span>
  );
};

interface ITurnSummaryProps {
  turn: ITurn;
  live: ITurnStatus | null;
  expanded: boolean;
  onToggle: () => void;
  provider: TProvider;
}

/** One line for the whole turn, the same element while it runs and after: live verb and timer, then «Worked …». */
export const TurnSummary = ({ turn, live, expanded, onToggle, provider }: ITurnSummaryProps) => {
  const step = useMemo(
    () => (live?.phase === 'acting' ? runningStepOfTurn(turn) : null),
    [live?.phase, turn],
  );
  if (!live && turn.work.length === 0) return null;
  const empty = turn.work.length === 0;
  return (
    <button
      type="button"
      className={cn('tw-turn-summary', live && 'tw-turn-summary-live')}
      aria-expanded={empty ? undefined : expanded}
      disabled={empty}
      onClick={onToggle}
    >
      <span className="tw-turn-icon" aria-hidden="true">
        {live && provider === 'claude' && <ClaudeSpark state={live.label ? 'writing' : 'thinking'} />}
        {live && provider === 'codex' && <SiOpenai />}
        {!live && <ChevronRight size={14} className="tw-turn-chevron" />}
      </span>
      {live ? (
        <LiveLabel text={live.label ?? t('timeline.turnRow.thinking')} />
      ) : (
        <span className="tw-turn-label">{workedLabel(turnWorkMs(turn), turn.actions, turn.interrupted)}</span>
      )}
      {step && <StepTime since={step.startedAt} />}
      {live && (
        <span className="sr-only" role="status" aria-live="polite">
          {live.label ?? t('timeline.turnRow.thinking')}
        </span>
      )}
      {live && live.since !== null && (
        <span className="tw-turn-time" aria-hidden="true">
          <ElapsedTime since={live.since} />
          {live.phase === 'stale' &&
            live.quietMs !== null &&
            ` · ${t('timeline.turnRow.quietFor', { span: formatSpan(live.quietMs) })}`}
        </span>
      )}
    </button>
  );
};

const answerModel = (entry: ITimelineAssistantMessage, sessionModel: string | null) =>
  entry.model && entry.model !== '<synthetic>' ? prettyModelName(entry.model) : sessionModel;

interface ITurnRowProps {
  turn: ITurn;
  /** The held phase of the agent, only for the last turn while it works. */
  live: ITurnStatus | null;
  provider: TProvider;
  sessionName?: string;
  tabId?: string;
  model: string | null;
  fresh: boolean;
  /** The agent waits for an answer to a prompt shown under the timeline: the turn holds still, without its summary. */
  awaiting: boolean;
  /** A jump target folded inside this turn; a new object each time it is asked for. */
  reveal: { itemId: string } | null;
}

/**
 * An agent turn: one summary line, then the agent's messages and the rows that never fold in the order they came, with
 * one quiet line for the work between two of them; the last line of a running turn says what runs now. Keyed by the turn id, so the live turn becomes the
 * finished one in place and the open state survives that; a message shown once is never moved or hidden by the next.
 */
export const TurnRow = memo(
  ({ turn, live, provider, sessionName, tabId, model, fresh, awaiting, reveal }: ITurnRowProps) => {
    const holdPosition = useHoldTimelinePosition();
    const [expanded, setExpanded] = useState(false);
    const [lastReveal, setLastReveal] = useState(reveal);
    if (reveal !== lastReveal) {
      setLastReveal(reveal);
      if (reveal) setExpanded(true);
    }
    const working = live !== null || awaiting;
    const renderShown = (item: TGroupedItem) => {
      const entry = item.type === 'entry' ? item.entry : null;
      // The stop is said once, quietly, at the end of the turn.
      if (entry?.type === 'interrupt') return null;
      if (entry?.type === 'assistant-message') {
        const closes = item === turn.closing;
        return (
          <div key={item.id} className="tw-turn-final" data-entry-id={item.id}>
            <AssistantMessageItem
              entry={entry}
              provider={provider}
              showActions={closes && !working && turn.closingIsFinal}
              label={closes && turn.closingIsFinal ? (answerModel(entry, model) ?? undefined) : undefined}
            />
          </div>
        );
      }
      return (
        <div key={item.id} className="tw-turn-pinned" data-entry-id={item.id}>
          <TimelineItemBody item={item} sessionName={sessionName} tabId={tabId} provider={provider} />
        </div>
      );
    };
    const lastActions = turn.flow.findLastIndex((row) => row.kind === 'actions');
    return (
      <div className="tw-turn" data-testid="turn" data-fresh={fresh || undefined}>
        {!awaiting && (
          <TurnSummary
            turn={turn}
            live={live}
            expanded={expanded}
            provider={provider}
            onToggle={() => {
              holdPosition();
              setExpanded((v) => !v);
            }}
          />
        )}
        {turn.flow.map((row, i) =>
          row.kind === 'shown' ? (
            renderShown(row.item)
          ) : (
            <ActionsLine
              key={row.id}
              turn={turn}
              items={row.items}
              live={working && i === lastActions && i === turn.flow.length - 1}
              forced={expanded}
              revealItemId={
                reveal && row.items.some((item) => item.id === reveal.itemId) ? reveal.itemId : null
              }
              quiet={working}
              provider={provider}
              sessionName={sessionName}
            />
          ),
        )}
        {turn.interrupted && !working && (
          <div className="tw-turn-stopped">{t('timeline.turnRow.interrupted')}</div>
        )}
        <TurnCheckpoint tabId={tabId} sessionName={sessionName} endedAt={turn.endedAt} settled={!working} />
      </div>
    );
  },
);
TurnRow.displayName = 'TurnRow';
