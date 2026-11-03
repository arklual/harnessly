// Runtime schemas of analytics.ts, for the fixture test.

import { currency, money, period, personRef } from './common.schema';
import {
  machineCloud,
  machineHardware,
  machineLoad,
  machineManaged,
  machineOp,
  machineWeek,
} from './machines.schema';
import { scope } from './orgs.schema';
import { arr, bool, day, lit, nullable, num, obj, oneOf, opt, str, time } from './schema';

export const kpiId = lit(
  'money_per_mr',
  'completion',
  'time_to_mr',
  'mrs_per_person',
  'autonomy',
  'waiting_human',
  'parallelism',
  'tool_errors',
  'reread_context',
  'session_start_ctx',
  'cache_hit',
  'skills_in_use',
);
const kpiUnit = lit('usd', 'share', 'ms', 'count', 'tokens');

export const kpiValue = obj({
  kpi: kpiId,
  unit: kpiUnit,
  betterWhen: lit('up', 'down'),
  value: nullable(num),
  n: num,
  cmpValue: nullable(num),
  cmpN: num,
  delta: nullable(num),
  noise: nullable(num),
  verdict: lit('better', 'worse', 'noise', 'too_few'),
});

export const kpisResponse = obj({
  scope: oneOf(scope, lit('me')),
  period,
  cmpPeriod: period,
  kpis: arr(kpiValue),
  series: nullable(arr(obj({ key: str, label: str, kpis: arr(kpiValue) }))),
});

const seriesPoint = obj({ day, value: nullable(num), n: num });
const effectVerdict = lit('better', 'worse', 'noise', 'mixed', 'too_few');
const changeMarker = obj({
  changeId: str,
  at: time,
  title: str,
  layer: lit('org', 'tenant', 'team'),
  author: nullable(personRef),
  icon: lit('edit', 'announce', 'rocket'),
  verdict: nullable(effectVerdict),
});

export const kpiSeriesResponse = obj({
  kpi: kpiId,
  unit: kpiUnit,
  by: lit('day', 'week'),
  points: arr(seriesPoint),
  splits: nullable(arr(obj({ key: str, label: str, points: arr(seriesPoint) }))),
  markers: arr(changeMarker),
});

export const signalAction = oneOf(
  obj({ kind: lit('raise_limit'), label: str, target: str, scope, userId: nullable(str) }),
  obj({ kind: lit('move_account'), label: str, target: str, fromAccountId: str, toAccountId: str }),
  obj({ kind: lit('open_machine'), label: str, target: str, machineId: str }),
  obj({ kind: lit('resend_invite'), label: str, target: str, inviteId: str, tenantId: str }),
  obj({ kind: lit('register_account'), label: str, target: str, accountId: str }),
  obj({ kind: lit('open_kpi'), label: str, target: str, kpi: kpiId, scope }),
  obj({ kind: lit('open_chat'), label: str, target: str, machineId: str, sessionKey: str }),
  obj({ kind: lit('open_prices'), label: str, target: str, model: str }),
);

export const signal = obj({
  id: str,
  rule: lit(
    'budget_runs_out',
    'over_80',
    'window_90',
    'tool_errors_up',
    'invite_pending',
    'machine_offline',
    'machine_idle',
    'unknown_account',
    'model_no_price',
    'invoice_mismatch',
    'chat_waiting',
    'limit_request',
  ),
  severity: lit('info', 'warn', 'critical'),
  scope,
  subject: obj({
    kind: lit('user', 'team', 'tenant', 'org', 'account', 'machine', 'model', 'invite'),
    id: str,
    label: str,
  }),
  title: str,
  detail: str,
  firstAt: time,
  lastAt: time,
  snoozedUntil: nullable(time),
  escalated: bool,
  action: nullable(signalAction),
});
export const signalsResponse = obj({ signals: arr(signal) });

const spendBy = lit('team', 'person', 'model', 'account', 'tenant');
export const limitBar = obj({
  ruleId: str,
  amount: money,
  pct: num,
  status: lit('ok', 'warn', 'stop'),
  setBy: nullable(personRef),
});
export const spendRow = obj({
  key: str,
  label: str,
  list: money,
  paid: money,
  calls: num,
  unpricedCalls: num,
  tokens: obj({ total: num, input: num, output: num, cacheRead: num, cacheWrite: num }),
  limit: nullable(limitBar),
  forecast: nullable(money),
  guessedShare: num,
});
const spendTotal = obj({ list: money, paid: money, limit: nullable(limitBar), forecast: nullable(money) });
export const spendResponse = obj({
  scope,
  by: spendBy,
  period,
  currency,
  total: spendTotal,
  rows: arr(spendRow),
  accountedUntil: nullable(time),
});

export const windowView = obj({
  name: str,
  scope: str,
  lengthSec: num,
  usedPct: nullable(num),
  resetsAt: nullable(time),
  measuredAt: time,
  confidence: lit('official', 'undocumented', 'estimated'),
  waiting: bool,
  forecastFullAt: nullable(time),
});
export const subscription = obj({
  accountId: str,
  label: str,
  provider: str,
  plan: str,
  monthlyFee: money,
  preliminary: bool,
  shares: arr(obj({ person: personRef, share: num, amount: money, windowPct: nullable(num) })),
  windows: arr(windowView),
});
export const subscriptionsResponse = obj({ scope, period, currency, subscriptions: arr(subscription) });

