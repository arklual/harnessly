// @vitest-environment jsdom
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  aboutOf,
  cachedMade,
  dedupeMade,
  hostOf,
  iconOf,
  kindWord,
  mergeImages,
  rememberMade,
  resetMade,
  sameKey,
  taskEntries,
  whereOf,
} from '@/next/features/pane/made-model';
import type { MadeItem, MadeTaskRef, Ms, Picture, Shot } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));
afterEach(resetMade);

const shot = (id: string, name: string, at: number): Shot => ({ id, name, at: at as Ms });
const pic = (id: string, text: string): Picture => ({ id, text });
const item = (over: Partial<MadeItem>): MadeItem => ({
  kind: 'doc',
  ref: 'https://docs.example.com/a/spec',
  title: '',
  op: 'created',
  at: 1 as Ms,
  cover: null,
  designKind: null,
  ...over,
});

describe('mergeImages', () => {
  it('puts shots first, then the named pictures newest first', () => {
    const out = mergeImages(
      [shot('s1', 'a.png', 20), shot('s2', 'b.png', 10)],
      [pic('p1', '/tmp/old.png'), pic('p2', '/tmp/new.png')],
    );
    expect(out.map((x) => x.id)).toEqual(['s1', 's2', 'p2', 'p1']);
    expect(out[0].at).toBe(20);
    expect(out[2].at).toBeNull();
  });

  it('shows a picture whose file is already a shot once', () => {
    const out = mergeImages(
      [shot('s1', 'home.png', 5)],
      [pic('p1', '/work/out/home.png'), pic('p2', '/w/x.png')],
    );
    expect(out.map((x) => x.id)).toEqual(['s1', 'p2']);
  });

  it('keeps who looked at a shot', () => {
    const out = mergeImages([{ ...shot('s1', 'a.png', 1), agent: 'Phone' }], []);
    expect(out[0].agent).toBe('Phone');
  });
});

describe('a card', () => {
  it('knows its icon, host and kind word', () => {
    expect(iconOf(item({ kind: 'board' }))).toBe('layout-dashboard');
    expect(iconOf(item({ kind: 'design', ref: 'd1', designKind: 'slides' }))).toBe('presentation');
    expect(hostOf(item({}))).toBe('docs.example.com');
    expect(hostOf(item({ kind: 'design', ref: 'd1' }))).toBe('');
    expect(kindWord(item({ kind: 'board' }))).toBe('Board');
    expect(aboutOf(item({ kind: 'design', ref: 'd1', designKind: 'site' }))).toBe('Site');
  });

  it('does not show a board key as text', () => {
    expect(hostOf(item({ kind: 'board', ref: 'https://docs.example.com/b/flow?k=SECRET' }))).toBe(
      'docs.example.com',
    );
  });
});

describe('the kept list', () => {
  it('comes back after the memory is gone, and a broken one does not', () => {
    const made = { items: [item({ title: 'Spec' })], complete: true };
    rememberMade('c1', made);
    expect(cachedMade('c1')).toBe(made);
    localStorage.setItem('hx-next:made:v1', JSON.stringify({ c2: { items: 'x' } }));
    expect(cachedMade('c2')).toBeNull();
    expect(cachedMade('nobody')).toBeNull();
  });

  it('keeps the latest dozen chats', () => {
    for (let i = 0; i < 14; i += 1) rememberMade(`c${i}`, { items: [], complete: true });
    const kept = Object.keys(JSON.parse(localStorage.getItem('hx-next:made:v1') ?? '{}'));
    expect(kept).toHaveLength(12);
    expect(kept).not.toContain('c0');
  });
});

