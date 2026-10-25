// The right column of a chat (the design's .hx-side): Claude's recap of the session on top, then its subagents and the commands
// it left running, each group folded into one line — what it is and how many run — and opened into rows. A subagent's
// row says what it is, how long it has gone and the model and depth it runs on, nothing of its steps; a command's, how long.
// A named teammate is a subagent row with its name, a muted «напарник» and what it was started for; it works, waits for
// a message (idle: not counted as at work) or is done. A subagent's row opens it in the right pane.
// A dynamic workflow is a row of its own group: its name, how far its phases are («Review 3/5 · Fix 1/4») and how long
// it has gone; the row opens its agents in the right pane.
// Of the subagents five rows show at first (the running ones, then the latest) and «Ещё N» opens the rest by height.
// A stop waits for the runtime: the row says «Останавливаем…» until the task's own event ends it. Each group's name
// carries its icon, and the person's fold is kept per browser (the commands start folded). Under the recap stand the
// pictures the agent looked at (shots-strip.tsx).
import { forwardRef, useCallback, useState } from 'react';
import type { BackgroundTask, Recap, Shot } from '@/next/protocol/v2';
import { orderAgents, splitAgents } from '@/next/features/background/agent-order';
import { isWorkflow, progressOf, workflowName } from '@/next/features/workflows/run';
import { openSubagent } from '@/next/features/subagents/open';
import { toggleGroup, useGroupOpen } from '@/next/features/background/rail-folds';
import RecapCard from '@/next/features/background/recap-card';
import ShotsStrip from '@/next/features/background/shots-strip';
import { effortLabel, shortModelName } from '@/next/features/composer/agents';
import Icon from '@/next/ui/icon';
import { SheetGrip, useSheetDrag } from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import { elapsed } from '@/next/i18n/format';
import { plural, t } from '@/next/i18n';
import '@/next/styles/background.css';

const GROUP = { subagent: 'subagent', workflow: 'workflow', command: 'command' } as const;
type TGroup = (typeof GROUP)[keyof typeof GROUP];
const GROUP_ICON = { subagent: 'workflow', workflow: 'layers', command: 'terminal' } as const;

const RUN_KEY = {
  subagent: { all: 'next.bg.agentsRun', of: 'next.bg.agentsRunOf' },
  workflow: { all: 'next.flow.flowsRun', of: 'next.bg.agentsRunOf' },
  command: { all: 'next.bg.commandsRun', of: 'next.bg.commandsRunOf' },
} as const satisfies Record<TGroup, { all: string; of: string }>;

/** The design's row states: run, wait (an idle teammate), done, stopped (a failed task reads as stopped with its own word). */
const ROW_STATUS = {
  running: 'run',
  idle: 'wait',
  done: 'done',
  stopped: 'stopped',
  failed: 'stopped',
} as const satisfies Record<BackgroundTask['status'], string>;

export interface IBackgroundPanelProps {
  tasks: readonly BackgroundTask[];
  recap: Recap | null;
  /** The pictures the agent looked at, newest first; null while the conversation's snapshot has not said. */
  shots: readonly Shot[] | null;
  /** Where a picture's bytes are. */
  shotSrc: (shot: Shot) => string;
  /** The column is on screen (it has room, or the person asked for it); off, it waits out of sight with its width. */
  shown: boolean;
  now: number;
  /** Ids whose stop was asked and not answered yet. */
  stopping: ReadonlySet<string>;
  onStop: (task: BackgroundTask) => void;
  /** Folds the column away: on the phone, where it is a bottom sheet, its scrim and a swipe down do it. */
  onHide?: () => void;
}

const OVER_WORD = {
  idle: 'next.bg.idle',
  done: 'next.bg.done',
  failed: 'next.bg.failed',
  stopped: 'next.bg.stopped',
} as const;

