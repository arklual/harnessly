// The rows of the team's catalog and what the person can do with each: pure, so the screen only draws them.
import { TEAM_ROLE, type ICatalogEntry, type ITeamInfo } from '@/next/features/app-catalog/api';

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
      else if (installable(entry)) action = ROW_ACTION.install;
    } else if (entry.installed) {
      action = muted ? ROW_ACTION.unmute : ROW_ACTION.mute;
    }
    return { entry, action, muted };
  });
