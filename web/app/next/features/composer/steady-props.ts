// What a screen hands the window's one field, kept steady: a screen draws again for its own reasons (a clock
// of a second, a store's tick), and each time it makes its handlers and its small objects anew. The field should draw
// only when something it shows has changed. So the functions it gets are stand-ins that never change and call the
// screen's newest one; plain objects, arrays and React elements (a button the screen puts on the field) are compared by
// what they hold; a ref and anything else by identity.
import { cloneElement, isValidElement, type ReactElement } from 'react';

type TPath = readonly (string | number)[];

/** How deep plain objects and arrays are looked into (a picker's value, a quote's fields). */
const DEPTH = 6;

const isPlain = (value: unknown): value is Record<string, unknown> => {
  if (typeof value !== 'object' || value === null) return false;
  if ('$$typeof' in value) return false;
  const proto = Object.getPrototypeOf(value) as unknown;
  if (proto !== Object.prototype && proto !== null) return false;
  // A ref: the field attaches to it, so it must stay the screen's own object.
  const keys = Object.keys(value);
  return !(keys.length === 1 && keys[0] === 'current');
};

const at = (root: unknown, path: TPath): unknown => {
  let value = root;
  for (const step of path) {
    if (typeof value !== 'object' || value === null) return undefined;
    value = (value as Record<string | number, unknown>)[step];
  }
  return value;
};

/** One screen's steady props: its newest props, and the stand-ins for their functions by where they lie. */
export class SteadyProps<T extends object> {
  private latest: T;
  private readonly standIns = new Map<string, (...args: unknown[]) => unknown>();
  private shown: T;

  constructor(props: T) {
    this.latest = props;
    this.shown = this.steady(props, [], 0) as T;
  }

  /** The props as the field draws them. */
  get value(): T {
    return this.shown;
  }

  /** Takes the screen's newest props; true when the field has something new to draw. */
  update(props: T): boolean {
    this.latest = props;
    const next = this.steady(props, [], 0) as T;
    if (same(this.shown, next, 0)) return false;
    // What did not change stays the same object: the field can skip drawing a part whose props are the same.
    this.shown = share(this.shown, next, 0) as T;
    return true;
  }

  private standIn(path: TPath) {
    const key = path.join('\u0000');
    let fn = this.standIns.get(key);
    if (!fn) {
      fn = (...args: unknown[]) => {
        const current = at(this.latest, path);
        return typeof current === 'function' ? (current as (...a: unknown[]) => unknown)(...args) : undefined;
      };
      this.standIns.set(key, fn);
    }
    return fn;
  }

  private steady(value: unknown, path: TPath, depth: number): unknown {
    if (typeof value === 'function') return this.standIn(path);
    if (depth >= DEPTH) return value;
    if (Array.isArray(value)) return value.map((item, i) => this.steady(item, [...path, i], depth + 1));
    if (isValidElement(value)) {
      const element = value as ReactElement<Record<string, unknown>>;
      return cloneElement(
        element,
        this.steady(element.props, [...path, 'props'], depth + 1) as Record<string, unknown>,
      );
    }
    if (isPlain(value)) {
      const out: Record<string, unknown> = {};
      for (const [key, item] of Object.entries(value))
        out[key] = this.steady(item, [...path, key], depth + 1);
      return out;
    }
    return value;
  }
}

/** Equal for the field: the same stand-ins, plain objects and arrays with equal contents, the rest identical. */
const same = (a: unknown, b: unknown, depth: number): boolean => {
  if (Object.is(a, b)) return true;
  if (depth >= DEPTH) return false;
  if (Array.isArray(a) && Array.isArray(b))
    return a.length === b.length && a.every((item, i) => same(item, b[i], depth + 1));
  if (isValidElement(a) && isValidElement(b))
    return a.type === b.type && a.key === b.key && same(a.props, b.props, depth + 1);
  if (isPlain(a) && isPlain(b)) {
    const keys = Object.keys(b);
    return (
      keys.length === Object.keys(a).length &&
      keys.every((key) => key in a && same(a[key], b[key], depth + 1))
    );
  }
  return false;
};

/** The next value, made of the old one's parts wherever they are equal. */
const share = (was: unknown, next: unknown, depth: number): unknown => {
  if (same(was, next, depth)) return was;
  if (depth >= DEPTH) return next;
  if (Array.isArray(was) && Array.isArray(next)) return next.map((item, i) => share(was[i], item, depth + 1));
  if (isPlain(was) && isPlain(next)) {
    const out: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(next)) out[key] = share(was[key], item, depth + 1);
    return out;
  }
  return next;
};
