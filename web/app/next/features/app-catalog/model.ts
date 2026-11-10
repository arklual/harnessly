// The rows of the team's catalog and what the person can do with each: pure, so the screen only draws them.
import {
  REFUSAL,
  TEAM_ROLE,
  type ICatalogEntry,
  type IPrecheck,
  type ITeamInfo,
} from '@/next/features/app-catalog/api';
import { t } from '@/next/i18n';

/** The actions of a row. */
export const ROW_ACTION = {
  /** An owner puts the app on the team's machines. */
  install: 'install',
  /** An owner takes it off them. */
  uninstall: 'uninstall',
  /** A member turns a team app off for themselves. */
  mute: 'mute',
  /** A member turns it on again. */
  unmute: 'unmute',
} as const;
export type TRowAction = (typeof ROW_ACTION)[keyof typeof ROW_ACTION];

export interface ICatalogRow {
  entry: ICatalogEntry;
  /** The one action the row offers this person; null for none (a member before an owner installed the app). */
  action: TRowAction | null;
  muted: boolean;
}

/**
 * An app of the trusted tier runs code on the machine: it is never installed for a team, so the catalog does not offer
 * it (the runtime refuses it too).
 */
export const installable = (entry: ICatalogEntry): boolean => entry.trust !== 'trusted';

export const rowsOf = (info: ITeamInfo): ICatalogRow[] =>
  info.catalog.map((entry) => {
    const muted = info.muted.includes(entry.id);
    let action: TRowAction | null = null;
    if (info.role === TEAM_ROLE.owner) {
      if (entry.installed) action = ROW_ACTION.uninstall;
      else if (installable(entry) && refusalOf(entry.check) === null) action = ROW_ACTION.install;
    } else if (entry.installed) {
      action = muted ? ROW_ACTION.unmute : ROW_ACTION.mute;
    }
    return { entry, action, muted };
  });

/** Why this machine cannot install the entry, in words, before anything is downloaded (HRN-700); null when it can. */
export const refusalOf = (check: IPrecheck | undefined): string | null => {
  const first = check?.refusals[0];
  if (first === undefined) return null;
  const [what = '', app = ''] = (first.detail ?? '').split(' ');
  switch (first.code) {
    case REFUSAL.trust:
      return t('next.appteam.refuse.trust');
    case REFUSAL.host:
      return t('next.appteam.refuse.host', { range: what });
    case REFUSAL.name:
      return t('next.appteam.refuse.name', { app: what });
    case REFUSAL.command:
      return t('next.appteam.refuse.command', { command: what, app });
    case REFUSAL.kind:
      return t('next.appteam.refuse.kind', { kind: what, app });
    default:
      return null;
  }
};

/** The secrets the app will ask for after the install, said before it. */
export const secretsOf = (check: IPrecheck | undefined): string | null =>
  check === undefined || check.secrets.length === 0
    ? null
    : t('next.appteam.secrets', { names: check.secrets.join(', ') });
