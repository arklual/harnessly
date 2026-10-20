import { t } from '@/lib/i18n';
import { memo } from 'react';
import { CheckCircle2, Circle, CircleDot, CircleSlash } from 'lucide-react';
import { METER_TONE, TwMeter } from '@/components/agents/ui';
import { cn } from '@/lib/utils';
import type { ITaskItem } from '@/types/timeline';

const ICONS = {
  completed: CheckCircle2,
  in_progress: CircleDot,
  pending: Circle,
  blocked: CircleSlash,
} as const;

const PlanTab = ({ tasks }: { tasks: ITaskItem[] }) => {
  const done = tasks.filter((task) => task.status === 'completed').length;
  return (
    <div className="tw-plan">
      <TwMeter
        value={done}
        max={tasks.length}
        label={t('inspector.planTab.progress')}
        valueText={t('inspector.planTab.doneOf', { done, total: tasks.length })}
        tone={METER_TONE.success}
      />
      <ol className="tw-plan-list">
        {tasks.map((task) => {
          const Icon = ICONS[task.status] ?? Circle;
          return (
            <li
              key={task.taskId}
              className={cn('tw-plan-item', `tw-plan-${task.status}`)}
              title={task.description}
            >
              <Icon />
              <span>{task.subject}</span>
            </li>
          );
        })}
      </ol>
    </div>
  );
};

export default memo(PlanTab);
