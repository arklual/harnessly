// The person's connections (internal/connections, the runtime's <platform>/connections routes) as the page reads and
// changes them: a pasted link is detected, then connected with the credentials its sign-in method asks for. A
// credential goes one way only: the page sends it with the connect and never gets one back.
import { getPage, sendBody, sendJson, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** Detection probes the instance and connecting signs in to it: a slow self-hosted server gets more time. */
const PROBE_TIMEOUT_MS = 30_000;

/** The reasons the runtime refuses with ({error}); anything else is shown by its code. */
export const CONN_REASONS = [
  'denied',
  'unreachable',
  'unknown-product',
  'unsupported',
  'not-found',
  'read-only',
  'bad-request',
] as const;

export const DEPLOYMENT = { cloud: 'cloud', selfHosted: 'self-hosted' } as const;
export type TDeployment = (typeof DEPLOYMENT)[keyof typeof DEPLOYMENT];

/** The state of a connection's last check. */
export const CONN_STATE = { ok: 'ok', denied: 'denied', unreachable: 'unreachable' } as const;

export interface IField {
  id: string;
  secret: boolean;
  optional: boolean;
  placeholder: string;
}

export interface IMethod {
  id: string;
  fields: readonly IField[];
  tokenUrl: string;
  recommended: boolean;
}

export interface IDetection {
  kind: string;
  baseUrl: string;
  deployment: TDeployment;
  version: string;
  title: string;
  methods: readonly IMethod[];
  capabilities: readonly string[];
  /** The detection as the runtime sent it: the connect sends it back unchanged. */
  wire: Record<string, unknown>;
}

export interface IPerson {
  name: string;
}

export interface IConn {
  id: string;
  kind: string;
  name: string;
  baseUrl: string;
  deployment: TDeployment;
  version: string;
  login: string;
  capabilities: readonly string[];
  me: IPerson | null;
  /** «ok», «denied», «unreachable». */
  state: string;
  primary: boolean;
  /** Set by the machine's deployment: listed, not changed from here. */
  env: boolean;
}

export interface IConnRefusal {
  refused: string;
}

export type TConnAnswer<T> = T | IConnRefusal;
export type TConnOutcome<T> = PageOutcome<TConnAnswer<T>>;

export const isConnRefusal = <T>(v: TConnAnswer<T>): v is IConnRefusal =>
  typeof v === 'object' && v !== null && 'refused' in v;

export const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`connections: ${what}`);
  return v as Record<string, unknown>;
};
export const str = (v: unknown, what: string): string => {
  if (typeof v !== 'string') throw new Error(`connections: ${what}`);
  return v;
};
/** An omitempty string of the runtime: absent is empty. */
export const optStr = (v: unknown, what: string): string =>
  v === undefined || v === null ? '' : str(v, what);
export const list = <T>(v: unknown, what: string, read: (x: unknown) => T): T[] => {
  if (v === undefined || v === null) return [];
  if (!Array.isArray(v)) throw new Error(`connections: ${what}`);
  return v.map(read);
};
const deployment = (v: unknown): TDeployment => {
  if (v === DEPLOYMENT.cloud || v === DEPLOYMENT.selfHosted) return v;
  throw new Error('connections: deployment');
};

const readField = (v: unknown): IField => {
  const o = obj(v, 'field');
  return {
    id: str(o.id, 'field.id'),
    secret: o.secret === true,
    optional: o.optional === true,
    placeholder: optStr(o.placeholder, 'field.placeholder'),
  };
};

const readMethod = (v: unknown): IMethod => {
  const o = obj(v, 'method');
  return {
    id: str(o.id, 'method.id'),
    fields: list(o.fields, 'method.fields', readField),
    tokenUrl: optStr(o.tokenUrl, 'method.tokenUrl'),
    recommended: o.recommended === true,
  };
};

