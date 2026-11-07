// The «Инструменты» screen: which MCP servers, skills and built-in tools the agents call, how often, how many calls
// failed and who called. The period, the kind, the search, the sort and the open tool live in the address.
import { useMemo, useState } from 'react';

import { navigate } from '../../lib/router';
import { addDays, isoDay } from '../../org/dates';
import type { ToolItem, ToolsData } from '../../org/api/tools';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { useLoad } from '../../org/hooks';
import { Count } from '../live';
import { readSort, nextSort, searchRows, sortRows, useParams, type Getters, type SortState } from '../sort';
import { Titles } from '../shell';
import { Btn, Icon } from '../ui';
import { plural } from './overview/fmt';
import '../styles/tools.css';

const KINDS = [
  { id: 'all', label: 'Все' },
  { id: 'mcp', label: 'MCP' },
  { id: 'skill', label: 'Скиллы' },
  { id: 'builtin', label: 'Встроенные' },
] as const;

const KIND_NAME: Record<string, string> = { mcp: 'MCP', skill: 'Скилл', builtin: 'Встроенный' };

const QUICK = [
  { id: '1', label: 'Сегодня' },
  { id: '7', label: '7 дн' },
  { id: '30', label: '30 дн' },
] as const;

const SORTS = ['name', 'calls', 'errors', 'users', 'last'] as const;
const FALLBACK: SortState = { key: 'calls', dir: 'desc' };

/** Under this many calls a share or a trend says nothing. */
const FEW_CALLS = 20;

const GET: Getters<ToolItem> = {
  name: (t) => t.name,
  calls: (t) => t.calls,
  errors: (t) => (t.calls > 0 ? t.errors / t.calls : 0),
  users: (t) => t.users,
  last: (t) => t.lastAt,
};

const num = (n: number): string => n.toLocaleString('ru-RU');

