// «Аккаунты» on GET/PATCH /api/orgs/{org}/accounts (internal/hub/analytics/accounts.go).
import type { Currency, Money, PersonRef } from '../../../../hub-api/common';
import { setShownCurrency } from '../../money';
import type { Account, AccountKind, AccountsApi, AccountFunding } from '../accounts';
import { http, orgPath } from './http';

interface Wire {
  id: string;
  label: string;
  provider: string;
  kind: string;
  plan: string;
  funding: string;
  monthlyFee: Money;
  discovered: boolean;
  scope: { kind: string; id: string };
  owner: PersonRef | null;
  monthList: Money;
  people: PersonRef[] | null;
}

const KINDS: readonly AccountKind[] = ['subscription', 'api_key', 'prepaid'];

const accountOf = (a: Wire): Account => ({
  id: a.id,
  label: a.label,
  provider: a.provider,
  kind: KINDS.find((k) => k === a.kind) ?? 'subscription',
  plan: a.plan,
  funding: (a.funding === 'personal' ? 'personal' : 'org') as AccountFunding,
  feeRub: a.monthlyFee.shown,
  feeUsd: a.monthlyFee.usd,
  discovered: a.discovered,
  tenantId: a.scope.kind === 'tenant' ? a.scope.id : null,
  owner: a.owner,
  monthRub: a.monthList.shown,
  people: a.people ?? [],
});

export const liveAccounts = (
  ctx: () => { org: string; scope: { kind: string; id: string } },
): AccountsApi => ({
  accounts: async () => {
    const c = ctx();
    const r = await http.get<{ currency: Currency; accounts: Wire[] }>(orgPath(c.org, '/accounts'), {
      scope: `${c.scope.kind}:${c.scope.id}`,
    });
    setShownCurrency(r.currency.code);
    return { accounts: r.accounts.map(accountOf) };
  },
  registerAccount: async (id, b) => {
    const c = ctx();
    const r = await http.patch<{ account: Wire | null }>(
      orgPath(c.org, `/accounts/${encodeURIComponent(id)}`),
      {
        label: b.label,
        plan: b.plan,
        kind: b.kind,
        funding: b.funding,
        ...(b.kind === 'subscription' && b.monthlyFeeUsd !== null ? { monthlyFee: b.monthlyFeeUsd } : {}),
        ...(b.ownerUserId ? { ownerUserId: b.ownerUserId } : {}),
        ...(b.tenantId ? { tenantId: b.tenantId } : {}),
        history: b.history,
      },
    );
    return r.account ? accountOf(r.account) : null;
  },
});
