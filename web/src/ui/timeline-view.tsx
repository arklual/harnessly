import { Button, Spinner } from '@kit';
import { AlertTriangle, Check, ChevronRight, Circle, CornerDownRight, X } from 'lucide-react';
import { memo, useLayoutEffect, useMemo, useRef, useState } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

import {
  callStatus,
  entryLabel,
  groupTimeline,
  type TimelineEntry,
  type TimelineItem,
  type TimelineState,
  type ToolResult,
  workSummary,
} from '../api/timeline';

const Text = memo(function Text({ markdown }: { markdown: string }) {
  return (
    <div className="md">
      <Markdown remarkPlugins={[remarkGfm]}>{markdown}</Markdown>
    </div>
  );
});

function StatusIcon({ status }: { status: 'pending' | 'success' | 'error' }) {
  if (status === 'success') return <Check size={12} className="step__ok" />;
  if (status === 'error') return <X size={12} className="step__fail" />;
  return <Circle size={10} className="step__pending" />;
}

function Step({ entry, results }: { entry: TimelineEntry; results: Record<string, ToolResult> }) {
  const thinking = entry.type === 'thinking' || entry.type === 'reasoning-summary';
  const [open, setOpen] = useState(false);
  const status =
    entry.type === 'tool-call' ? callStatus(entry, results) : entry.status === 'error' ? 'error' : 'success';
  const result = typeof entry.toolUseId === 'string' ? results[entry.toolUseId] : undefined;
  const detail = thinking
    ? String(entry.thinking ?? (Array.isArray(entry.summary) ? entry.summary.join('\n') : ''))
    : entry.type === 'exec-command-stream'
      ? String(entry.stdout ?? '')
      : entry.type === 'command-output'
        ? String(entry.output ?? '')
        : (result?.summary ?? '');
  return (
    <li className="step" data-kind={thinking ? 'thought' : 'call'}>
      <button
        type="button"
        className="step__head"
        onClick={() => detail && setOpen((o) => !o)}
        aria-expanded={detail ? open : undefined}
      >
        {thinking ? <CornerDownRight size={12} className="step__pending" /> : <StatusIcon status={status} />}
        <span className="step__label">{entryLabel(entry)}</span>
      </button>
      {open && detail && <pre className="step__detail">{detail}</pre>}
    </li>
  );
}

function Work({
  item,
  results,
  live,
}: {
  item: Extract<TimelineItem, { kind: 'work' }>;
  results: Record<string, ToolResult>;
  live: boolean;
}) {
  const [open, setOpen] = useState(false);
  const running =
    live && item.entries.some((e) => e.type === 'tool-call' && callStatus(e, results) === 'pending');
  return (
    <div className="work" data-open={open || undefined}>
      <button type="button" className="work__head" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <ChevronRight size={14} className="work__chevron" />
        {running && <Spinner size="xs" />}
        <span className="work__summary">{workSummary(item.entries)}</span>
      </button>
      {open && (
        <ul className="work__steps">
          {item.entries.map((e) => (
            <Step key={e.id} entry={e} results={results} />
          ))}
        </ul>
      )}
    </div>
  );
}

function Notice({ entry }: { entry: TimelineEntry }) {
  const text =
    entry.type === 'interrupt'
      ? 'Interrupted'
      : entry.type === 'context-compacted'
        ? 'Context compacted'
        : entry.type === 'session-exit'
          ? 'The agent exited'
          : entry.type === 'error-notice'
            ? String(entry.message ?? 'Error')
            : entry.type === 'plan'
              ? 'Plan proposed'
              : entry.type === 'ask-user-question'
                ? 'The agent asks a question — answer it in the terminal'
                : entry.type === 'approval-request'
                  ? 'The agent asks for approval — answer it in the terminal'
                  : String(entry.summary ?? entry.type);
  const warn =
    entry.type === 'error-notice' || entry.type === 'ask-user-question' || entry.type === 'approval-request';
  return (
    <div className="notice" data-warn={warn || undefined}>
      {warn && <AlertTriangle size={13} />}
      <span>{text}</span>
      {entry.type === 'plan' && typeof entry.markdown === 'string' && <Text markdown={entry.markdown} />}
    </div>
  );
}

export function TimelineView({
  state,
  pending,
  onLoadOlder,
  working,
}: {
  state: TimelineState;
  pending: string[];
  onLoadOlder: () => void;
  working: boolean;
}) {
  const items = useMemo(() => groupTimeline(state.entries), [state.entries]);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [items, pending, state.liveText]);

  return (
    <div
      ref={scroller}
      className="timeline"
      data-testid="timeline"
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
      }}
    >
      <div className="timeline__inner">
        {state.hasMore && (
          <div className="timeline__more">
            <Button size="xs" appearance="ghost" onClick={onLoadOlder}>
              Load earlier messages
            </Button>
          </div>
        )}
        {!state.loaded && (
          <div className="timeline__empty">
            <Spinner size="s" /> Loading the conversation…
          </div>
        )}
        {state.loaded && state.waiting && items.length === 0 && pending.length === 0 && (
          <div className="timeline__empty">
            <Spinner size="s" /> Waiting for the agent's conversation…
          </div>
        )}
        {state.error && (
          <div className="notice" data-warn>
            {state.error}
          </div>
        )}
        {items.map((item) => {
          if (item.kind === 'user')
            return (
              <div key={item.entry.id} className="msg msg--user" data-testid="msg-user">
                <div className="msg__bubble">{String(item.entry.text ?? '')}</div>
              </div>
            );
          if (item.kind === 'assistant')
            return (
              <div key={item.entry.id} className="msg msg--agent" data-testid="msg-agent">
                <Text markdown={String(item.entry.markdown ?? '')} />
              </div>
            );
          if (item.kind === 'work')
            return <Work key={item.id} item={item} results={state.results} live={working} />;
          return <Notice key={item.entry.id} entry={item.entry} />;
        })}
        {pending.map((p, i) => (
          <div key={`pending-${i}`} className="msg msg--user" data-pending>
            <div className="msg__bubble">{p}</div>
          </div>
        ))}
        {working && !state.liveText && items.at(-1)?.kind !== 'work' && (
          <div className="timeline__working">
            <Spinner size="xs" /> Working…
          </div>
        )}
        {state.liveText && (
          <div className="msg msg--agent" data-live>
            <Text markdown={state.liveText} />
          </div>
        )}
      </div>
    </div>
  );
}
