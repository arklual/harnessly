// «Аккаунты» (HRN-661): the subscriptions and keys the machines of the organization work on, with who used each this
// month and what it cost at list prices; and registering one the hub only discovered on a machine.
// GET/PATCH /api/orgs/{org}/accounts of internal/hub/analytics/accounts.go.

export type AccountKind = 'subscription' | 'api_key' | 'prepaid';
export type AccountFunding = 'org' | 'personal';

export interface AccountPerson {
  userId: string;
  name: string;
}

export interface Account {
  id: string;
  label: string;
  /** `anthropic`, `openai`, … */
  provider: string;
  kind: AccountKind;
  plan: string;
  funding: AccountFunding;
  /** The month's fee of a subscription, in the shown currency and in dollars. */
  feeRub: number;
  feeUsd: number;
  /** Seen on a machine, nobody said yet what it is and who pays. */
  discovered: boolean;
  /** The tenant it belongs to; null: the whole organization. */
  tenantId: string | null;
  owner: AccountPerson | null;
  /** This month at list prices. */
  monthRub: number;
  people: AccountPerson[];
}

export interface AccountsView {
  accounts: Account[];
}

export interface RegisterAccount {
  label: string;
  plan: string;
  kind: AccountKind;
  funding: AccountFunding;
  /** Dollars a month; only for a subscription. */
  monthlyFeeUsd: number | null;
  ownerUserId: string | null;
  /** Null: the whole organization. */
  tenantId: string | null;
  /** Give the owner's earlier chats without an account to this one. */
  history: boolean;
}

export interface AccountsApi {
  accounts(): Promise<AccountsView>;
  registerAccount(id: string, body: RegisterAccount): Promise<Account | null>;
}
