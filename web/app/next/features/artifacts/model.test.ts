import { describe, expect, it } from 'vitest';
import {
  ARTIFACT_KIND,
  LOOK,
  readMeta,
  readableInOrder,
  rowOf,
  sectionsOf,
  toneOfIssue,
  type IArtifactItem,
  type IMrLabels,
} from '@/next/features/artifacts/model';

const item = (patch: Partial<IArtifactItem>): IArtifactItem => ({
  url: 'https://a.example.com/a/plan',
  kind: ARTIFACT_KIND.doc,
  key: null,
  label: null,
  fallback: 'Document',
  where: 'a.example.com/a/plan',
  github: false,
  readable: true,
  at: 1,
  ...patch,
});

const MR: IMrLabels = { opened: 'Open', merged: 'Merged', closed: 'Closed', draft: 'Draft' };

describe('readMeta', () => {
  it('reads the three shapes of the runtime and nothing else', () => {
    expect(readMeta({ kind: 'artifact', title: 'Plan', description: 'What goes out' })).toEqual({
      kind: 'artifact',
      title: 'Plan',
      description: 'What goes out',
    });
    expect(
      readMeta({
        kind: 'issue',
        key: 'ABC-1',
        title: 'Fix',
        state: 'In Progress',
        resolved: false,
        assignee: { name: 'Ann', avatar: '/a?login=ann' },
      }),
    ).toMatchObject({ kind: 'issue', state: 'In Progress', assignee: { name: 'Ann' } });
    expect(
      readMeta({ kind: 'mr', title: 'Cache', state: 'merged', project: 'a/b', iid: 4, draft: false }),
    ).toMatchObject({
      kind: 'mr',
      state: 'merged',
    });
    expect(readMeta({ kind: 'artifact' })).toBeNull();
    expect(readMeta({ kind: 'weird', title: 'x' })).toBeNull();
    expect(readMeta(null)).toBeNull();
  });
});

describe('toneOfIssue', () => {
  it('tells done, review, work and the rest by the tracker state', () => {
    expect(toneOfIssue('Done', true)).toBe('done');
    expect(toneOfIssue('Testing', false)).toBe('review');
    expect(toneOfIssue('В работе', false)).toBe('progress');
    expect(toneOfIssue('Open', false)).toBe('open');
    expect(toneOfIssue(null, false)).toBe('open');
  });
});

describe('rowOf', () => {
  it('a document whose title is still being read has no title yet, not «Document»', () => {
    const row = rowOf(item({}), null, LOOK.wait, MR);
    expect(row.title).toBeNull();
    expect(row.pending).toBe(true);
    expect(row.sub).toBe('a.example.com/a/plan');
  });

  it('a document with its title shows it and what it is about', () => {
    const row = rowOf(
      item({}),
      { kind: 'artifact', title: 'Release plan', description: 'What goes out' },
      LOOK.got,
      MR,
    );
    expect(row).toMatchObject({ title: 'Release plan', sub: 'What goes out', pending: false });
  });

  it('a document the runtime does not know is called what the agent called it, else by its kind', () => {
    expect(rowOf(item({ label: 'The plan' }), null, LOOK.none, MR).title).toBe('The plan');
    expect(rowOf(item({}), null, LOOK.none, MR).title).toBe('Document');
  });

  it('a task says its key and state, rests when done and brings its assignee', () => {
    const open = rowOf(
      item({ kind: ARTIFACT_KIND.task, key: 'ABC-7', fallback: 'ABC-7' }),
      {
        kind: 'issue',
        key: 'ABC-7',
        title: 'Fix it',
        state: 'In Progress',
        resolved: false,
        assignee: { name: 'Ann', avatar: null },
      },
      LOOK.got,
      MR,
    );
    expect(open).toMatchObject({
      title: 'Fix it',
      sub: 'ABC-7 · In Progress',
      tone: 'progress',
      rest: false,
    });
    expect(open.person?.name).toBe('Ann');
    const done = rowOf(
      item({ kind: ARTIFACT_KIND.task, key: 'ABC-7' }),
      { kind: 'issue', key: 'ABC-7', title: 'Fix it', state: 'Done', resolved: true, assignee: null },
      LOOK.got,
      MR,
    );
    expect(done).toMatchObject({ tone: 'done', rest: true });
  });

  it('a pending task shows its key while the title is read', () => {
    const row = rowOf(item({ kind: ARTIFACT_KIND.task, key: 'ABC-7' }), null, LOOK.wait, MR);
    expect(row).toMatchObject({ title: null, sub: 'ABC-7' });
  });

  it('a merge request says its state in words, a draft says draft', () => {
    const base = { kind: 'mr' as const, title: 'Cache', project: 'a/b', iid: 4 };
    const it = item({ kind: ARTIFACT_KIND.mr, key: 'b !4' });
    expect(rowOf(it, { ...base, state: 'merged', draft: false }, LOOK.got, MR)).toMatchObject({
      sub: 'b !4 · Merged',
      rest: true,
    });
    expect(rowOf(it, { ...base, state: 'opened', draft: true }, LOOK.got, MR).sub).toBe('b !4 · Draft');
  });

  it('a link with only an address has nothing to wait for', () => {
    const row = rowOf(
      item({ kind: ARTIFACT_KIND.link, readable: false, fallback: 'example.com' }),
      null,
      LOOK.wait,
      MR,
    );
    expect(row.pending).toBe(false);
    expect(row.title).toBe('example.com');
  });
});

describe('sections', () => {
  const items = [
    item({ url: 'u1', kind: ARTIFACT_KIND.task }),
    item({ url: 'u2', kind: ARTIFACT_KIND.doc }),
    item({ url: 'u3', kind: ARTIFACT_KIND.board }),
    item({ url: 'u4', kind: ARTIFACT_KIND.link, readable: false }),
    item({ url: 'u5', kind: ARTIFACT_KIND.mr }),
  ];

  it('go documents, tasks, merge requests, links; documents and boards together; empty ones are left out', () => {
    expect(sectionsOf(items).map((s) => [s.id, s.items.map((i) => i.url)])).toEqual([
      ['docs', ['u2', 'u3']],
      ['tasks', ['u1']],
      ['mrs', ['u5']],
      ['links', ['u4']],
    ]);
    expect(sectionsOf([])).toEqual([]);
  });

  it('are read in the order the rows stand, links without a title left out', () => {
    expect(readableInOrder(items).map((i) => i.url)).toEqual(['u2', 'u3', 'u1', 'u5']);
  });
});
