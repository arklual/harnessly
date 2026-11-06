// Environment and catalog, as docs/hub/environment.md writes them: what a machine gets installed, layer by layer
// (organization, tenant, team), the catalog the entries are picked from, and a person's own switches. Times are RFC 3339
// UTC strings, days are YYYY-MM-DD (UTC). A number the hub has no facts for is null, never 0.

export type CatalogKind = 'skill' | 'mcp' | 'repo' | 'doc' | 'script' | 'access';
export type EnvLayerKind = 'org' | 'tenant' | 'team';
export type AudienceMode = 'tenant' | 'teams' | 'off';

// Why a row cannot be changed by the person looking at it; null: it can.
export type LockedReason = 'pinned_by_org' | 'pinned_by_tenant' | 'no_rights';

// The source of a catalog item. Only the keys of its kind are accepted (skill: body, ref; mcp: transport, command, args, url,
// secretRefs; repo: url, branch, dir; doc: url, text; access: service, host, secret; script: from, body, fileName, repo,
// repoUrl, branch, path, url, run, rerun, check, secrets); secrets are named, never carried.
export type ScriptFrom = 'text' | 'repo' | 'file' | 'url';
export type CatalogSource = {
  body?: string;
  ref?: string;
  transport?: 'stdio' | 'http' | 'sse';
  command?: string;
  args?: string[];
  url?: string;
  secretRefs?: string[];
  branch?: string;
  text?: string;
  // repo: the folder on the machine (`~/…` or relative to the home); empty: `~/code/<name of the repository>`
  dir?: string;
  // access
  service?: 'gitlab' | 'github';
  host?: string;
  secret?: string;
  // script
  from?: ScriptFrom;
  fileName?: string;
  repo?: string; // the item id of a `repo` of the list
  repoUrl?: string;
  path?: string;
  run?: string;
  rerun?: 'once' | 'check';
  check?: string;
  secrets?: string[];
};

// `script` and `access` are counted by a hub that has the machine setup; an older one omits them.
export type KindCounts = {
  skill: number;
  mcp: number;
  repo: number;
  doc: number;
  script?: number;
  access?: number;
};

export type Audience = {
  mode: AudienceMode; // 'tenant': everyone the layer reaches; 'teams': only the listed teams; 'off': nobody
  teams: string[]; // team ids, only for 'teams'
};

export type EnvSummary = {
  total: number;
  byKind: KindCounts;
  callsWeek: number | null; // null: the organization has no tool facts yet
  top: { id: string; name: string }[]; // catalog items, the most called first
  unused: number | null;
  pinnedByOrg: number;
};

export type EnvEntryRow = {
  entryId: string;
  itemId: string;
  kind: CatalogKind;
  name: string;
  hint: string;
  audience: Audience;
  pinned: boolean; // a lower layer cannot switch it off
  locked: boolean; // the viewer cannot change this row
  lockedReason: LockedReason | null;
  enabled: boolean;
  callsWeek: number | null;
  spark14: number[]; // 14 UTC days, the last one is today
  lastUsedAt: string | null;
  unused: boolean | null; // no calls for 30 days and the entry is older than that; null: unknown
  // What the machines holding the current list said about the item; null: none of them reported it.
  delivery: ItemDelivery | null;
};

// How many machines put an item in, keep it without being able to load it yet, or failed it; `message` is the first
// reason a machine gave (a missing secret, an unsupported source).
export type ItemDelivery = { ok: number; pending: number; error: number; message: string | null };

// How far the screen's environment got: the working machines of the screen, those that reported what they hold, and
// those that hold the current list.
export type EnvDelivery = { machines: number; reported: number; current: number; lastAt: string | null };

export type EnvLayer = {
  layer: EnvLayerKind;
  scopeId: string; // the organization, tenant or team id
  tenantId: string; // '' for the organization layer
  title: string;
  subtitle: string;
  locked: boolean; // the viewer cannot edit this layer
  callsWeek: number | null;
  counts: KindCounts;
  items: EnvEntryRow[];
};

export type TeamRef = { id: string; name: string; tenantId: string };

// GET /api/orgs/{org}/env?scope=<tenant slug|all>&team=<id>
// A skill or an MCP server the machines of the screen called in the last 30 days, in the environment or not. `name` is
// as the chats name it (the skill; the MCP server), `itemId` the catalog item it is (null: the catalog has none yet),
// `inEnv` whether an enabled entry of the screen gives the item.
export type ObservedTool = {
  kind: 'skill' | 'mcp';
  name: string;
  itemId: string | null;
  inEnv: boolean;
  calls: number; // 30 days
  callsWeek: number;
  errors: number;
  people: number;
  lastUsedAt: string | null;
};
export type EnvResponse = {
  summary: EnvSummary;
  layers: EnvLayer[];
  teams: TeamRef[];
  observed: ObservedTool[];
  delivery: EnvDelivery | null;
};

export type EnvCard = {
  entryId: string;
  itemId: string;
  kind: CatalogKind;
  name: string;
  hint: string;
  layer: EnvLayerKind;
  scopeId: string;
  enabled: boolean;
  callsWeek: number | null;
  people: number | null; // different people with a call in the week
  perDay: number | null;
  bars14: { day: string; calls: number }[];
  lastUsedAt: string | null;
  unused: boolean | null;
  audience: Audience;
  pinned: boolean;
  locked: boolean;
  lockedReason: LockedReason | null;
};