describe('the same thing met twice', () => {
  it('is one by its slug whatever address named it', () => {
    const a = item({ ref: 'https://a.example.com/a/spec' });
    const b = item({ ref: 'https://ai.example.com/agents/docs/spec' });
    expect(sameKey(a)).toBe(sameKey(b));
    expect(sameKey(item({ ref: 'https://a.example.com/a/other' }))).not.toBe(sameKey(a));
    expect(sameKey(item({ kind: 'board', ref: 'https://b.example.com/b/spec?k=x' }))).not.toBe(sameKey(a));
  });

  it('shows each once, keeping the newest time, a name once known and a board key once seen', () => {
    const out = dedupeMade([
      item({ ref: 'https://ai.example.com/agents/docs/spec', at: 30 as Ms, op: 'changed' }),
      item({ ref: 'https://a.example.com/a/spec', at: 10 as Ms, title: 'The spec', op: 'created' }),
      item({ kind: 'board', ref: 'https://b.example.com/b/flow', at: 20 as Ms }),
      item({ kind: 'board', ref: 'https://b.example.com/b/flow?k=abc', at: 5 as Ms }),
      item({ kind: 'design', ref: 'landing', designKind: 'site' }),
    ]);
    expect(out).toHaveLength(3);
    expect(out[0]).toMatchObject({ title: 'The spec', at: 30, op: 'created' });
    expect(out[1].ref).toBe('https://b.example.com/b/flow?k=abc');
  });

  it("leaves out a bare address: the portal's front door is not something made", () => {
    const out = dedupeMade([
      item({ ref: 'https://a.example.com/' }),
      item({ ref: 'https://a.example.com' }),
      item({ ref: 'https://a.example.com/a/spec' }),
    ]);
    expect(out.map((x) => x.ref)).toEqual(['https://a.example.com/a/spec']);
  });

  it('tells two unnamed pages apart by where they live, never by a key', () => {
    expect(whereOf(item({ ref: 'https://ai.example.com/agents/docs/one' }))).toBe('ai.example.com/one');
    expect(whereOf(item({ kind: 'board', ref: 'https://b.example.com/b/two?k=secret' }))).toBe(
      'b.example.com/two',
    );
    expect(whereOf(item({ kind: 'design', ref: 'landing', designKind: 'site' }))).toBe('');
  });
});

const TRACKER = 'https://tracker.example.com/issue';
const taskRef = (key: string, over: Partial<MadeTaskRef> = {}): MadeTaskRef => ({
  key,
  title: `Title ${key}`,
  url: `${TRACKER}/${key}`,
  state: '',
  resolved: false,
  ...over,
});
const task = (key: string, at: number, over: Partial<MadeItem['task']> = {}): MadeItem =>
  item({
    kind: 'task',
    ref: `${TRACKER}/${key}`,
    title: `Title ${key}`,
    at: at as Ms,
    task: {
      key,
      type: 'Feature',
      state: 'In Progress',
      resolved: false,
      parent: null,
      children: [],
      ...over,
    },
  });

describe('taskEntries', () => {
  it('is a row for each task while there are few, an epic opening into its tasks', () => {
    const epic = task('HRN-1', 30, {
      type: 'Epic',
      children: [taskRef('HRN-2'), taskRef('HRN-3', { resolved: true })],
    });
    const out = taskEntries([epic, task('HRN-9', 10)]);
    expect(out.map((x) => x.card.title)).toEqual(['Title HRN-1', 'Title HRN-9']);
    expect(out[0].card).toMatchObject({ icon: 'layers', about: '2 tasks' });
    expect(out[0].card.kids?.map((x) => [x.key, x.done])).toEqual([
      ['HRN-2', false],
      ['HRN-3', true],
    ]);
    expect(out[0].targets.map(([id]) => id)).toContain(`task:${TRACKER}/HRN-3`);
    expect(out[1].card.kids).toBeUndefined();
  });

  it('tells many tasks by their epic: one row for the epic, the tasks of it inside, the rest alone', () => {
    const parent = taskRef('HRN-1', { title: 'The epic' });
    const out = taskEntries([
      task('HRN-2', 40, { parent }),
      task('HRN-3', 30, { parent }),
      task('HRN-4', 20, { parent }),
      task('HRN-9', 10),
    ]);
    expect(out).toHaveLength(2);
    expect(out[0].card).toMatchObject({ title: 'The epic', icon: 'layers', about: '3 tasks' });
    expect(out[0].card.kids?.map((x) => x.key)).toEqual(['HRN-2', 'HRN-3', 'HRN-4']);
    // The epic opens by its own address, though the chat never touched it.
    expect(out[0].targets[0]).toEqual([`task:${TRACKER}/HRN-1`, { kind: 'task', ref: `${TRACKER}/HRN-1` }]);
    expect(out[1].card.title).toBe('Title HRN-9');
  });

  it("puts all of an epic's tasks in its row when the chat touched the epic itself", () => {
    const epic = task('HRN-1', 50, { type: 'Epic', children: [taskRef('HRN-2'), taskRef('HRN-5')] });
    const out = taskEntries([
      epic,
      task('HRN-2', 40, { parent: taskRef('HRN-1') }),
      task('HRN-3', 30),
      task('HRN-4', 20),
    ]);
    const row = out.find((x) => x.card.title === 'Title HRN-1');
    expect(row?.card.kids?.map((x) => x.key)).toEqual(['HRN-2', 'HRN-5']);
  });

  it('names a task the tracker did not answer for by its key', () => {
    const lone = item({ kind: 'task', ref: `${TRACKER}/HRN-7`, title: '' });
    expect(taskEntries([lone])[0].card).toMatchObject({ title: 'HRN-7', about: '' });
    expect(whereOf(lone)).toBe('HRN-7');
  });
});
