// Overview, spend, subscriptions, KPI, signals, changes, waste, machines and the person's own view (/api/me/*), as
// docs/hub/analytics.md writes them. Days are 'YYYY-MM-DD' in the organization's time zone; times are RFC 3339 UTC.

import type { Currency, Money, Period, PersonRef } from './common';
import type { Scope } from './orgs';

// ── KPI ──────────────────────────────────────────────────────────────────────────────────────────────────────────────

export type KpiId =
  | 'money_per_mr'
  | 'completion'
  | 'time_to_mr'
  | 'mrs_per_person'
  | 'autonomy'
  | 'waiting_human'
  | 'parallelism'
  | 'tool_errors'
  | 'reread_context'
  | 'session_start_ctx'
  | 'cache_hit'
  | 'skills_in_use';

export type KpiUnit = 'usd' | 'share' | 'ms' | 'count' | 'tokens';

export type KpiValue = {
  kpi: KpiId;
  unit: KpiUnit;
  betterWhen: 'up' | 'down';
  value: number | null; // null: too few sessions in the period
  n: number; // sessions behind the value
  cmpValue: number | null; // the comparison period's
  cmpN: number;
  delta: number | null; // value − cmpValue
  noise: number | null; // ±2σ of the weekly values of the 8 weeks before the period; null below 4 weeks of data
  verdict: 'better' | 'worse' | 'noise' | 'too_few';
};

export type KpisResponse = {
  scope: Scope | 'me';
  period: Period;
  cmpPeriod: Period;
  kpis: KpiValue[];
  // ?series=team: the same KPI per team of the scope, «Команды рядом».
  series: { key: string; label: string; kpis: KpiValue[] }[] | null;
};

export type SeriesPoint = { day: string; value: number | null; n: number };

export type ChangeMarker = { changeId: string; at: string; title: string; verdict: EffectVerdict | null };

export type KpiSeriesResponse = {
  kpi: KpiId;
  unit: KpiUnit;
  by: 'day' | 'week';
  points: SeriesPoint[];
  splits: { key: string; label: string; points: SeriesPoint[] }[] | null;
  markers: ChangeMarker[];
};

// ── Signals ──────────────────────────────────────────────────────────────────────────────────────────────────────────

export type SignalRule =
  | 'budget_runs_out'
  | 'over_80'
  | 'window_90'
  | 'tool_errors_up'
  | 'invite_pending'
  | 'machine_offline'
  | 'machine_idle'
  | 'unknown_account'
  | 'model_no_price'
  | 'invoice_mismatch'
  | 'chat_waiting'
  | 'limit_request';

export type SignalAction =
  | { kind: 'raise_limit'; label: string; scope: Scope; userId: string | null }
  | { kind: 'move_account'; label: string; fromAccountId: string; toAccountId: string }
  | { kind: 'open_machine'; label: string; machineId: string }
  | { kind: 'resend_invite'; label: string; inviteId: string; tenantId: string }
  | { kind: 'register_account'; label: string; accountId: string }
  | { kind: 'open_kpi'; label: string; kpi: KpiId; scope: Scope }
  | { kind: 'open_chat'; label: string; machineId: string; sessionKey: string }
  | { kind: 'open_prices'; label: string; model: string };

export type Signal = {
  id: string;
  rule: SignalRule;
  severity: 'info' | 'warn' | 'critical';
  scope: Scope;
  subject: {
    kind: 'user' | 'team' | 'tenant' | 'org' | 'account' | 'machine' | 'model' | 'invite';
    id: string;
    label: string;
  };
  title: string; // Russian, ready to show
  detail: string;
  firstAt: string;
  lastAt: string;
  snoozedUntil: string | null;
  escalated: boolean; // nobody acted for 48 h: raised to the level above
  action: SignalAction | null;
};

export type SignalsResponse = { signals: Signal[] };
export type SnoozeRequest = { until: string };

// ── Spend and subscriptions ──────────────────────────────────────────────────────────────────────────────────────────

export type SpendBy = 'team' | 'person' | 'model' | 'account' | 'tenant';

// The limit a row is held to, as the row's own scope sees it (limits.md, «effective limit»).
export type LimitBar = {
  ruleId: string;
  amount: Money;
  pct: number; // used ÷ amount, 0..∞
  status: 'ok' | 'warn' | 'stop';
  setBy: PersonRef | null;
};

export type SpendRow = {
  key: string; // the team, person, model, account or tenant id (the model's name)
  label: string;
  list: Money; // at list prices
  paid: Money; // what it actually costs: the key's price, or the share of the subscription
  calls: number;
  unpricedCalls: number; // calls of models without a price (their cost is not in list)
  tokens: { input: number; output: number; cacheRead: number; cacheWrite: number };
  limit: LimitBar | null;
  forecast: Money | null; // at the pace of the last 7 days, to the period's end; null for a closed period
  guessedShare: number; // share of list from sessions with attribution 'guessed'
};

export type SpendResponse = {
  scope: Scope;
  by: SpendBy;
  period: Period;
  currency: Currency;
  total: { list: Money; paid: Money; limit: LimitBar | null; forecast: Money | null };
  rows: SpendRow[];
  accountedUntil: string | null; // the newest fact the hub has from any machine of the scope
};

export type WindowView = {
  name: string;
  scope: string;
  lengthSec: number;
  usedPct: number | null;
  resetsAt: string | null;
  measuredAt: string;
  confidence: 'official' | 'undocumented' | 'estimated';
  waiting: boolean; // reset passed and no new measurement yet: «ждём замер»
  forecastFullAt: string | null;
};