// POST /api/orgs/{org}/env/entries → 201 EnvEntryRow
export type AddEntryRequest = {
  layer: EnvLayerKind;
  scopeId: string;
  itemId: string;
  audience?: { mode: AudienceMode; teams?: string[] };
  enabled?: boolean;
  pinned?: boolean;
};
// PATCH /api/orgs/{org}/env/entries/{entryId} → 200 EnvEntryRow
export type PatchEntryRequest = {
  enabled?: boolean;
  audience?: { mode: AudienceMode; teams?: string[] };
  pinned?: boolean;
};

export type CatalogItem = {
  id: string;
  kind: CatalogKind;
  name: string;
  hint: string;
  source: CatalogSource;
  builtIn: boolean; // shipped with the hub, not editable
  archived: boolean;
  added: boolean; // the layer named by ?layer=&scopeId= already has it
  createdAt: string;
  // The layer that owns the item; null: the organization. Its managers (and the organization's admins) change it.
  owner: CatalogOwner | null;
};
export type CatalogOwner = { layer: EnvLayerKind; scopeId: string };
// GET /api/orgs/{org}/catalog?kind=&layer=&scopeId=
export type CatalogResponse = { items: CatalogItem[] };
// POST /api/orgs/{org}/catalog → 201 CatalogItemResponse; PATCH …/catalog/{itemId} → 200; DELETE archives → 204
export type AddCatalogRequest = {
  kind: CatalogKind;
  name: string;
  hint?: string;
  source: CatalogSource;
  owner?: CatalogOwner; // default the organization
};
export type PatchCatalogRequest = {
  name?: string;
  hint?: string;
  source?: CatalogSource;
  archived?: boolean;
};
export type CatalogItemResponse = { item: CatalogItem };

export type MeEnvItem = {
  itemId: string;
  entryId: string;
  kind: CatalogKind;
  name: string;
  hint: string;
  layer: EnvLayerKind; // the layer that gives the person the item
  scopeId: string;
  layerTitle: string;
  pinned: boolean;
  off: boolean; // the person's own switch
  canToggle: boolean; // false for a pinned item
};
// GET /api/me/env?space=
export type MeEnvResponse = {
  space: { id: string; slug: string; name: string };
  org: { id: string; name: string };
  items: MeEnvItem[];
  // Some machine of theirs in the tenant is not current or has a failed item.
  outdated: boolean;
  machines: MeMachine[];
};
// PUT /api/me/env/{itemId}?space= → 200 MeEnvItem
export type MeEnvToggleRequest = { off: boolean; space?: string };

// ── machine setup (docs/hub/environment.md, «Machine setup») ──────────────────────────────────────────────────────

// PUT /api/orgs/{org}/env/order → 200 { entryIds }: the scripts of a layer in their new order (all of them, each once).
export type EnvOrderRequest = { layer: EnvLayerKind; scopeId: string; entryIds: string[] };
export type EnvOrderResponse = { entryIds: string[] };

export type RunMode = 'apply' | 'update' | 'check' | 'sandbox';
export type RunStatus = 'running' | 'ok' | 'error' | 'pending';
// A step is `pending` (not started, or waits for the person), `running`, `ok`, `skipped` or `error`.
export type StepStatus = 'pending' | 'running' | 'ok' | 'skipped' | 'error';
export type EnvRunStep = {
  itemId: string;
  kind: CatalogKind;
  name: string;
  status: StepStatus;
  message: string | null;
  exit: number | null;
  ms: number;
  log?: string; // only in GET …/runs/{runId}; secrets are replaced by ••••••
};
export type EnvRunRow = {
  id: string;
  machine: { id: string; name: string };
  person: { id: string; name: string };
  mode: RunMode;
  by: string | null;
  status: RunStatus;
  startedAt: string;
  finishedAt: string | null;
  steps: EnvRunStep[];
};
// GET /api/orgs/{org}/env/runs?layer=&scopeId=&errors=1&limit=
export type EnvRunsResponse = { runs: EnvRunRow[] };
// GET /api/orgs/{org}/env/runs/{runId}
export type EnvRunResponse = { run: EnvRunRow };

// POST /api/orgs/{org}/env/try → 202: a sandbox run of the layer's list with the draft added (or put in place of `entryId`)
export type EnvTryRequest = {
  layer: EnvLayerKind;
  scopeId: string;
  draft?: { entryId?: string; kind: CatalogKind; name: string; source: CatalogSource };
  machineId?: string;
};
export type EnvTryResponse = { runId: string; machine: { id: string; name: string } };

export type MeMachine = {
  id: string;
  name: string;
  online: boolean;
  team: { id: string; name: string };
  setup: boolean; // has the feature environment.setup; false: not «устарело», the machine cannot be set up
  current: boolean; // holds the list the hub computes for it now
  failed: number; // setup items in error or waiting for the person
  lastRun: EnvRunRow | null;
};
// GET /api/me/env/machines?space=
export type MeMachinesResponse = { machines: MeMachine[] };
// POST /api/me/env/run?space= → 202
export type MeRunRequest = { mode: 'update' | 'check'; machineId?: string };
export type MeRunResponse = { runs: { runId: string; machineId: string }[] };
// GET /api/me/env/runs?space=
export type MeRunsResponse = { runs: EnvRunRow[] };
