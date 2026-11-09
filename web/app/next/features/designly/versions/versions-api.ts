// The versions of a project's kit and of the project itself, over the machine's design routes (kitversions_routes.go):
// the kit's draft and its published versions, the project's pin on a version, and the project's history points.
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

type TPath = `/${string}`;

const q = (params: Record<string, string>) => new URLSearchParams(params).toString();

export const KIT_PART = { patch: 'patch', minor: 'minor', major: 'major' } as const;
export type TKitPart = (typeof KIT_PART)[keyof typeof KIT_PART];

export const CHANGE_KIND = { added: 'added', changed: 'changed', removed: 'removed' } as const;
export type TChangeKind = (typeof CHANGE_KIND)[keyof typeof CHANGE_KIND];

export interface IKitChange {
  path: string;
  kind: TChangeKind;
}

export interface IKitPin {
  id: string;
  version: string;
  mounts?: string[];
  /** The project is shown on the kit's draft for now. */
  draft?: boolean;
  dismissed?: string;
}

export interface IKitVersion {
  version: string;
  notes?: string;
  from?: string;
  changed?: IKitChange[];
  changedCount?: number;
}

export interface IProjectKit {
  pin: IKitPin | null;
  latest?: string;
  /** A newer version than the pin, unless the person stayed on theirs. */
  update: IKitVersion | null;
  draftProject?: string;
  /** The project is itself the draft of this kit. */
  kitDraftOf?: string;
}

export interface IKitDraft {
  project: string;
  base: string;
  /** Null from a runtime that had none to list. */
  changes: IKitChange[] | null;
  changedCount: number;
}

export interface IKitSettings {
  /** The kit is for viewing only, for everyone: nobody edits it until its maker turns this off. */
  readOnly: boolean;
  /** The person made the kit: they turn viewing only on and off. */
  owner: boolean;
}

export interface IKitState {
  id: string;
  settings?: IKitSettings;
  /** The person may start and edit the kit's draft now. */
  mayEdit?: boolean;
  latest: string;
  versions: IKitVersion[];
  draft: IKitDraft | null;
  projects: { project: string; title: string; version: string; draft: boolean }[];
}

export interface IProjectPoint {
  id: string;
  at: number;
  who: string;
  agent: boolean;
  subject: string;
  name?: string;
}

export interface INamedVersion {
  id: string;
  name: string;
  at: number;
  who: string;
  kit: IKitPin | null;
}

export interface IProjectHistory {
  points: IProjectPoint[];
  named: INamedVersion[];
  unsaved: boolean;
}

export interface IProjectChange {
  path: string;
  kind: TChangeKind;
  board?: string;
}

export interface IProjectDiff {
  revision: string;
  boards: IProjectChange[];
  other: number;
  kitThen: IKitPin | null;
  kitNow: IKitPin | null;
}

/** The version's own record as the runtime answers it (Published: the Version fields inline). */
interface IPublishedWire {
  version: string;
  notes?: string;
  from?: string;
  changed?: IKitChange[];
  changedCount?: number;
}

const kitPath = (p: string) => platformPath(`/design/kits${p}` as TPath);
const projectPath = (p: string) => platformPath(`/design/project${p}` as TPath);

export const fetchProjectKit = (m: MachineId, artifact: string, signal?: AbortSignal) =>
  runtimeJson<IProjectKit>(m, projectPath(`/kit?${q({ artifact })}`), { signal, cache: 'no-store' });

export const KIT_ACTION = { edit: 'edit', use: 'use', stay: 'stay' } as const;
export type TKitAction = (typeof KIT_ACTION)[keyof typeof KIT_ACTION];

export const changeProjectKit = (m: MachineId, artifact: string, action: TKitAction, version?: string) =>
  runtimeJson<IProjectKit>(m, projectPath('/kit'), postJson({ artifact, action, version: version ?? '' }));

/** The kit's state; without `changes` the draft's changed files are not compared (a cheap read). */
export const fetchKitState = (m: MachineId, kit: string, signal?: AbortSignal, changes = true) =>
  runtimeJson<IKitState>(m, kitPath(`/state?${q(changes ? { kit } : { kit, changes: '0' })}`), {
    signal,
    cache: 'no-store',
  });

export const publishKitDraft = (m: MachineId, kit: string, part: TKitPart, notes: string) =>
  runtimeJson<IPublishedWire>(m, kitPath('/publish'), postJson({ kit, part, notes }));

export const startKitDraft = (m: MachineId, kit: string) =>
  runtimeJson<{ project: string; base: string }>(m, kitPath('/draft'), postJson({ kit, action: 'start' }));

export const setKitReadOnly = (m: MachineId, kit: string, readOnly: boolean) =>
  runtimeJson<IKitSettings>(m, kitPath('/settings'), postJson({ kit, readOnly }));

/** The kit screen reads the draft where a version goes. */
export const KIT_DRAFT_VERSION = 'draft';

export const discardKitDraft = (m: MachineId, kit: string) =>
  runtimeJson<null>(m, kitPath('/draft'), postJson({ kit, action: 'discard' }));

export const fetchProjectHistory = (m: MachineId, artifact: string, signal?: AbortSignal) =>
  runtimeJson<IProjectHistory>(m, projectPath(`/versions?${q({ artifact })}`), { signal, cache: 'no-store' });

export const saveProjectVersion = (m: MachineId, artifact: string, name: string) =>
  runtimeJson<INamedVersion>(m, projectPath('/versions'), postJson({ artifact, name }));

export const fetchProjectDiff = (m: MachineId, artifact: string, revision: string, signal?: AbortSignal) =>
  runtimeJson<IProjectDiff>(m, projectPath(`/versions/diff?${q({ artifact, revision })}`), {
    signal,
    cache: 'no-store',
  });

export const restoreProjectVersion = (m: MachineId, artifact: string, revision: string) =>
  runtimeJson<{ revision: string }>(m, projectPath('/versions/restore'), postJson({ artifact, revision }));

const encodeSegments = (p: string) => p.split('/').map(encodeURIComponent).join('/');

/** A page of the project as it was at a version of its history: read only, with its kit then. */
export const versionPageHref = (origin: string, id: string, revision: string, page: string): string =>
  `${origin.replace(/\/+$/, '')}/a/${id}/r/${revision}/${encodeSegments(page.split('?')[0] ?? page)}`;

/** A page of the project on another version of its kit, to look before taking it. */
export const kitPreviewHref = (origin: string, id: string, version: string, page: string): string =>
  `${origin.replace(/\/+$/, '')}/a/${id}/k/${version}/${encodeSegments(page.split('?')[0] ?? page)}`;

/** The next version's number for a part, as the runtime counts it (Bump). */
export const nextVersion = (latest: string, part: TKitPart): string => {
  const [major = 0, minor = 0, patch = 0] = (latest.split('-')[0] ?? '')
    .split('.')
    .map((n) => Number(n) || 0);
  if (part === KIT_PART.patch) return `${major}.${minor}.${patch + 1}`;
  if (part === KIT_PART.major) return `${major + 1}.0.0`;
  return `${major}.${minor + 1}.0`;
};
