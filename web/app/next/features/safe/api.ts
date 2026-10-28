// The safe of a machine (internal/secrets, cmd/runtime/routes_secrets.go) as the page reads and changes it. A value
// goes one way only: the page sends it with a PUT and never gets one back — a secret here is its name and dates.
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

/** The reasons the runtime refuses with; anything else is shown by its code. */
export const SAFE_REASONS = ['bad-name', 'empty', 'too-large', 'not-found', 'key-lost', 'failed'] as const;

/** What a secret's name may be: as the runtime checks it (secrets.ValidName). */
export const SECRET_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

export interface ISecretUse {
  conv: string;
  title: string;
  at: number;
}

export interface ISecret {
  name: string;
  createdAt: number;
  updatedAt: number;
  /** 0: never used. */
  lastUsedAt: number;
  /** The latest conversations that used it, the newest first. */
  usedBy: readonly ISecretUse[];
}

/** An agent's request to put a secret in the safe. */
export interface ISecretAsk {
  id: string;
  name: string;
  why: string;
  conv: string;
  title: string;
  replace: boolean;
  at: number;
}

export interface ISafeRefusal {
  refused: string;
}

export type TSafeAnswer<T> = T | ISafeRefusal;
export type TSafeOutcome<T> = PageOutcome<TSafeAnswer<T>>;

export const isSafeRefusal = <T>(v: TSafeAnswer<T>): v is ISafeRefusal =>
  typeof v === 'object' && v !== null && 'refused' in v;

const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`safe: ${what}`);
  return v as Record<string, unknown>;
};
const str = (v: unknown, what: string): string => {
  if (typeof v !== 'string') throw new Error(`safe: ${what}`);
  return v;
};
const num = (v: unknown, what: string): number => {
  if (typeof v !== 'number') throw new Error(`safe: ${what}`);
  return v;
};
const list = <T>(v: unknown, what: string, read: (x: unknown) => T): T[] => {
  if (!Array.isArray(v)) throw new Error(`safe: ${what}`);
  return v.map(read);
};

const readUse = (v: unknown): ISecretUse => {
  const o = obj(v, 'use');
  return { conv: str(o.conv, 'conv'), title: str(o.title, 'title'), at: num(o.at, 'at') };
};

export const readSecret = (v: unknown): ISecret => {
  const o = obj(v, 'secret');
  return {
    name: str(o.name, 'name'),
    createdAt: num(o.createdAt, 'createdAt'),
    updatedAt: num(o.updatedAt, 'updatedAt'),
    lastUsedAt: num(o.lastUsedAt, 'lastUsedAt'),
    usedBy: o.usedBy === null || o.usedBy === undefined ? [] : list(o.usedBy, 'usedBy', readUse),
  };
};

export const readAsk = (v: unknown): ISecretAsk => {
  const o = obj(v, 'ask');
  return {
    id: str(o.id, 'id'),
    name: str(o.name, 'name'),
    why: str(o.why, 'why'),
    conv: str(o.conv, 'conv'),
    title: str(o.title, 'title'),
    replace: o.replace === true,
    at: num(o.at, 'at'),
  };
};

/** An answer of the runtime: what `read` makes of it, or its refusal ({error} with a 4xx or 5xx). */
const orRefusal =
  <T>(read: (o: Record<string, unknown>) => T) =>
  (v: unknown): TSafeAnswer<T> => {
    const o = obj(v, 'answer');
    if (typeof o.error === 'string') return { refused: o.error };
    return read(o);
  };

export interface ISafeList {
  secrets: ISecret[];
  asks: ISecretAsk[];
}

const readList = orRefusal((o): ISafeList => ({
  secrets: list(o.secrets, 'secrets', readSecret),
  asks: list(o.asks, 'asks', readAsk),
}));
const readAsks = orRefusal((o) => list(o.asks, 'asks', readAsk));
const readOne = orRefusal((o) => readSecret(o.secret));
const readRemoved = orRefusal((o) => str(o.removed, 'removed'));
const readDeclined = orRefusal((o) => str(o.declined, 'declined'));

const path = (machine: MachineId, rest: `/${string}`) => runtimePath(platformPath(rest), machine);
const one = (name: string): `/${string}` => `/secrets/${encodeURIComponent(name)}`;

export const loadSafe = (machine: MachineId): Promise<TSafeOutcome<ISafeList>> =>
  getPage(path(machine, '/secrets'), readList, TIMEOUT_MS);

/** Puts a secret: a new one, or a new value of one (rotation). Answers any ask waiting for that name. */
export const putSecret = (machine: MachineId, name: string, value: string): Promise<TSafeOutcome<ISecret>> =>
  sendBody('PUT', path(machine, one(name)), { value }, readOne, TIMEOUT_MS);

export const removeSecret = (machine: MachineId, name: string): Promise<TSafeOutcome<string>> =>
  sendPage('DELETE', path(machine, one(name)), readRemoved, TIMEOUT_MS);

export const loadAsks = (machine: MachineId): Promise<TSafeOutcome<ISecretAsk[]>> =>
  getPage(path(machine, '/secret-asks'), readAsks, TIMEOUT_MS);

export const declineAsk = (machine: MachineId, id: string): Promise<TSafeOutcome<string>> =>
  sendPage('POST', path(machine, `/secret-asks/${encodeURIComponent(id)}/decline`), readDeclined, TIMEOUT_MS);
