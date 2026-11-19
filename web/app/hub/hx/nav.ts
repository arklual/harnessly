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

/** The hint each item of the menu shows on hover, word for word from the board (`NAVH`). */
export const NAV_HINT: Partial<Record<Section, string>> = {
  overview: 'Что происходит сейчас и что требует действия: по одной кнопке на каждую проблему.',
  teams: 'Команды (вложенные), люди, приглашения, роли. Здесь же лимиты расходов.',
  team: 'Ваша команда: люди, лимиты, машины.',
  machines: 'Все машины, на которых работают агенты: чьи они, готовы ли к чатам и что сделать, если нет.',
  env: 'Что стоит на машинах: репозитории, доступы, MCP, скиллы. Слоями: организация → команда → человек.',
  analytics: 'Расходы и инструменты: сколько ушло, на что и чем агенты пользуются.',
  efficiency:
    'В каком состоянии харнес агентов: цена MR, что чинить первым, скиллы, изменения и что они сдвинули, гипотезы, сессии, прогоны.',
  accounts: 'Подписки Claude и Codex, ключи API: кто на чём работает и сколько осталось.',
  settings: 'Вход, политика машин и чатов, ссылки, оплата или лицензия, журнал действий.',
};

/**
 * The board's one group «Организация»: Обзор, Люди и команды, Машины, Окружение, Аналитика, Эффективность, Модели и
 * подписки, Настройки организации — in this order. A lead and a member get «Моя команда» in place of the people.
 */
export const navOf = (
  role: Role,
  _teamName: string | null,
  features: readonly string[],
  _scope: 'org' | 'tenant' = 'tenant',
  extViews = false,
): HxNavGroup[] => {
  const live = features.includes('ui.hx.live');
  const items: HxNavItem[] = [item('overview', 'Обзор', 'house', 'overview')];
  if (role === 'admin') items.push(item('teams', 'Люди и команды', 'users', 'teams'));
  else if (role === 'lead') items.push(item('team', 'Моя команда', 'users', 'team'));
  items.push(item('machines', 'Машины', 'server', 'machines'));
  if (features.includes('env')) items.push(item('env', 'Окружение', 'layers', 'env'));
  items.push(item('analytics', 'Аналитика', 'chart-column', 'analytics'));
  if (live) items.push(item('efficiency', 'Эффективность', 'gauge', 'efficiency'));
  if (role !== 'member') items.push(item('accounts', 'Модели и подписки', 'cpu', 'accounts'));
  if (extViews && role !== 'member') items.push(item('extensions', 'Расширения', 'blocks', 'extensions'));
  if (role === 'admin') items.push(item('settings', 'Настройки организации', 'settings', 'settings'));
  return [{ label: 'Организация', items }];
};

/** The sections a role may open in the new hub; anything else goes to the overview. */
export const hxSectionsOf = (role: Role, features: readonly string[], extViews = false): Section[] => {
  const env: Section[] = features.includes('env') ? ['env'] : [];
  const tools: Section[] = features.includes('ui.hx.live') ? ['efficiency', 'tools'] : [];
  // The organization's own settings are its admin's only.
  const settings: Section[] = role === 'admin' ? ['settings'] : [];
  return role === 'member'
    ? ['overview', 'analytics', 'machines', 'map', 'profile', ...tools, ...env]
    : [
        'overview',
        'analytics',
        'machines',
        'map',
        'profile',
        'teams',
        'team',
        'accounts',
        ...tools,
        ...env,
        ...(extViews ? (['extensions'] as Section[]) : []),
        ...settings,
      ];
};
