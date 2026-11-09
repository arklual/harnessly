// The hub's /api/me/* answers as the Manage screen reads them: only the fields it uses, each checked at runtime so a body
// of another shape is refused instead of drawn. The new interface does not reach into @/hub-api (spec §6); the golden
// fixtures in fixtures/ (written by the Go test) keep this file and the hub in step.

export type Check<T> = { check: (v: unknown, path: string) => string[]; _t?: T };

const fail = (path: string, want: string, v: unknown) => [
  `${path}: want ${want}, got ${JSON.stringify(v)?.slice(0, 60)}`,
];

const str: Check<string> = { check: (v, p) => (typeof v === 'string' ? [] : fail(p, 'string', v)) };
const num: Check<number> = {
  check: (v, p) => (typeof v === 'number' && Number.isFinite(v) ? [] : fail(p, 'number', v)),
};
const bool: Check<boolean> = { check: (v, p) => (typeof v === 'boolean' ? [] : fail(p, 'boolean', v)) };

const lit = <const L extends readonly string[]>(...values: L): Check<L[number]> => ({
  check: (v, p) => (values.includes(v as L[number]) ? [] : fail(p, values.join(' | '), v)),
});

const nullable = <T>(s: Check<T>): Check<T | null> => ({
  check: (v, p) => (v === null ? [] : s.check(v, p)),
});

const arr = <T>(s: Check<T>): Check<T[]> => ({
  check: (v, p) => (Array.isArray(v) ? v.flatMap((x, i) => s.check(x, `${p}[${i}]`)) : fail(p, 'array', v)),
});

type Shape = Record<string, Check<unknown>>;
type ObjOf<S extends Shape> = { [K in keyof S]: S[K] extends Check<infer T> ? T : never };

/** Open: the fields named must be there and fit; a field the hub adds later is not this screen's business. */
const obj = <S extends Shape>(shape: S): Check<ObjOf<S>> => ({
  check: (v, p) => {
    if (typeof v !== 'object' || v === null || Array.isArray(v)) return fail(p, 'object', v);
    const o = v as Record<string, unknown>;
    return Object.entries(shape).flatMap(([k, s]) => s.check(o[k], `${p}.${k}`));
  },
});

const currency = obj({ code: str, perUsd: num });
const shown = obj({ shown: num });

export const meLimitsWire = obj({
  mode: lit('off', 'warn', 'stop'),
  currency,
  effective: arr(obj({ bindingRuleId: str })),
  rules: arr(
    obj({
      id: str,
      forbid: bool,
      period: str,
      unit: str,
      amount: num,
      warnPct: num,
      used: num,
      resetsAt: nullable(str),
      setBy: nullable(obj({ name: str })),
    }),
  ),
});

export const meUsageWire = obj({
  currency,
  accountedUntil: nullable(str),
  byDay: arr(obj({ day: str, paid: shown })),
  byModel: arr(obj({ model: str, list: shown })),
});

export const meSessionsWire = obj({
  sessions: arr(
    obj({
      sessionKey: str,
      endedAt: nullable(str),
      paid: shown,
      machine: nullable(obj({ name: str })),
      mrs: num,
    }),
  ),
});

export const kpisWire = obj({
  kpis: arr(obj({ kpi: str, value: nullable(num), cmpValue: nullable(num) })),
});

type Of<C> = C extends Check<infer T> ? T : never;
export type MeLimitsResponse = Of<typeof meLimitsWire>;
export type MeUsageResponse = Of<typeof meUsageWire>;
export type MeSessionsResponse = Of<typeof meSessionsWire>;
export type KpisResponse = Of<typeof kpisWire>;
