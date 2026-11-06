// How the setup of the team's environment list stands on a machine (cmd/runtime/routes_environment.go), as the start
// page's «Настройка машины» card reads it, and the card's «Повторить».
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const SETUP_STATUS = { ok: 'ok', error: 'error', pending: 'pending', running: 'running' } as const;
export type TSetupStatus = (typeof SETUP_STATUS)[keyof typeof SETUP_STATUS];

export const SETUP_KIND = {
  base: 'base',
  access: 'access',
  repo: 'repo',
  claude: 'claude-settings',
  plugin: 'plugin',
  app: 'app',
  script: 'script',
} as const;
export type TSetupKind = (typeof SETUP_KIND)[keyof typeof SETUP_KIND];

export interface ISetupItem {
  itemId: string;
  kind: TSetupKind;
  name: string;
  status: TSetupStatus;
  message: string;
  /** The safe's name the item waits for; empty when it waits for nothing of the person's. */
  secret: string;
  /** An item of the base package: what the card offers for it. */
  base: IBaseExtra | null;
}

/** What only an item of the base package has (cmd/runtime/basepkg.go). */
export interface IBaseExtra {
  status: TBaseStatus;
  /** The address it could not reach (no_access). */
  host: string;
  /** The line a person runs to install it, on a machine without the base package's service. */
  command: string;
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
  /** Everything of the base package to install by hand, in one line ('' when nothing or a service does it). */
  baseCommand?: string;
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
    base: null,
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

// ── the base package (GET <platform>/basepkg) ──────────────────────────────────────────────────────────────────────

export const BASE_STATUS = {
  ok: 'ok',
  missing: 'missing',
  installing: 'installing',
  noAccess: 'no_access',
  waitingDisk: 'waiting_disk',
  deferred: 'deferred',
  skipped: 'skipped',
  error: 'error',
} as const;
export type TBaseStatus = (typeof BASE_STATUS)[keyof typeof BASE_STATUS];

export interface IBaseItem {
  id: string;
  title: string;
  without: string;
  status: TBaseStatus;
  message: string;
  host: string;
  command: string;
}

export interface IBaseView {
  /** A root service installs the system part here; without it the person runs the command. */
  service: boolean;
  running: boolean;
  done: boolean;
  proxy: string;
  /** Everything missing in one line, for a machine without the service. */
  command: string;
  items: readonly IBaseItem[];
}

const readBaseItem = (v: unknown): IBaseItem => {
  const o = obj(v, 'base item');
  return {
    id: str(o.id, 'id'),
    title: str(o.title, 'title'),
    without: optStr(o.without),
    status: oneOf(o.status, BASE_STATUS, 'base status'),
    message: optStr(o.message),
    host: optStr(o.host),
    command: optStr(o.command),
  };
};

export const readBaseView = (v: unknown): IBaseView => {
  const o = obj(v, 'base');
  if (!Array.isArray(o.items)) throw new Error('setup: base items');
  return {
    service: o.service === true,
    running: o.running === true,
    done: o.done === true,
    proxy: optStr(o.proxy),
    command: optStr(o.command),
    items: o.items.map(readBaseItem),
  };
};

const BASE_AS: Record<TBaseStatus, TSetupStatus> = {
  ok: SETUP_STATUS.ok,
  skipped: SETUP_STATUS.ok,
  installing: SETUP_STATUS.running,
  no_access: SETUP_STATUS.error,
  error: SETUP_STATUS.error,
  missing: SETUP_STATUS.pending,
  waiting_disk: SETUP_STATUS.pending,
  deferred: SETUP_STATUS.pending,
};

/**
 * One view of the machine for the card: the base package as the stage «Основа» before the hub's list. The card shows
 * the base package on every machine, with a hub list or without one.
 */
export const withBase = (setup: ISetupView | null, base: IBaseView | null): ISetupView | null => {
  if (!base) return setup;
  const own = setup && setup.allowed ? setup : null;
  const items: ISetupItem[] = base.items
    .filter((b) => b.status !== BASE_STATUS.skipped)
    .map((b) => ({
      itemId: `base:${b.id}`,
      kind: SETUP_KIND.base,
      name: b.title,
      status: BASE_AS[b.status],
      message: b.message || b.without,
      secret: '',
      base: { status: b.status, host: b.host, command: b.command },
    }));
  const left = base.items
    .filter((b) => b.status !== BASE_STATUS.ok && b.status !== BASE_STATUS.skipped)
    .map((b) => b.id);
  return {
    allowed: true,
    hash: `${own?.hash ?? ''}|${left.join(',')}`,
    items: [...items, ...(own?.items ?? [])],
    done: base.done && (own === null || own.items.length === 0 || own.done),
    run: own?.run ?? null,
    baseCommand: base.command,
  };
};

export const loadBase = (machine: MachineId): Promise<PageOutcome<IBaseView>> =>
  getPage(path(machine, '/basepkg'), readBaseView, TIMEOUT_MS);

/** «Повторить» for the base package. */
export const askBaseRun = (machine: MachineId): Promise<PageOutcome<IBaseView>> =>
  sendPage('POST', path(machine, '/basepkg/run'), readBaseView, TIMEOUT_MS);

/** «Пропустить»: the person does without the item (skip false takes it back). */
export const askBaseSkip = (machine: MachineId, id: string, skip: boolean): Promise<PageOutcome<IBaseView>> =>
  sendBody('POST', path(machine, '/basepkg/skip'), { id, skip }, readBaseView, TIMEOUT_MS);

/** «Указать прокси»: the address the package manager and the downloads go through ('' takes it away). */
export const askBaseProxy = (machine: MachineId, url: string): Promise<PageOutcome<IBaseView>> =>
  sendBody('POST', path(machine, '/basepkg/proxy'), { url }, readBaseView, TIMEOUT_MS);
