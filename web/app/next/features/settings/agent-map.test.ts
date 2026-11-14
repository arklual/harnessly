import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { answerParts, harnesslyMap } from '@/next/features/settings/agent-map';

beforeAll(() => pinLocale('ru'));

describe('the map a screen agent gets', () => {
  it('names every section of Settings with its address', () => {
    const map = harnesslyMap();
    expect(map).toContain('Настройки → Подключения: /settings/connections');
    expect(map).toContain('Sinopia Design: /designly');
    expect(map).not.toContain('next.');
  });
});

describe('the answer with links', () => {
  it('turns the links to the window’s places into jumps and leaves the rest as words', () => {
    expect(answerParts('Это в [Настройки → Подключения](/settings/connections).')).toEqual([
      { k: 'text', text: 'Это в ' },
      { k: 'link', text: 'Настройки → Подключения', path: '/settings/connections' },
      { k: 'text', text: '.' },
    ]);
  });

  it('keeps a link outside the window as words', () => {
    expect(answerParts('[сайт](https://example.com)')).toEqual([
      { k: 'text', text: '[сайт](https://example.com)' },
    ]);
  });
});
