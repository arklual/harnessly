// «Окружение» and the catalog (docs/hub/environment.md): what a machine gets installed, layer by layer, and a person's own
// switches. The wire types are the hub's own (hub-api/env.ts); the screens use them as they are.
import type {
  AddEntryRequest,
  CatalogItem,
  CatalogKind,
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
  /** A person's own environment in a tenant (its slug; absent: the hub picks the one they are in). */
  envMine(space?: string): Promise<MeEnvResponse>;
  envMineSet(itemId: string, off: boolean, space?: string): Promise<MeEnvItem>;
}
