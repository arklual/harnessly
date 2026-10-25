// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IPickerAgent } from '@/next/features/composer/agents';
import Composer, { type IComposerProps } from '@/next/features/composer/composer';
import type { ISlashSource } from '@/next/features/composer/slash-palette';
import type { ICatalogItem } from '@/next/features/settings/api';

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  window.localStorage.clear();
});
afterEach(cleanup);

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

const CLAUDE: ICatalogItem[] = [
  item({ name: 'compact', description: 'Compact the history' }),
  item({ name: 'init', description: 'Create a CLAUDE.md' }),
  item({ name: 'brainstorming', source: 'skill', description: 'Think before building' }),
];
const CODEX: ICatalogItem[] = [
  item({ name: 'review', description: 'Review the changes' }),
  item({ name: 'tdd', trigger: '$', source: 'skill', description: 'Test first' }),
];

const source = (over: Partial<ISlashSource> = {}): ISlashSource => ({
  provider: 'claude',
  wake: vi.fn(),
  catalog: CLAUDE,
  mcp: [{ name: 'gitlab', target: 'gitlab.example.com' }],
  actions: {},
  starting: false,
  filesScope: 'c1',
  listFolder: null,
  ...over,
});

const AGENT: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  canSwitch: true,
  available: true,
  status: 'ready',
  models: [
    { id: 'opus', label: 'Opus 5.5', hint: 'The deepest', efforts: ['low', 'high'], defaultEffort: 'high' },
    { id: 'sonnet', label: 'Sonnet 5.5', hint: 'Fast', efforts: ['low', 'high'], defaultEffort: 'low' },
  ],
};

const picker = (
  over: Partial<NonNullable<IComposerProps['picker']>> = {},
): NonNullable<IComposerProps['picker']> => ({
  value: { provider: 'claude', model: 'opus', effort: 'high', ultracode: false },
  agents: [AGENT],
  locked: true,
  onChange: vi.fn(),
  ...over,
});

interface IHarness {
  slash: ISlashSource | null;
  picker?: IComposerProps['picker'];
  onSubmit?: IComposerProps['onSubmit'];
  initial?: string;
}

const Harness = ({ slash, picker: pill = null, onSubmit = vi.fn(), initial = '' }: IHarness) => {
  const [value, setValue] = useState(initial);
  return (
    <Composer
      value={value}
      onChange={setValue}
      onSubmit={onSubmit}
      mode="idle"
      onStop={() => Promise.resolve()}
      picker={pill}
      slash={slash}
      attachments={[]}
      onFiles={vi.fn()}
      onRemoveAttachment={vi.fn()}
      onRetryAttachment={vi.fn()}
      recording={null}
      onCancelRecording={vi.fn()}
      onFinishRecording={vi.fn()}
      maxText={1000}
    />
  );
};

const field = () => screen.getByRole('textbox') as HTMLTextAreaElement;
const type = (text: string) => fireEvent.change(field(), { target: { value: text } });
const key = (name: string, init: KeyboardEventInit = {}) =>
  fireEvent.keyDown(field(), { key: name, ...init });
const rows = () => screen.queryAllByRole('option').map((row) => row.textContent ?? '');

