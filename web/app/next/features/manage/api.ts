// The hub's side of «Управление»: one interface, a live client over `<hubUrl>/api/me/*` and a mock. The mock is chosen
// by `?hub=mock` (a gallery, tests); otherwise a configured hub is asked, and without one the screen runs on the local
// report alone, as it always did.
import { appConfig } from '@/platform/config';
import {
  type Check,
  type KpisResponse,
  type MeLimitsResponse,
  type MeSessionsResponse,
  type MeUsageResponse,
  kpisWire,
  meLimitsWire,
  meSessionsWire,
  meUsageWire,
} from '@/next/features/manage/wire';
import type { HubMe, MeKpis, MeLimit, MeLimits, MeSessions, MeUsage } from '@/next/features/manage/hub-types';
import { t } from '@/next/i18n';
import { mockHub } from '@/next/features/manage/mock';

export class HubError extends Error {
  /** `off`: the hub does not serve the organization endpoints (404): not a failure, the feature is just not on. */
  readonly kind: 'off' | 'failed';
  constructor(kind: HubError['kind'], message: string) {
    super(message);
    this.name = 'HubError';
    this.kind = kind;
  }
}

export interface ManageHub {
  load(): Promise<HubMe>;
  /** Asks the person who set the limit for more. Resolves when the hub has taken the request. */
  askMore(message: string): Promise<void>;
}

/** 404 and 501: the hub does not serve the person's endpoints (organizations are off, an older hub). */
const call = async <T>(
  base: string,
  path: string,
  schema: Check<T> | null,
  init?: RequestInit,
): Promise<T> => {
  let res: Response;
  try {
    res = await fetch(`${base}${path}`, { credentials: 'include', ...init });
  } catch {
    throw new HubError('failed', 'network');
  }
  if (res.status === 404 || res.status === 501) throw new HubError('off', 'not found');
  if (!res.ok) throw new HubError('failed', String(res.status));
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    throw new HubError('failed', 'not json');
  }
  if (schema) {
    const problems = schema.check(body, path);
    if (problems.length > 0) throw new HubError('failed', problems[0] ?? 'shape');
  }
  return body as T;
};

const finite = (v: number): number => (Number.isFinite(v) ? v : 0);

/** The rules that are money per month, in display money; the one the hub names as binding is `effective`. */
export const mapLimits = (wire: MeLimitsResponse, usage: MeUsageResponse): MeLimits => {
  const rate = wire.currency.perUsd > 0 ? wire.currency.perUsd : 1;
  const binding = new Set(wire.effective.map((e) => e.bindingRuleId));
  const limits: MeLimit[] = wire.rules
    .filter((r) => r.unit === 'money' && !r.forbid && r.period === 'month' && r.amount > 0)
    .map((r) => ({
      id: r.id,
      label: t('next.manage.limit.title'),
      used: finite(r.used * rate),
      amount: finite(r.amount * rate),
      warnPct: Math.round(r.warnPct * 100),
      setBy: r.setBy ? { name: r.setBy.name } : null,
      resetsAt: r.resetsAt,
      effective: binding.has(r.id),
    }));
  return {
    currency: wire.currency.code,
    mode: wire.mode,
    measuredUntil: usage.accountedUntil,
    limits,
    askTo: null,
    pendingRequest: null,
  };
};

export const mapUsage = (wire: MeUsageResponse): MeUsage => ({
  currency: wire.currency.code,
  days: wire.byDay.map((d) => ({ day: d.day, cost: d.paid.shown })),
  byModel: wire.byModel.map((m) => ({ model: m.model, cost: m.list.shown })),
  forecast: null,
});

export const mapSessions = (wire: MeSessionsResponse): MeSessions => ({
  sessions: wire.sessions.map((s) => ({
    key: s.sessionKey,
    endedAt: s.endedAt,
    cost: s.paid.shown,
    machine: s.machine?.name ?? '',
    outcome: s.endedAt === null ? 'running' : s.mrs > 0 ? 'mr_open' : 'done',
  })),
});

const KPI_OF = {
  money_per_mr: 'moneyPerMr',
  completion: 'yield',
  time_to_mr: 'timeToMr',
  mrs_per_person: 'mrPerMonth',
} as const;

/** The four KPI the screen shows; one with too few sessions (value null) is left out, not drawn as a zero. */
export const mapKpis = (wire: KpisResponse, currency: MeUsageResponse['currency']): MeKpis => {
  const rate = currency.perUsd > 0 ? currency.perUsd : 1;
  const kpis: MeKpis['kpis'] = [];
  for (const k of wire.kpis) {
    if (!(k.kpi in KPI_OF) || k.value === null) continue;
    const id = KPI_OF[k.kpi as keyof typeof KPI_OF];
    const conv = (v: number): number =>
      k.kpi === 'money_per_mr' ? v * rate : k.kpi === 'time_to_mr' ? v / 60_000 : v;
    kpis.push({ id, value: conv(k.value), previous: k.cmpValue === null ? null : conv(k.cmpValue) });
  }
  return { kpis };
};

export const liveHub = (base: string): ManageHub => ({
  load: async () => {
    const [limits, usage, sessions, kpis] = await Promise.all([
      call<MeLimitsResponse>(base, '/api/me/limits', meLimitsWire),
      call<MeUsageResponse>(base, '/api/me/usage', meUsageWire),
      call<MeSessionsResponse>(base, '/api/me/sessions', meSessionsWire),
      call<KpisResponse>(base, '/api/me/kpis', kpisWire),
    ]);
    return {
      limits: mapLimits(limits, usage),
      usage: mapUsage(usage),
      sessions: mapSessions(sessions),
      kpis: mapKpis(kpis, usage.currency),
    };
  },
  askMore: async (message) => {
    await call<unknown>(base, '/api/me/limit-requests', null, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ message }),
    });
  },
});

/** The hub the screen talks to, or null when this runtime has none. */
export const manageHub = (): ManageHub | null => {
  if (new URLSearchParams(location.search).get('hub') === 'mock') return mockHub();
  const url = appConfig().hubUrl;
  return url ? liveHub(url) : null;
};
