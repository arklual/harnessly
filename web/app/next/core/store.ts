// The new interface's stores: plain values outside React, read through useSyncExternalStore. Writes inside batch()
// land together and every store notifies once per flush; a flush runs on the next animation frame (the scheduler
// is swappable, tests flush by hand). Keyed stores notify only the keys that changed, so a long feed of entries
// does not run every row's selector on each step.

import { useCallback, useRef, useSyncExternalStore } from 'react';

type Listener = () => void;

interface Flushable {
  flush(): void;
}

const dirty = new Set<Flushable>();
let depth = 0;
let scheduled = false;

type Scheduler = (run: () => void) => void;

const frame: Scheduler = (run) => {
  if (typeof requestAnimationFrame === 'function' && typeof document !== 'undefined' && !document.hidden) {
    // A hidden tab gets no frames: a timer keeps its stores current for the moment it comes back.
    let done = false;
    const once = () => {
      if (done) return;
      done = true;
      run();
    };
    requestAnimationFrame(once);
    setTimeout(once, 100);
  } else {
    queueMicrotask(run);
  }
};
let scheduler: Scheduler = frame;

/** Replaces how a flush is scheduled; 'sync' notifies at the end of each write or batch (tests, benchmarks). */
export function setScheduler(next: Scheduler | 'sync' | 'frame'): void {
  scheduler = next === 'sync' ? (run) => run() : next === 'frame' ? frame : next;
}

/** Notifies every listener of what changed since the last flush. */
export function flush(): void {
  scheduled = false;
  while (dirty.size > 0) {
    const stores = [...dirty];
    dirty.clear();
    for (const s of stores) s.flush();
  }
}

function markDirty(s: Flushable): void {
  dirty.add(s);
  if (depth === 0 && !scheduled) {
    scheduled = true;
    scheduler(flush);
  }
}

const batchEnds = new Set<() => void>();

/** fn runs as the outermost batch ends, before anyone is notified: for writes a batch gathers and commits once. */
export function onBatchEnd(fn: () => void): () => void {
  batchEnds.add(fn);
  return () => batchEnds.delete(fn);
}

export const inBatch = (): boolean => depth > 0;

/** Runs fn with every store write held back until it returns: one notification per store for the whole batch. */
export function batch<T>(fn: () => T): T {
  depth += 1;
  try {
    return fn();
  } finally {
    if (depth === 1) for (const end of [...batchEnds]) end();
    depth -= 1;
    if (depth === 0 && dirty.size > 0 && !scheduled) {
      scheduled = true;
      scheduler(flush);
    }
  }
}

// ---- a single value

export interface Store<T> {
  get(): T;
  set(next: T): void;
  update(fn: (prev: T) => T): void;
  subscribe(fn: Listener): () => void;
}

export function createStore<T>(initial: T): Store<T> {
  let value = initial;
  let changed = false;
  const listeners = new Set<Listener>();
  const self: Store<T> & Flushable = {
    get: () => value,
    set(next) {
      if (Object.is(next, value)) return;
      value = next;
      changed = true;
      markDirty(self);
    },
    update(fn) {
      self.set(fn(value));
    },
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    flush() {
      if (!changed) return;
      changed = false;
      for (const l of [...listeners]) l();
    },
  };
  return self;
}

// ---- values by key

export interface KeyedStore<K, V> {
  get(key: K): V | undefined;
  has(key: K): boolean;
  set(key: K, value: V): void;
  /** Writes what fn returns; undefined deletes, the same value changes nothing. */
  patch(key: K, fn: (prev: V | undefined) => V | undefined): void;
  delete(key: K): void;
  keys(): IterableIterator<K>;
  values(): IterableIterator<V>;
  readonly size: number;
  subscribeKey(key: K, fn: Listener): () => void;
  /** fn hears every flush that changed something, with the keys that changed. */
  subscribeAll(fn: (changed: ReadonlySet<K>) => void): () => void;
  /** Bumps on every flush that changed something: a cheap snapshot for whole-store readers. */
  version(): number;
}

