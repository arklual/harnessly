// Limits, as docs/hub/limits.md writes them: rules on a scope, a person, an account and a model, and the effective limit
// of a person (the tightest rule wins). Times are RFC 3339 UTC strings; amounts follow common.ts.

import type { Currency, Money, PersonRef } from './common';
import type { Scope } from './orgs';

export type LimitPeriod = 'month' | 'five_hour' | 'seven_day';
export type LimitUnit = 'money' | 'tokens' | 'window_pct';
export type LimitStatus = 'ok' | 'warn' | 'stop';
export type EnforceMode = 'off' | 'warn' | 'stop';

export type LimitRule = {
  id: string;
  scope: Scope;
  userId: string | null; // set: a personal limit (team) or ceiling (tenant); null: the scope's own budget
  accountId: string | null;
  modelPattern: string | null;
  forbid: boolean;
  period: LimitPeriod;
  unit: LimitUnit;
  amount: number; // dollars for 'money', tokens for 'tokens', percent of the window for 'window_pct'
  warnPct: number; // 0..1, 0.8 unless set
  used: number; // in the rule's unit, over its current period
  pct: number; // used ÷ amount, 0..∞; 1 for a forbid rule
  status: LimitStatus;
  resetsAt: string | null; // the end of the current period; null for a forbid rule
  estimated: boolean; // 'window_pct': the person's share of a shared window is an estimate («примерно»)
  setBy: PersonRef | null;
  setAt: string;
};

// The effective limit of a person for one account and one model family ('' = any): the worst rule that applies.
export type EffectiveLimit = {
  account: { id: string; label: string } | null;
  modelPattern: string;
  status: LimitStatus;
  pct: number;
  bindingRuleId: string;
  ruleIds: string[]; // every rule that applies, the binding one first
  reason: string; // Russian, the same text the machine shows
  resetsAt: string | null;
};

export type PersonLimits = { person: PersonRef; effective: EffectiveLimit[] };

export type LimitsResponse = {
  scope: Scope;
  currency: Currency;
  mode: EnforceMode;
  rules: LimitRule[]; // of the scope and of its ancestors that bind its people
  people: PersonLimits[];
  handedOutPct: number | null; // Σ personal limits ÷ the scope's budget, for a team or tenant with a money budget
  budget: Money | null; // the scope's own money budget for the month
};

export type PutLimitRequest = {
  scope: Scope;
  userId: string | null;
  accountId: string | null;
  modelPattern: string | null;
  forbid: boolean;
  period: LimitPeriod;
  unit: LimitUnit;
  amount: number;
  warnPct?: number;
};

export type PutLimitResponse = { rule: LimitRule };

// What one of the person's machines holds (budget.set, internal/hubproto/budget.go).
export type MachineBudget = { machineId: string; name: string; version: number; syncedAt: string | null };

export type MeLimitsResponse = {
  mode: EnforceMode;
  currency: Currency;
  rules: LimitRule[];
  effective: EffectiveLimit[];
  machines: MachineBudget[];
};