describe('the slash palette', () => {
  it('opens on «/», asks for the catalog, and lists commands, skills and servers with their sources', () => {
    const slash = source();
    render(<Harness slash={slash} />);
    expect(screen.queryByRole('listbox')).toBeNull();
    type('/');
    expect(screen.getByRole('listbox')).toBeTruthy();
    expect(slash.wake).toHaveBeenCalled();
    const text = rows().join('|');
    expect(text).toContain('/compact');
    expect(text).toContain('Compact the history');
    expect(text).toContain('command');
    expect(text).toContain('/brainstorming');
    expect(text).toContain('skill');
    expect(text).toContain('gitlab');
    expect(text).toContain('MCP');
  });

  it('narrows by fuzzy match and marks the letters', () => {
    render(<Harness slash={source()} />);
    type('/cmp');
    expect(rows()).toHaveLength(1);
    const marks = [...document.querySelectorAll('mark.hx-slash__hit')].map((m) => m.textContent);
    expect(marks).toEqual(['c', 'mp']);
  });

  it('↓ moves, Tab completes the chosen row and puts the caret after it', () => {
    render(<Harness slash={source()} />);
    type('/');
    expect(screen.getAllByRole('option')[0].getAttribute('aria-selected')).toBe('true');
    key('ArrowDown');
    expect(screen.getAllByRole('option')[1].getAttribute('aria-selected')).toBe('true');
    key('Tab');
    expect(field().value).toBe('/init ');
    expect(field().selectionStart).toBe(6);
  });

  it('Enter completes a name that is not typed in full', () => {
    const onSubmit = vi.fn();
    render(<Harness slash={source()} onSubmit={onSubmit} />);
    type('/comp');
    key('Enter');
    expect(field().value).toBe('/compact ');
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Enter on a name typed in full sends it to the agent as it is', () => {
    const onSubmit = vi.fn();
    render(<Harness slash={source()} onSubmit={onSubmit} />);
    type('/compact');
    key('Enter');
    expect(onSubmit).toHaveBeenCalledWith('queue');
  });

  it('Esc closes it, and typing opens it again', async () => {
    render(<Harness slash={source()} />);
    type('/co');
    key('Escape');
    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
    expect(field().value).toBe('/co');
    type('/com');
    expect(screen.getByRole('listbox')).toBeTruthy();
  });

  it('the palette leaves with a closing phase before it goes', async () => {
    render(<Harness slash={source()} />);
    type('/co');
    key('Escape');
    expect(screen.getByRole('listbox').getAttribute('data-state')).toBe('closing');
    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
  });

  it('a word it has no row for goes to the agent as text', () => {
    const onSubmit = vi.fn();
    render(<Harness slash={source()} onSubmit={onSubmit} />);
    type('/zzzz');
    expect(screen.queryByRole('listbox')).toBeNull();
    key('Enter');
    expect(onSubmit).toHaveBeenCalledWith('queue');
  });

  it('a path is not a command', () => {
    render(<Harness slash={source()} />);
    type('/usr/bin');
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('after a space it lists skills but not commands', () => {
    render(<Harness slash={source()} />);
    type('use /');
    const text = rows().join('|');
    expect(text).toContain('brainstorming');
    expect(text).not.toContain('compact');
  });

  it('shows what the field is waiting for while the catalog is read', () => {
    render(<Harness slash={source({ catalog: null })} />);
    type('/');
    expect(screen.getByRole('listbox').textContent).toContain('Loading');
  });

  it('keeps the quick prompts among the rows', () => {
    window.localStorage.setItem(
      'hx.next.quickPrompts',
      JSON.stringify([{ name: 'explain', text: 'Explain it.' }]),
    );
    render(<Harness slash={source()} />);
    type('/expl');
    key('Tab');
    expect(field().value).toBe('Explain it.');
  });

  it('Codex lists its own commands, and «$» its skills', () => {
    render(<Harness slash={source({ provider: 'codex', catalog: CODEX, mcp: [] })} />);
    type('/');
    expect(rows().join('|')).toContain('/review');
    expect(rows().join('|')).not.toContain('compact');
    type('use $t');
    expect(rows()).toHaveLength(1);
    key('Tab');
    expect(field().value).toBe('use $tdd ');
  });

  it('«$» means nothing in a Claude chat', () => {
    render(<Harness slash={source()} />);
    type('use $');
    expect(screen.queryByRole('listbox')).toBeNull();
  });
});

describe('settings in the palette', () => {
  it('/model turns the field into the list of models; arrows choose and Enter switches through the pill', () => {
    const pill = picker();
    render(<Harness slash={source()} picker={pill} />);
    type('/mod');
    expect(rows()[0]).toContain('/model');
    key('Tab');
    expect(field().value).toBe('/model ');
    expect(rows().map((r) => r.replace(/\s+/g, ' '))[0]).toContain('Opus 5.5');
    expect(screen.getAllByRole('option')[0].getAttribute('data-current')).toBe('true');
    key('ArrowDown');
    key('Enter');
    expect(pill.onChange).toHaveBeenCalledWith({
      provider: 'claude',
      model: 'sonnet',
      effort: 'high',
      ultracode: false,
    });
    expect(field().value).toBe('');
  });

  it('narrows the models by what follows /model', () => {
    const pill = picker();
    render(<Harness slash={source()} picker={pill} initial="" />);
    type('/model son');
    expect(rows()).toHaveLength(1);
    key('Enter');
    expect(pill.onChange).toHaveBeenCalledWith({
      provider: 'claude',
      model: 'sonnet',
      effort: 'high',
      ultracode: false,
    });
  });

  it('/effort lists the depths of the model', () => {
    const pill = picker();
    render(<Harness slash={source()} picker={pill} />);
    type('/effort ');
    expect(rows()).toHaveLength(2);
    key('ArrowUp');
    key('Enter');
    expect(pill.onChange).toHaveBeenCalledWith({
      provider: 'claude',
      model: 'opus',
      effort: 'high',
      ultracode: false,
    });
  });

  it('does not change the model where the pill cannot, and says why', () => {
    const pill = picker({ blocked: 'This agent cannot switch' });
    render(<Harness slash={source()} picker={pill} />);
    type('/model ');
    expect(screen.getByRole('listbox').textContent).toContain('This agent cannot switch');
    key('ArrowDown');
    key('Enter');
    expect(pill.onChange).not.toHaveBeenCalled();
  });

  it('there is no /model without a pill', () => {
    render(<Harness slash={source()} picker={null} />);
    type('/mod');
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('/goal, /clear and /resume do what the screen does and leave nothing in the field', () => {
    const actions = { goal: vi.fn(), newChat: vi.fn(), resume: vi.fn(), compact: vi.fn() };
    render(<Harness slash={source({ actions })} />);
    type('/goal');
    key('Enter');
    expect(actions.goal).toHaveBeenCalledTimes(1);
    expect(field().value).toBe('');
    type('/clear');
    key('Tab');
    expect(actions.newChat).toHaveBeenCalledTimes(1);
    type('/resu');
    key('Tab');
    expect(actions.resume).toHaveBeenCalledTimes(1);
    type('/compact');
    key('Enter');
    expect(actions.compact).toHaveBeenCalledTimes(1);
  });
});

describe('files in the palette', () => {
  it("«@» lists the chat's folder, a folder goes on, a file completes", async () => {
    const listFolder = vi.fn((dir: string) =>
      Promise.resolve(
        dir === ''
          ? [
              { name: 'src', path: 'src', kind: 'directory' as const },
              { name: 'README.md', path: 'README.md', kind: 'file' as const },
            ]
          : [{ name: 'app.ts', path: 'src/app.ts', kind: 'file' as const }],
      ),
    );
    render(<Harness slash={source({ listFolder })} />);
    await act(async () => type('look at @'));
    await waitFor(() => expect(rows()).toHaveLength(2));
    key('Tab');
    expect(field().value).toBe('look at @src/');
    await waitFor(() => expect(rows().join('|')).toContain('app.ts'));
    expect(listFolder).toHaveBeenCalledWith('src', expect.anything());
    key('Tab');
    expect(field().value).toBe('look at @src/app.ts ');
  });

  it('is nothing where the chat has no folder to list', () => {
    render(<Harness slash={source({ listFolder: null })} />);
    type('look at @');
    expect(screen.queryByRole('listbox')).toBeNull();
  });
});
