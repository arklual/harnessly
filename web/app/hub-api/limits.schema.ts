import { currency, money, personRef } from './common.schema';
import { scope } from './orgs.schema';
import { arr, bool, lit, nullable, num, obj, opt, str, time } from './schema';

const limitPeriod = lit('month', 'five_hour', 'seven_day');
const limitUnit = lit('money', 'tokens', 'window_pct');
const limitStatus = lit('ok', 'warn', 'stop');
const enforceMode = lit('off', 'warn', 'stop');

export const limitRule = obj({
  id: str,
  scope,
  userId: nullable(str),
  accountId: nullable(str),
  modelPattern: nullable(str),
  forbid: bool,
  period: limitPeriod,
  unit: limitUnit,
  amount: num,
  warnPct: num,
  used: num,
  pct: num,
  status: limitStatus,
  resetsAt: nullable(time),
  estimated: bool,
  setBy: nullable(personRef),
  setAt: time,
});
export const effectiveLimit = obj({
  account: nullable(obj({ id: str, label: str })),
  modelPattern: str,
  status: limitStatus,
  pct: num,
  bindingRuleId: str,
  ruleIds: arr(str),
  reason: str,
  resetsAt: nullable(time),
});
export const personLimits = obj({ person: personRef, effective: arr(effectiveLimit) });
export const limitsResponse = obj({
  scope,
  currency,
  mode: enforceMode,
  rules: arr(limitRule),
  people: arr(personLimits),
  handedOutPct: nullable(num),
  budget: nullable(money),
});
export const putLimitRequest = obj({
  scope,
  userId: nullable(str),
  accountId: nullable(str),
  modelPattern: nullable(str),
  forbid: bool,
  period: limitPeriod,
  unit: limitUnit,
  amount: num,
  warnPct: opt(num),
});
export const putLimitResponse = obj({ rule: limitRule });
export const machineBudget = obj({ machineId: str, name: str, version: num, syncedAt: nullable(time) });
export const meLimitsResponse = obj({
  mode: enforceMode,
  currency,
  rules: arr(limitRule),
  effective: arr(effectiveLimit),
  machines: arr(machineBudget),
});