export function createKeyedStore<K, V>(): KeyedStore<K, V> {
  const values = new Map<K, V>();
  const changed = new Set<K>();
  const byKey = new Map<K, Set<Listener>>();
  const all = new Set<(changed: ReadonlySet<K>) => void>();
  let ver = 0;

  const self: KeyedStore<K, V> & Flushable = {
    get: (key) => values.get(key),
    has: (key) => values.has(key),
    set(key, value) {
      if (values.has(key) && Object.is(values.get(key), value)) return;
      values.set(key, value);
      changed.add(key);
      markDirty(self);
    },
    patch(key, fn) {
      const prev = values.get(key);
      const next = fn(prev);
      if (next === undefined) self.delete(key);
      else self.set(key, next);
    },
    delete(key) {
      if (!values.has(key)) return;
      values.delete(key);
      changed.add(key);
      markDirty(self);
    },
    keys: () => values.keys(),
    values: () => values.values(),
    get size() {
      return values.size;
    },
    subscribeKey(key, fn) {
      let set = byKey.get(key);
      if (!set) byKey.set(key, (set = new Set()));
      set.add(fn);
      return () => {
        set.delete(fn);
        if (set.size === 0) byKey.delete(key);
      };
    },
    subscribeAll(fn) {
      all.add(fn);
      return () => all.delete(fn);
    },
    version: () => ver,
    flush() {
      if (changed.size === 0) return;
      const keys = new Set(changed);
      changed.clear();
      ver += 1;
      for (const k of keys) {
        const set = byKey.get(k);
        if (set) for (const l of [...set]) l();
      }
      for (const l of [...all]) l(keys);
    },
  };
  return self;
}

// ---- hooks

export function useStore<T>(store: Store<T>): T {
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}

/** One key of a keyed store; re-renders only when that key changes. */
export function useKey<K, V>(store: KeyedStore<K, V>, key: K | null): V | undefined {
  const subscribe = useCallback(
    (fn: Listener) => (key === null ? () => {} : store.subscribeKey(key, fn)),
    [store, key],
  );
  const get = () => (key === null ? undefined : store.get(key));
  return useSyncExternalStore(subscribe, get, get);
}

/** A derived value of one key: re-renders only when eq says the selection changed. */
export function useSlice<K, V, S>(
  store: KeyedStore<K, V>,
  key: K | null,
  select: (v: V | undefined) => S,
  eq: (a: S, b: S) => boolean = Object.is,
): S {
  const memo = useRef<{ v: V | undefined; s: S; has: boolean }>({
    v: undefined,
    s: undefined as S,
    has: false,
  });
  const subscribe = useCallback(
    (fn: Listener) => (key === null ? () => {} : store.subscribeKey(key, fn)),
    [store, key],
  );
  const get = () => {
    const v = key === null ? undefined : store.get(key);
    const m = memo.current;
    if (m.has && Object.is(m.v, v)) return m.s;
    const s = select(v);
    if (m.has && eq(m.s, s)) {
      m.v = v;
      return m.s;
    }
    memo.current = { v, s, has: true };
    return s;
  };
  return useSyncExternalStore(subscribe, get, get);
}

/** A value derived from the whole keyed store (lists, counts); re-renders only when eq says it changed. */
export function useAll<K, V, S>(
  store: KeyedStore<K, V>,
  select: (store: KeyedStore<K, V>) => S,
  eq: (a: S, b: S) => boolean = Object.is,
): S {
  const memo = useRef<{ ver: number; s: S; has: boolean }>({ ver: -1, s: undefined as S, has: false });
  const subscribe = useCallback((fn: Listener) => store.subscribeAll(fn), [store]);
  const get = () => {
    const ver = store.version();
    const m = memo.current;
    if (m.has && m.ver === ver) return m.s;
    const s = select(store);
    if (m.has && eq(m.s, s)) {
      m.ver = ver;
      return m.s;
    }
    memo.current = { ver, s, has: true };
    return s;
  };
  return useSyncExternalStore(subscribe, get, get);
}

/** Equal when both arrays hold the same items in the same order. */
export const sameItems = <T>(a: readonly T[], b: readonly T[]): boolean =>
  a === b || (a.length === b.length && a.every((x, i) => Object.is(x, b[i])));
