// The new hub's menu by role, as the design draws it: the admin sees the tenant, its settings and the organization;
// the lead sees their team; the member sees their own screens. A section that is off for the organization is not shown at all.
import type { Role, Section } from '../org/types';

export interface HxNavItem {
  id: string;
  label: string;
  icon: string;
  /** Absent: shown greyed out and not clickable. */
  section?: Section;
}

export interface HxNavGroup {
  label: string;
  items: HxNavItem[];
}

const item = (id: string, label: string, icon: string, section?: Section): HxNavItem => ({
  id,
  label,
  icon,
  section,
});

export const navOf = (
  role: Role,
  teamName: string | null,
  features: readonly string[],
  scope: 'org' | 'tenant' = 'tenant',
): HxNavGroup[] => {
  const env = features.includes('env') ? [item('env', 'Окружение', 'layers', 'env')] : [];
  const overview = item('overview', 'Обзор', 'chart-column', 'overview');
  const analytics = item('analytics', 'Аналитика', 'activity', 'analytics');
  const machines = item('machines', 'Машины', 'server', 'machines');
  const accounts = item('accounts', 'Аккаунты', 'key-round', 'accounts');
  // The tools screen comes with the lively hub (`ui.hx.live`).
  const tools = features.includes('ui.hx.live')
    ? [
        item('efficiency', 'Эффективность', 'gauge', 'efficiency'),
        item('tools', 'Инструменты', 'blocks', 'tools'),
      ]
    : [];
  if (role === 'member') return [{ label: '', items: [overview, analytics, ...tools, machines, ...env] }];
  if (role === 'lead') {
    return [
      {
        label: teamName ? `Команда ${teamName}` : 'Команда',
        items: [overview, analytics, ...tools, machines, item('team', 'Моя команда', 'users', 'team')],
      },
      { label: 'Настройки', items: [...env, accounts] },
    ];
  }
  return [
    {
      label: scope === 'org' ? 'Организация' : 'Тенант',
      items: [overview, analytics, ...tools, machines, item('teams', 'Команды', 'users', 'teams')],
    },
    {
      label: 'Настройки',
      items: [...env, accounts, item('settings', 'Организация', 'settings-2', 'settings')],
    },
  ];
};

/** The sections a role may open in the new hub; anything else goes to the overview. */
export const hxSectionsOf = (role: Role, features: readonly string[]): Section[] => {
  const env: Section[] = features.includes('env') ? ['env'] : [];
  const tools: Section[] = features.includes('ui.hx.live') ? ['efficiency', 'tools'] : [];
  // The organization's own settings are its admin's only.
  const settings: Section[] = role === 'admin' ? ['settings'] : [];
  return role === 'member'
    ? ['overview', 'analytics', 'machines', ...tools, ...env]
    : ['overview', 'analytics', 'machines', 'teams', 'team', 'accounts', ...tools, ...env, ...settings];
};
