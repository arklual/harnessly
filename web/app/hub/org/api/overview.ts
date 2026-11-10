// Обзор: the numbers of the first screen for an admin and a team lead. Money is in rubles (the display
// currency, `$` next to it is the same money at the month's rate). Shapes to align with the analytics and limits
// contracts (docs/hub/orgs.md has none of these yet): spend/limits → limits.md, subscriptions/signals → analytics.md.
import type { Section } from '../types';

export type OverviewPeriod = 'week' | 'month';

/** The head tiles. `bars` is the last days, oldest first, the last one is today. */
export interface OverviewTiles {
  /** What the head tiles count: today (the default), or the whole period while the hub gives no «today» yet. */
  basis?: 'today' | 'period';
  spend: { rub: number; usd: number; tokens: number; bars: number[] };
  /** `who`: everyone of the scope, the ones at work now first; the «Активны сегодня» drawer lists them. */
  active: { now: number; total: number; bars: number[]; who: ActivePerson[] };
  tasks: { closed: number; inProgress: number; bars: number[] };
  subscriptions: { accounts: number; worstName: string; worstPct: number };
}

/** One person of «Активны сегодня»: what they did today and when they last worked through a machine. */
export interface ActivePerson {
  userId: string;
  name: string;
  email: string;
  /** A chat of theirs was heard from in the last minutes. */
  working: boolean;
  /** Chats going on at some moment of today, and of them ended today with a merge request. */
  chats: number;
  closed: number;
  /** When they last worked through a machine, any day; null: never. */
  lastAt: string | null;
  /** Today's money: what the accounts cost (the subscription's share), and the same work at API prices. */
  rub: number;
  listRub: number;
  tokens: number;
}

export interface Signal {
  id: string;
  severity: 'critical' | 'warn';
  title: string;
  detail: string;
  /** The label of the action and where it leads: a screen of the hub with its card (`query`), or a chat of a machine. Null: nothing to do but read. */
  action: SignalGo | null;
}

/** Where a signal's button leads: the section, the card on it, or a chat that opens on its machine. */
export interface SignalGo {
  label: string;
  section: Section;
  /** The address of the card on that screen, without `?`: `p=<user>`, `a=<account>&dlg=register`. */
  query?: string;
  chat?: { machineId: string; sessionKey: string };
}

export interface Money {
  rub: number;
  usd: number;
  tokens: number;
}

export interface TeamSpend extends Money {
  id: string;
  name: string;
  /** The row of the people of no team: drawn quieter. */
  noTeam?: boolean;
}

export interface TeamEfficiency {
  id: string;
  name: string;
  /** Tasks closed per person in the period. */
  tasksPerPerson: number;
  /** Share of chats that ended in a result, 0..100; null while there are too few chats to tell. */
  resultPct: number | null;
  /** The row of the people of no team: drawn quieter. */
  noTeam?: boolean;
}

export interface SubscriptionUse {
  id: string;
  name: string;
  teams: string[];
  /** When the window resets, a short weekday: «пн». */
  resetsOn: string;
  usedPct: number;
}

export interface PersonSpend {
  userId: string;
  name: string;
  isLead: boolean;
  /** ISO time; null: never signed in. */
  lastActiveAt: string | null;
  invited: boolean;
  spentRub: number;
  /** The personal limit in effect; null: none set. */
  limitRub: number | null;
}

export interface OverviewData {
  /** The spend is at list prices: the accounts have no month's price, and money limits do not bind them. */
  estimated: boolean;
  tiles: OverviewTiles;
  signals: Signal[];
  subscriptions: SubscriptionUse[];
  /** An admin's: spend and efficiency by team. Null for a lead. */
  byTeam: { total: Money; teams: TeamSpend[]; efficiency: TeamEfficiency[] } | null;
  /** A lead's: the team's spend by person. Null for an admin. */
  byPerson: { teamName: string; spentRub: number; limitRub: number | null; people: PersonSpend[] } | null;
}

/** The cuts of «Детализация расхода» and its periods: today, this week, this month. */
export type SpendCut = 'team' | 'person' | 'model' | 'account';
export type SpendSpan = 'day' | 'week' | 'month';

/** A limit a row or the whole is held to: its amount and how much of it is used. */
export interface SpendLimit {
  rub: number;
  pct: number;
  status: 'ok' | 'warn' | 'stop';
}

export interface SpendLine {
  /** The team, person, account id or the model's name; `''` is «без команды». */
  key: string;
  label: string;
  /** What it costs (the key's price or the share of a subscription); at list prices when `estimated`. */
  rub: number;
  usd: number;
  /** At list prices: what the same tokens would cost by API. */
  listRub: number;
  estimated: boolean;
  tokens: number;
  calls: number;
  /** Calls of models without a price: their money is not in the numbers. */
  unpricedCalls: number;
  limit: SpendLimit | null;
  /** At the pace of the last 7 days, to the period's end; null for today. */
  forecastRub: number | null;
}

export interface SpendDetail {
  by: SpendCut;
  span: SpendSpan;
  total: { rub: number; usd: number; listRub: number; estimated: boolean; tokens: number; calls: number };
  limit: SpendLimit | null;
  forecastRub: number | null;
  rows: SpendLine[];
  /** ISO time of the newest fact any machine of the scope sent; null: none yet. */
  accountedUntil: string | null;
}

/** How long a signal may be put off: the hub takes any time up to 30 days; the screen offers these two. */
export type SnoozeFor = 'day' | 'week';

export interface OverviewApi {
  overview(tenant: string, period: OverviewPeriod): Promise<OverviewData>;
  /** `POST …/signals/{id}/snooze`: the signal leaves the list until it is over, or comes back worse. */
  snoozeSignal(id: string, span: SnoozeFor): Promise<void>;
  /** `DELETE …/signals/{id}/snooze`: the snooze is taken back and the signal is listed again. */
  unsnoozeSignal(id: string): Promise<void>;
  spendDetail(by: SpendCut, span: SpendSpan): Promise<SpendDetail>;
}
