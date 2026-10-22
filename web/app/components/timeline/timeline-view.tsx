// The conversation of an agent tab: the person's messages, the agent's turns (one summary line, its replies, the
// work folded in between) and lone rows, kept pinned to the bottom while new entries arrive; older entries load at
// the top on demand.
import { memo, useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ChevronUp, MessageSquareDashed, RefreshCw } from 'lucide-react';
import { useStickToBottom } from 'use-stick-to-bottom';
import TextSelectionAnnotator from '@/components/agents/text-selection-annotator';
import BackgroundWorkLine from '@/components/timeline/background-work-line';
import { TurnRow } from '@/components/timeline/turn-row';
import TaskChecklist from '@/components/timeline/task-checklist';
import UserMessageItem from '@/components/timeline/user-message-item';
import { TimelineItemBody } from '@/components/timeline/timeline-entry';
import ScrollToBottomButton from '@/components/timeline/scroll-to-bottom-button';
import { TimelineScrollContext } from '@/components/timeline/timeline-scroll-context';
import { trackFreshRows } from '@/lib/fresh-rows';
import { groupTimelineItems } from '@/lib/timeline-grouping';
import { createTurnDeriver, withPendingTurn, type TTurnRow } from '@/lib/timeline-turns';
import { isLivePhase, type ITurnStatus } from '@/lib/turn-phase';
import { useTimelineScrollAnchor } from '@/hooks/use-timeline-scroll-anchor';
import { useTimelineScrollRetention } from '@/hooks/use-timeline-scroll-retention';
import { useTurnPhase } from '@/hooks/use-turn-phase';
import type { IPendingLaunch } from '@/lib/hl-pending-launch';
import type { ITaskItem, ITimelineEntry, TCliState } from '@/types/timeline';
import { t } from '@/lib/i18n';

type TProvider = 'claude' | 'codex';

const NO_FRESH: ReadonlySet<string> = new Set();

const Rows = memo(function Rows({
  rows,
  provider,
  sessionName,
  tabId,
  live,
  fresh,
  model,
  awaiting,
}: {
  rows: TTurnRow[];
  provider: TProvider;
  sessionName?: string;
  tabId?: string;
  live: ITurnStatus | null;
  fresh: ReadonlySet<string>;
  model: string | null;
  awaiting: boolean;
}) {
  const lastTurn = rows.findLast((r) => r.kind === 'turn');
  return (
    <>
      {rows.map((row) => {
        if (row.kind === 'user' && row.item.type === 'entry' && row.item.entry.type === 'user-message') {
          return (
            <div
              key={row.id}
              className="px-4 py-1.5"
              data-entry-id={row.id}
              data-fresh={fresh.has(row.id) || undefined}
            >
              <UserMessageItem entry={row.item.entry} />
            </div>
          );
        }
        if (row.kind === 'turn') {
          const isLast = row === lastTurn;
          return (
            <div key={row.id} className="px-4 py-1.5" data-entry-id={row.id}>
              <TurnRow
                turn={row.turn}
                live={isLast ? live : null}
                provider={provider}
                sessionName={sessionName}
                tabId={tabId}
                model={model}
                fresh={fresh.has(row.id)}
                awaiting={isLast && awaiting}
                reveal={null}
              />
            </div>
          );
        }
        return (
          <div
            key={row.id}
            className="px-4 py-1.5"
            data-entry-id={row.id}
            data-fresh={fresh.has(row.id) || undefined}
          >
            <TimelineItemBody item={row.item} sessionName={sessionName} tabId={tabId} provider={provider} />
          </div>
        );
      })}
    </>
  );
});

interface ITimelineViewProps {
  provider: TProvider;
  entries: ITimelineEntry[];
  tasks?: ITaskItem[];
  sessionId?: string | null;
  sessionName?: string;
  tabId?: string;
  initMeta?: Record<string, unknown> | null;
  sessionStats?: Record<string, unknown> | null;
  cliState: TCliState;
  wsStatus?: string;
  isLoading?: boolean;
  error?: string | null;
  onRetry?: () => void;
  onLoadMore?: () => Promise<void> | void;
  hasMore?: boolean;
  jsonlPath?: string | null;
  liveText?: string;
  launchingSince?: number | null;
  compactingSince?: number | null;
  /** The agent waits for an answer to a prompt shown under the timeline. */
  awaiting?: boolean;
}

