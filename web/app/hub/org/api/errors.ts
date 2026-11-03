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
    if (e.code === 'not_ready') return 'Живые данные хаба ещё не подключены.';
    if (e.code === 'network') return 'Нет связи с хабом.';
  }
  return e instanceof Error && e.message ? e.message : 'Не получилось. Попробуйте ещё раз.';
};
