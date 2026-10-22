import { describe, expect, it } from 'vitest';
import { checklistChanged, summarizeChecklist } from '@/lib/task-checklist';
import { foldTasks } from '@/hooks/use-timeline';
import type { ITaskItem, ITimelineEntry } from '@/types/timeline';

const task = (taskId: string, status: ITaskItem['status']): ITaskItem => ({
  taskId,
  subject: `s${taskId}`,
  status,
});

describe('summarizeChecklist', () => {
  it('counts finished tasks and names the one in progress', () => {
    const s = summarizeChecklist([task('1', 'completed'), task('2', 'in_progress'), task('3', 'pending')]);
    expect(s).toMatchObject({ done: 1, total: 3, allDone: false });
    expect(s.current?.taskId).toBe('2');
  });

  it('falls to the next waiting task when none is in progress', () => {
    expect(summarizeChecklist([task('1', 'completed'), task('2', 'pending')]).current?.taskId).toBe('2');
  });

  it('is done only when there are tasks and all are completed', () => {
    expect(summarizeChecklist([]).allDone).toBe(false);
    const s = summarizeChecklist([task('1', 'completed')]);
    expect(s.allDone).toBe(true);
    expect(s.current).toBeNull();
  });
});

describe('checklistChanged', () => {
  it('sees a new task and a new status, not an identical list', () => {
    const a = [task('1', 'pending')];
    expect(checklistChanged(a, [...a, task('2', 'pending')])).toBe(true);
    expect(checklistChanged(a, [task('1', 'completed')])).toBe(true);
    expect(checklistChanged(a, [task('1', 'pending')])).toBe(false);
  });
});

describe('foldTasks', () => {
  const progress = (e: Partial<ITimelineEntry> & Record<string, unknown>): ITimelineEntry =>
    ({ id: 'x', type: 'task-progress', timestamp: 1, ...e }) as ITimelineEntry;

  it('creates tasks and applies updates by task id', () => {
    const tasks = foldTasks([
      progress({ action: 'create', taskId: '1', subject: 'a', status: 'pending' }),
      progress({ action: 'create', taskId: '2', subject: 'b', status: 'pending' }),
      progress({ action: 'update', taskId: '1', status: 'completed' }),
      progress({ action: 'update', taskId: '9', status: 'completed' }),
    ]);
    expect(tasks.map((t) => [t.taskId, t.status])).toEqual([
      ['1', 'completed'],
      ['2', 'pending'],
    ]);
  });
});
