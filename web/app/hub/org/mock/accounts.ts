// Mock «Аккаунты»: the board's subscriptions and one account the hub found on a machine.
import type { Account, AccountsApi } from '../api/accounts';
import { later, type MockCtx } from './ctx';

const ACCOUNTS = (): Account[] => [
  {
    id: 'a-found',
    label: 'anthropic · gleb@…',
    provider: 'anthropic',
    kind: 'subscription',
    plan: '',
    funding: 'personal',
    feeRub: 0,
    feeUsd: 0,
    discovered: true,
    tenantId: null,
    owner: null,
    monthRub: 4500,
    people: [{ userId: 'u-gleb', name: 'Глеб' }],
  },
  {
    id: 'a2',
    label: 'Claude Max №2',
    provider: 'anthropic',
    kind: 'subscription',
    plan: 'max',
    funding: 'org',
    feeRub: 18000,
    feeUsd: 200,
    discovered: false,
    tenantId: null,
    owner: { userId: 'u-anya', name: 'Аня' },
    monthRub: 96000,
    people: [
      { userId: 'u-anya', name: 'Аня' },
      { userId: 'u-misha', name: 'Миша' },
      { userId: 'u-oleg', name: 'Олег' },
    ],
  },
  {
    id: 'a1',
    label: 'Claude Max №1',
    provider: 'anthropic',
    kind: 'subscription',
    plan: 'max',
    funding: 'org',
    feeRub: 18000,
    feeUsd: 200,
    discovered: false,
    tenantId: null,
    owner: null,
    monthRub: 71000,
    people: [
      { userId: 'u-andrey', name: 'Андрей' },
      { userId: 'u-pasha', name: 'Паша' },
    ],
  },
  {
    id: 'a3',
    label: 'Codex Pro',
    provider: 'openai',
    kind: 'subscription',
    plan: 'pro',
    funding: 'org',
    feeRub: 18000,
    feeUsd: 200,
    discovered: false,
    tenantId: null,
    owner: null,
    monthRub: 33000,
    people: [{ userId: 'u-timur', name: 'Тимур' }],
  },
];

export const accountsMock = (_ctx: MockCtx): AccountsApi => {
  let list = ACCOUNTS();
  return {
    accounts: async () => later({ accounts: list }),
    registerAccount: async (id, b) => {
      list = list.map((a) =>
        a.id === id
          ? {
              ...a,
              label: b.label,
              plan: b.plan,
              kind: b.kind,
              funding: b.funding,
              feeUsd: b.kind === 'subscription' ? (b.monthlyFeeUsd ?? 0) : 0,
              feeRub: b.kind === 'subscription' ? (b.monthlyFeeUsd ?? 0) * 90 : 0,
              discovered: false,
              tenantId: b.tenantId,
              owner: a.people.find((p) => p.userId === b.ownerUserId) ?? null,
            }
          : a,
      );
      list = [...list.filter((a) => a.discovered), ...list.filter((a) => !a.discovered)];
      return later(list.find((a) => a.id === id) ?? null, 400);
    },
  };
};
