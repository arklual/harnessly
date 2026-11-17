import { describe, expect, it } from 'vitest';
import { LOOK, type TMeta } from '@/next/features/artifacts/model';
import { groupTasks, trackerOf, type ILinkMade } from '@/next/features/chat/made';
import { linkOf } from '@/next/features/chat/link-output';

const config = { artifactsUrl: null, boardsUrl: null, readerUrl: null, githubHost: 'github.com' };

const task = (key: string, op: 'created' | 'changed' = 'created'): ILinkMade => {
  const output = { kind: 'task' as const, op, title: key, ref: `https://yt.example.com/issue/${key}` };
  const link = linkOf(output, config);
  if (!link) throw new Error('no link');
  return { output, link };
};

const issue = (key: string, parent: string | null, resolved = false): TMeta => ({
  kind: 'issue',
  key,
  title: `Задача ${key}`,
  state: resolved ? 'Done' : 'Open',
  resolved,
  assignee: null,
  parent: parent ? { key: parent, title: 'Возвраты 2.0' } : null,
});

describe('groupTasks', () => {
  it('puts the tasks of one epic under it, the epic itself as its head', () => {
    const tasks = [task('PAY-200', 'changed'), task('PAY-201'), task('PAY-202'), task('HRN-1')];
    const parents: Record<string, string | null> = { 'PAY-201': 'PAY-200', 'PAY-202': 'PAY-200' };
    const { epics, batch, single } = groupTasks(tasks, (made) => ({
      meta: issue(made.link.key, parents[made.link.key] ?? null, made.link.key === 'PAY-201'),
      look: LOOK.got,
    }));
    expect(epics).toHaveLength(1);
    expect(epics[0].kids.map((m) => m.link.key)).toEqual(['PAY-201', 'PAY-202']);
    expect(epics[0].head?.link.key).toBe('PAY-200');
    expect(batch).toEqual([]);
    expect(single.map((m) => m.link.key)).toEqual(['HRN-1']);
  });

  it('makes a batch of four tasks created at once, not of three', () => {
    const none = () => ({ meta: null, look: LOOK.wait });
    expect(
      groupTasks(
        ['A-1', 'A-2', 'A-3', 'A-4'].map((k) => task(k)),
        none,
      ).batch,
    ).toHaveLength(4);
    expect(
      groupTasks(
        ['A-1', 'A-2', 'A-3'].map((k) => task(k)),
        none,
      ).batch,
    ).toEqual([]);
  });
});

describe('trackerOf', () => {
  it('tells the tracker by the address', () => {
    expect(trackerOf('https://yt.example.com/issue/HRN-1')).toBe('youtrack');
    expect(trackerOf('https://acme.atlassian.net/browse/PAY-1')).toBe('jira');
    expect(trackerOf('https://example.com/tasks/1')).toBeNull();
  });
});
