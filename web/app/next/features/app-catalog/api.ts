// What the page asks the runtime for the team's catalog (cmd/runtime/routes_apps_share.go): the team, its catalog and
// what the person may do with it. An owner installs an app for the team; a member turns a team app off for themselves.
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { IAccess } from '@/next/features/app-views/api';
import type { MachineId } from '@/next/protocol/v2';

export const CHANNELS = ['dev', 'beta', 'stable'] as const;
export type TChannel = (typeof CHANNELS)[number];

export const TEAM_ROLE = { owner: 'owner', member: 'member' } as const;
export type TTeamRole = (typeof TEAM_ROLE)[keyof typeof TEAM_ROLE];

export const REFUSAL = {
  trust: 'trust',
  host: 'host',
  name: 'name',
  command: 'command',
  kind: 'kind',
} as const;
export type TRefusal = (typeof REFUSAL)[keyof typeof REFUSAL];

/** This machine's check of an entry before the download: why it cannot be installed here, and the secrets it will ask. */
export interface IPrecheck {
  refusals: { code: TRefusal; detail?: string }[];
  secrets: string[];
  /** What the entry says the app reaches, in words, for the install screen; absent from an older machine. */
  access?: IAccess[];
}

export interface ICatalogEntry {
  id: string;
  channel: TChannel;
  version: string;
  name: string;
  description: string;
  trust: string;
  permissions: string[];
  by: string;
  /** The team has this app installed on this channel. */
  installed: boolean;
  installedVersion?: string;
  /** Absent from a runtime that does not check before the download. */
  check?: IPrecheck;
}

export interface ITeamInfo {
  /** "" for a machine that is in no team. */
  space: string;
  role: TTeamRole | '';
  catalog: ICatalogEntry[];
  /** The team's apps the person turned off for themselves. */
  muted: string[];
}

export const loadTeam = (m: MachineId): Promise<ITeamInfo> =>
  runtimeJson<ITeamInfo>(m, platformPath('/apps/team'));

export const installForTeam = (m: MachineId, id: string, channel: TChannel): Promise<unknown> =>
  runtimeJson(m, platformPath('/apps/team/install'), postJson({ id, channel }));

export const uninstallForTeam = (m: MachineId, id: string): Promise<unknown> =>
  runtimeJson(m, platformPath('/apps/team/uninstall'), postJson({ id }));

export const muteTeamApp = (m: MachineId, id: string, mute: boolean): Promise<unknown> =>
  runtimeJson(m, platformPath('/apps/team/mute'), postJson({ id, mute }));

/** An app in the public catalog, as the runtime lists it (cmd/runtime/routes_apps_public.go). */
export interface IPublicEntry {
  id: string;
  version: string;
  name: string;
  description: string;
  trust: string;
  /** The rights the app asks for: the person sees them before the install. */
  permissions: string[];
  /** The archive that was shown; the install names it, so an entry that changed in between is refused. */
  sum: string;
  by: string;
  check?: IPrecheck;
}

export const searchPublic = async (m: MachineId, query: string): Promise<IPublicEntry[]> => {
  const q = query.trim() === '' ? '' : `?q=${encodeURIComponent(query.trim())}`;
  return (await runtimeJson<{ apps: IPublicEntry[] }>(m, platformPath(`/apps/public${q}`))).apps;
};

/** The install after the install screen: `access` are the keys it listed, all the person agreed to. */
export const installPublic = (m: MachineId, entry: IPublicEntry): Promise<unknown> =>
  runtimeJson(
    m,
    platformPath('/apps/public/install'),
    postJson(
      entry.check?.access
        ? { id: entry.id, sum: entry.sum, access: entry.check.access.map((a) => a.key) }
        : { id: entry.id, sum: entry.sum },
    ),
  );

/** A document type an installed app adds to Sinopia (GET <platform>/apps/kinds). */
export interface IAppKind {
  id: string;
  title: string;
  app: string;
  appName: string;
  schemaVersion: number;
  /** The type is made of Sinopia's own site: its documents open in Sinopia's editor. */
  extends?: 'site';
}

export const loadKinds = async (m: MachineId): Promise<IAppKind[]> =>
  (await runtimeJson<{ kinds: IAppKind[] }>(m, platformPath('/apps/kinds'))).kinds;

/** Makes a document of the type; the host makes the project, the app's editor fills it. */
export const createKindDocument = (
  m: MachineId,
  kind: string,
  title: string,
): Promise<{ document: string; title: string }> =>
  runtimeJson(m, platformPath('/apps/kinds/create'), postJson({ kind, title }));

/** A document of an app's type, as the library lists it (GET <platform>/apps/kinds/docs). */
export interface IKindDoc {
  document: string;
  title: string;
  updatedAt: number;
  kind: string;
  /** The type's name; the type's id when its app is not installed. */
  kindTitle: string;
  needsApp: boolean;
}

export const loadKindDocs = async (m: MachineId): Promise<IKindDoc[]> =>
  (await runtimeJson<{ docs: IKindDoc[] }>(m, platformPath('/apps/kinds/docs'))).docs;

/** Why a document opens read-only (the codes of internal/apps/kindopen.go). */
export const READ_ONLY_CODES = [
  'newer',
  'failed',
  'no-room',
  'unreadable',
  'no-package',
  'view-only',
] as const;
export type TReadOnlyCode = (typeof READ_ONLY_CODES)[number];

/** What opening a document of an app's type found (POST <platform>/apps/kinds/open). */
export interface IKindOpen {
  kind: string;
  needsApp: boolean;
  app?: string;
  editor?: string;
  target?: string;
  readOnly?: boolean;
  code?: string;
  reason?: string;
  /** The document is made of Sinopia's own site: it opens in Sinopia's editor, not in an app's view. */
  extends?: 'site';
}

export const openKindDocument = (m: MachineId, document: string): Promise<IKindOpen> =>
  runtimeJson(m, platformPath('/apps/kinds/open'), postJson({ document }));
