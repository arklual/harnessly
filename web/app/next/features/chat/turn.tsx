// One turn of the agent: live, its work is open and the «Now» block runs under it; finished, the
// work folds into «Worked 4 min · 12 actions» with the answer below, and what it made stays at the bottom. A turn
// that ended without an answer says how: «Turn interrupted» with the reason and Continue / Retry turn, «Stopped by
// you», or that it stopped on the usage limit.
import { Fragment, memo, useEffect, useState, type ReactNode } from 'react';
import { useSeen } from '@/next/ui/presence';
import Md from '@/next/features/chat/md';
import NowBlock from '@/next/features/chat/now';
import Steps, { isWorkflowCall } from '@/next/features/chat/steps';
import WorkflowCard from '@/next/features/workflows/workflow-card';
import SubsBlock from '@/next/features/subagents/subs-block';
import UserMessage from '@/next/features/chat/user-message';
import Outputs from '@/next/features/chat/outputs';
import DelegationCards from '@/next/features/chat/delegation-card';
import type { Entry, TurnFailureCode, TurnItem, UserEntry } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { Skeleton, useDelayed } from '@/next/ui/skeleton';
import Spinner from '@/next/ui/spinner';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { plural, t } from '@/next/i18n';
import { clock, duration } from '@/next/i18n/format';
import '@/next/styles/handoff.css';

const FAILURE_KEYS: Record<TurnFailureCode, string> = {
  'api-error': 'next.turnEnd.apiError',
  'rate-limit': 'next.turnEnd.rateLimit',
  'context-overflow': 'next.turnEnd.contextOverflow',
  'agent-exited': 'next.turnEnd.agentExited',
  unknown: 'next.turnEnd.unknown',
};

export interface ITurnHandlers {
  onToggle: (turnId: string) => void;
  /** Loads a finished turn's steps; resolves true once they are in the store, false when the load failed. */
  onLoadSteps?: (turnId: string) => Promise<boolean>;
  onContinueTurn: (turnId: string) => Promise<unknown>;
  onRetryTurn: (turnId: string) => Promise<unknown>;
  onRetryMessage: (clientId: string) => Promise<unknown>;
  onDeleteMessage: (clientId: string) => Promise<unknown>;
  /** A refused message sent again, as a new one; the refused one goes. */
  onResendMessage?: (clientId: string) => Promise<unknown>;
  renderGate?: (permissionId: string) => ReactNode;
  /** A new chat that branches off after this finished turn; absent where the machine cannot fork. */
  onForkTurn?: (turnId: string) => Promise<unknown>;
}

/** Under a finished answer: «Ответвить отсюда», a new chat with the history up to here. */
const TurnActions = ({
  turnId,
  onFork,
}: {
  turnId: string;
  onFork: (turnId: string) => Promise<unknown>;
}) => {
  const action = usePending<'fork'>();
  const busy = isBusy(action.phase);
  return (
    <div className="hx-turnacts">
      <button
        type="button"
        className="hx-turnacts__btn hx-tipped"
        aria-label={t('next.handoff.forkHere')}
        aria-busy={busy || undefined}
        disabled={busy}
        onClick={() => void action.run('fork', () => onFork(turnId))}
      >
        {busy ? <Spinner /> : <Icon name="git-fork" size="sm" />}
        <span className="hx-tip">{t('next.handoff.forkHere')}</span>
      </button>
    </div>
  );
};

