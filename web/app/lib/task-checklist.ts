// The agent's task checklist reduced to what its header shows.
import type { ITaskItem } from '@/types/timeline';

export interface IChecklistSummary {
  done: number;
  total: number;
  allDone: boolean;
  /** The task the agent is on, or the next one waiting; what the folded header names. */
  current: ITaskItem | null;
}

export const summarizeChecklist = (tasks: readonly ITaskItem[]): IChecklistSummary => {
  const done = tasks.filter((task) => task.status === 'completed').length;
  return {
    done,
    total: tasks.length,
    allDone: tasks.length > 0 && done === tasks.length,
    current:
      tasks.find((task) => task.status === 'in_progress') ??
      tasks.find((task) => task.status === 'pending') ??
      null,
  };
};

/** Whether a change to the list should unfold a folded checklist: a task added, or one changed its status. */
export const checklistChanged = (before: readonly ITaskItem[], after: readonly ITaskItem[]): boolean =>
  before.length !== after.length || after.some((task, i) => before[i]?.status !== task.status);
