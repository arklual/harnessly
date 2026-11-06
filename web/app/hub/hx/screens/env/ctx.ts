// What a page inside the window gets from the screen.
import type { CatalogItem, EnvLayer, EnvResponse } from '../../../../hub-api/env';
import type { Role } from '../../../org/types';
import type { Actions } from './actions';
import type { Draft, RepoRef } from './model';
import type { LayerMeta, Target } from './scope';

export type SheetMode =
  | { mode: 'add'; step: 'home' | 'link' }
  | { mode: 'add'; step: 'kind'; kind: 'access' | 'repo' | 'script' | 'skill' | 'mcp'; draft?: Draft }
  | { mode: 'edit'; entryId: string }
  | { mode: 'view'; entryId: string }
  | { mode: 'copy' }
  | { mode: 'test' }
  | { mode: 'logs' };

export interface SheetCtx {
  role: Role;
  /** An organization admin (not a tenant admin or a lead): the only one who creates access and connects safe keys. */
  orgAdmin: boolean;
  /** The layers the screen shows and the teams of the picker. */
  view: Pick<EnvResponse, 'layers' | 'teams'>;
  layers: EnvLayer[];
  target: Target;
  setTarget: (t: Target) => void;
  chain: EnvLayer[];
  meta: LayerMeta;
  catalog: CatalogItem[] | null;
  repos: RepoRef[];
  secretNames: string[];
  actions: Actions;
  go: (m: SheetMode) => void;
  close: () => void;
  /** Whether other teams exist to take items from. */
  canCopy: boolean;
  /** Lights the rows of these entries for a moment. */
  flash: (entryIds: string[]) => void;
}
