import type { Needs } from '../../../hub-api/orgs';

const WHO: Record<Needs, string> = {
  org_admin: 'админ организации',
  tenant_admin: 'админ тенанта',
  team_lead: 'лид команды',
  instance_admin: 'администратор хаба',
};

/** A failure of the org API, shown as it is (no fallback hides it). */
export class OrgApiError extends Error {
  readonly code:
    | 'forbidden'
    | 'self_change'
    | 'last_admin'
    | 'no_recipient'
    | 'restart_unsupported'
    | 'not_found'
    | 'not_ready'
    | 'network'
    | 'invalid_request'
    // Environment: the entry is already there, a pinned or foreign row, a taken catalog name, a built-in item; and the
    // feature being off for the organization.
    | 'exists'
    | 'locked'
    | 'name_taken'
    | 'built_in'
    | 'not_enabled'
    // Machine setup: no online machine of the person with the setup feature; the machine is already running something.
    | 'no_machine'
    | 'busy'
    // The hub opens only machines it trusts on its own origin (no HUB_RUNTIME_DOMAIN): the tenant's owner may trust one.
    | 'untrusted'
    // A refusal of the space endpoints (machine codes, invites, members): the message is already the text to show.
    | 'hub'
    | 'internal';
  /** Who could do it, when the hub says. */
  readonly needs: Needs | null;
  constructor(code: OrgApiError['code'], message: string, needs: Needs | null = null) {
    super(message);
    this.name = 'OrgApiError';
    this.code = code;
    this.needs = needs;
  }
}

export const orgErrorText = (e: unknown): string => {
  if (e instanceof OrgApiError) {
    if (e.code === 'self_change' || e.code === 'forbidden') {
      const lead = e.code === 'self_change' ? 'Себе менять нельзя.' : 'У вашей роли нет доступа к этому.';
      return e.needs ? `${lead} Может поменять: ${WHO[e.needs]}.` : lead;
    }
    if (e.code === 'last_admin') return 'Нельзя снять последнего администратора организации.';
    if (e.code === 'no_recipient')
      return 'Лимит поднять некому: в организации нет того, кто может это сделать.';
    if (e.code === 'restart_unsupported')
      return 'Раннер на этой машине старый и не умеет перезапускаться отсюда. Сначала обновите его.';
    if (e.code === 'not_found') return 'Такого нет.';
    if (e.code === 'exists') return 'Это уже есть в окружении.';
    if (e.code === 'locked') return 'Закреплено выше: здесь это не изменить.';
    if (e.code === 'name_taken') return 'Такое название в каталоге уже есть.';
    if (e.code === 'built_in') return 'Встроенное из каталога не изменить.';
    if (e.code === 'not_enabled') return 'Окружение для этой организации не включено.';
    if (e.code === 'no_machine')
      return 'Нет вашей машины в сети, которая умеет настраиваться. Включите машину и попробуйте ещё раз.';
    if (e.code === 'busy') return 'На машине уже что-то идёт. Подождите, пока закончится.';
    if (e.code === 'not_ready') return 'Живые данные хаба ещё не подключены.';
    if (e.code === 'network') return 'Нет связи с хабом.';
  }
  // A message of the hub is Russian; a raw English one (a client-side throw, a library) is never shown as it is.
  if (e instanceof Error && /[а-яё]/i.test(e.message)) return e.message;
  if (e instanceof OrgApiError && e.code === 'internal')
    return 'Хаб не ответил как надо. Попробуйте ещё раз.';
  return 'Не получилось. Попробуйте ещё раз.';
};
