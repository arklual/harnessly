import { arr, bool, lit, nullable, num, obj, opt, str, time, day, unknownValue } from './schema';

const catalogKind = lit(
  'skill',
  'mcp',
  'repo',
  'doc',
  'script',
  'access',
  'plugin',
  'claude-settings',
  'app',
);
const layerKind = lit('org', 'tenant', 'team');
const lockedReason = nullable(lit('pinned_by_org', 'pinned_by_tenant', 'no_rights', 'built_in'));
const kindCounts = obj({
  skill: num,
  mcp: num,
  repo: num,
  doc: num,
  script: opt(num),
  access: opt(num),
  plugin: opt(num),
  'claude-settings': opt(num),
  app: opt(num),
});
const audience = obj({ mode: lit('tenant', 'teams', 'off'), teams: arr(str) });

export const envSummary = obj({
  total: num,
  byKind: kindCounts,
  callsWeek: nullable(num),
  top: arr(obj({ id: str, name: str })),
  unused: nullable(num),
  pinnedByOrg: num,
});
export const envEntryRow = obj({
  entryId: str,
  itemId: str,
  kind: catalogKind,
  name: str,
  hint: str,
  audience,
  pinned: bool,
  locked: bool,
  lockedReason,
  enabled: bool,
  callsWeek: nullable(num),
  spark14: arr(num),
  lastUsedAt: nullable(time),
  unused: nullable(bool),
  delivery: nullable(obj({ ok: num, pending: num, error: num, message: nullable(str) })),
  groupId: str,
});
export const envGroup = obj({
  id: str,
  name: str,
  hint: str,
  required: bool,
  defaultOn: bool,
  position: num,
  builtIn: bool,
});
export const envPreset = obj({
  id: str,
  name: str,
  hint: str,
  groupIds: arr(str),
  position: num,
});
export const envLayer = obj({
  layer: layerKind,
  scopeId: str,
  tenantId: str,
  title: str,
  subtitle: str,
  locked: bool,
  callsWeek: nullable(num),
  counts: kindCounts,
  items: arr(envEntryRow),
  groups: arr(envGroup),
  presets: arr(envPreset),
});
export const teamRef = obj({ id: str, name: str, tenantId: str });
export const observedTool = obj({
  kind: lit('skill', 'mcp'),
  name: str,
  itemId: nullable(str),
  inEnv: bool,
  calls: num,
  callsWeek: num,
  errors: num,
  people: num,
  lastUsedAt: nullable(str),
});
export const envResponse = obj({
  summary: envSummary,
  layers: arr(envLayer),
  teams: arr(teamRef),
  observed: arr(observedTool),
  delivery: nullable(obj({ machines: num, reported: num, current: num, lastAt: nullable(time) })),
});
export const envCard = obj({
  entryId: str,
  itemId: str,
  kind: catalogKind,
  name: str,
  hint: str,
  layer: layerKind,
  scopeId: str,
  enabled: bool,
  callsWeek: nullable(num),
  people: nullable(num),
  perDay: nullable(num),
  bars14: arr(obj({ day, calls: num })),
  lastUsedAt: nullable(time),
  unused: nullable(bool),
  audience,
  pinned: bool,
  locked: bool,
  lockedReason,
});

const source = obj({
  body: opt(str),
  ref: opt(str),
  transport: opt(lit('stdio', 'http', 'sse')),
  command: opt(str),
  args: opt(arr(str)),
  url: opt(str),
  secretRefs: opt(arr(str)),
  branch: opt(str),
  text: opt(str),
  dir: opt(str),
  service: opt(lit('gitlab', 'github')),
  host: opt(str),
  secret: opt(str),
  from: opt(lit('text', 'repo', 'file', 'url')),
  fileName: opt(str),
  repo: opt(str),
  repoUrl: opt(str),
  path: opt(str),
  run: opt(str),
  rerun: opt(lit('once', 'check')),
  check: opt(str),
  secrets: opt(arr(str)),
  marketplace: opt(str),
  plugin: opt(str),
  always: opt(unknownValue),
  ifAbsent: opt(unknownValue),
  claudeMd: opt(str),
});
const catalogOwner = obj({ layer: layerKind, scopeId: str });
export const catalogItem = obj({
  id: str,
  kind: catalogKind,
  name: str,
  hint: str,
  source,
  builtIn: bool,
  archived: bool,
  added: bool,
  createdAt: time,
  owner: nullable(catalogOwner),
});
export const catalogResponse = obj({ items: arr(catalogItem) });
export const catalogItemResponse = obj({ item: catalogItem });

export const meEnvItem = obj({
  itemId: str,
  entryId: str,
  kind: catalogKind,
  name: str,
  hint: str,
  layer: layerKind,
  scopeId: str,
  layerTitle: str,
  pinned: bool,
  off: bool,
  canToggle: bool,
  groupId: str,
});
export const meEnvPreset = obj({
  id: str,
  name: str,
  hint: str,
  layer: layerKind,
  groupIds: arr(str),
  active: bool,
});
export const meEnvGroup = obj({
  id: str,
  name: str,
  hint: str,
  layer: layerKind,
  required: bool,
  on: bool,
  canToggle: bool,
  items: num,
  builtIn: bool,
});

const runStatus = lit('running', 'ok', 'error', 'pending');
export const envRunStep = obj({
  itemId: str,
  kind: catalogKind,
  name: str,
  status: lit('pending', 'running', 'ok', 'skipped', 'error'),
  message: nullable(str),
  exit: nullable(num),
  ms: num,
  log: opt(str),
});
export const envRunRow = obj({
  id: str,
  machine: obj({ id: str, name: str }),
  person: obj({ id: str, name: str }),
  mode: lit('apply', 'update', 'check', 'sandbox'),
  by: nullable(str),
  status: runStatus,
  startedAt: time,
  finishedAt: nullable(time),
  steps: arr(envRunStep),
});
export const envRunsResponse = obj({ runs: arr(envRunRow) });
export const envRunResponse = obj({ run: envRunRow });
export const envTryResponse = obj({ runId: str, machine: obj({ id: str, name: str }) });
export const envOrderResponse = obj({ entryIds: arr(str) });
export const meMachine = obj({
  id: str,
  name: str,
  online: bool,
  team: obj({ id: str, name: str }),
  setup: bool,
  current: bool,
  failed: num,
  lastRun: nullable(envRunRow),
});
export const meMachinesResponse = obj({ machines: arr(meMachine) });
export const meRunResponse = obj({ runs: arr(obj({ runId: str, machineId: str })) });
export const meRunsResponse = obj({ runs: arr(envRunRow) });
export const meEnvResponse = obj({
  space: obj({ id: str, slug: str, name: str }),
  org: obj({ id: str, name: str }),
  items: arr(meEnvItem),
  groups: arr(meEnvGroup),
  presets: arr(meEnvPreset),
  outdated: bool,
  machines: arr(meMachine),
});
