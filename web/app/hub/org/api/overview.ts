// Обзор (HRN-537): the numbers of the first screen for an admin and a team lead. Money is in rubles (the display
// currency, `$` next to it is the same money at the month's rate). Shapes to align with the analytics and limits
// contracts (docs/hub/orgs.md has none of these yet): spend/limits → limits.md, subscriptions/signals → analytics.md.
import type { Section } from '../types';

export type OverviewPeriod = 'week' | 'month';

/** The head tiles. `bars` is the last days, oldest first, the last one is today. */
export interface OverviewTiles {
  spend: { rub: number; usd: number; tokens: number; bars: number[] };
  active: { now: number; total: number; bars: number[] };
  tasks: { closed: number; inProgress: number; bars: number[] };
  subscriptions: { accounts: number; worstName: string; worstPct: number };
}

export interface Signal {
  id: string;
  severity: 'critical' | 'warn';
  title: string;
  detail: string;
  /** The label of the action and the screen it opens (a route of the hub). */
  action: { label: string; section: Section };
}

export interface Money {
  rub: number;
  usd: number;
  tokens: number;
}

export interface TeamSpend extends Money {
  id: string;
  name: string;
}

export interface TeamEfficiency {
  id: string;
  name: string;
  /** Tasks closed per person in the period. */
  tasksPerPerson: number;
  /** Share of chats that ended in a result, 0..100. */
  resultPct: number;
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

export interface OverviewApi {
  overview(tenant: string, period: OverviewPeriod): Promise<OverviewData>;
}
