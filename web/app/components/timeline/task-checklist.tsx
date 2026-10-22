// The agent's task list above the feed: a folded header with the progress, unfolding into one line per task.
import { useEffect, useState } from 'react';
import { Check, ChevronDown, CircleSlash, ListChecks, X } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { checklistChanged, summarizeChecklist } from '@/lib/task-checklist';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITaskItem, TCliState, TTaskStatus } from '@/types/timeline';

/** The list folds on its own this long after the agent finished the last task and went idle. */
const FOLD_AFTER_MS = 3000;

const StatusMark = ({ status }: { status: TTaskStatus }) => {
  if (status === 'completed') return <Check size={13} />;
  if (status === 'blocked') return <CircleSlash size={13} />;
  return <span className="tw-checklist-dot" />;
};

export default function TaskChecklist({ tasks, cliState }: { tasks: ITaskItem[]; cliState: TCliState }) {
  const holdPosition = useHoldTimelinePosition();
  const { done, total, allDone, current } = summarizeChecklist(tasks);
  const [folded, setFolded] = useState(allDone);
  const [dismissed, setDismissed] = useState(false);
  const [seen, setSeen] = useState(tasks);

  if (tasks !== seen) {
    if (folded && checklistChanged(seen, tasks)) setFolded(false);
    setSeen(tasks);
  }

  useEffect(() => {
    if (!allDone || cliState !== 'idle') return;
    const timer = setTimeout(() => setFolded(true), FOLD_AFTER_MS);
    return () => clearTimeout(timer);
  }, [allDone, cliState]);

  if (dismissed || total === 0) return null;
  return (
    <section
      className={cn('tw-checklist', allDone && 'tw-checklist-done')}
      aria-label={t('timeline.taskChecklist.label')}
      data-testid="task-checklist"
    >
      <div className="tw-checklist-head">
        <button
          type="button"
          className="tw-checklist-toggle"
          aria-expanded={!folded}
          onClick={() => {
            holdPosition();
            setFolded((v) => !v);
          }}
        >
          {allDone ? <Check size={14} /> : <ListChecks size={14} />}
          <span className="tw-checklist-count" aria-live="polite">
            {t('timeline.taskChecklist.progress', { done, total })}
          </span>
          {folded && current && <span className="tw-checklist-current">{current.subject}</span>}
          <ChevronDown size={14} className="tw-checklist-chevron" />
        </button>
        <button
          type="button"
          className="tw-checklist-close"
          aria-label={t('timeline.taskChecklist.close')}
          onClick={() => setDismissed(true)}
        >
          <X size={12} />
        </button>
      </div>
      {!folded && (
        <ol className="tw-checklist-list">
          {tasks.map((task) => (
            <li
              key={task.taskId}
              className={cn('tw-checklist-item', `tw-checklist-${task.status}`)}
              title={task.description}
            >
              <span className="tw-checklist-mark" aria-hidden>
                <StatusMark status={task.status} />
              </span>
              <span className="tw-checklist-subject">{task.subject}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