/** A running task says how long it has gone; one that is over says how it ended and how long it took. */
const stateLabel = (task: BackgroundTask, now: number): string => {
  if (task.status === 'running') return elapsed(now - task.startedAt);
  const word = t(OVER_WORD[task.status]);
  if (task.endedAt === null || task.endedAt <= task.startedAt) return word;
  return `${word} · ${elapsed(task.endedAt - task.startedAt)}`;
};

/** «3 из 8 работают» while some run, «8 работают» when all do; once none runs, how the others ended. */
const summaryOf = (group: TGroup, tasks: readonly BackgroundTask[]): string => {
  const run = tasks.filter((x) => x.status === 'running').length;
  const idle = tasks.filter((x) => x.status === 'idle').length;
  const done = tasks.filter((x) => x.status === 'done').length;
  const stopped = tasks.length - run - idle - done;
  if (run === tasks.length) return plural(run, RUN_KEY[group].all);
  if (run > 0) return plural(run, RUN_KEY[group].of, { total: tasks.length });
  const parts: string[] = [];
  if (idle) parts.push(plural(idle, 'next.bg.countIdle'));
  if (done) parts.push(plural(done, 'next.bg.countDone'));
  if (stopped) parts.push(plural(stopped, 'next.bg.countStopped'));
  return parts.join(' · ');
};

/** «Sonnet 5.5 · Высокое»: the model a subagent runs on and its depth, whichever the agent said. */
const runsOn = (task: BackgroundTask): string => {
  const parts: string[] = [];
  if (task.model) parts.push(shortModelName(task.model));
  if (task.effort) parts.push(effortLabel(task.effort));
  return parts.join(' · ');
};

const Row = ({
  task,
  now,
  stopping,
  onStop,
}: {
  task: BackgroundTask;
  now: number;
  stopping: boolean;
  onStop: () => void;
}) => {
  const status = ROW_STATUS[task.status];
  const flow = isWorkflow(task) ? task : null;
  const agent = task.kind === 'subagent' || flow !== null;
  const mate = task.teammate === true;
  const on = task.kind === 'subagent' ? runsOn(task) : '';
  // A subagent's row is quiet: no current tool or last words, only what it runs on; a teammate's says what it was
  // started for. A command keeps its own line.
  let last = agent ? '' : (task.last ?? '');
  if (mate) last = task.role ?? '';
  if (flow) last = progressOf(flow.workflow);
  if (stopping) last = t('next.bg.stopping');
  else if (task.devServer) last = t('next.bg.devServer');
  const txt = (
    <span className="hx-bg__txt">
      <span className="hx-bg__top">
        <span className="hx-bg__name">{flow ? workflowName(flow) : task.title}</span>
        {mate && <span className="hx-bg__mate">{t('next.bg.teammate')}</span>}
        <span className="hx-bg__state">
          <span className="hx-bg__dot" />
          {stateLabel(task, now)}
        </span>
      </span>
      {(on || last) && (
        <span className="hx-bg__sub">
          {on && <span className="hx-bg__on">{on}</span>}
          {last && <span className="hx-bg__last">{last}</span>}
        </span>
      )}
    </span>
  );
  return (
    <div className="hx-bg__row" data-status={status}>
      {agent ? (
        <button type="button" className="hx-bg__main" onClick={() => openSubagent(task.id)}>
          {txt}
        </button>
      ) : (
        <div className="hx-bg__main">{txt}</div>
      )}
      {status === 'run' && task.stoppable && (
        <button
          type="button"
          className="hx-bg__stop hx-tipped"
          disabled={stopping}
          aria-label={agent ? t('next.bg.stopAgent') : t('next.bg.stopCommand')}
          onClick={onStop}
        >
          <Icon name={stopping ? 'loader-circle' : 'circle-stop'} size="sm" />
          <span className="hx-tip" data-align="end">
            {agent ? t('next.bg.stopAgent') : t('next.bg.stopCommand')}
          </span>
        </button>
      )}
    </div>
  );
};

