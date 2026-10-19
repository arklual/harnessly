import { Badge, Button, IconButton, Tooltip } from '@kit';
import { ArrowUp, Square, SquareTerminal } from 'lucide-react';
import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react';

import { agents } from '../api/endpoints';
import { activityLabel, activityOf, activityTone } from '../api/status';
import type { AgentPanel, Tab } from '../api/types';
import { formatClock } from '../lib/format';
import { dismissTab, reportError, statusStore } from '../stores/runtime';
import { useStore } from '../stores/store';
import { actions } from './actions';
import { TerminalView } from './terminal-view';
import { TimelineView } from './timeline-view';
import { useTimeline } from './use-timeline';

function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);
  return now;
}

function newMessageId(): string {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export function AgentView({ tab, workspaceId, paneId }: { tab: Tab; workspaceId: string; paneId: string }) {
  const panelType = tab.panelType as AgentPanel;
  const entry = useStore(statusStore, (s) => s.tabs[tab.id]);
  const activity = activityOf(entry);
  const agentSessionId = entry?.agentSessionId ?? tab.agentState?.sessionId ?? null;
  const { state, loadOlder } = useTimeline(tab.sessionName, panelType, agentSessionId);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [pending, setPending] = useState<string[]>([]);
  const [showTerminal, setShowTerminal] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const busy = activity === 'working' || activity === 'waiting';
  const now = useNow(activity === 'working');

  // A sent message shows as pending until the transcript has it.
  const said = useMemo(
    () =>
      new Set(state.entries.filter((e) => e.type === 'user-message').map((e) => String(e.text ?? '').trim())),
    [state.entries],
  );
  const waiting = useMemo(() => pending.filter((p) => !said.has(p.trim())), [pending, said]);

  useEffect(() => {
    if (activity === 'done') dismissTab(tab.id);
  }, [activity, tab.id]);

  const send = async () => {
    const message = text.trim();
    if (!message || sending) return;
    setSending(true);
    try {
      await agents.send(tab.id, message, newMessageId());
      setPending((p) => [...p.filter((x) => !said.has(x.trim())), message]);
      setText('');
    } catch (err) {
      reportError(err);
    } finally {
      setSending(false);
      input.current?.focus();
    }
  };

  const interrupt = async () => {
    try {
      await agents.interrupt(tab.id);
    } catch (err) {
      reportError(err);
    }
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    } else if (e.key === 'Escape' && busy && !text) {
      e.preventDefault();
      void interrupt();
    }
  };

  const action = entry?.currentAction;
  return (
    <div className="agent" data-testid="agent-view" data-activity={activity}>
      <div className="agent__head">
        <Badge tone={activityTone[activity]} data-testid="agent-status">
          {activityLabel[activity]}
        </Badge>
        {activity === 'working' && entry?.busySince ? (
          <span className="agent__clock">{formatClock(now - entry.busySince)}</span>
        ) : null}
        <span className="agent__action">
          {activity === 'working' && action
            ? [action.toolName, action.summary].filter(Boolean).join(' · ')
            : (entry?.agentSummary ?? '')}
        </span>
        <span className="agent__spacer" />
        {entry?.hooksMissing && (
          <Tooltip content="This agent was started without the runtime's hooks, so its state is guessed from the screen.">
            <Badge tone="warning">No hooks</Badge>
          </Tooltip>
        )}
        <Button
          size="xs"
          appearance={showTerminal ? 'secondary' : 'ghost'}
          icon={<SquareTerminal size={14} />}
          aria-pressed={showTerminal}
          onClick={() => setShowTerminal((v) => !v)}
        >
          Terminal
        </Button>
      </div>
      <div className="agent__body" data-split={showTerminal || undefined}>
        <TimelineView
          state={state}
          pending={waiting}
          onLoadOlder={() => void loadOlder().catch(reportError)}
          working={activity === 'working'}
        />
        {showTerminal && (
          <div className="agent__terminal">
            <TerminalView
              session={tab.sessionName}
              onRestart={() => actions.restart(workspaceId, paneId, tab.id)}
            />
          </div>
        )}
      </div>
      <div className="composer">
        <textarea
          ref={input}
          className="composer__input"
          rows={1}
          placeholder={busy ? 'Message the agent (Esc stops it)' : 'Message the agent'}
          aria-label="Message"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKey}
        />
        {busy && !text ? (
          <IconButton
            label="Stop"
            appearance="secondary"
            size="m"
            onClick={() => void interrupt()}
            data-testid="agent-stop"
          >
            <Square size={14} fill="currentColor" />
          </IconButton>
        ) : (
          <IconButton
            label="Send"
            appearance="primary"
            size="m"
            loading={sending}
            disabled={!text.trim()}
            onClick={() => void send()}
            data-testid="agent-send"
          >
            <ArrowUp size={16} />
          </IconButton>
        )}
      </div>
    </div>
  );
}
