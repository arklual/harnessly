// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import TaskTab from '@/components/agents/pane/task-tab';
import type { ITask } from '@/lib/hl-task';

const TASK: ITask = {
  key: 'EX-1',
  title: 'One',
  project: 'EX',
  type: 'Task',
  state: 'Open',
  priority: null,
  resolved: false,
  assignee: null,
  parent: null,
  children: [],
  description: '',
  comments: [],
  updated: null,
};

const serve = (task: ITask) =>
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) =>
      String(input).includes('/task?')
        ? new Response(JSON.stringify(task), { status: 200 })
        : new Response('{}', { status: 404 }),
    ),
  );

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('TaskTab fields', () => {
  it('shows the tracker fields that have a value, a link as a link', async () => {
    serve({
      ...TASK,
      fields: [
        { name: 'Story points', value: '5', kind: 'number', url: null },
        { name: 'Sprint', value: ' ', kind: null, url: null },
        { name: 'Epic', value: 'EX-7', kind: 'link', url: 'https://tracker.example.com/issue/EX-7' },
      ],
    });
    render(<TaskTab taskKey="conn:c1:issue:EX-1" onOpenTask={() => {}} />);
    expect(await screen.findByText('Story points')).toBeTruthy();
    expect(screen.getByText('5')).toBeTruthy();
    expect(screen.queryByText('Sprint')).toBeNull();
    expect(screen.getByText('EX-7').closest('a')?.getAttribute('href')).toBe(
      'https://tracker.example.com/issue/EX-7',
    );
  });

  it('draws no field list for a task without fields', async () => {
    serve(TASK);
    const { container } = render(<TaskTab taskKey="conn:c1:issue:EX-1" onOpenTask={() => {}} />);
    expect(await screen.findByText('One')).toBeTruthy();
    expect(container.querySelector('.tw-task-fields')).toBeNull();
  });
});
