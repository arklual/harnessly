import { describe, expect, it } from 'vitest';
import type { ISearchGroup, ISearchRow } from '@/next/features/search/search-dialog';
import {
  SCOPE,
  connScope,
  connTabs,
  countIn,
  inScope,
  settingsGroup,
} from '@/next/features/search/smart-rows';
import type { ICatalogSetting } from '@/next/features/search/sources-api';
import { termsOf } from '@/next/features/search/terms';
import { pinLocale } from '@/next/i18n';
import { DEFAULT_SETTINGS } from '@/next/stores/me';

pinLocale('ru');

const setting: ICatalogSetting = {
  key: 'idle.stop',
  section: 'Основные',
  title: 'Останавливать простаивающие терминалы',
  description: 'Через сколько часов простоя терминал останавливается',
  kind: 'enum',
  options: [{ value: 'off', label: 'Выкл' }],
  value: 'off',
  valueLabel: 'Выкл',
};

const titles = (group: ISearchGroup | null) => group?.rows.map((row) => row.title) ?? [];

describe('settingsGroup', () => {
  it('finds a setting by the label its page shows, not only by the catalog', () => {
    expect(titles(settingsGroup([setting], termsOf('Enter отправляет')))).toContain('Enter отправляет');
    expect(titles(settingsGroup([setting], termsOf('вкладки сверху')))).toContain('Вкладки сверху');
    expect(titles(settingsGroup([setting], termsOf('язык')))).toContain('Язык');
  });

  it('finds settings and sections by other words for them', () => {
    expect(titles(settingsGroup([], termsOf('горячие клавиши')))).toContain('Клавиши');
    expect(titles(settingsGroup([], termsOf('шрифт')))).toContain('Размер текста');
    expect(titles(settingsGroup([], termsOf('тёмная')))).toContain('Тема');
  });

  it('does not say a setting twice when the catalog names it as the page does', () => {
    const found = titles(settingsGroup([setting], termsOf('простаивающие терминалы')));
    expect(found.filter((title) => title === setting.title)).toHaveLength(1);
  });

  it('lists nothing without words in «Всё» and everything in its own tab', () => {
    expect(settingsGroup([setting], termsOf(''))).toBeNull();
    const own = titles(settingsGroup([setting], termsOf(''), true));
    expect(own).toContain(setting.title);
    expect(own).toContain('Клавиши');
    expect(own).toContain('Enter отправляет');
  });
});

describe('connTabs', () => {
  const conn = (id: string, kind: string, capabilities: string[], name = `${kind}.example.com`) => ({
    id,
    kind,
    name,
    baseUrl: `https://${kind}.example.com/`,
    capabilities,
  });

  it('names a tab by what the system is, with its address in the hint', () => {
    const tabs = connTabs([conn('a', 'youtrack', ['issues']), conn('b', 'gitlab', ['code', 'issues'])]);
    expect(tabs.map((tab) => tab.label)).toEqual(['Трекер', 'GitLab']);
    expect(tabs[0]?.hint).toBe('youtrack.example.com');
  });

  it('tells two trackers apart and leaves out what cannot be searched', () => {
    const tabs = connTabs([
      conn('a', 'youtrack', ['issues']),
      conn('b', 'jira', ['issues']),
      conn('c', 'notion', []),
      conn('d', 'confluence', ['pages'], 'Вики команды'),
    ]);
    expect(tabs.map((tab) => tab.label)).toEqual(['Трекер · YouTrack', 'Трекер · Jira', 'Вики']);
    expect(tabs[2]?.hint).toBe('Вики команды · confluence.example.com');
  });
});

describe('inScope and countIn', () => {
  const rows = (n: number, prefix: string): ISearchRow[] =>
    Array.from({ length: n }, (_, i) => ({
      id: `${prefix}${i}`,
      kind: 'x',
      icon: 'x',
      title: prefix,
      meta: '',
    }));
  const groups: ISearchGroup[] = [
    { id: SCOPE.settings, label: 'Настройки', rows: rows(9, 's') },
    { id: SCOPE.chats, label: 'Чаты', rows: rows(2, 'c') },
    { id: 'messages:tab-1', label: 'Чат', rows: rows(3, 'm') },
    { id: connScope('a'), label: 'Трекер', rows: rows(4, 'f') },
  ];

  it('cuts the long groups in «Всё» and keeps a tab whole', () => {
    expect(inScope(groups, SCOPE.all)[0]?.rows).toHaveLength(5);
    expect(inScope(groups, SCOPE.settings)[0]?.rows).toHaveLength(9);
    expect(inScope(groups, SCOPE.chats).map((group) => group.id)).toEqual([SCOPE.chats, 'messages:tab-1']);
    expect(inScope(groups, connScope('a'))).toHaveLength(1);
  });

  it('counts what a tab would show', () => {
    expect(countIn(groups, SCOPE.all)).toBe(18);
    expect(countIn(groups, SCOPE.chats)).toBe(5);
    expect(countIn(groups, SCOPE.docs)).toBe(0);
  });
});

describe("the person's own settings in the row", () => {
  it('carries a switch for «Enter отправляет» and keeps the plain ones as links', () => {
    const rows = settingsGroup([], termsOf('Enter отправляет'), false, DEFAULT_SETTINGS)?.rows ?? [];
    const enter = rows.find((row) => row.title === 'Enter отправляет');
    expect(enter?.widget).toEqual({ kind: 'toggle', on: DEFAULT_SETTINGS.enterSends });
    expect(enter?.go).toMatchObject({ k: 'setting', key: 'person:enterSends' });
    const lang = settingsGroup([], termsOf('язык'), false, DEFAULT_SETTINGS)?.rows.find(
      (r) => r.title === 'Язык',
    );
    expect(lang?.widget).toBeUndefined();
  });

  it('has no control while the account is not read', () => {
    const rows = settingsGroup([], termsOf('Enter отправляет'))?.rows ?? [];
    expect(rows.find((row) => row.title === 'Enter отправляет')?.widget).toBeUndefined();
  });
});