export default function TimelineView({
  provider,
  entries,
  tasks,
  sessionId = null,
  wsStatus,
  sessionName,
  tabId,
  initMeta,
  cliState,
  isLoading,
  error,
  onRetry,
  onLoadMore,
  hasMore,
  liveText,
  launchingSince = null,
  compactingSince = null,
  awaiting = false,
}: ITimelineViewProps) {
  const lastEntryAt = entries.length ? entries[entries.length - 1]!.timestamp : null;
  const { status } = useTurnPhase({
    tabId,
    cliState,
    launchingSince,
    compactingSince,
    lastEntryAt,
    launchLabel: t('timeline.timelineView.launching'),
  });
  const working = isLivePhase(status.phase);
  const [deriveRows] = useState(createTurnDeriver);
  const derived = useMemo(() => deriveRows(groupTimelineItems(entries)), [deriveRows, entries]);
  const rows = useMemo(() => (working ? withPendingTurn(derived) : derived), [working, derived]);
  const model = typeof initMeta?.model === 'string' ? initMeta.model : null;
  // Content that grows under a reader at the end keeps them at the end in the same frame; a spring would chase every
  // late picture or code block for half a second and read as the feed scrolling by itself.
  const { scrollRef, contentRef, scrollToBottom, stopScroll, isAtBottom } = useStickToBottom({
    initial: 'instant',
    resize: 'instant',
  });
  const [loadingMore, setLoadingMore] = useState(false);
  // A row the person opens must not drag the view: the pinned spring is stopped before it grows.
  const scrollContext = useMemo(() => ({ holdPosition: stopScroll }), [stopScroll]);
  const stayAtEnd = useCallback(() => void scrollToBottom('instant'), [scrollToBottom]);
  const shown = !isLoading && !(error && entries.length === 0);
  const [seenRows, setSeenRows] = useState(() =>
    trackFreshRows(
      null,
      sessionId,
      rows.map((row) => row.id),
    ),
  );
  const nextSeenRows = trackFreshRows(
    seenRows,
    sessionId,
    rows.map((row) => row.id),
  );
  if (nextSeenRows !== seenRows) setSeenRows(nextSeenRows);
  useTimelineScrollRetention(scrollRef, sessionId, stopScroll, shown && rows.length > 0);
  useTimelineScrollAnchor(scrollRef, contentRef, stayAtEnd, sessionId, rows, shown);

  // An opened conversation is set at its end before the first paint, so it never looks like scrolling down from the top.
  const scrolledFor = useRef<string | null | undefined>(undefined);
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!shown || !scroller || entries.length === 0 || scrolledFor.current === sessionId) return;
    scroller.scrollTop = scroller.scrollHeight;
    void scrollToBottom('instant');
    scrolledFor.current = sessionId;
  }, [shown, entries.length, sessionId, scrollRef, scrollToBottom]);

  const loadMore = async () => {
    if (!onLoadMore || loadingMore) return;
    setLoadingMore(true);
    try {
      await onLoadMore();
    } finally {
      setLoadingMore(false);
    }
  };

  if (isLoading) {
    return (
      <div className="agent-timeline relative flex h-full flex-col">
        <div className="flex-1 overflow-hidden py-2">
          <div className="mx-auto max-w-content">
            <div className="animate-delayed-fade-in flex flex-col gap-4 p-4">
              <div className="flex flex-col gap-2">
                <div className="h-4 w-2/3 animate-pulse rounded bg-[var(--t-surface-raised)]" />
                <div className="h-4 w-1/2 animate-pulse rounded bg-[var(--t-surface-raised)]" />
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  if (error && entries.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
        <MessageSquareDashed className="opacity-40" size={28} />
        <div className="text-center">
          <p className="text-sm font-medium">{t('timeline.timelineView.loadFailed')}</p>
          <p className="mt-1 text-xs">{error}</p>
        </div>
        {onRetry && (
          <button type="button" className="hl-btn" data-variant="outline" data-size="sm" onClick={onRetry}>
            {t('timeline.timelineView.retry')}
          </button>
        )}
      </div>
    );
  }

  return (
    <TimelineScrollContext.Provider value={scrollContext}>
      <div className="agent-timeline relative flex h-full flex-col">
        <div ref={scrollRef} className="agent-timeline-scroller flex-1 overflow-y-auto py-2">
          <div
            ref={contentRef}
            className="mx-auto max-w-content"
            role="log"
            aria-live="polite"
            data-testid="timeline"
          >
            {hasMore && (
              <div className="flex justify-center py-2">
                <button
                  type="button"
                  className="h-7 text-xs text-muted-foreground inline-flex items-center"
                  onClick={() => void loadMore()}
                  disabled={loadingMore}
                >
                  <ChevronUp className="mr-1" size={14} />
                  {loadingMore ? t('timeline.timelineView.loading') : t('timeline.timelineView.earlier')}
                </button>
              </div>
            )}
            {tasks && tasks.length > 0 && <TaskChecklist tasks={tasks} cliState={cliState} />}
            <Rows
              rows={rows}
              provider={provider}
              sessionName={sessionName}
              tabId={tabId}
              live={working ? status : null}
              fresh={nextSeenRows.fresh}
              model={model}
              awaiting={awaiting}
            />
            {liveText ? (
              <div className="px-4 py-1.5">
                <div className="tw-live-reply">
                  {liveText}
                  <span className="tw-live-reply-caret" aria-hidden />
                </div>
              </div>
            ) : null}
            <BackgroundWorkLine tabId={tabId} phase={status.phase} />
            <div className="h-px" />
          </div>
        </div>
        {tabId && wsStatus === 'connecting' && entries.length > 0 && (
          <div className="tw-timeline-banner" role="status">
            <RefreshCw size={11} className="tw-pulse" aria-hidden />
            {t('timeline.timelineView.reconnecting')}
          </div>
        )}
        {tabId && wsStatus === 'disconnected' && entries.length > 0 && (
          <div className="tw-timeline-banner" role="status">
            {t('timeline.timelineView.connectionLost')}
            {onRetry && (
              <button type="button" className="tw-fold-toggle" onClick={onRetry}>
                {t('timeline.timelineView.retry')}
              </button>
            )}
          </div>
        )}
        {tabId && <TextSelectionAnnotator tabId={tabId} />}
        <ScrollToBottomButton
          visible={!isAtBottom && entries.length > 0}
          onClick={() => void scrollToBottom('smooth')}
        />
      </div>
    </TimelineScrollContext.Provider>
  );
}

/** What the pane shows while an agent starts with a first prompt: the prompt and the turn that waits for it. */
export const LaunchPreview = ({
  launch,
  provider = 'claude',
}: {
  launch: IPendingLaunch;
  provider?: TProvider;
}) => {
  const rows = useMemo(
    () =>
      withPendingTurn(
        launch.prompt
          ? [
              {
                kind: 'user',
                id: 'launch-preview',
                item: {
                  type: 'entry',
                  id: 'launch-preview',
                  entry: {
                    id: 'launch-preview',
                    type: 'user-message',
                    timestamp: launch.at,
                    text: launch.prompt,
                    pending: true,
                  },
                },
              },
            ]
          : [],
      ),
    [launch.prompt, launch.at],
  );
  const live: ITurnStatus = {
    phase: 'launching',
    label: t('timeline.timelineView.launching'),
    since: launch.at,
    quietMs: null,
  } as ITurnStatus;
  return (
    <div className="agent-timeline relative flex h-full flex-col">
      <div className="flex-1 overflow-hidden py-2">
        <div className="mx-auto max-w-content">
          <Rows rows={rows} provider={provider} live={live} fresh={NO_FRESH} model={null} awaiting={false} />
        </div>
      </div>
    </div>
  );
};
