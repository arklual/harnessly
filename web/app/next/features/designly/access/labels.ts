// The words of roles and levels, and the menus they are chosen from.
import type { TGrantRole, TInviteRole, TRole, TTeamLevel } from '@/next/features/designly/access/api';
import { locale, t } from '@/next/i18n';

export const roleLabel = (role: TRole): string => t(`next.access.role.${role}`);

/** «Может смотреть»: a role as a person's ability, the words of the role menus. */
export const canLabel = (role: TGrantRole): string => t(`next.access.can.${role}`);

const MAY_MANAGE = ['view', 'comment', 'edit', 'manage'] as const;
const MAY_NOT_MANAGE = ['view', 'comment', 'edit'] as const;

/** The roles to give: «Управлять» only where the giver manages. */
export const grantRoles = (mayManage: boolean): readonly TGrantRole[] =>
  mayManage ? MAY_MANAGE : MAY_NOT_MANAGE;

export const INVITE_ROLES = ['view', 'comment', 'edit'] as const satisfies readonly TInviteRole[];

export const TEAM_LEVELS = ['none', 'view', 'comment', 'edit'] as const satisfies readonly TTeamLevel[];

/** One line under «Общий доступ»: who opens the design at this level. */
export const teamHint = (level: TTeamLevel): string => t(`next.access.general.${level}.hint`);

const RU_MONTHS = [
  'янв',
  'фев',
  'мар',
  'апр',
  'мая',
  'июн',
  'июл',
  'авг',
  'сен',
  'окт',
  'ноя',
  'дек',
] as const;

/** «12 ноя» / «Nov 12»: the day of a moment in the person's clock; null for one that does not read. */
export const shortDay = (iso: string): string | null => {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return null;
  if (locale() === 'ru') return `${at.getDate()} ${RU_MONTHS[at.getMonth()]}`;
  return at.toLocaleDateString('en', { day: 'numeric', month: 'short' });
};

/** «до 12 ноя», or nothing for a grant with no end. */
export const untilLabel = (iso: string | null): string | null => {
  if (iso === null) return null;
  const day = shortDay(iso);
  return day === null ? null : t('next.access.ttl.until', { date: day });
};

/** The first letter of a name or an address, for the round mark beside it. */
export const initialOf = (who: string): string => (who.trim()[0] ?? '?').toUpperCase();
