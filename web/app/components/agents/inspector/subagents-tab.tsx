import { t } from '@/lib/i18n';
import { memo, useEffect, useRef, useState } from 'react';
import dayjs from 'dayjs';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  AlertCircle,
  Bot,
  Check,
  ChevronRight,
  Compass,
  ListChecks,
  Loader2,
  Search,
  Wrench,
} from 'lucide-react';
import { appFetch } from '@/lib/app-path';
import useSubagents from '@/hooks/use-subagents';
import { cn } from '@/lib/utils';
import MessageLink from '@/components/timeline/message-link';
import MessageCodeBlock from '@/components/timeline/message-code-block';
import ShimmerText from '@/components/timeline/shimmer-text';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import type { ISubagentSummary } from '@/types/server/hl-subagents';
import type { ITimelineEntry } from '@/types/timeline';

/** Merges a burst of transcript changes into one subagent list refresh. */
const SIGNAL_DELAY_MS = 1_000;
const MARKDOWN_COMPONENTS = { a: MessageLink, pre: MessageCodeBlock };
const REMARK_PLUGINS = [remarkGfm];

const AGENT_ICONS: Record<string, typeof Bot> = {
  Explore: Search,
  Plan: ListChecks,
  'general-purpose': Bot,
  claude: Compass,
};

const STATUS_LABEL = {
  running: t('inspector.subagentsTab.statusRunning'),
  done: t('inspector.subagentsTab.statusDone'),
  stalled: t('inspector.subagentsTab.statusStalled'),
} as const;

const formatDuration = (ms: number) => {
  const total = Math.max(0, Math.round(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return m
    ? t('inspector.subagentsTab.durationMinSec', { m, s: s.toString().padStart(2, '0') })
    : t('inspector.subagentsTab.durationSec', { s });
};

const useNow = (active: boolean) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active]);
  return now;
};

const SubagentSteps = ({
  jsonlPath,
  id,
  live,
  revision,
}: {
  jsonlPath: string;
  id: string;
  live: boolean;
  revision: number;
}) => {
  const [entries, setEntries] = useState<ITimelineEntry[] | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let cancelled = false;
    appFetch(`/api/platform/subagents/${encodeURIComponent(id)}?jsonlPath=${encodeURIComponent(jsonlPath)}`)
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((data) => {
        if (!cancelled) {
          setEntries(data.entries);
          setError('');
        }
      })
      .catch(() => {
        if (!cancelled && !live) setError(t('inspector.subagentsTab.stepsFailed'));
      });
    return () => {
      cancelled = true;
    };
  }, [jsonlPath, id, live, revision]);
  if (error) return <p className="tw-inspector-muted">{error}</p>;
  if (!entries)
    return (
      <p className="tw-inspector-muted">
        <Loader2 className="tw-inline-spin" /> {t('inspector.subagentsTab.stepsLoading')}
      </p>
    );
  const steps = entries.filter((e) => e.type === 'tool-call' || e.type === 'assistant-message');
  return (
    <ol className="tw-steps">
      {steps.map((entry) => (
        <li key={entry.id} className={cn('tw-step', entry.type === 'tool-call' && `tw-step-${entry.status}`)}>
          {entry.type === 'tool-call' ? (
            <>
              <Wrench className="tw-step-icon" />
              <code>{entry.summary}</code>
            </>
          ) : entry.type === 'assistant-message' ? (
            <div className="tw-step-text">
              {entry.markdown.slice(0, 400)}
              {entry.markdown.length > 400 ? '…' : ''}
            </div>
          ) : null}
        </li>
      ))}
    </ol>
  );
};