const TurnEnd = ({ item, handlers }: { item: TurnItem; handlers: ITurnHandlers }) => {
  const action = usePending<'continue' | 'retry'>();
  const { turn } = item;
  const busy = isBusy(action.phase);
  if (turn.status === 'interrupted') {
    const at = turn.endedAt === null ? '' : ` · ${clock(turn.endedAt)}`;
    return (
      <div className="hx-turnend" data-kind="stopped" role="status">
        <Icon name="circle-stop" size="sm" className="hx-turnend__ic" />
        <span className="hx-turnend__text">
          <span className="hx-turnend__title">{t('next.turnEnd.stopped') + at}</span>
          {item.endNote && <span className="hx-turnend__why">{item.endNote}</span>}
        </span>
      </div>
    );
  }
  if (turn.status !== 'failed') return null;
  const code = turn.failure?.code ?? 'unknown';
  if (code === 'rate-limit') {
    // The usage limit is no breakdown: the limit card under the feed says when the agent goes on and offers the
    // choices, so the turn only notes where it stopped, quietly and without buttons of its own.
    const at = turn.endedAt === null ? '' : ` · ${clock(turn.endedAt)}`;
    return (
      <div className="hx-turnend" data-kind="stopped" role="status">
        <Icon name="clock" size="sm" className="hx-turnend__ic" />
        <span className="hx-turnend__text">
          <span className="hx-turnend__title">{t('next.turnEnd.limitHit') + at}</span>
        </span>
      </div>
    );
  }
  const why = turn.failure?.detail
    ? `${t(FAILURE_KEYS[code])}: ${turn.failure.detail}`
    : t(FAILURE_KEYS[code]);
  return (
    <div className="hx-turnend" data-kind="failed" role="status">
      <Icon name="circle-alert" size="sm" className="hx-turnend__ic" />
      <span className="hx-turnend__text">
        <span className="hx-turnend__title">{t('next.turnEnd.failed')}</span>
        <span className="hx-turnend__why">
          {action.phase === PENDING.failed ? t('next.turnEnd.actionFailed') : why}
        </span>
      </span>
      <span className="hx-turnend__acts">
        <button
          type="button"
          className="hx-btn"
          data-size="sm"
          data-busy={(busy && action.key === 'continue') || undefined}
          disabled={busy}
          onClick={() => action.run('continue', () => handlers.onContinueTurn(turn.id))}
        >
          <span>{t('next.turnEnd.continue')}</span>
          <Spinner />
        </button>
        <button
          type="button"
          className="hx-btn"
          data-size="sm"
          data-variant="ghost"
          data-busy={(busy && action.key === 'retry') || undefined}
          disabled={busy}
          onClick={() => action.run('retry', () => handlers.onRetryTurn(turn.id))}
        >
          <span>{t('next.turnEnd.retry')}</span>
          <Spinner />
        </button>
      </span>
    </div>
  );
};

const StepsLoading = () =>
  useDelayed(true) ? (
    <div className="hx-work__loading" role="status" aria-label={t('next.work.loading')}>
      <Skeleton />
      <Skeleton />
    </div>
  ) : null;

/**
 * The turn's steps cut at the messages the person steered into it: they are messages of the feed, not steps. The
 * work before the first one, the message, the work after it, and so on: `parts` is one longer than `messages`.
 */
export const splitAtMessages = (steps: Entry[]): { parts: Entry[][]; messages: UserEntry[] } => {
  const parts: Entry[][] = [[]];
  const messages: UserEntry[] = [];
  steps.forEach((entry) => {
    if (entry.t === 'user') {
      messages.push(entry);
      parts.push([]);
    } else parts[parts.length - 1].push(entry);
  });
  return { parts, messages };
};

interface ITurnViewProps {
  item: TurnItem;
  open: boolean;
  machineName: string;
  handlers: ITurnHandlers;
}

