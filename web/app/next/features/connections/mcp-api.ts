// The person's MCP servers (internal/mcpconn, the runtime's <platform>/mcp routes) as the page reads and changes them:
// a catalog entry or a pasted address is probed, kept, and signed in to — in a popup of the service's own sign-in, with a
// typed header, or not at all. A typed credential goes one way only: the page sends it and never gets one back.
import {
  list,
  obj,
  optStr,
  orRefusal,
  str,
  type TConnAnswer,
  type TConnOutcome,
} from '@/next/features/connections/api';
import { getPage, sendBody, sendJson, sendPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** A probe, a connect and a check reach the server itself (and its sign-in discovery): they get more time. */
const PROBE_TIMEOUT_MS = 30_000;

/** The reasons the runtime refuses with ({error}); anything else is shown by its code. */
export const MCP_REASONS = [
  'denied',
  'unreachable',
  'not-found',
  'bad-request',
  'signin-required',
  'oauth-failed',
] as const;

export const MCP_AUTH = { oauth: 'oauth', header: 'header', none: 'none' } as const;
export type TMcpAuth = (typeof MCP_AUTH)[keyof typeof MCP_AUTH];

export const MCP_TRANSPORT = { http: 'http', sse: 'sse' } as const;
export type TMcpTransport = (typeof MCP_TRANSPORT)[keyof typeof MCP_TRANSPORT];

/** How the server answered last. */
export const MCP_STATE = {
  ok: 'ok',
  signin: 'signin',
  denied: 'denied',
  unreachable: 'unreachable',
} as const;

/** The header a typed credential goes in when nothing says otherwise. */
export const DEFAULT_HEADER = 'Authorization';

export interface IMcpTool {
  name: string;
  description: string;
}

export interface IMcpServer {
  id: string;
  name: string;
  slug: string;
  url: string;
  transport: TMcpTransport;
  auth: TMcpAuth;
  headerName: string;
  catalogId: string;
  enabled: boolean;
  /** «ok», «signin», «denied», «unreachable». */
  state: string;
  /** Who or what it signs in as; empty when the server does not say. */
  me: string;
  tools: readonly IMcpTool[];
  /** Signs in with OAuth but lets no new app register: it takes a typed token here. */
  oauthUnavailable: boolean;
}

export interface IMcpCatalogEntry {
  id: string;
  name: string;
  url: string;
  transport: TMcpTransport;
  /** The sign-in the entry expects; connecting probes anyway, so a wrong hint corrects itself. */
  auth: TMcpAuth;
  headerName: string;
  tokenUrl: string;
  docsUrl: string;
  blurb: string;
  blurbRu: string;
}

export interface IMcpProbe {
  url: string;
  transport: TMcpTransport;
  auth: TMcpAuth;
  name: string;
  /** Signs in with OAuth but lets no new app register: it takes a typed token here. */
  oauthUnavailable: boolean;
  headerName: string;
}

export interface IMcpList {
  servers: readonly IMcpServer[];
  catalog: readonly IMcpCatalogEntry[];
}

const auth = (v: unknown): TMcpAuth => {
  if (v === MCP_AUTH.oauth || v === MCP_AUTH.header || v === MCP_AUTH.none) return v;
  throw new Error('connections: mcp auth');
};
/** An omitted transport is streamable HTTP. */
const transport = (v: unknown): TMcpTransport => {
  if (v === undefined || v === null || v === MCP_TRANSPORT.http) return MCP_TRANSPORT.http;
  if (v === MCP_TRANSPORT.sse) return v;
  throw new Error('connections: mcp transport');
};

const readTool = (v: unknown): IMcpTool => {
  const o = obj(v, 'mcp tool');
  return { name: str(o.name, 'tool.name'), description: optStr(o.description, 'tool.description') };
};

export const readMcpServer = (v: unknown): IMcpServer => {
  const o = obj(v, 'mcp server');
  return {
    id: str(o.id, 'id'),
    name: str(o.name, 'name'),
    slug: str(o.slug, 'slug'),
    url: str(o.url, 'url'),
    transport: transport(o.transport),
    auth: auth(o.auth),
    headerName: optStr(o.headerName, 'headerName'),
    catalogId: optStr(o.catalogId, 'catalogId'),
    enabled: o.enabled === true,
    state: str(o.state, 'state'),
    me: optStr(o.me, 'me'),
    tools: list(o.tools, 'tools', readTool),
    oauthUnavailable: o.oauthUnavailable === true,
  };
};

export const readMcpCatalogEntry = (v: unknown): IMcpCatalogEntry => {
  const o = obj(v, 'mcp catalog entry');
  return {
    id: str(o.id, 'id'),
    name: str(o.name, 'name'),
    url: str(o.url, 'url'),
    transport: transport(o.transport),
    auth: auth(o.auth),
    headerName: optStr(o.headerName, 'headerName'),
    tokenUrl: optStr(o.tokenUrl, 'tokenUrl'),
    docsUrl: optStr(o.docsUrl, 'docsUrl'),
    blurb: optStr(o.blurb, 'blurb'),
    blurbRu: optStr(o.blurbRu, 'blurbRu'),
  };
};

export const readMcpProbe = (v: unknown): IMcpProbe => {
  const o = obj(v, 'mcp probe');
  return {
    url: str(o.url, 'url'),
    transport: transport(o.transport),
    auth: auth(o.auth),
    name: optStr(o.name, 'name'),
    oauthUnavailable: o.oauthUnavailable === true,
    headerName: optStr(o.headerName, 'headerName'),
  };
};

/** What a kept server still needs from the person: a sign-in in its popup, or a token. */
export interface IMcpAuthStart {
  auth: TMcpAuth;
  headerName: string;
  oauthUnavailable: boolean;
  tokenUrl: string;
}

export interface IMcpAdded {
  server: IMcpServer;
  /** null when the server connected at once. */
  authStart: IMcpAuthStart | null;
}

export const readAuthStart = (v: unknown): IMcpAuthStart => {
  const o = obj(v, 'mcp authStart');
  return {
    auth: auth(o.auth),
    headerName: optStr(o.headerName, 'authStart.headerName'),
    oauthUnavailable: o.oauthUnavailable === true,
    tokenUrl: optStr(o.tokenUrl, 'authStart.tokenUrl'),
  };
};

const readList = orRefusal((o): IMcpList => ({
  servers: list(o.servers, 'servers', readMcpServer),
  catalog: list(o.catalog, 'catalog', readMcpCatalogEntry),
}));
const readProbed = orRefusal((o) => readMcpProbe(o.probe));
const readOne = orRefusal((o) => readMcpServer(o.server));
const readAdded = orRefusal((o): IMcpAdded => ({
  server: readMcpServer(o.server),
  authStart: o.authStart === undefined || o.authStart === null ? null : readAuthStart(o.authStart),
}));
const readSignIn = orRefusal((o) => str(o.authUrl, 'authUrl'));
/** DELETE answers 204: nothing to read but a refusal. */
const readRemoved = (v: unknown): TConnAnswer<true> =>
  v === null ? true : orRefusal(() => true as const)(v);

const path = (machine: MachineId, rest: `/${string}`) => runtimePath(platformPath(rest), machine);
const one = (id: string, rest = ''): `/${string}` => `/mcp/${encodeURIComponent(id)}${rest}`;

export const loadMcp = (machine: MachineId): Promise<TConnOutcome<IMcpList>> =>
  getPage(path(machine, '/mcp'), readList, TIMEOUT_MS);

/** What a pasted address is: its transport and how it signs in. */
export const probeMcp = (machine: MachineId, url: string): Promise<TConnOutcome<IMcpProbe>> =>
  sendBody('POST', path(machine, '/mcp/probe'), { url }, readProbed, PROBE_TIMEOUT_MS);

export interface IMcpHeader {
  name: string;
  value: string;
}

export interface IMcpAddInput {
  url: string;
  name: string;
  catalogId: string;
  transport: TMcpTransport | null;
  header: IMcpHeader | null;
}

/** Keeps the server; one that signs in with OAuth is kept in state «signin» until the person does. */
export const addMcp = (machine: MachineId, input: IMcpAddInput): Promise<TConnOutcome<IMcpAdded>> =>
  sendBody(
    'POST',
    path(machine, '/mcp'),
    {
      url: input.url,
      ...(input.name === '' ? {} : { name: input.name }),
      ...(input.catalogId === '' ? {} : { catalogId: input.catalogId }),
      ...(input.transport === null ? {} : { transport: input.transport }),
      ...(input.header === null ? {} : { header: input.header }),
    },
    readAdded,
    PROBE_TIMEOUT_MS,
  );

/**
 * Where the service's own sign-in is. The runtime builds its callback on the page's origin; the callback's page leads
 * back to `returnTo` (a path of this page) when it has no opener to tell.
 */
export const startMcpSignIn = (
  machine: MachineId,
  id: string,
  origin: string,
  returnTo: string,
): Promise<TConnOutcome<string>> =>
  sendBody('POST', path(machine, one(id, '/signin')), { origin, returnTo }, readSignIn, TIMEOUT_MS);

export const setMcpHeader = (
  machine: MachineId,
  id: string,
  header: IMcpHeader,
): Promise<TConnOutcome<IMcpServer>> =>
  sendBody('PUT', path(machine, one(id, '/header')), header, readOne, PROBE_TIMEOUT_MS);

/** Asks the server again (initialize and tools/list through its sign-in): its state and tools as they are now. */
export const checkMcp = (machine: MachineId, id: string): Promise<TConnOutcome<IMcpServer>> =>
  sendPage('POST', path(machine, one(id, '/check')), readOne, PROBE_TIMEOUT_MS);

export const renameMcp = (machine: MachineId, id: string, name: string): Promise<TConnOutcome<IMcpServer>> =>
  sendJson(path(machine, one(id)), { name }, readOne, TIMEOUT_MS);

export const enableMcp = (
  machine: MachineId,
  id: string,
  enabled: boolean,
): Promise<TConnOutcome<IMcpServer>> => sendJson(path(machine, one(id)), { enabled }, readOne, TIMEOUT_MS);

export const removeMcp = (machine: MachineId, id: string): Promise<TConnOutcome<true>> =>
  sendPage('DELETE', path(machine, one(id)), readRemoved, TIMEOUT_MS);
