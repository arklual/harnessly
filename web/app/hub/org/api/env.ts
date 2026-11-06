// «Окружение» and the catalog (docs/hub/environment.md): what a machine gets installed, layer by layer, and a person's own
// switches. The wire types are the hub's own (hub-api/env.ts); the screens use them as they are.
import type {
  AddCatalogRequest,
  AddGroupRequest,
  ApplyRequest,
  ApplyResponse,
  EnvGroup,
  MeEnvGroup,
  PatchGroupRequest,
  AddEntryRequest,
  CatalogItem,
  CatalogKind,
  EnvOrderRequest,
  EnvRunRow,
  EnvTryRequest,
  EnvTryResponse,
  MeMachine,
  MeRunRequest,
  MeRunResponse,
  PatchCatalogRequest,
  EnvCard,
  EnvEntryRow,
  EnvLayerKind,
  EnvResponse,
  MeEnvItem,
  MeEnvResponse,
  PatchEntryRequest,
} from '../../../hub-api/env';

/** What the catalog drawer adds to: the layer and its owner (the organization, tenant or team id). */
export interface CatalogTarget {
  layer: EnvLayerKind;
  scopeId: string;
}

export interface EnvApi {
  /** The layers of a tenant (its slug) or of all the tenants the person looks at (`all`), with the summary tiles. */
  envView(org: string, scope: string): Promise<EnvResponse>;
  /** One entry with its numbers: calls, people, the 14 days. */
  envCard(org: string, entryId: string): Promise<EnvCard>;
  envAdd(org: string, req: AddEntryRequest): Promise<EnvEntryRow>;
  envPatch(org: string, entryId: string, req: PatchEntryRequest): Promise<EnvEntryRow>;
  envRemove(org: string, entryId: string): Promise<void>;
  /** The catalog; `added` tells which items the target layer already has. */
  envCatalog(org: string, target: CatalogTarget, kind?: CatalogKind): Promise<CatalogItem[]>;
  /** A new item of the organization's catalog (an organization admin's right). */
  catalogAdd(org: string, req: AddCatalogRequest): Promise<CatalogItem>;
  /** A person's own environment in a tenant (its slug; absent: the hub picks the one they are in). */
  envMine(space?: string): Promise<MeEnvResponse>;
  envMineSet(itemId: string, off: boolean, space?: string): Promise<MeEnvItem>;
  /** The person turns an optional group on or off for themselves and their machines. */
  envMineGroup(groupId: string, on: boolean, space?: string): Promise<MeEnvGroup>;
  /** A group of a layer's entries: required pins its items, optional lets each person choose. */
  envGroupAdd(org: string, req: AddGroupRequest): Promise<EnvGroup>;
  envGroupPatch(org: string, groupId: string, req: PatchGroupRequest): Promise<EnvGroup>;
  /** Removes a group; its entries stay in the layer without a group. */
  envGroupRemove(org: string, groupId: string): Promise<void>;
  /** «Применить на машинах»: an update run on the machines a layer reaches, now or at their next connection. */
  envApply(org: string, req: ApplyRequest): Promise<ApplyResponse>;

  /** Changes an item of the catalog (its owner layer's managers, the organization's admins); the change shows in every layer that has it. */
  catalogPatch(org: string, itemId: string, req: PatchCatalogRequest): Promise<CatalogItem>;
  /** Archives an item: the entries that used it stop putting it in. */
  catalogDelete(org: string, itemId: string): Promise<void>;
  /** The scripts of a layer in their new order: all of its script entries, each once. Answers the order the hub kept. */
  envOrder(org: string, req: EnvOrderRequest): Promise<string[]>;
  /** The runs of the machines a layer reaches, newest first, 14 days; `errors`: only runs with an error. */
  envRuns(org: string, target: CatalogTarget, errors: boolean): Promise<EnvRunRow[]>;
  /** One run with the log of every step. */
  envRun(org: string, runId: string): Promise<EnvRunRow>;
  /** A sandbox run of a layer's list with a draft added or put in place of an entry. */
  envTry(org: string, req: EnvTryRequest): Promise<EnvTryResponse>;
  /** The person's machines in a tenant, and how far each one got. */
  meMachines(space?: string): Promise<MeMachine[]>;
  /** Asks the person's online machines in the tenant (or the one) to update or to check. */
  meRun(req: MeRunRequest, space?: string): Promise<MeRunResponse>;
  /** The runs of the person's machines. */
  meRuns(space?: string): Promise<EnvRunRow[]>;
}
