import type { MachineBusyKind, MovePlan, OrgMachine } from '../../api/machines';

/** 1 → one, 2–4 → few, 5+ → many (Russian). */
export const plural = (n: number, one: string, few: string, many: string): string => {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return many;
  if (b === 1) return one;
  if (b >= 2 && b <= 4) return few;
  return many;
};

/** «2 ч», «12 дн», «40 мин» since an RFC 3339 time. */
export const ago = (iso: string | null, now: number): string => {
  if (!iso) return '';
  const min = Math.max(0, Math.round((now - Date.parse(iso)) / 60000));
  if (min < 60) return `${Math.max(1, min)} мин`;
  const h = Math.round(min / 60);
  if (h < 48) return `${h} ч`;
  return `${Math.round(h / 24)} дн`;
};

export const rub = (n: number): string => `${Math.round(n).toLocaleString('ru-RU')} ₽`;
export const usd = (n: number): string => `$${Math.round(n).toLocaleString('ru-RU')}`;
export const gb = (v: number): string => (v >= 1000 ? `${v / 1000} ТБ` : `${v} ГБ`);

export const BUSY_TEXT: Record<MachineBusyKind, (chats: number) => [string, string]> = {
  resize: () => ['Меняем ресурсы', 'машина перезагружается, около 2 минут'],
  restart: () => ['Перезапускаем раннер', 'около 30 секунд'],
  reboot: () => ['Перезагружаем машину', 'около 2 минут'],
  drain: (n) => [
    'Ждём окончания чатов',
    `${n} ${plural(n, 'чат', 'чата', 'чатов')} ещё ${n === 1 ? 'идёт' : 'идут'}, новые на машину не пойдут — потом перезапуск`,
  ],
  off: () => ['Отключаем', 'останавливаем раннер и сервер'],
  on: () => ['Подключаем', 'запускаем сервер и раннер, около минуты'],
};

export type Tone = 'ok' | 'warn' | 'bad' | 'off' | 'busy';

/** What the row and the card say about a machine's state, and in which tone. */
export const stateOf = (m: OrgMachine, now: number): { tone: Tone; label: string; hint: string } => {
  if (m.busy) {
    const [label, hint] = BUSY_TEXT[m.busy.kind](m.busy.chats ?? 0);
    return { tone: 'busy', label, hint };
  }
  if (m.state === 'offline') {
    return { tone: 'bad', label: 'Не на связи', hint: `раннер не отвечает ${ago(m.since, now)}` };
  }
  if (m.state === 'idle') return { tone: 'warn', label: 'Простаивает', hint: ago(m.since, now) };
  if (m.state === 'disabled') {
    const by = m.disabledBy ? `${ago(m.disabledAt, now)} назад, ${m.disabledBy}` : 'отключена';
    return { tone: 'off', label: 'Отключена', hint: `${by} · чаты на ней не запустить` };
  }
  return { tone: 'ok', label: 'На связи', hint: 'раннер отвечает' };
};

/** The share of the three bars of a group's strip: online, idle, offline. */
export const strip = (g: { online: number; idle: number; offline: number }) => {
  const total = Math.max(1, g.online + g.idle + g.offline);
  return [
    { tone: 'ok', pct: (g.online / total) * 100 },
    { tone: 'warn', pct: (g.idle / total) * 100 },
    { tone: 'bad', pct: (g.offline / total) * 100 },
  ];
};

export const priceOf = (
  plan: { core: number; ramGb: number; diskGb: number },
  c: number,
  r: number,
  d: number,
) => plan.core * c + plan.ramGb * r + plan.diskGb * d;

/** Lines of the plan of a move, as the dry run of the hub reports them. */
export const movePlanLines = (p: MovePlan): { text: string; bad?: boolean }[] => {
  const out: { text: string; bad?: boolean }[] = [];
  if (p.noop) return [{ text: 'Машина уже в этом тенанте.' }];
  if (p.teamCleared) out.push({ text: `Привязка к команде «${p.teamCleared}» снимется.` });
  if (p.teamAssigned) out.push({ text: `Машина войдёт в команду «${p.teamAssigned}».` });
  if (p.accessRevoked.length > 0) {
    out.push({ text: `Доступ потеряют: ${p.accessRevoked.join(', ')}. Выдайте его заново в новом тенанте.` });
  }
  out.push({
    text:
      p.limits.toRules > 0
        ? `Новые траты пойдут в лимиты «${p.to.name}» (правил: ${p.limits.toRules}); прошлые останутся в истории «${p.from.name}».`
        : `Новые траты пойдут в «${p.to.name}» — лимитов там нет; прошлые останутся в истории «${p.from.name}».`,
  });
  if (p.kept.length > 0) out.push({ text: `Остаётся как было: ${p.kept.join(', ')}.` });
  return out;
};
