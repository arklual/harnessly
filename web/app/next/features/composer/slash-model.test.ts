import { describe, expect, it } from 'vitest';
import type { IPickerAgent } from '@/next/features/composer/agents';
import {
  buildRows,
  detectSub,
  detectTrigger,
  effortRows,
  fileRows,
  GROUP,
  isTyped,
  modelRows,
  removeWord,
  replaceWord,
  splitMention,
  SUB,
  TRIGGER_CHAR,
  type ISlashAvailable,
  type ITrigger,
} from '@/next/features/composer/slash-model';
import type { ICatalogItem } from '@/next/features/settings/api';
import type { FileEntry } from '@/next/protocol/v2';

const item = (over: Partial<ICatalogItem> & { name: string }): ICatalogItem => ({
  trigger: '/',
  aliases: [],
  description: '',
  argumentHint: null,
  source: 'builtin',
  sourceLabel: 'built-in',
  execution: 'send',
  welcome: true,
  hidden: false,
  ...over,
});

const CATALOG: ICatalogItem[] = [
  item({ name: 'compact', description: 'Compact the history', argumentHint: '[instructions]' }),
  item({ name: 'model', execution: 'model', description: 'Change the model' }),
  item({ name: 'usage', execution: 'menu', description: 'Limits and usage' }),
  item({ name: 'doctor', execution: 'terminal', hidden: true }),
  item({ name: 'review', source: 'user', description: 'Review my changes', welcome: false }),
  item({ name: 'commit-commands:commit', source: 'plugin', sourceLabel: 'plugin · commit-commands' }),
  item({ name: 'brainstorming', source: 'skill', description: 'Think before building' }),
  item({ name: 'tdd', trigger: '$', source: 'skill', description: 'Test first' }),
];

const trigger = (query: string, start = 0, char: ITrigger['char'] = TRIGGER_CHAR.command): ITrigger => ({
  char,
  start,
  end: start + 1 + query.length,
  query,
});

const NONE: ISlashAvailable = { model: false, effort: false, actions: {} };

const names = (rows: { name: string }[]) => rows.map((row) => row.name);

describe('detectTrigger', () => {
  const both = { skills: true, files: true };
  it('finds «/» at the start of the field and after a space', () => {
    expect(detectTrigger('/co', 3, both)).toEqual({ char: '/', start: 0, end: 3, query: 'co' });
    expect(detectTrigger('see /re', 7, both)).toEqual({ char: '/', start: 4, end: 7, query: 're' });
  });

  it('is not a word that merely contains «/»', () => {
    expect(detectTrigger('a/b', 3, both)).toBeNull();
    expect(detectTrigger('/usr/bin', 8, both)).toBeNull();
  });

  it('takes the whole word when the caret is inside it', () => {
    expect(detectTrigger('/compact now', 3, both)).toEqual({ char: '/', start: 0, end: 8, query: 'co' });
  });

  it('knows «$» and «@» only where they are used', () => {
    expect(detectTrigger('run $td', 7, both)?.char).toBe('$');
    expect(detectTrigger('run $td', 7, { skills: false, files: true })).toBeNull();
    expect(detectTrigger('open @src/a', 11, both)?.query).toBe('src/a');
    expect(detectTrigger('open @src/a', 11, { skills: true, files: false })).toBeNull();
  });

  it('is nothing for a word that was left behind', () => {
    expect(detectTrigger('/compact ', 9, both)).toBeNull();
  });
});

describe('detectSub', () => {
  it('turns «/model so» into a choice among the models', () => {
    expect(detectSub('/model so', [SUB.model, SUB.effort])).toEqual({ sub: 'model', query: 'so' });
    expect(detectSub('/effort ', [SUB.effort])).toEqual({ sub: 'effort', query: '' });
  });

  it('is nothing when the agent has no such choice or the text is something else', () => {
    expect(detectSub('/effort hi', [SUB.model])).toBeNull();
    expect(detectSub('/model', [SUB.model])).toBeNull();
    expect(detectSub('say /model x', [SUB.model])).toBeNull();
  });
});

