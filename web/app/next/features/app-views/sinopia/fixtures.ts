// Apps and answers for the tests of the Sinopia side of the page.
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { ISinopiaBoard, ISinopiaState } from '@/next/features/app-views/sinopia/types';

export const SLOT = 'https://slot7.example.org';

const base = {
  version: '1.0.0',
  trust: 'sandboxed',
  description: '',
  permissions: ['sinopia.read', 'sinopia.write'],
  mcpServers: [],
};

/** «Валидатор презентаций»: slides only, a panel and one check. */
export const lint: IInstalledApp = {
  ...base,
  id: 'harnessly.deck-lint',
  name: 'Валидатор презентаций',
  targets: ['sinopia.slides'],
  views: [{ id: 'panel', targets: ['sinopia.slides'], path: '/harnessly.deck-lint/panel.html' }],
  sinopia: {
    panels: [{ id: 'issues', view: 'panel', title: 'Проверка' }],
    checks: [{ id: 'rules', on: ['save', 'share'], blockShare: 'error' }],
  },
};

/** Cashback: its inspector opens on a click on a bound element of a site. */
export const cashback: IInstalledApp = {
  ...base,
  id: 'harnessly.cashback',
  name: 'Кэшбэк',
  targets: ['chat', 'sinopia.site'],
  views: [
    { id: 'inspector', targets: ['sinopia.site'], path: '/harnessly.cashback/inspector.html' },
    { id: 'card', targets: ['chat'], path: '/harnessly.cashback/card.html' },
  ],
  sinopia: {
    bindings: { schema: 'schema/cashback.json', inspector: 'inspector', lockUnbound: true },
    selection: [{ id: 'explain', title: 'Объяснить', agent: 'Объясни это место' }],
  },
};

export const boards: ISinopiaBoard[] = [
  { id: '01HQ0000000000000000000001', title: 'Главная', file: 'index.dc.html' },
  { id: '01HQ0000000000000000000002', title: 'Кэшбэк', file: 'cashback/card.dc.html' },
];

export const stateOf = (over: Partial<ISinopiaState> = {}): ISinopiaState => ({
  kind: 'site',
  rev: 1,
  boards,
  apps: [],
  lockUnbound: false,
  marks: [],
  results: [],
  ...over,
});
