// The menu of the organization hub by role (the board «Хаб»): one source for the sidebar, the router guard and the
// «где мы» line. Disabled items are shown greyed and are not links.
import type { Role, Section } from './types';

export interface NavItem {
  id: string;
  label: string;
  icon: 'overview' | 'analytics' | 'machines' | 'teams' | 'env' | 'accounts' | 'connections';
  /** Absent: the item is disabled until its screen exists. */
  section?: Section;
}

export interface NavGroup {
  label: string | null;
  items: NavItem[];
}

const OVERVIEW: NavItem = { id: 'overview', label: 'Обзор', icon: 'overview', section: 'overview' };
const ANALYTICS: NavItem = { id: 'analytics', label: 'Аналитика', icon: 'analytics', section: 'analytics' };
const MACHINES: NavItem = { id: 'machines', label: 'Машины', icon: 'machines', section: 'machines' };
const ENV: NavItem = { id: 'env', label: 'Окружение', icon: 'env' };

export const navFor = (role: Role, scopeLabel: string, teamName: string | null): NavGroup[] => {
  if (role === 'member') return [{ label: null, items: [OVERVIEW, ANALYTICS, ENV] }];
  if (role === 'lead') {
    return [
      {
        label: teamName ? `Команда ${teamName}` : scopeLabel,
        items: [
          OVERVIEW,
          ANALYTICS,
          MACHINES,
          { id: 'teams', label: 'Моя команда', icon: 'teams', section: 'teams' },
        ],
      },
      { label: 'Настройки', items: [ENV, { id: 'accounts', label: 'Аккаунты и секреты', icon: 'accounts' }] },
    ];
  }
  return [
    {
      label: scopeLabel,
      items: [
        OVERVIEW,
        ANALYTICS,
        MACHINES,
        { id: 'teams', label: 'Команды', icon: 'teams', section: 'teams' },
      ],
    },
    { label: 'Настройки', items: [ENV, { id: 'accounts', label: 'Аккаунты и секреты', icon: 'accounts' }] },
    { label: 'Организация', items: [{ id: 'connections', label: 'Подключения', icon: 'connections' }] },
  ];
};

/** The sections a role may open at all; the router sends anything else to the first one. */
export const sectionsOf = (role: Role): Section[] =>
  role === 'member' ? ['overview', 'analytics'] : ['overview', 'analytics', 'machines', 'teams'];