describe('buildRows', () => {
  const input = (query: string, over: Partial<Parameters<typeof buildRows>[0]> = {}) => ({
    trigger: trigger(query),
    provider: 'claude' as const,
    catalog: CATALOG,
    mcp: [{ name: 'gitlab', target: 'gitlab.example.com' }],
    quick: [{ name: 'explain', text: 'Explain this in plain words.' }],
    available: NONE,
    starting: false,
    ...over,
  });

  it('lists what goes to the agent as a message and leaves its terminal screens out', () => {
    const { rows } = buildRows(input(''));
    expect(names(rows)).toContain('compact');
    expect(names(rows)).not.toContain('usage');
    expect(names(rows)).not.toContain('doctor');
    expect(names(rows)).not.toContain('model');
  });

  it("puts the chat settings first and the agent's own commands of the same name away", () => {
    const { rows } = buildRows(
      input('', {
        available: {
          model: true,
          effort: false,
          actions: { compact: () => undefined, goal: () => undefined },
        },
      }),
    );
    expect(rows.slice(0, 3).map((row) => [row.group, row.name])).toEqual([
      [GROUP.setting, 'model'],
      [GROUP.setting, 'goal'],
      [GROUP.setting, 'compact'],
    ]);
    expect(rows.filter((row) => row.name === 'compact')).toHaveLength(1);
  });

  it('sorts the groups: commands, skills, MCP servers, quick prompts', () => {
    const { rows } = buildRows(input(''));
    const groups = rows.map((row) => row.group);
    expect(groups).toEqual([...groups].sort((a, b) => groups.indexOf(a) - groups.indexOf(b)));
    expect(new Set(groups)).toEqual(new Set([GROUP.command, GROUP.skill, GROUP.mcp, GROUP.quick]));
  });

  it('matches by fuzzy name and marks the letters', () => {
    const { rows } = buildRows(input('cmp'));
    expect(rows[0].name).toBe('compact');
    expect(rows[0].ranges).toEqual([
      [0, 1],
      [2, 4],
    ]);
  });

  it('finds a row by its description too', () => {
    const { rows } = buildRows(input('history'));
    expect(names(rows)).toEqual(['compact']);
  });

  it('inserts the word with its trigger, and names an MCP server as its agent does', () => {
    const claude = buildRows(input('gitl')).rows[0];
    expect(claude.action).toEqual({ k: 'insert', text: 'mcp__gitlab ' });
    const codex = buildRows(input('gitl', { provider: 'codex' })).rows[0];
    expect(codex.action).toEqual({ k: 'insert', text: 'gitlab ' });
    expect(buildRows(input('compa')).rows[0].action).toEqual({ k: 'insert', text: '/compact ' });
  });

  it('after a space offers skills, servers and quick prompts but not commands', () => {
    const { rows } = buildRows(input('', { trigger: trigger('', 5) }));
    expect(names(rows)).toContain('brainstorming');
    expect(names(rows)).toContain('explain');
    expect(names(rows)).not.toContain('compact');
    expect(names(rows)).not.toContain('review');
  });

  it('«$» lists only the skills of Codex', () => {
    const { rows } = buildRows(input('', { trigger: trigger('', 3, TRIGGER_CHAR.skill), provider: 'codex' }));
    expect(rows.map((row) => [row.prefix, row.name])).toEqual([['$', 'tdd']]);
  });

  it('on the start page offers only what may be the first message', () => {
    const { rows } = buildRows(input('', { starting: true }));
    expect(names(rows)).not.toContain('review');
    expect(names(rows)).toContain('compact');
  });

  it('shows a few rows of a group for an empty query and says how many more there are', () => {
    const many = Array.from({ length: 12 }, (_, i) => item({ name: `cmd-${String(i).padStart(2, '0')}` }));
    const list = buildRows(input('', { catalog: many, mcp: [], quick: [] }));
    expect(list.rows).toHaveLength(8);
    expect(list.more).toBe(4);
    expect(buildRows(input('cmd', { catalog: many, mcp: [], quick: [] })).rows).toHaveLength(12);
  });

  it('waits for a catalog that is not here yet with the rest', () => {
    const { rows } = buildRows(input('', { catalog: null }));
    expect(names(rows)).toEqual(['gitlab', 'explain']);
  });
});