const dayValue = obj({ day, value: num });
const reached = obj({ value: nullable(num), n: num });
export const todayView = obj({
  day,
  spend: obj({ paid: money, list: money, tokens: num }),
  active: obj({ people: num, of: num }),
  tasks: obj({ closed: num, inWork: num }),
  sparks: obj({ spend: arr(dayValue), active: arr(dayValue), tasks: arr(dayValue) }),
});
export const efficiencyView = obj({
  teams: arr(obj({ key: str, label: str, people: num, closed: num, perPersonMonth: nullable(num), reached })),
  reached,
});

const tile = obj({ value: nullable(num), cmpValue: nullable(num), unit: oneOf(kpiUnit, lit('people')) });
export const overviewResponse = obj({
  scope,
  period,
  currency,
  spend: spendTotal,
  tiles: obj({ activePeople: tile, sessions: tile, mrs: tile, moneyPerMr: tile }),
  attention: arr(signal),
  spendBy: obj({ by: spendBy, rows: arr(spendRow), totals: obj({ list: money, paid: money, tokens: num }) }),
  today: todayView,
  efficiency: efficiencyView,
  subscriptions: arr(subscription),
  accountedUntil: nullable(time),
});

const chats = obj({ n: num, running: num });
const mrs = obj({ n: num, merged: num });
export const peopleResponse = obj({
  scope,
  period,
  currency,
  total: obj({
    list: money,
    paid: money,
    limit: nullable(limitBar),
    tone: nullable(lit('ok', 'warn', 'risk')),
  }),
  week: period,
  people: arr(
    obj({
      userId: str,
      name: str,
      role: str,
      list: money,
      paid: money,
      share: nullable(num),
      limit: nullable(limitBar),
      hot: bool,
      chats,
      mrs,
    }),
  ),
});
export const meWeek = obj({ week: period, chats, mrs });

export const change = obj({
  id: str,
  scope,
  kind: lit(
    'manual',
    'claude_version',
    'codex_version',
    'default_model',
    'skills',
    'plugins',
    'policy',
    'env',
  ),
  title: str,
  at: time,
  author: nullable(personRef),
  source: lit('manual', 'detected'),
  machines: num,
});
export const changesResponse = obj({ changes: arr(change) });
export const effect = obj({
  kpi: kpiId,
  scope,
  before: nullable(num),
  after: nullable(num),
  delta: nullable(num),
  noise: nullable(num),
  nBefore: num,
  nAfter: num,
  verdict: effectVerdict,
});
export const effectResponse = obj({
  change,
  before: period,
  after: period,
  overlaps: arr(str),
  effects: arr(effect),
});

export const wasteResponse = obj({
  scope,
  period,
  currency,
  items: arr(
    obj({
      kind: lit('abandoned_sessions', 'retries_after_errors', 'long_context', 'extra_skills'),
      title: str,
      amount: money,
      money,
      share: num,
      sessions: num,
      hint: str,
      explain: str,
    }),
  ),
});

const accessSubject = obj({ kind: lit('user', 'team'), id: str, label: str });
export const hubMachine = obj({
  id: str,
  name: str,
  hostname: str,
  tenantId: str,
  teamId: nullable(str),
  status: lit('pending', 'online', 'offline'),
  disabledAt: nullable(time),
  agentVersion: str,
  lastSeenAt: nullable(time),
  lastActiveAt: nullable(time),
  chatsRunning: num,
  people: arr(personRef),
  access: arr(accessSubject),
  problems: arr(lit('offline', 'idle_week', 'agent_old', 'disk_low', 'login_expired', 'disabled')),
  cloud: nullable(machineCloud),
  hardware: nullable(machineHardware),
  load: nullable(machineLoad),
  week: nullable(machineWeek),
  managed: nullable(machineManaged),
  activeOps: arr(machineOp),
});
export const machinesResponse = obj({ scope, currency, machines: arr(hubMachine) });
export const restartRequest = obj({
  what: lit('runner', 'machine', 'vm'),
  when: opt(lit('now', 'drain', 'after_chats')),
});
export const restartResponse = obj({ jobId: str, op: opt(machineOp) });

const funding = lit('org', 'personal');
export const meUsageResponse = obj({
  period,
  currency,
  accountedUntil: nullable(time),
  total: obj({ list: money, paid: money }),
  byDay: arr(obj({ day, list: money, paid: money })),
  byModel: arr(obj({ model: str, list: money, calls: num })),
  byAccount: arr(obj({ accountId: str, label: str, funding, list: money, paid: money })),
});
export const meSession = obj({
  sessionKey: str,
  provider: lit('claude', 'codex'),
  startedAt: time,
  endedAt: nullable(time),
  machine: nullable(obj({ id: str, name: str })),
  models: arr(str),
  list: money,
  paid: money,
  funding,
  origin: lit('live', 'eval', 'automation'),
  turns: num,
  mrs: num,
  subagents: num,
});
export const meSessionsResponse = obj({ currency, sessions: arr(meSession), next: nullable(str) });
export const limitRequestResponse = obj({
  request: obj({ id: str, status: lit('sent'), to: arr(personRef) }),
});