export const readDetection = (v: unknown): IDetection => {
  const o = obj(v, 'detection');
  return {
    kind: str(o.kind, 'kind'),
    baseUrl: str(o.baseUrl, 'baseUrl'),
    deployment: deployment(o.deployment),
    version: optStr(o.version, 'version'),
    title: optStr(o.title, 'title'),
    methods: list(o.methods, 'methods', readMethod),
    capabilities: list(o.capabilities, 'capabilities', (x) => str(x, 'capability')),
    wire: o,
  };
};

export const readConn = (v: unknown): IConn => {
  const o = obj(v, 'connection');
  const me = o.me === undefined || o.me === null ? null : obj(o.me, 'me');
  return {
    id: str(o.id, 'id'),
    kind: str(o.kind, 'kind'),
    name: str(o.name, 'name'),
    baseUrl: str(o.baseUrl, 'baseUrl'),
    deployment: deployment(o.deployment),
    version: optStr(o.version, 'version'),
    login: optStr(o.login, 'login'),
    capabilities: list(o.capabilities, 'capabilities', (x) => str(x, 'capability')),
    me: me === null ? null : { name: optStr(me.name, 'me.name') },
    state: str(o.state, 'state'),
    primary: o.primary === true,
    env: o.env === true,
  };
};

/** An answer of the runtime: what `read` makes of it, or its refusal ({error} with a 4xx or 5xx). */
export const orRefusal =
  <T>(read: (o: Record<string, unknown>) => T) =>
  (v: unknown): TConnAnswer<T> => {
    const o = obj(v, 'answer');
    if (typeof o.error === 'string') return { refused: o.error };
    return read(o);
  };

const readList = orRefusal((o) => list(o.connections, 'connections', readConn));
const readDetected = orRefusal((o) => readDetection(o.detection));
const readOne = orRefusal((o) => readConn(o.connection));
/** DELETE answers 204: nothing to read but a refusal. */
const readRemoved = (v: unknown): TConnAnswer<true> =>
  v === null ? true : orRefusal(() => true as const)(v);

const path = (machine: MachineId, rest: `/${string}`) => runtimePath(platformPath(rest), machine);
const one = (id: string, rest = ''): `/${string}` => `/connections/${encodeURIComponent(id)}${rest}`;

export const loadConnections = (machine: MachineId): Promise<TConnOutcome<IConn[]>> =>
  getPage(path(machine, '/connections'), readList, TIMEOUT_MS);

/** What a pasted link is: the product, where it runs, and how to sign in to it. */
export const detectLink = (machine: MachineId, url: string): Promise<TConnOutcome<IDetection>> =>
  sendBody('POST', path(machine, '/connections/detect'), { url }, readDetected, PROBE_TIMEOUT_MS);

export interface IConnectInput {
  detection: IDetection;
  method: string;
  credentials: Readonly<Record<string, string>>;
  name: string;
}

/** Signs in with the credentials and keeps the connection. */
export const connectLink = (machine: MachineId, input: IConnectInput): Promise<TConnOutcome<IConn>> =>
  sendBody(
    'POST',
    path(machine, '/connections'),
    {
      detection: input.detection.wire,
      method: input.method,
      credentials: input.credentials,
      ...(input.name === '' ? {} : { name: input.name }),
    },
    readOne,
    PROBE_TIMEOUT_MS,
  );

export const checkConnection = (machine: MachineId, id: string): Promise<TConnOutcome<IConn>> =>
  sendPage('POST', path(machine, one(id, '/check')), readOne, PROBE_TIMEOUT_MS);

export const renameConnection = (
  machine: MachineId,
  id: string,
  name: string,
): Promise<TConnOutcome<IConn>> => sendJson(path(machine, one(id)), { name }, readOne, TIMEOUT_MS);

export const makePrimary = (machine: MachineId, id: string): Promise<TConnOutcome<IConn>> =>
  sendJson(path(machine, one(id)), { primary: true }, readOne, TIMEOUT_MS);

export const removeConnection = (machine: MachineId, id: string): Promise<TConnOutcome<true>> =>
  sendPage('DELETE', path(machine, one(id)), readRemoved, TIMEOUT_MS);