const TurnView = ({ item, open, machineName, handlers }: ITurnViewProps) => {
  const { turn, steps } = item;
  const live = turn.status === 'live';
  // The messages steered into the turn are messages of the feed, as any: the work is cut where each of them stands.
  const { parts, messages: steered } = splitAtMessages(steps ?? []);
  const worked = parts.flat();
  // A finished turn keeps its dynamic workflows in view under the folded work: they run on after the turn.
  const flows = live ? [] : worked.filter(isWorkflowCall);
  const expanded = live || open;
  const seen = useSeen(expanded);
  // A turn whose steps loaded empty has nothing to unfold: no «0 actions» header over a skeleton that never fills.
  const [loadedEmpty, setLoadedEmpty] = useState(false);
  const hasWork = live
    ? (steps?.length ?? 0) > 0
    : (turn.stepCount - (item.woke ? 1 : 0) > 0 && !loadedEmpty) || worked.length > 0;
  const toggle = () => handlers.onToggle(turn.id);
  // Steps load whenever the turn is unfolded without them: by a tap, or open from the start (the last turn).
  const { onLoadSteps } = handlers;
  const needSteps = expanded && hasWork && steps === null;
  useEffect(() => {
    if (!needSteps || !onLoadSteps) return;
    let gone = false;
    void onLoadSteps(turn.id).then((loaded) => {
      if (loaded && !gone) setLoadedEmpty(true);
    });
    return () => {
      gone = true;
    };
  }, [needSteps, onLoadSteps, turn.id]);
  let label = '';
  if (!live && turn.toolCount === 0) label = t('next.work.thought', { time: duration(turn.durationMs ?? 0) });
  else if (!live) {
    label = t('next.work.folded', {
      time: duration(turn.durationMs ?? 0),
      actions: plural(turn.toolCount, 'next.work.actions'),
    });
  }
  const folded = (
    <button type="button" className="hx-work__head" aria-expanded={expanded} onClick={toggle}>
      <span>{label}</span>
      <Icon name="chevron-right" size="sm" className="hx-work__chev" />
    </button>
  );
  // Each part of the work stands under the message it follows; the last one carries the answer and what ends the turn.
  const headAt = Math.max(
    0,
    parts.findIndex((part) => part.length > 0),
  );
  const work = (part: Entry[] | null, index: number) => {
    const last = index === steered.length;
    const first = index === headAt;
    const shown = first ? hasWork : part !== null && part.length > 0 && expanded;
    if (!shown) return null;
    return (
      <div className="hx-work" data-live={live || undefined} aria-expanded={expanded}>
        {first && !live && folded}
        <div className="hx-work__body">
          <div>
            {seen && (
              <div className="hx-work__inner">
                {part === null ? (
                  <StepsLoading />
                ) : (
                  <Steps
                    steps={live ? part : part.filter((entry) => !isWorkflowCall(entry))}
                    live={live && last}
                    renderGate={handlers.renderGate}
                  />
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    );
  };
  const ends = (
    <>
      {live && (
        <NowBlock
          now={item.now}
          startedAt={turn.startedAt}
          wait={item.wait}
          machineName={machineName}
          steps={seen ? steps : null}
        />
      )}
      {flows.map((entry) => (
        <WorkflowCard key={entry.id} entry={entry} />
      ))}
      {item.answer && <Md md={item.answer.md} />}
      <DelegationCards startedAt={turn.startedAt} endedAt={turn.endedAt} />
      <Outputs item={item} />
      <TurnEnd item={item} handlers={handlers} />
      {handlers.onForkTurn && turn.status === 'done' && item.answer && (
        <TurnActions turnId={turn.id} onFork={handlers.onForkTurn} />
      )}
    </>
  );
  return (
    <>
      <div className="hx-msg" data-kind="turn">
        {turn.author.role && (
          <div className="hx-author">
            <span className="hx-author__name">{t(`next.agent.${turn.author.provider}`)}</span>
            <span className="hx-author__role">{turn.author.role}</span>
          </div>
        )}
        {item.woke && <SubsBlock agents={[item.woke]} />}
        {work(steps === null ? null : parts[0], 0)}
        {steered.length === 0 && ends}
      </div>
      {steered.map((entry, index) => (
        <Fragment key={entry.id}>
          <UserMessage
            text={entry.text}
            attachments={entry.attachments}
            outgoing={null}
            own={entry.clientId !== null}
            steered
            offline={false}
            onRetry={handlers.onRetryMessage}
            onResend={handlers.onResendMessage}
            onDelete={handlers.onDeleteMessage}
          />
          <div className="hx-msg" data-kind="turn">
            {work(parts[index + 1], index + 1)}
            {index === steered.length - 1 && ends}
          </div>
        </Fragment>
      ))}
    </>
  );
};

export default memo(TurnView);