describe('isTyped', () => {
  it('is a name typed in full, which Enter sends as it is', () => {
    const [row] = buildRows({
      trigger: trigger('compact'),
      provider: 'claude',
      catalog: CATALOG,
      mcp: [],
      quick: [],
      available: NONE,
      starting: false,
    }).rows;
    expect(isTyped(row, 'compact')).toBe(true);
    expect(isTyped(row, 'comp')).toBe(false);
  });
});

const AGENT: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  canSwitch: true,
  available: true,
  status: 'ready',
  models: [
    {
      id: 'opus',
      label: 'Opus 5.5',
      hint: 'The deepest',
      efforts: ['low', 'high', 'max'],
      defaultEffort: 'high',
    },
    { id: 'sonnet', label: 'Sonnet 5.5', hint: 'Fast', efforts: ['low', 'high'], defaultEffort: 'low' },
  ],
};

describe('modelRows and effortRows', () => {
  const value = { provider: 'claude' as const, model: 'opus', effort: 'high' as const, ultracode: false };

  it("lists the models of the agent, the chat's own marked", () => {
    const rows = modelRows(AGENT, value, '');
    expect(rows.map((row) => [row.name, row.current])).toEqual([
      ['Opus 5.5', true],
      ['Sonnet 5.5', false],
    ]);
  });

  it('a model chosen keeps the depth it takes, else takes its own', () => {
    const [, sonnet] = modelRows(AGENT, value, '');
    expect(sonnet.action).toEqual({
      k: 'choose',
      choice: { provider: 'claude', model: 'sonnet', effort: 'high', ultracode: false },
    });
    const max = { ...value, effort: 'max' as const };
    expect(modelRows(AGENT, max, 'son')[0].action).toEqual({
      k: 'choose',
      choice: { provider: 'claude', model: 'sonnet', effort: 'low', ultracode: false },
    });
  });

  it('narrows the models by what is typed', () => {
    expect(names(modelRows(AGENT, value, 'sonn'))).toEqual(['Sonnet 5.5']);
    expect(modelRows(AGENT, value, 'zzz')).toEqual([]);
  });

  it("lists the depths of the chat's model in the order of the slider", () => {
    const rows = effortRows(AGENT, value, '');
    expect(rows.map((row) => [row.source, row.current])).toEqual([
      ['low', false],
      ['high', true],
      ['max', false],
    ]);
    expect(rows[2].action).toEqual({
      k: 'choose',
      choice: { provider: 'claude', model: 'opus', effort: 'max', ultracode: false },
    });
  });

  it('has no depths for a model the agent does not list', () => {
    expect(effortRows(AGENT, { ...value, model: 'mystery' }, '')).toEqual([]);
  });
});

describe('files', () => {
  const entries: FileEntry[] = [
    { name: 'src', path: 'src', kind: 'directory' },
    { name: 'README.md', path: 'README.md', kind: 'file' },
    { name: 'app.ts', path: 'src/app.ts', kind: 'file' },
  ];

  it('splits «@src/ap» into a folder and a name', () => {
    expect(splitMention('src/ap')).toEqual({ dir: 'src', name: 'ap' });
    expect(splitMention('ap')).toEqual({ dir: '', name: 'ap' });
    expect(splitMention('a/b/')).toEqual({ dir: 'a/b', name: '' });
  });

  it('lists the folder as it is, folders going on and files completing', () => {
    const { rows } = fileRows(entries, '');
    expect(rows.map((row) => [row.name, row.action])).toEqual([
      ['src/', { k: 'dir', text: '@src/' }],
      ['README.md', { k: 'insert', text: '@README.md ' }],
      ['app.ts', { k: 'insert', text: '@src/app.ts ' }],
    ]);
  });

  it('narrows by the name', () => {
    expect(names(fileRows(entries, 'read').rows)).toEqual(['README.md']);
  });
});

describe('editing the word', () => {
  it('replaces the word and puts the caret after it', () => {
    expect(replaceWord('see /co now', 4, 7, '/compact ')).toEqual({ text: 'see /compact now', caret: 13 });
    expect(replaceWord('/s', 0, 2, '@src/')).toEqual({ text: '@src/', caret: 5 });
  });

  it('takes the word out with the space after it', () => {
    expect(removeWord('/goal', 0, 5)).toEqual({ text: '', caret: 0 });
    expect(removeWord('a /goal b', 2, 7)).toEqual({ text: 'a b', caret: 2 });
  });
});
