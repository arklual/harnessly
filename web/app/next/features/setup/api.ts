// How the setup of the team's environment list stands on a machine (cmd/runtime/routes_environment.go), as the start
// page's «Настройка машины» card reads it, and the card's «Повторить».
import { getPage, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const SETUP_STATUS = { ok: 'ok', error: 'error', pending: 'pending', running: 'running' } as const;
export type TSetupStatus = (typeof SETUP_STATUS)[keyof typeof SETUP_STATUS];

export const SETUP_KIND = { access: 'access', repo: 'repo', script: 'script' } as const;
export type TSetupKind = (typeof SETUP_KIND)[keyof typeof SETUP_KIND];

export interface ISetupItem {
  itemId: string;
  kind: TSetupKind;
  name: string;
  status: TSetupStatus;
  message: string;
  /** The safe's name the item waits for; empty when it waits for nothing of the person's. */
  secret: string;
}

export interface ISetupRun {
  runId: string;
  status: string;
  startedAt: number;
  finishedAt: number;
}

export interface ISetupView {
  allowed: boolean;
  hash: string;
  items: readonly ISetupItem[];
  done: boolean;
  run: ISetupRun | null;
}

const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`setup: ${what}`);
  return v as Record<string, unknown>;
};
const str = (v: unknown, what: string): string => {
  if (typeof v !== 'string') throw new Error(`setup: ${what}`);
  return v;
};
const optStr = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown, what: string): number => {
  if (typeof v !== 'number') throw new Error(`setup: ${what}`);
  return v;
};
const oneOf = <T extends string>(v: unknown, all: Record<string, T>, what: string): T => {
  const s = str(v, what);
  const hit = Object.values(all).find((x) => x === s);
  if (!hit) throw new Error(`setup: ${what} ${s}`);
  return hit;
};

const readItem = (v: unknown): ISetupItem => {
  const o = obj(v, 'item');
  return {
    itemId: str(o.itemId, 'itemId'),
    kind: oneOf(o.kind, SETUP_KIND, 'kind'),
    name: str(o.name, 'name'),
    status: oneOf(o.status, SETUP_STATUS, 'status'),
    message: optStr(o.message),
    secret: optStr(o.secret),
  };
};

const readRun = (v: unknown): ISetupRun | null => {
  if (v === undefined || v === null) return null;
  const o = obj(v, 'run');
  return {
    runId: str(o.runId, 'runId'),
    status: str(o.status, 'run.status'),
    startedAt: num(o.startedAt, 'startedAt'),
    finishedAt: num(o.finishedAt, 'finishedAt'),
  };
};

export const readSetupView = (v: unknown): ISetupView => {
  const o = obj(v, 'view');
  if (!Array.isArray(o.items)) throw new Error('setup: items');
  return {
    allowed: o.allowed === true,
    hash: optStr(o.hash),
    items: o.items.map(readItem),
    done: o.done === true,
    run: readRun(o.run),
  };
};

const readAsked = (v: unknown): string => str(obj(v, 'asked').runId, 'runId');

const path = (machine: MachineId, rest: `/${string}`) => runtimePath(platformPath(rest), machine);

export const loadSetup = (machine: MachineId): Promise<PageOutcome<ISetupView>> =>
  getPage(path(machine, '/environment'), readSetupView, TIMEOUT_MS);

export const askSetupRun = (machine: MachineId): Promise<PageOutcome<string>> =>
  sendPage('POST', path(machine, '/environment/run'), readAsked, TIMEOUT_MS);
