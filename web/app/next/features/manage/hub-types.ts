// What the hub says about the person (`/api/me/*`, plan §2.11). The contract is still being written
// (docs/hub/orgs.md and its neighbours); these are the shapes the screen needs, to be aligned when it lands.
// Times are RFC 3339 UTC strings, money is in the organization's display currency.

export type HubCurrency = 'RUB' | 'USD';

/** One limit that acts on the person; the tightest one decides (`MeLimits.limits[].effective`). */
export interface MeLimit {
  id: string;
  /** The axis it limits: the person, their team, the tenant, a shared account, a model. */
  axis: 'person' | 'team' | 'tenant' | 'account' | 'model';
  label: string;
  used: number;
  amount: number;
  /** Share of the amount at which a warning comes. */
  warnPct: number;
  /** The person who set it; null when the hub itself did. */
  setBy: { name: string; role: string } | null;
  resetsAt: string;
  effective: boolean;
}

export interface MeLimits {
  currency: HubCurrency;
  measuredUntil: string;
  limits: MeLimit[];
  /** The lead who gets «Попросить больше»; null when nobody can grant it. */
  askTo: { name: string } | null;
  /** A request already sent in this period. */
  pendingRequest: { at: string } | null;
}

export interface MeUsage {
  currency: HubCurrency;
  measuredUntil: string;
  days: { day: string; cost: number }[];
  byModel: { model: string; cost: number }[];
  /** What the agent's own machine cost, apart from the models. */
  machineCost: number;
  /** A guess at the month's end at the current pace. */
  forecast: number | null;
}

export interface MeSession {
  /** The provider's session id: the same key the local report has, so the two lists meet on it. */
  key: string;
  endedAt: string | null;
  cost: number;
  machine: string;
  outcome: 'running' | 'mr_merged' | 'mr_open' | 'abandoned' | 'done';
}

export interface MeSessions {
  measuredUntil: string;
  sessions: MeSession[];
}

export interface MeKpis {
  measuredUntil: string;
  kpis: { id: 'moneyPerMr' | 'yield' | 'timeToMr' | 'mrPerMonth'; value: number; previous: number | null }[];
}

export interface HubMe {
  limits: MeLimits;
  usage: MeUsage;
  sessions: MeSessions;
  kpis: MeKpis;
}
