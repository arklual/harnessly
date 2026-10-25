// The plan the agent keeps (.hx-plan): how much of it is done with a meter, then its steps — done, the one in work,
// the ones ahead. A blocked step says so. The agent writes the plan; the pane only shows it.
import { useLayoutEffect, useRef } from 'react';
import type { Task, TaskStatus } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import LoadFailed from '@/next/ui/load-failed';
import Swap from '@/next/ui/swap';

const ROW_STATE: Record<TaskStatus, string> = {
  completed: 'done',
  in_progress: 'run',
  pending: 'todo',
  blocked: 'todo',
};

const PlanSkeleton = () => (
  <>
    <span className="hx-sk hx-plan__sk" />
    <span className="hx-sk hx-plan__sk" />
    <span className="hx-sk hx-plan__sk" />
  </>
);

export interface IPlanViewProps {
  /** null while it loads. */
  tasks: readonly Task[] | null;
  error: string | null;
  onRetry: () => void;
}

const PlanView = ({ tasks, error, onRetry }: IPlanViewProps) => {
  const meterRef = useRef<HTMLDivElement>(null);
  const done = tasks?.filter((x) => x.status === 'completed').length ?? 0;
  const all = tasks?.length ?? 0;

  useLayoutEffect(() => {
    meterRef.current?.style.setProperty('--fill', `${all ? Math.round((done / all) * 100) : 0}%`);
  }, [done, all]);

  if (tasks === null && error !== null)
    return (
      <div className="hx-tv" data-tool="plan">
        <LoadFailed title={t('next.load.plan')} reason={error} onRetry={onRetry} />
      </div>
    );
  return (
    <div
      className="hx-tv"
      data-tool="plan"
      role={tasks === null ? 'status' : undefined}
      aria-label={tasks === null ? t('next.plan.loading') : undefined}
    >
      <Swap ready={tasks !== null} skeleton={<PlanSkeleton />}>
        {tasks !== null && tasks.length === 0 && <div className="hx-tv__empty">{t('next.plan.empty')}</div>}
        {tasks !== null && tasks.length > 0 && (
          <>
            <div className="hx-tv__sum">
              <span className="hx-tv__big">{t('next.plan.doneOf', { done, all })}</span>
              <span className="hx-tv__muted">{t('next.plan.done')}</span>
            </div>
            <div className="hx-meter" ref={meterRef} data-tone={done === all ? 'ok' : undefined}>
              <i />
            </div>
            <ol className="hx-plan">
              {tasks.map((task, i) => (
                <li
                  key={task.id}
                  className="hx-plan__row"
                  data-st={ROW_STATE[task.status]}
                  data-i={Math.min(i, 8)}
                >
                  <span className="hx-plan__mark">
                    <i className="hx-icon" data-size="sm" data-i="check" aria-hidden="true" />
                  </span>
                  <span>
                    {task.subject}
                    {task.status === 'blocked' && (
                      <span className="hx-plan__blocked"> · {t('next.plan.blocked')}</span>
                    )}
                  </span>
                </li>
              ))}
            </ol>
            <p className="hx-tv__note">{t('next.plan.note')}</p>
          </>
        )}
      </Swap>
    </div>
  );
};

export default PlanView;