interface IGroupProps {
  group: TGroup;
  label: string;
  rows: readonly BackgroundTask[];
  head: readonly BackgroundTask[];
  rest: readonly BackgroundTask[];
  renderRow: (task: BackgroundTask) => React.ReactNode;
}

const Group = ({ group, label, rows, head, rest, renderRow }: IGroupProps) => {
  const open = useGroupOpen(group);
  // The subagents past the fifth row: folded until «Ещё N» is pressed.
  const [more, setMore] = useState(false);
  return (
    <div className="hx-side__sec" aria-expanded={open}>
      <button type="button" className="hx-side__head" onClick={() => toggleGroup(group)}>
        <span className="hx-side__ttl">
          <Icon name={GROUP_ICON[group]} size="sm" />
          {label}
        </span>
        <span className="hx-side__text" data-run={rows.some((x) => x.status === 'running')}>
          {summaryOf(group, rows)}
        </span>
        <Icon name="chevron-down" size="sm" className="hx-side__chev" />
      </button>
      <div className="hx-side__body">
        <div className="hx-side__clip" inert={!open || undefined}>
          {head.map(renderRow)}
          {rest.length > 0 && (
            <>
              <div className="hx-side__rest" data-open={more}>
                <div className="hx-side__rest-clip" inert={!more || undefined}>
                  {rest.map(renderRow)}
                </div>
              </div>
              <button
                type="button"
                className="hx-side__more"
                aria-expanded={more}
                onClick={() => setMore((v) => !v)}
              >
                {more ? t('next.bg.moreLess') : t('next.bg.more', { n: rest.length })}
                <Icon name="chevron-down" size="sm" className="hx-side__more-chev" />
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

const BackgroundPanel = forwardRef<HTMLElement, IBackgroundPanelProps>(
  ({ tasks, recap, shots, shotSrc, shown, now, stopping, onStop, onHide }, ref) => {
    const phone = usePhone();
    const [aside, setAside] = useState<HTMLElement | null>(null);
    const setRef = useCallback(
      (node: HTMLElement | null) => {
        setAside(node);
        if (typeof ref === 'function') ref(node);
        else if (ref) ref.current = node;
      },
      [ref],
    );
    useSheetDrag(aside, () => onHide?.(), phone && shown && onHide !== undefined);
    const agents = tasks.filter((x) => x.kind === 'subagent');
    const flows = orderAgents(tasks.filter((x) => x.kind === 'workflow'));
    const commands = tasks.filter((x) => x.kind === 'command');
    const split = splitAgents(agents);
    const groups = (
      [
        { key: GROUP.workflow, label: t('next.bg.workflows'), rows: flows, head: flows, rest: [] },
        { key: GROUP.subagent, label: t('next.bg.agents'), rows: agents, head: split.head, rest: split.rest },
        { key: GROUP.command, label: t('next.bg.commands'), rows: commands, head: commands, rest: [] },
      ] as const
    ).filter((g) => g.rows.length > 0);

    const row = (task: BackgroundTask) => (
      <Row key={task.id} task={task} now={now} stopping={stopping.has(task.id)} onStop={() => onStop(task)} />
    );

    return (
      <>
        {onHide && <div className="hx-side__scrim" data-shown={shown} aria-hidden="true" onClick={onHide} />}
        <aside ref={setRef} className="hx-side" data-shown={shown} aria-label={t('next.header.background')}>
          <SheetGrip />
          <RecapCard recap={recap} now={now} />
          <ShotsStrip shots={shots} srcOf={shotSrc} now={now} />
          {groups.map((g) => (
            <Group
              key={g.key}
              group={g.key}
              label={g.label}
              rows={g.rows}
              head={g.head}
              rest={g.rest}
              renderRow={row}
            />
          ))}
        </aside>
      </>
    );
  },
);
BackgroundPanel.displayName = 'BackgroundPanel';

export default BackgroundPanel;
