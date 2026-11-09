// Environment and catalog, as docs/hub/environment.md writes them: what a machine gets installed, layer by layer
// (organization, tenant, team), the catalog the entries are picked from, and a person's own switches. Times are RFC 3339
// UTC strings, days are YYYY-MM-DD (UTC). A number the hub has no facts for is null, never 0.

export type CatalogKind =
  'skill' | 'mcp' | 'repo' | 'doc' | 'script' | 'access' | 'plugin' | 'claude-settings' | 'app';
export type EnvLayerKind = 'org' | 'tenant' | 'team';
export type AudienceMode = 'tenant' | 'teams' | 'off';

// Why a row cannot be changed by the person looking at it; null: it can.
// built_in: an item of the platform's own group «Рекомендованное».
export type LockedReason = 'pinned_by_org' | 'pinned_by_tenant' | 'no_rights' | 'built_in';

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
  // plugin: `name@marketplace` and the marketplace's address
  marketplace?: string;
  plugin?: string;
  // claude-settings: keys of settings.json set always or only when absent, and the rules for CLAUDE.md
  always?: unknown;
  ifAbsent?: unknown;
  claudeMd?: string;
};

// `script` and `access` are counted by a hub that has the machine setup; an older one omits them.
export type KindCounts = {
  skill: number;
  mcp: number;
  repo: number;
  doc: number;
  script?: number;
  access?: number;
  plugin?: number;
  'claude-settings'?: number;
  app?: number;
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
  groupId: string; // the group of the layer the entry is in; '' for none
  position: number; // the entry's place among the layer's entries: scripts run in this order (0 until placed)
};

// A group of a layer's entries (docs/hub/environment.md, «Groups»). Required: its items are pinned. Optional: each
// person switches it on or off for themselves and their machines; defaultOn is how it starts.
export type EnvGroup = {
  id: string;
  name: string;
  hint: string;
  required: boolean;
  defaultOn: boolean;
  position: number;
  /** The platform's own group («Рекомендованное»): nobody changes it in the hub. */
  builtIn: boolean;
};
// POST /api/orgs/{org}/env/groups → 201 { group }; PATCH …/env/groups/{groupId} → 200 { group }; DELETE → 204.
export type AddGroupRequest = {
  layer: EnvLayerKind;
  scopeId: string;
  name: string;
  hint?: string;
  required?: boolean;
  defaultOn?: boolean;
};
export type PatchGroupRequest = {
  name?: string;
  hint?: string;
  required?: boolean;
  defaultOn?: boolean;
  position?: number;
};
export type GroupResponse = { group: EnvGroup };

// A preset of a layer: a named set of the layer's groups (docs/hub/environment.md, «Presets»).
export type EnvPreset = {
  id: string;
  name: string;
  hint: string;
  groupIds: string[];
  position: number;
};
// POST /api/orgs/{org}/env/presets → 201 { preset }; PATCH …/env/presets/{presetId} → 200 { preset }; DELETE → 204.
// 409 name_taken: the layer has a preset of that name; 400 on `groupIds`: a group of another layer.
export type AddPresetRequest = {
  layer: EnvLayerKind;
  scopeId: string;
  name: string;
  hint?: string;
  groupIds?: string[];
};
export type PatchPresetRequest = {
  name?: string;
  hint?: string;
  groupIds?: string[];
  position?: number;
};
export type PresetResponse = { preset: EnvPreset };

// POST /api/orgs/{org}/env/apply → 200: «Применить на машинах». `now` runs an update on the machines online now, the
// rest when they next connect; `next` waits for the next connection on every machine.
export type ApplyWhen = 'now' | 'next';
export type ApplyRequest = { layer: EnvLayerKind; scopeId: string; when: ApplyWhen };
export type ApplyResponse = { machines: number; started: number; waiting: number };

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
  groups: EnvGroup[];
  presets: EnvPreset[];
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
  groupId?: string;
};
// PATCH /api/orgs/{org}/env/entries/{entryId} → 200 EnvEntryRow
export type PatchEntryRequest = {
  enabled?: boolean;
  audience?: { mode: AudienceMode; teams?: string[] };
  pinned?: boolean;
  groupId?: string; // '' takes the entry out of its group
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
  groupId: string; // '' for none; an optional group switched off makes `off` true
};
// A group the person's items are in.
export type MeEnvGroup = {
  id: string;
  name: string;
  hint: string;
  layer: EnvLayerKind;
  required: boolean;
  on: boolean;
  canToggle: boolean;
  items: number;
  builtIn: boolean;
};
// PUT /api/me/env/groups/{groupId}?space= → 200 MeEnvGroup
export type MeEnvGroupRequest = { on: boolean; space?: string };
// A preset of a layer, for the person: only presets with a group they have items in. `active`: their choice of the
// layer's optional groups is the preset's.
export type MeEnvPreset = {
  id: string;
  name: string;
  hint: string;
  layer: EnvLayerKind;
  groupIds: string[];
  active: boolean;
};
// PUT /api/me/env/presets/{presetId}?space= → 200: the person's presets and groups as they are now
export type MeEnvPresetRequest = { space?: string };
export type MeEnvPresetResponse = { presets: MeEnvPreset[]; groups: MeEnvGroup[] };
// GET /api/me/env?space=
export type MeEnvResponse = {
  space: { id: string; slug: string; name: string };
  org: { id: string; name: string };
  items: MeEnvItem[];
  groups: MeEnvGroup[];
  presets: MeEnvPreset[];
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
