// Environment and catalog, as docs/hub/environment.md writes them: what a machine gets installed, layer by layer
// (organization, tenant, team), the catalog the entries are picked from, and a person's own switches. Times are RFC 3339
// UTC strings, days are YYYY-MM-DD (UTC). A number the hub has no facts for is null, never 0.

export type CatalogKind = 'skill' | 'mcp' | 'repo' | 'doc';
export type EnvLayerKind = 'org' | 'tenant' | 'team';
export type AudienceMode = 'tenant' | 'teams' | 'off';

// Why a row cannot be changed by the person looking at it; null: it can.
export type LockedReason = 'pinned_by_org' | 'pinned_by_tenant' | 'no_rights';

// The source of a catalog item. Only the keys of its kind are accepted (skill: body, ref; mcp: transport, command, args, url,
// secretRefs; repo: url, branch; doc: url, text); secrets are named in secretRefs, never carried.
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
};

export type KindCounts = { skill: number; mcp: number; repo: number; doc: number };

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
};

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
};
// GET /api/orgs/{org}/catalog?kind=&layer=&scopeId=
export type CatalogResponse = { items: CatalogItem[] };
// POST /api/orgs/{org}/catalog → 201 CatalogItemResponse; PATCH …/catalog/{itemId} → 200; DELETE archives → 204
export type AddCatalogRequest = { kind: CatalogKind; name: string; hint?: string; source: CatalogSource };
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
};
// PUT /api/me/env/{itemId}?space= → 200 MeEnvItem
export type MeEnvToggleRequest = { off: boolean; space?: string };
