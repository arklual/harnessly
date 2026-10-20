import dayjs from 'dayjs';
import { Activity, AlarmClock, Bot, SquareTerminal } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { POPOVER_SIDE, TwPopover, twPopoverTriggerProps } from '@/components/agents/ui';
import useBackgroundTasks from '@/hooks/use-background-tasks';
import type { IBackgroundTask, TBackgroundKind } from '@/types/server/hl-background';
import { t, plural } from '@/lib/i18n';

const KIND: Record<TBackgroundKind, { icon: typeof Bot; one: string; count: string }> = {
  shell: {
    icon: SquareTerminal,
    one: t('agents.backgroundChip.shell'),
    count: 'agents.backgroundChip.shells',
  },
  monitor: {
    icon: Activity,
    one: t('agents.backgroundChip.monitor'),
    count: 'agents.backgroundChip.monitors',
  },
  agent: { icon: Bot, one: t('agents.backgroundChip.agent'), count: 'agents.backgroundChip.agents' },
  wakeup: {
    icon: AlarmClock,
    one: t('agents.backgroundChip.wakeup'),
    count: 'agents.backgroundChip.wakeups',
  },
};

const KIND_ORDER = ['shell', 'monitor', 'agent', 'wakeup'] as const;

const countOf = (n: number, kind: TBackgroundKind) => plural(n, KIND[kind].count);

export const backgroundSummary = (tasks: IBackgroundTask[]) =>
  KIND_ORDER.map((kind) => [kind, tasks.filter((task) => task.kind === kind).length] as const)
    .filter(([, n]) => n > 0)
    .map(([kind, n]) => countOf(n, kind))
    .join(' · ');

const TaskRow = ({ task }: { task: IBackgroundTask }) => {
  const Icon = KIND[task.kind].icon;
  const when =
    task.kind === 'wakeup' && task.until
      ? t('agents.backgroundChip.at', { time: dayjs(task.until).format('HH:mm') })
      : task.startedAt
        ? dayjs(task.startedAt).fromNow()
        : '';
  return (
    <li className={`tw-bg-task tw-bg-${task.kind}`}>
      <span className="tw-bg-task-icon">
        <Icon aria-hidden="true" />
      </span>
      <span className="tw-bg-task-label">{task.label || KIND[task.kind].one}</span>
      {when && <small>{when}</small>}
    </li>
  );
};

/**
 * What the agent keeps running after its turn, the way Claude Code shows it in its footer: a quiet control like
 * the context ring beside it, the icon of what runs and how many; the list opens in the shared glass popover.
 */
const BackgroundChip = ({ tabId }: { tabId: string | null }) => {
  const tasks = useBackgroundTasks(tabId);
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  if (tasks.length === 0) return null;
  const summary = backgroundSummary(tasks);
  const kinds = KIND_ORDER.filter((kind) => tasks.some((task) => task.kind === kind));
  // The chip sits inside the clickable header row; its clicks must not toggle the session details.
  return (
    <span
      className="tw-bg-chip-wrap"
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <button
        ref={triggerRef}
        type="button"
        className="tw-bg-chip"
        title={t('agents.backgroundChip.running', { summary })}
        aria-label={t('agents.backgroundChip.running', { summary })}
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => setOpen((value) => !value)}
      >
        {kinds.map((kind) => {
          const Icon = KIND[kind].icon;
          return <Icon key={kind} className={`tw-bg-${kind}-icon`} aria-hidden="true" />;
        })}
        <span>{tasks.length}</span>
      </button>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={triggerRef}
        side={POPOVER_SIDE.top}
        align="start"
        title={t('agents.backgroundChip.heading', { summary })}
        className="tw-bg-pop"
      >
        <ul className="tw-bg-list">
          {tasks.map((task) => (
            <TaskRow key={task.id} task={task} />
          ))}
        </ul>
      </TwPopover>
    </span>
  );
};

/** The same list as a section of the composer's session sheet on a phone, where a popover in a sheet would not fit. */
export const BackgroundTasksSection = ({ tabId }: { tabId: string | null }) => {
  const tasks = useBackgroundTasks(tabId);
  if (tasks.length === 0) return null;
  return (
    <section className="tw-csheet-section">
      <h3 className="tw-csheet-heading">
        {t('agents.backgroundChip.heading', { summary: backgroundSummary(tasks) })}
      </h3>
      <ul className="tw-bg-list">
        {tasks.map((task) => (
          <TaskRow key={task.id} task={task} />
        ))}
      </ul>
    </section>
  );
};

export default BackgroundChip;