const ago = (ms: number, now: number): string => {
  if (!ms) return '—';
  const m = Math.max(0, Math.round((now - ms) / 60_000));
  if (m < 1) return 'только что';
  if (m < 60) return `${m} мин назад`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h} ч назад`;
  return `${Math.round(h / 24)} дн назад`;
};

const errShare = (t: ToolItem): string =>
  t.calls === 0 ? '—' : `${(Math.round((t.errors / t.calls) * 1000) / 10).toLocaleString('ru-RU')}%`;

interface Window {
  from: string;
  /** Not including. */
  to: string;
  /** The quick choice that makes it, or null for dates picked by hand. */
  quick: string | null;
}

/** The period from the address: `d=<days>` or `from`/`to` (the last day included); a week by default. */
const windowOf = (get: (k: string) => string | null, today: string): Window => {
  const f = get('from');
  const t = get('to');
  const ok = (v: string | null): v is string => !!v && /^\d{4}-\d{2}-\d{2}$/.test(v);
  if (ok(f) && ok(t) && f <= t) return { from: f, to: addDays(t, 1), quick: null };
  const d = get('d');
  const days = d === '1' || d === '30' ? Number(d) : 7;
  return { from: addDays(today, 1 - days), to: addDays(today, 1), quick: String(days) };
};

const Head = ({ sort, onSort }: { sort: SortState; onSort: (k: string, first?: 'asc' | 'desc') => void }) => {
  const col = (key: string, label: string, first?: 'asc' | 'desc') => (
    <span
      role="columnheader"
      aria-sort={sort.key === key ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
    >
      <button
        type="button"
        className="live-sort"
        onClick={() => onSort(key, first)}
        aria-label={`Сортировать: ${label}`}
      >
        {label}
        <Icon i="chevron-up" size="xs" />
      </button>
    </span>
  );
  return (
    <div className="pt-row tl-row mt-head" role="row">
      {col('name', 'Инструмент', 'asc')}
      {col('calls', 'Вызовов')}
      {col('errors', 'Ошибок')}
      {col('users', 'Кто вызывает')}
      {col('last', 'Последний')}
    </div>
  );
};

const Spark = ({ days }: { days: number[] }) => {
  const top = Math.max(1, ...days);
  return (
    <span className="tl-spark" aria-hidden="true">
      {days.map((v, i) => (
        <i key={i} style={{ height: `${Math.max(v > 0 ? 8 : 2, (v / top) * 100)}%` }} />
      ))}
    </span>
  );
};

const Row = ({
  t,
  open,
  now,
  onToggle,
  onWho,
}: {
  t: ToolItem;
  open: boolean;
  now: number;
  onToggle: () => void;
  onWho: () => void;
}) => (
  <div className="tl-item" data-open={open ? '1' : undefined}>
    <button type="button" className="pt-row tl-row" aria-expanded={open} onClick={onToggle}>
      <span className="tl-name">
        <b>{t.name}</b>
        <small>{KIND_NAME[t.kind] ?? t.kind}</small>
      </span>
      <span className="tl-num">{num(t.calls)}</span>
      <span
        className="tl-num"
        data-bad={t.errors > 0 && t.errors / Math.max(1, t.calls) >= 0.1 ? '1' : undefined}
      >
        {t.errors === 0 ? '0' : `${num(t.errors)} · ${errShare(t)}`}
      </span>
      <span className="tl-who">
        {t.users} {plural(t.users, 'человек', 'человека', 'человек')}
        {t.teams > 1 ? ` · ${t.teams} ${plural(t.teams, 'команда', 'команды', 'команд')}` : ''}
      </span>
      <span className="tl-last">{ago(t.lastAt, now)}</span>
    </button>
    {open && (
      <div className="tl-more">
        <div className="tl-more__chart">
          <small>Вызовы по дням</small>
          <Spark days={t.days} />
        </div>
        <div className="tl-more__who">
          <small>Кто вызывает</small>
          <ul>
            {t.people.map((p) => (
              <li key={p.userId}>
                <span>{p.name}</span>
                <span>
                  {num(p.calls)}
                  {p.errors > 0 ? ` · ошибок ${num(p.errors)}` : ''}
                </span>
              </li>
            ))}
          </ul>
          {t.users > t.people.length && <small>и ещё {t.users - t.people.length}</small>}
          <Btn size="sm" icon="activity" onClick={onWho}>
            Разбор в аналитике
          </Btn>
        </div>
      </div>
    )}
  </div>
);

const Skeleton = () => (
  <div className="mt" aria-busy="true">
    {Array.from({ length: 5 }, (_, i) => (
      <div key={i} className="pt-row tl-row">
        <span className="hx-sk" style={{ height: 32 }} />
      </div>
    ))}
  </div>
);

const Totals = ({ data, list }: { data: ToolsData; list: ToolItem[] }) => {
  const errors = list.reduce((n, t) => n + t.errors, 0);
  const calls = list.reduce((n, t) => n + t.calls, 0);
  const top = list[0];
  return (
    <div className="tl-totals">
      <div>
        <small>Вызовов</small>
        <b>
          <Count text={num(calls)} />
        </b>
      </div>
      <div>
        <small>Инструментов</small>
        <b>
          <Count text={num(list.length)} />
        </b>
      </div>
      <div>
        <small>Ошибок</small>
        <b>
          <Count
            text={
              calls === 0 ? '—' : `${(Math.round((errors / calls) * 1000) / 10).toLocaleString('ru-RU')}%`
            }
          />
        </b>
      </div>
      <div>
        <small>Чаще всего</small>
        <b className="tl-top">{top ? top.name : '—'}</b>
      </div>
      <span hidden>{data.days.length}</span>
    </div>
  );
};

const Screen = () => {
  const { shell, base, scope } = useOrg();
  const p = useParams();
  const today = isoDay(new Date());
  const win = windowOf(p.get, today);
  const loaded = useLoad(
    (api) => api.toolsList(shell.org.slug, { from: win.from, to: win.to }),
    [shell.org.slug, scope.kind === 'org' ? 'all' : scope.tenant.slug, win.from, win.to],
  );
  const { data, error, reload } = loaded;

  const kind = KINDS.find((k) => k.id === p.get('kind'))?.id ?? 'all';
  const q = p.get('q') ?? '';
  const sort = readSort(p.search, SORTS, FALLBACK);
  const openName = p.get('tool');
  const [now] = useState(() => Date.now());

  const all = useMemo(() => data?.items ?? [], [data]);
  const shown = useMemo(() => {
    const byKind = kind === 'all' ? all : all.filter((t) => t.kind === kind);
    return sortRows(
      searchRows(byKind, q, (t) => [t.name, t.kind, ...t.people.map((x) => x.name)]),
      sort,
      GET,
    );
  }, [all, kind, q, sort]);

  const days = data?.days.length ?? 0;
  const few = data !== null && data.calls > 0 && data.calls < FEW_CALLS;
  const reset = () => p.set({ q: null, kind: null });

  const empty = () => {
    if (all.length === 0) {
      return (
        <div className="tl-empty">
          <b>За этот период вызовов нет</b>
          <span>
            Вызовы приходят с машин организации. Подключите машину и запустите на ней агента, или выберите
            период подлиннее.
          </span>
          <div>
            <Btn size="sm" icon="server" onClick={() => navigate(`${base}/machines`)}>
              Подключить машину
            </Btn>
            {win.quick !== '30' && (
              <Btn size="sm" onClick={() => p.set({ d: '30', from: null, to: null })}>
                Показать 30 дней
              </Btn>
            )}
          </div>
        </div>
      );
    }
    return (
      <div className="tl-empty">
        <b>Ничего не нашлось</b>
        <span>Под выбранный вид и поиск не подошёл ни один инструмент.</span>
        <div>
          <Btn size="sm" icon="rotate-cw" onClick={reset}>
            Сбросить поиск и вид
          </Btn>
        </div>
      </div>
    );
  };

  return (
    <>
      <div className="hb-head">
        <Titles
          title="Инструменты"
          lead={
            data
              ? `${num(data.calls)} ${plural(data.calls, 'вызов', 'вызова', 'вызовов')} за ${days} ${plural(days, 'день', 'дня', 'дней')}`
              : 'Какие MCP и скиллы вызывают агенты'
          }
        />
      </div>
      <div className="tl-bar">
        <div className="hx-set__seg" role="radiogroup" aria-label="Период">
          {QUICK.map((o) => (
            <button
              key={o.id}
              type="button"
              className="hx-set__seg-item"
              role="radio"
              aria-checked={win.quick === o.id}
              onClick={() => p.set({ d: o.id, from: null, to: null })}
            >
              {o.label}
            </button>
          ))}
        </div>
        <label className="tl-date">
          с
          <input
            type="date"
            className="hx-set__input"
            max={today}
            value={win.from}
            onChange={(e) =>
              e.target.value && p.set({ from: e.target.value, to: addDays(win.to, -1), d: null })
            }
          />
        </label>
        <label className="tl-date">
          по
          <input
            type="date"
            className="hx-set__input"
            max={today}
            value={addDays(win.to, -1)}
            onChange={(e) => e.target.value && p.set({ to: e.target.value, from: win.from, d: null })}
          />
        </label>
        <div className="hx-set__seg" role="radiogroup" aria-label="Вид инструмента">
          {KINDS.map((o) => (
            <button
              key={o.id}
              type="button"
              className="hx-set__seg-item"
              role="radio"
              aria-checked={kind === o.id}
              onClick={() => p.set({ kind: o.id === 'all' ? null : o.id })}
            >
              {o.label}
            </button>
          ))}
        </div>
        <input
          type="search"
          className="hx-set__input tl-search"
          placeholder="Найти инструмент или человека"
          aria-label="Поиск"
          value={q}
          onChange={(e) => p.set({ q: e.target.value })}
        />
      </div>
      {error && !data ? (
        <p className="an-err" role="alert">
          <b>Инструменты не загрузились</b>
          <span>{orgErrorText(error)}</span>
          <Btn size="sm" icon="rotate-cw" onClick={reload}>
            Повторить
          </Btn>
        </p>
      ) : !data ? (
        <Skeleton />
      ) : (
        <div className="live-swap" key={`${win.from}${win.to}${kind}`}>
          <Totals data={data} list={shown} />
          {few && (
            <p className="tl-few" role="note">
              Мало данных: за период меньше {FEW_CALLS} вызовов. Доли ошибок и порядок в списке могут
              оказаться случайными, возьмите период подлиннее.
            </p>
          )}
          {shown.length === 0 ? (
            empty()
          ) : (
            <div className="mt tl-rows" role="table" aria-label="Инструменты">
              <Head
                sort={sort}
                onSort={(k, first) => {
                  const n = nextSort(sort, k, first);
                  p.set({ sort: n.key, dir: n.dir });
                }}
              />
              {shown.map((t) => {
                const id = `${t.kind}:${t.name}`;
                return (
                  <Row
                    key={id}
                    t={t}
                    now={now}
                    open={openName === id}
                    onToggle={() => p.set({ tool: openName === id ? null : id })}
                    onWho={() => navigate(`${base}/analytics`)}
                  />
                );
              })}
            </div>
          )}
        </div>
      )}
    </>
  );
};

export default Screen;