export const SubagentCard = memo(
  ({
    agent,
    jsonlPath,
    defaultOpen = false,
  }: {
    agent: ISubagentSummary;
    jsonlPath: string;
    defaultOpen?: boolean;
  }) => {
    const holdPosition = useHoldTimelinePosition();
    const [open, setOpen] = useState(defaultOpen);
    const [showSteps, setShowSteps] = useState(defaultOpen && agent.status === 'running');
    const running = agent.status === 'running';
    const now = useNow(running);
    const Icon = AGENT_ICONS[agent.agentType] ?? Bot;
    const elapsed = agent.startedAt ? (running ? now : agent.updatedAt) - agent.startedAt : null;

    return (
      <article className={cn('tw-agent-card', `tw-agent-${agent.status}`, open && 'tw-agent-open')}>
        <button
          type="button"
          className="tw-agent-head"
          onClick={() => {
            holdPosition();
            setOpen((v) => !v);
          }}
          aria-expanded={open}
        >
          <span className="tw-agent-icon">
            <Icon />
          </span>
          <span className="tw-agent-main">
            <span className="tw-agent-title">{agent.description}</span>
            <span className="tw-agent-meta">
              <span className="tw-agent-type">{agent.agentType}</span>
              {agent.background && <span>{t('inspector.subagentsTab.background')}</span>}
              <span>{t('inspector.subagentsTab.stepsShort', { count: agent.toolUses })}</span>
              {elapsed !== null && <span className="tabular-nums">{formatDuration(elapsed)}</span>}
            </span>
          </span>
          <span className={cn('tw-agent-status', `tw-agent-status-${agent.status}`)}>
            {running ? (
              <Loader2 className="tw-inline-spin" />
            ) : agent.status === 'done' ? (
              <Check />
            ) : (
              <AlertCircle />
            )}
            {STATUS_LABEL[agent.status]}
          </span>
          <ChevronRight className="tw-agent-chevron" />
        </button>
        {agent.lastAction && agent.status !== 'done' && (
          <ShimmerText active={running} className="tw-agent-action">
            {agent.lastAction}
          </ShimmerText>
        )}
        {open && (
          <div className="tw-agent-body">
            {agent.result && (
              <div className="tw-agent-result agent-message-body prose prose-sm dark:prose-invert max-w-none">
                <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>
                  {agent.result}
                </ReactMarkdown>
              </div>
            )}
            <details className="tw-agent-prompt">
              <summary>{t('inspector.subagentsTab.task')}</summary>
              <p>{agent.prompt}</p>
            </details>
            <button
              type="button"
              className="tw-link-button"
              onClick={() => {
                holdPosition();
                setShowSteps((v) => !v);
              }}
            >
              {showSteps
                ? t('inspector.subagentsTab.hideSteps')
                : t('inspector.subagentsTab.showSteps', { count: agent.toolUses })}
            </button>
            {showSteps && (
              <SubagentSteps jsonlPath={jsonlPath} id={agent.id} live={running} revision={agent.updatedAt} />
            )}
            <p className="tw-inspector-muted">
              {t('inspector.subagentsTab.startedAt', {
                time: agent.startedAt ? dayjs(agent.startedAt).format('HH:mm:ss') : '—',
              })}
            </p>
          </div>
        )}
      </article>
    );
  },
);
SubagentCard.displayName = 'SubagentCard';

interface ISubagentsTabProps {
  jsonlPath: string | null;
  provider: string;
  /** `subagentActivityKey` of the main transcript: a change means the list is worth fetching now. */
  activity: string;
  onCount?: (running: number, total: number) => void;
}

const SubagentsTab = ({ jsonlPath, provider, activity, onCount }: ISubagentsTabProps) => {
  const { agents, supported, error, poke } = useSubagents(jsonlPath, provider !== 'codex');
  const running = agents?.filter((a) => a.status === 'running').length ?? 0;

  const seenActivity = useRef(activity);
  useEffect(() => {
    if (seenActivity.current === activity) return;
    seenActivity.current = activity;
    poke(SIGNAL_DELAY_MS);
  }, [activity, poke]);

  useEffect(() => {
    onCount?.(running, agents?.length ?? 0);
  }, [running, agents?.length, onCount]);

  if (provider === 'codex' || !supported)
    return (
      <div className="tw-inspector-empty">
        <Bot />
        <p>{t('inspector.subagentsTab.codexNone')}</p>
      </div>
    );
  if (!jsonlPath)
    return (
      <div className="tw-inspector-empty">
        <Bot />
        <p>{t('inspector.subagentsTab.notStarted')}</p>
      </div>
    );
  if (!agents && error)
    return (
      <div className="tw-inspector-empty">
        <AlertCircle />
        <p>{error}</p>
      </div>
    );
  if (!agents)
    return (
      <div className="tw-inspector-empty">
        <Loader2 className="tw-inline-spin" />
        <p>{t('inspector.subagentsTab.loading')}</p>
      </div>
    );
  if (agents.length === 0)
    return (
      <div className="tw-inspector-empty">
        <Bot />
        <p>{t('inspector.subagentsTab.empty')}</p>
      </div>
    );

  const active = agents.filter((a) => a.status !== 'done');
  const finished = agents.filter((a) => a.status === 'done');
  return (
    <div className="tw-agent-list">
      {active.map((agent) => (
        <SubagentCard key={agent.id} agent={agent} jsonlPath={jsonlPath} />
      ))}
      {active.length === 0 && (
        <p className="tw-inspector-summary">{t('inspector.subagentsTab.noneActive')}</p>
      )}
      {finished.length > 0 && (
        <details className="tw-agent-finished">
          <summary>
            <Check /> {t('inspector.subagentsTab.finished', { count: finished.length })}
          </summary>
          <div className="tw-agent-list">
            {finished.map((agent) => (
              <SubagentCard key={agent.id} agent={agent} jsonlPath={jsonlPath} />
            ))}
          </div>
        </details>
      )}
    </div>
  );
};

export default SubagentsTab;
