import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  CHOICES,
  choicesOf,
  groupsOf,
  optionValue,
  shownValue,
  type IOptionRow,
} from '@/next/features/settings/options';
import type { AgentOptionKey, AgentOptionValue, AgentSettings } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));

const settings = (options: [AgentOptionKey, AgentOptionValue][]): AgentSettings => ({
  provider: 'claude',
  path: '/home/x/.claude/settings.json',
  rev: 'r1',
  account: { signedIn: false, method: null, plan: null, who: null },
  options: options.map(([key, value]) => ({ key, value })),
  rules: { allow: [], ask: [], deny: [] },
  env: [],
  mcp: [],
});

const rowOf = (provider: 'claude' | 'codex', key: AgentOptionKey): IOptionRow => {
  const row = groupsOf(provider)
    .flatMap((group) => group.rows)
    .find((item) => item.key === key);
  if (!row) throw new Error(`no row ${key}`);
  return row;
};

describe('groupsOf', () => {
  it('lists the groups of each CLI, and each option once', () => {
    expect(groupsOf('claude').map((group) => group.id)).toEqual(['model', 'permissions', 'context', 'git']);
    expect(groupsOf('codex').map((group) => group.id)).toEqual(['model', 'sandbox', 'tools']);
    for (const provider of ['claude', 'codex'] as const) {
      const keys = groupsOf(provider).flatMap((group) => group.rows.map((row) => row.key));
      expect(new Set(keys).size).toBe(keys.length);
    }
  });

  it('keeps the base URL off the agents pages: it is on «Models and keys»', () => {
    for (const provider of ['claude', 'codex'] as const)
      expect(groupsOf(provider).flatMap((group) => group.rows.map((row) => row.key))).not.toContain(
        'baseUrl',
      );
  });
});

describe('shownValue', () => {
  it('shows the file value, else what the CLI does without one, else nothing', () => {
    expect(shownValue(rowOf('claude', 'effort'), settings([['effort', 'max']]))).toBe('max');
    expect(shownValue(rowOf('claude', 'memory'), settings([['memory', null]]))).toBe(true);
    expect(shownValue(rowOf('claude', 'memory'), settings([['memory', false]]))).toBe(false);
    expect(shownValue(rowOf('claude', 'outputStyle'), settings([]))).toBe('default');
    expect(shownValue(rowOf('claude', 'effort'), settings([]))).toBeNull();
  });

  it('reads an option the file does not list as undefined', () => {
    expect(optionValue(settings([]), 'effort')).toBeUndefined();
    expect(optionValue(settings([['effort', 'low']]), 'effort')).toBe('low');
  });
});

describe('choicesOf', () => {
  const models = [
    { id: 'opus', label: 'Opus', hint: 'The most capable', efforts: [], defaultEffort: null },
    { id: 'haiku', label: 'Haiku', hint: '', efforts: [], defaultEffort: null },
  ];

  it('takes the models of the machine and offers «not set» first', () => {
    const list = choicesOf('claude', rowOf('claude', 'model'), models, null);
    expect(list.map((choice) => choice.value)).toEqual([null, 'opus', 'haiku']);
    expect(list[0]?.label).toBe('Default');
    expect(list[1]?.sub).toBe('The most capable');
    expect(list[2]?.sub).toBeNull();
    expect(rowOf('claude', 'model').choices).toBe(CHOICES.models);
  });

  it('never hides what the file holds: a model the machine does not list is added', () => {
    const list = choicesOf('claude', rowOf('claude', 'model'), models, 'sonnet-old');
    expect(list.at(-1)).toMatchObject({ value: 'sonnet-old', label: 'sonnet-old' });
  });

  it('words a CLI own value its own way: Codex top effort is «Max», Claude one is below Max', () => {
    const claude = choicesOf('claude', rowOf('claude', 'effort'), [], 'high');
    const codex = choicesOf('codex', rowOf('codex', 'effort'), [], 'high');
    expect(claude.map((choice) => choice.label)).toEqual(['Low', 'Medium', 'High', 'Extra high', 'Max']);
    expect(codex.map((choice) => choice.label)).toEqual(['Low', 'Medium', 'High', 'Max']);
  });

  it('words a context window in tokens and the days of keeping chats', () => {
    expect(
      choicesOf('claude', rowOf('claude', 'compactWindow'), [], null).map((choice) => choice.label),
    ).toEqual(['200K', '400K', '1M']);
    expect(choicesOf('claude', rowOf('claude', 'keepDays'), [], 90).map((choice) => choice.label)).toEqual([
      '30 days',
      '90 days',
      'A year',
    ]);
  });

  it('adds a number the file holds that the list does not have', () => {
    const list = choicesOf('claude', rowOf('claude', 'keepDays'), [], 7);
    expect(list.at(-1)).toMatchObject({ value: 7, label: '7 days' });
  });
});
