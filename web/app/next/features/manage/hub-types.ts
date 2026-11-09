// What the Manage screen needs from the hub, already in display money. The hub's own answers (`/api/me/limits`,
// `/usage`, `/sessions`, `/kpis`) are read by `wire.ts` and mapped into these shapes by `api.ts`; nothing here is a
// wire type. Times are RFC 3339 UTC strings, money is in the organization's display currency.

/** An ISO 4217 code as the hub sends it (`currency.code`). */
export type HubCurrency = string;

/** One limit that acts on the person; the tightest one decides (`MeLimit.effective`). */
export interface MeLimit {
  id: string;
  label: string;
  used: number;
  amount: number;
  /** Share of the amount, in percent, at which a warning comes. */
  warnPct: number;
  /** The person who set it; null when the hub did not name one. */
  setBy: { name: string } | null;
  /** The end of the limit's period; null when the hub does not say. */
  resetsAt: string | null;
  effective: boolean;
}

export interface MeLimits {
  currency: HubCurrency;
  /** `off`: the hub keeps the rules but does not enforce them. */
  mode: 'off' | 'warn' | 'stop';
  /** When the server last counted; null until it has counted anything. */
  measuredUntil: string | null;
  limits: MeLimit[];
  /** «Попросить больше» needs a request the hub's endpoint takes (amount, reason, scope); the page hides it for now. */
  askTo: { name: string } | null;
  pendingRequest: { at: string } | null;
}

export interface MeUsage {
  currency: HubCurrency;
  days: { day: string; cost: number }[];
  byModel: { model: string; cost: number }[];
  /** A guess at the month's end at the current pace; the hub does not give one. */
  forecast: number | null;
}

export interface MeSession {
  /** The provider's session id: the same key the local report has, so the two lists meet on it. */
  key: string;
  endedAt: string | null;
  cost: number;
  machine: string;
  outcome: 'running' | 'mr_open' | 'done';
}

export interface MeSessions {
  sessions: MeSession[];
}

export interface MeKpis {
  kpis: { id: 'moneyPerMr' | 'yield' | 'timeToMr' | 'mrPerMonth'; value: number; previous: number | null }[];
}

export interface HubMe {
  limits: MeLimits;
  usage: MeUsage;
  sessions: MeSessions;
  kpis: MeKpis;
}
