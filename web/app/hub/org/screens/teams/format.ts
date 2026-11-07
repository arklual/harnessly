import type { AuditEntry, Needs } from '../../api/teams';
import { inRubles, shownSymbol } from '../../money';

/** 17500 → «17,5 тыс», 120000 → «120 тыс», 800 → «0,8 тыс». */
export const thousand = (rub: number): string => {
  // Dollars are small numbers: «84», not «0,1 тыс».
  if (!inRubles()) return Math.round(rub).toLocaleString('ru-RU');
  const v = Math.round(rub / 100) / 10;
  return `${v.toLocaleString('ru-RU')} тыс`;
};
export const kRub = (rub: number): string => `${thousand(rub)} ${shownSymbol()}`;

/** The stops of a limit, written for rubles, in the shown currency: ₽15 000 is about $150, not $15 000. */
export const stopOf = (rub: number): number => (inRubles() ? rub : rub / 100);

export const ofLimit = (spent: number, limit: number | null): string =>
  limit === null ? `${thousand(spent)} ${shownSymbol()}` : `${thousand(spent)} из ${kRub(limit)}`;

export const sharePct = (spent: number, limit: number | null): number | null =>
  limit && limit > 0 ? Math.round((spent / limit) * 100) : null;

export type Level = 'ok' | 'warn' | 'bad';
/** From 80 % the hub warns, from 90 % the bar is red. */
export const levelOf = (pct: number | null): Level =>
  pct === null ? 'ok' : pct >= 90 ? 'bad' : pct >= 80 ? 'warn' : 'ok';

export const WHO: Record<Needs, string> = {
  org_admin: 'админ организации',
  tenant_admin: 'админ тенанта',
  team_lead: 'лид команды',
  instance_admin: 'администратор хаба',
};
export const needsText = (n: Needs): string => `Может поменять: ${WHO[n]}`;

export const plural = (n: number, one: string, few: string, many: string): string => {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return many;
  if (b === 1) return one;
  if (b >= 2 && b <= 4) return few;
  return many;
};

export const people = (n: number): string => `${n} ${plural(n, 'человек', 'человека', 'человек')}`;

export const ago = (iso: string | null, now: number): string => {
  if (!iso) return '';
  const min = Math.max(0, Math.round((now - Date.parse(iso)) / 60000));
  if (min < 60) return `${Math.max(1, min)} мин назад`;
  const h = Math.round(min / 60);
  if (h < 48) return `${h} ч назад`;
  const d = Math.round(h / 24);
  return `${d} ${plural(d, 'день', 'дня', 'дней')} назад`;
};

const ACTION: Partial<Record<AuditEntry['action'], string>> = {
  'org.create': 'создана организация',
  'org.update': 'изменена организация',
  'tenant.create': 'тенант создан',
  'tenant.move': 'тенант перенесён в организацию',
  'tenant.delete': 'тенант удалён',
  'machine.move': 'машина перенесена в другой тенант',
  'team.update': 'изменена команда',
  'team.delete': 'удалена команда',
  'member.add': 'добавлен в команду',
  'account.create': 'добавлен аккаунт',
  'account.update': 'изменён аккаунт',
  'account.grant': 'выдан аккаунт',
  'account.revoke': 'снят аккаунт',
  'machine.team': 'машина передана команде',
  'machine.access': 'изменён доступ к машине',
  'machine.disable': 'машина выключена',
  'machine.enable': 'машина включена',
  'machine.restart': 'машина перезапущена',
  'change.create': 'отмечено изменение',
  'signal.snooze': 'сигнал отложен',
  'signal.unsnooze': 'сигнал возвращён',
};

const money = (v: unknown): string => (typeof v === 'number' ? kRub(v) : 'без лимита');

/** One line of the journal: who, what, «было → стало». */
export const describe = (e: AuditEntry): { who: string; what: string } => {
  const who = e.actor?.name ?? 'Хаб';
  const label = e.target?.label ?? '';
  const b = e.before ?? {};
  const a = e.after ?? {};
  const self = e.self ? ' (себе)' : '';
  switch (e.action) {
    case 'limit.set':
      return { who, what: `лимит ${label}: ${money(b.limitRub)} → ${money(a.limitRub)}${self}` };
    case 'limit.remove':
      return { who, what: `снят лимит ${label}${self}` };
    case 'grant.add':
      return { who, what: `${label} — теперь лид команды${typeof a.team === 'string' ? ` ${a.team}` : ''}` };
    case 'grant.remove':
      return { who, what: `${label} — больше не лид` };
    case 'member.remove':
      return { who, what: `${label} убран из тенанта` };
    case 'member.primary':
      return { who, what: `${label} — в команде ${typeof a.team === 'string' ? a.team : 'без команды'}` };
    case 'team.create':
      return { who, what: `создана команда ${label}` };
    default:
      return { who, what: [ACTION[e.action] ?? e.action, label].filter(Boolean).join(' ') };
  }
};
