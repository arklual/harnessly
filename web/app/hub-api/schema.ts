// A small runtime check for the contract fixtures: each schema is a value whose inferred type the tests compare with the
// contract's hand-written TS type, so a fixture that passes the schema also fits the type the screens use.

export type Schema<T> = { check: (v: unknown, path: string) => string[]; _t?: T };
export type Infer<S> = S extends Schema<infer T> ? T : never;

const fail = (path: string, want: string, v: unknown) => [
  `${path}: want ${want}, got ${JSON.stringify(v)?.slice(0, 60)}`,
];

export const str: Schema<string> = { check: (v, p) => (typeof v === 'string' ? [] : fail(p, 'string', v)) };
export const num: Schema<number> = {
  check: (v, p) => (typeof v === 'number' && Number.isFinite(v) ? [] : fail(p, 'number', v)),
};
export const bool: Schema<boolean> = {
  check: (v, p) => (typeof v === 'boolean' ? [] : fail(p, 'boolean', v)),
};
export const unknownValue: Schema<unknown> = { check: () => [] };

const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$/;
export const time: Schema<string> = {
  check: (v, p) => (typeof v === 'string' && rfc3339.test(v) ? [] : fail(p, 'RFC 3339 UTC time', v)),
};
export const day: Schema<string> = {
  check: (v, p) => (typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v) ? [] : fail(p, 'YYYY-MM-DD', v)),
};

export function lit<const L extends readonly (string | number | boolean)[]>(...values: L): Schema<L[number]> {
  return { check: (v, p) => (values.includes(v as L[number]) ? [] : fail(p, values.join(' | '), v)) };
}

export function nullable<T>(s: Schema<T>): Schema<T | null> {
  return { check: (v, p) => (v === null ? [] : s.check(v, p)) };
}

export function arr<T>(s: Schema<T>): Schema<T[]> {
  return {
    check: (v, p) => (Array.isArray(v) ? v.flatMap((x, i) => s.check(x, `${p}[${i}]`)) : fail(p, 'array', v)),
  };
}

export function rec<T>(s: Schema<T>, keys?: RegExp): Schema<Record<string, T>> {
  return {
    check: (v, p) => {
      if (typeof v !== 'object' || v === null || Array.isArray(v)) return fail(p, 'object', v);
      return Object.entries(v).flatMap(([k, x]) => [
        ...(keys && !keys.test(k) ? [`${p}: key ${k} does not match ${keys}`] : []),
        ...s.check(x, `${p}.${k}`),
      ]);
    },
  };
}

type Shape = Record<string, Schema<unknown> | Opt<unknown>>;
type Opt<T> = { optional: Schema<T> };
export const opt = <T>(s: Schema<T>): Opt<T> => ({ optional: s });

type OptKeys<S extends Shape> = { [K in keyof S]: S[K] extends Opt<unknown> ? K : never }[keyof S];
type ReqKeys<S extends Shape> = Exclude<keyof S, OptKeys<S>>;
type Flat<T> = { [K in keyof T]: T[K] };
export type ObjOf<S extends Shape> = Flat<
  { [K in ReqKeys<S>]: Infer<S[K]> } & { [K in OptKeys<S>]?: S[K] extends Opt<infer T> ? T : never }
>;

// obj is exact: a key the schema does not name is an error, so a fixture cannot carry a field the contract lacks.
export function obj<S extends Shape>(shape: S): Schema<ObjOf<S>> {
  return {
    check: (v, p) => {
      if (typeof v !== 'object' || v === null || Array.isArray(v)) return fail(p, 'object', v);
      const o = v as Record<string, unknown>;
      const errs: string[] = [];
      for (const [k, s] of Object.entries(shape)) {
        if ('optional' in s) {
          if (k in o && o[k] !== undefined) errs.push(...s.optional.check(o[k], `${p}.${k}`));
        } else if (!(k in o)) errs.push(`${p}.${k}: missing`);
        else errs.push(...s.check(o[k], `${p}.${k}`));
      }
      for (const k of Object.keys(o)) if (!(k in shape)) errs.push(`${p}.${k}: not in the contract`);
      return errs;
    },
  };
}

export function union<A, B>(a: Schema<A>, b: Schema<B>): Schema<A | B> {
  return {
    check: (v, p) => {
      const ea = a.check(v, p);
      if (ea.length === 0) return [];
      const eb = b.check(v, p);
      return eb.length === 0 ? [] : [...ea, ...eb];
    },
  };
}

// Equal is true only when the two types are the same both ways; the tests assert it for each schema and its type.
export type Equal<X, Y> = (<T>() => T extends X ? 1 : 2) extends <T>() => T extends Y ? 1 : 2 ? true : false;
export const assertType = <T extends true>(_: T) => _;

export function oneOf<const S extends readonly Schema<unknown>[]>(...schemas: S): Schema<Infer<S[number]>> {
  return {
    check: (v, p) => {
      const errs: string[] = [];
      for (const s of schemas) {
        const e = s.check(v, p);
        if (e.length === 0) return [];
        errs.push(...e);
      }
      return [`${p}: matches none of ${schemas.length} shapes`, ...errs.slice(0, 4)];
    },
  };
}
