import { describe, expect, it } from 'vitest';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { appButtonsOf, appStartersOf } from './app-parts';

const app = (id: string, extra: Partial<IInstalledApp>): IInstalledApp =>
  ({
    id,
    name: id,
    description: '',
    version: '1.0.0',
    trust: 'sandboxed',
    targets: ['chat'],
    permissions: [],
    views: [],
    mcpServers: [],
    ...extra,
  }) as IInstalledApp;

describe('what the apps on in a chat put at its field', () => {
  const installed = [
    app('acme.cashback', {
      buttons: [
        { id: 'run', title: 'Проверить', command: '/check' },
        { id: 'open', title: 'Кешбэки', icon: 'percent', view: 'main' },
      ],
      starters: [{ text: 'Проверь кешбэк' }],
    }),
    app('acme.other', { starters: [{ text: 'a' }, { text: 'b' }, { text: 'c' }, { text: 'd' }] }),
  ];

  it('turns a command into the field and a view into the pane', () => {
    const [run, open] = appButtonsOf(installed, ['acme.cashback']);
    expect(run.run).toEqual({ field: '/acme-cashback:check ' });
    expect(open.run).toEqual({ pane: 'acme.cashback/main' });
    expect(open.icon).toBe('percent');
  });

  it('gives only the apps on in the chat, in its order, at most four starters', () => {
    expect(appButtonsOf(installed, ['acme.other'])).toEqual([]);
    expect(appStartersOf(installed, ['acme.cashback', 'acme.other'])).toEqual([
      'Проверь кешбэк',
      'a',
      'b',
      'c',
    ]);
  });
});