export type SubscriptionShare = { person: PersonRef; share: number; amount: Money; windowPct: number | null };

export type Subscription = {
  accountId: string;
  label: string;
  provider: string;
  plan: string;
  monthlyFee: Money;
  preliminary: boolean; // the month is open: shares change until the 1st
  shares: SubscriptionShare[];
  windows: WindowView[];
};

export type SubscriptionsResponse = {
  scope: Scope;
  period: Period;
  currency: Currency;
  subscriptions: Subscription[];
};

// ── Overview ─────────────────────────────────────────────────────────────────────────────────────────────────────────

export type Tile = { value: number | null; cmpValue: number | null; unit: KpiUnit | 'people' };

export type OverviewResponse = {
  scope: Scope;
  period: Period;
  currency: Currency;
  spend: { list: Money; paid: Money; limit: LimitBar | null; forecast: Money | null };
  tiles: { activePeople: Tile; sessions: Tile; mrs: Tile; moneyPerMr: Tile };
  attention: Signal[]; // the open, not snoozed signals addressed to the person, most severe first, at most 6
  spendBy: { by: SpendBy; rows: SpendRow[] }; // teams for an org or tenant, people for a team
  subscriptions: Subscription[];
  accountedUntil: string | null;
};

// ── Changes and their effect ─────────────────────────────────────────────────────────────────────────────────────────

export type ChangeKind =
  'manual' | 'claude_version' | 'codex_version' | 'default_model' | 'skills' | 'plugins' | 'policy';

export type Change = {
  id: string;
  scope: Scope;
  kind: ChangeKind;
  title: string;
  at: string;
  author: PersonRef | null; // null: detected by the hub
  source: 'manual' | 'detected';
  machines: number; // how many machines it touched (detected changes)
};

export type ChangesResponse = { changes: Change[] };
export type CreateChangeRequest = { scope: Scope; title: string; at: string };

export type EffectVerdict = 'better' | 'worse' | 'noise' | 'mixed' | 'too_few';

export type Effect = {
  kpi: KpiId;
  scope: Scope;
  before: number | null;
  after: number | null;
  delta: number | null;
  noise: number | null;
  nBefore: number;
  nAfter: number;
  verdict: EffectVerdict;
};

export type EffectResponse = {
  change: Change;
  before: Period;
  after: Period;
  overlaps: string[]; // ids of other changes within the windows: the effect is mixed
  effects: Effect[];
};

// ── Waste ────────────────────────────────────────────────────────────────────────────────────────────────────────────

export type WasteKind =
  'reread_context' | 'failed_tools' | 'abandoned_sessions' | 'long_waits' | 'oversized_model';

export type WasteItem = {
  kind: WasteKind;
  title: string;
  amount: Money;
  share: number; // of the scope's list spend
  sessions: number;
  hint: string;
};

export type WasteResponse = { scope: Scope; period: Period; currency: Currency; items: WasteItem[] };

// ── Machines in the new hub ──────────────────────────────────────────────────────────────────────────────────────────

export type MachineProblem =
  'offline' | 'idle_week' | 'agent_old' | 'disk_low' | 'login_expired' | 'disabled';

export type HubMachine = {
  id: string;
  name: string;
  hostname: string;
  tenantId: string;
  teamId: string | null;
  status: 'pending' | 'online' | 'offline';
  disabledAt: string | null;
  agentVersion: string;
  lastSeenAt: string | null;
  lastActiveAt: string | null; // the last chat activity
  chatsRunning: number;
  people: PersonRef[]; // who used it in the last 30 days
  access: { kind: 'user' | 'team'; id: string; label: string }[]; // empty: everyone in its tenant
  problems: MachineProblem[];
  cloud: {
    provider: 'selectel';
    flavor: string;
    vcpus: number;
    ramGb: number;
    diskGb: number;
    priceMonth: Money;
  } | null;
};

export type MachinesResponse = { scope: Scope; currency: Currency; machines: HubMachine[] };
export type PatchMachineRequest = {
  name?: string;
  teamId?: string | null;
  access?: { kind: 'user' | 'team'; id: string }[];
};
export type RestartRequest = { what: 'runner' | 'vm'; when: 'now' | 'after_chats' };

// ── The person's own view: /api/me/* ─────────────────────────────────────────────────────────────────────────────────

export type MeUsageResponse = {
  period: Period;
  currency: Currency;
  accountedUntil: string | null; // «на сервере учтено до …»
  total: { list: Money; paid: Money };
  byDay: { day: string; list: Money; paid: Money }[];
  byModel: { model: string; list: Money; calls: number }[];
  byAccount: { accountId: string; label: string; funding: 'org' | 'personal'; list: Money; paid: Money }[];
};

export type MeSession = {
  sessionKey: string;
  provider: 'claude' | 'codex';
  startedAt: string;
  endedAt: string | null;
  machine: { id: string; name: string } | null;
  models: string[];
  list: Money;
  paid: Money;
  funding: 'org' | 'personal';
  origin: 'live' | 'eval' | 'automation';
  turns: number;
  mrs: number;
  subagents: number;
};

export type MeSessionsResponse = { currency: Currency; sessions: MeSession[]; next: string | null };

export type LimitRequest = { amountUsd: number; reason: string; scope: Scope };
export type LimitRequestResponse = { request: { id: string; status: 'sent'; to: PersonRef[] } };
