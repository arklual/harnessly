// The pieces of «Обзор» in the board's markup: head tiles, cards, «Требует внимания», the donut, the efficiency and
// subscription bars and the lead's list of people. Every width and angle comes from calc.ts, every text from fmt.ts.
import { type CSSProperties, type KeyboardEvent, type ReactNode, useMemo, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import type {
  OverviewData,
  PersonSpend,
  Signal,
  SnoozeFor,
  SubscriptionUse,
  TeamEfficiency,
} from '../../../org/api/overview';
import { navigate } from '../../../lib/router';
import { goSignal } from '../../go';
import { Count } from '../../live';
import { Btn, Icon, initial } from '../../ui';
import {
  barRects,
  donutSlices,
  personBars,
  rankDesc,
  relativeWidths,
  stripParts,
  teamBar,
  windowIsHot,
} from './calc';
import {
  accountsLine,
  activeLine,
  limitLine,
  perPersonLong,
  perPersonShort,
  reachedLong,
  reachedShort,
  rub,
  spendLine,
  tasksLine,
  tileLabels,
  type Unit,
  unitCaption,
  unitsOf,
  unitValue,
  whoSub,
  forPeriod,
} from './fmt';
import type { OverviewPeriod } from '../../../org/api/overview';

/** The colours of the series, the board's: the people of no team are always the quiet grey. */
export const TEAM_COLORS = [
  '#8968f4',
  '#25a897',
  '#c8830b',
  '#3b82f6',
  '#e5484d',
  '#24ae56',
  '#d6409f',
] as const;
export const NO_TEAM_COLOR = '#7c7c86';
const TONE = {
  spend: '#8968f4',
  active: '#25a897',
  tasks: '#24ae56',
  subs: '#c8830b',
  bars: '#3b82f6',
} as const;
/** Below this a person has hardly worked in the period: their row is drawn quieter. */
const IDLE_BELOW = 2000;

export const css = (v: Record<string, string | number>): CSSProperties => v as CSSProperties;

// ── Frame ───────────────────────────────────────────────────────────────────────────────────────────────────────────

/** A card of the board: the heading with its description and actions, the body below. */
export const Card = ({
  label,
  title,
  description,
  actions,
  className,
  children,
}: {
  /** The accessible name of the region. */
  label: string;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className: string;
  children: ReactNode;
}) => (
  <div className={`t-card viz-card dash-tile ${className}`} role="region" aria-label={label}>
    <header className="viz-card__header">
      <div className="viz-card__heading">
        <div className="viz-card__title-row">
          <h3 className="viz-card__title">{title}</h3>
        </div>
        {description != null && <p className="viz-card__description">{description}</p>}
      </div>
      {actions != null && <div className="viz-card__actions">{actions}</div>}
    </header>
    <div className="viz-card__body">{children}</div>
  </div>
);

/** The heading of a card that leads to its section. */
export const TitleLink = ({ children, onClick }: { children: string; onClick: () => void }) => (
  <button type="button" className="mx-card-title" onClick={onClick}>
    {children}
  </button>
);

export const GoButton = ({ onClick, label }: { onClick: () => void; label: string }) => (
  <button type="button" className="mx-go" aria-label={label} onClick={onClick}>
    <Icon i="arrow-up-right" size="sm" />
  </button>
);

// ── Head tiles ──────────────────────────────────────────────────────────────────────────────────────────────────────

/** The last days as bars; the last bar is today and is drawn stronger. */
export const Bars = ({ values }: { values: readonly number[] }) => (
  <svg className="sx-bars" viewBox="0 0 100 40" preserveAspectRatio="none" aria-hidden="true">
    {barRects(values).map((r, i) => (
      <rect
        key={i}
        x={r.x.toFixed(2)}
        y={r.y.toFixed(2)}
        width={r.w.toFixed(2)}
        height={r.h.toFixed(2)}
        rx="1.2"
        data-recent={r.recent ? '' : undefined}
      />
    ))}
  </svg>
);

/** What is used and what is left, two parts of one strip. */
export const Strip = ({ used, whole, color }: { used: number; whole: number; color: string }) => {
  const p = stripParts(used, whole);
  return (
    <span className="sx-strip" aria-hidden="true">
      <span className="sx-strip__part" style={css({ '--sx-w': p.used, '--sx-c': color })} />
      <span className="sx-strip__part sx-strip__part--muted" style={css({ '--sx-w': p.rest })} />
    </span>
  );
};

export const SxTile = ({
  i,
  tone,
  icon,
  label,
  value,
  chart,
  onClick,
  member,
  hint,
}: {
  i: number;
  tone: string;
  icon: string;
  label: string;
  value: string;
  chart: ReactNode;
  onClick?: () => void;
  member?: boolean;
  /** How the number is counted; shown on hover. */
  hint?: string;
}) => {
  const body = (
    <>
      <span className="sx-tile__top">
        <span className="sx-tile__icon">
          <Icon i={icon} size="sm" />
        </span>
        {onClick && <Icon i="arrow-up-right" className="sx-tile__go" />}
      </span>
      <span className="sx-tile__chart">{chart}</span>
      <span className="sx-tile__label">{label}</span>
      <span className="sx-tile__value">
        <Count text={value} />
      </span>
    </>
  );
  const style = css({ '--sx-i': i, '--sx-tone': tone });
  return onClick ? (
    <button type="button" className="sx-tile" style={style} onClick={onClick} title={hint}>
      {body}
    </button>
  ) : (
    <div className={member ? 'sx-tile mo-tile' : 'sx-tile'} style={style} title={hint}>
      {body}
    </div>
  );
};

/** The four head tiles of an admin and a lead; each leads to the section that holds the numbers. */
export const HeadTiles = ({
  data,
  period,
  base,
  onSpend,
  onActive,
}: {
  data: OverviewData;
  period: OverviewPeriod;
  base: string;
  /** Opens the spend detail over the screen. */
  onSpend: () => void;
  /** Opens «Кто сегодня в работе» over the screen. */
  onActive: () => void;
}) => {
  const t = data.tiles;
  const names = tileLabels(t.basis ?? 'today', period);
  const go = (to: string) => () => navigate(`${base}/${to}`);
  const subs = t.subscriptions;
  return (
    <div className="sx-wrap">
      <nav className="sx-grid" aria-label={names.nav}>
        <SxTile
          i={0}
          tone={TONE.spend}
          icon="coins"
          label={names.spend}
          value={spendLine(t.spend, data.estimated)}
          chart={<Bars values={t.spend.bars} />}
          hint={`Сколько потрачено в рублях по прайс-листу провайдеров. ${names.scope}`}
          onClick={onSpend}
        />
        <SxTile
          i={1}
          tone={TONE.active}
          icon="users"
          label={names.active}
          value={activeLine(t.active.now, t.active.total)}
          chart={<Bars values={t.active.bars} />}
          hint={`Сколько людей запускали агентов. ${names.scope}`}
          onClick={onActive}
        />
        <SxTile
          i={2}
          tone={TONE.tasks}
          icon="list-checks"
          label={names.tasks}
          value={tasksLine(t.tasks.closed, t.tasks.inProgress)}
          chart={<Bars values={t.tasks.bars} />}
          hint={`Задачи агентов, дошедшие до готового MR, и те, что ещё в работе. ${names.scope}`}
          onClick={go('analytics?k=mrPerPerson')}
        />
        <SxTile
          i={3}
          tone={TONE.subs}
          icon="key-round"
          label="Подписки"
          value={accountsLine(subs.accounts, subs.worstName, subs.worstPct)}
          chart={
            subs.accounts === 0 ? (
              <span className="sx-strip sx-strip--empty" aria-hidden="true" />
            ) : (
              <Strip used={subs.worstPct} whole={100} color={TONE.subs} />
            )
          }
          hint="Подключённые подписки на агентов; в скобках самая заполненная по лимиту окна."
          onClick={go('accounts')}
        />
      </nav>
    </div>
  );
};

// ── «Требует внимания» ──────────────────────────────────────────────────────────────────────────────────────────────

const toneOfSignal = (s: Signal): 'risk' | 'warn' => (s.severity === 'critical' ? 'risk' : 'warn');

/** «Отложить»: the signal goes at once; if the hub refuses it comes back with the reason. */
const SNOOZE: { span: SnoozeFor; label: string; hint: string }[] = [
  { span: 'day', label: 'на день', hint: 'Скрыть сигнал на сутки. Если станет хуже, он вернётся раньше' },
  { span: 'week', label: 'на неделю', hint: 'Скрыть сигнал на 7 дней. Если станет хуже, он вернётся раньше' },
];

export const Attention = ({ signals, base }: { signals: Signal[]; base: string }) => {
  const { api } = useOrg();
  // The ids put off here: they leave the list before the hub answers.
  const [gone, setGone] = useState<ReadonlySet<string>>(new Set());
  const [failed, setFailed] = useState<{ id: string; text: string } | null>(null);
  // The last put off, so a wrong click is taken back on the spot.
  const [last, setLast] = useState<{ s: Signal; span: SnoozeFor; busy: boolean } | null>(null);
  const shown = signals.filter((s) => !gone.has(s.id));
  const back = (id: string) =>
    setGone((g) => {
      const n = new Set(g);
      n.delete(id);
      return n;
    });
  const put = (s: Signal, span: SnoozeFor) => {
    setFailed(null);
    setGone((g) => new Set(g).add(s.id));
    setLast({ s, span, busy: false });
    api.snoozeSignal(s.id, span).catch((e: unknown) => {
      back(s.id);
      setLast((l) => (l?.s.id === s.id ? null : l));
      setFailed({ id: s.id, text: `Не удалось отложить «${s.title}»: ${orgErrorText(e)}` });
    });
  };
  const undo = () => {
    if (!last || last.busy) return;
    const { s } = last;
    setFailed(null);
    setLast({ ...last, busy: true });
    api.unsnoozeSignal(s.id).then(
      () => {
        back(s.id);
        setLast(null);
      },
      (e: unknown) => {
        setLast((l) => (l ? { ...l, busy: false } : l));
        setFailed({ id: s.id, text: `Не удалось вернуть «${s.title}»: ${orgErrorText(e)}` });
      },
    );
  };
  return (
    <Card
      label="Требует внимания"
      className="dash-span-12 mx-attention"
      title={
        <span className="at-title">
          Требует внимания
          {shown.length > 0 && (
            <>
              {' '}
              <span className="at-title__count">{shown.length}</span>
            </>
          )}
        </span>
      }
      description="Что требует решения сегодня: кнопка ведёт к исправлению, «Отложить» прячет сигнал на время"
    >
      {last && (
        <p className="at-undo" role="status">
          <span>
            Сигнал «{last.s.title}» отложен {last.span === 'week' ? 'на неделю' : 'на день'}
          </span>
          <button type="button" className="at-undo__btn" disabled={last.busy} onClick={undo}>
            {last.busy ? 'Возвращаем' : 'Вернуть'}
          </button>
          <button type="button" className="at-undo__x" aria-label="Скрыть" onClick={() => setLast(null)}>
            <Icon i="x" size="sm" />
          </button>
        </p>
      )}
      {failed && (
        <p className="ov-error-line" role="alert">
          {failed.text}
        </p>
      )}
      {shown.length === 0 ? (
        <p className="ov-empty">Всё спокойно: сейчас ничего не требует внимания.</p>
      ) : (
        <div className="at-grid" style={css({ '--at-cols': 1 })}>
          {shown.map((s, i) => (
            <div
              key={s.id}
              className="at-cell"
              data-tone={toneOfSignal(s)}
              data-link={s.action ? '1' : undefined}
              style={css({ '--i': i, '--at-span': 1, '--j': 0, '--jr': 0 })}
              onClick={s.action ? () => goSignal(base, s.action!) : undefined}
            >
              <span className="at-cell__dot" aria-hidden="true" />
              <span className="at-cell__text">
                <b className="at-cell__title">{s.title}</b>
                <span className="at-cell__detail">{s.detail}</span>
              </span>
              <span className="at-cell__acts">
                {s.action && (
                  <button
                    type="button"
                    className="at-cell__action"
                    onClick={(e) => {
                      e.stopPropagation();
                      goSignal(base, s.action!);
                    }}
                  >
                    {s.action.label}
                    <Icon i="arrow-up-right" size="sm" />
                  </button>
                )}
                <span className="at-cell__snooze" role="group" aria-label="Отложить">
                  <span>Отложить</span>
                  {SNOOZE.map((o) => (
                    <button
                      key={o.span}
                      type="button"
                      title={o.hint}
                      onClick={(e) => {
                        e.stopPropagation();
                        put(s, o.span);
                      }}
                    >
                      {o.label}
                    </button>
                  ))}
                </span>
              </span>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
};

// ── The donut «Расход по командам» ──────────────────────────────────────────────────────────────────────────────────

const unitAmount = (unit: Unit, v: { rub: number; usd: number; tokens: number }): number =>
  unit === 'tok' ? v.tokens : unit === 'usd' ? v.usd : v.rub;

export const SpendDonut = ({
  data,
  period,
  base,
  onSpend,
}: {
  data: NonNullable<OverviewData['byTeam']>;
  period: OverviewPeriod;
  base: string;
  onSpend: () => void;
}) => {
  const units = unitsOf();
  const [picked, setPicked] = useState<Unit | null>(null);
  const unit = picked && units.some((u) => u.id === picked) ? picked : (units[0]?.id ?? 'usd');
  const [hov, setHov] = useState<string | null>(null);
  const teams = useMemo(
    () => [...data.teams].sort((a, b) => Number(a.noTeam ?? false) - Number(b.noTeam ?? false)),
    [data.teams],
  );
  const colorOf = (i: number) => (teams[i]?.noTeam ? NO_TEAM_COLOR : TEAM_COLORS[i % TEAM_COLORS.length]);
  // The slices of every unit are drawn once; the switch only shows another group.
  const rings = Object.fromEntries(
    units.map((u) => [u.id, donutSlices(teams.map((t) => unitAmount(u.id, t)))]),
  ) as Record<Unit, ReturnType<typeof donutSlices>>;
  const ring = rings[unit];
  const open = (id: string, noTeam?: boolean) =>
    navigate(noTeam ? `${base}/teams` : `${base}/team?t=${encodeURIComponent(id)}`);
  const hot = hov === null ? null : teams.findIndex((t) => t.id === hov);
  const big = hot === null || hot < 0 ? unitValue(unit, data.total) : unitValue(unit, teams[hot]!);
  const cap =
    hot === null || hot < 0 ? unitCaption(unit, period) : `${teams[hot]!.name} · ${ring[hot]?.pct ?? 0}%`;
  const st = (id: string) => (hov === null ? undefined : hov === id ? 'on' : 'dim');
  const rowKey = (e: KeyboardEvent, fn: () => void) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      fn();
    }
  };
  return (
    <Card
      label="Расход по командам"
      className="dash-span-12"
      title={<TitleLink onClick={onSpend}>Расход по командам</TitleLink>}
      actions={
        <div className="hx-set__seg hb-unit" role="radiogroup" aria-label="В чём считать">
          {units.map((u) => (
            <button
              key={u.id}
              type="button"
              className="hx-set__seg-item"
              role="radio"
              aria-checked={u.id === unit}
              onClick={() => setPicked(u.id)}
            >
              {u.label}
            </button>
          ))}
        </div>
      }
    >
      <div className="viz-donut viz-donut--semi">
        <div className="viz-donut__body" onPointerLeave={() => setHov(null)}>
          <svg className="viz-donut__ring" viewBox="0 0 200 108" role="img" aria-label="Расход по командам">
            <path d="M 22.00 100.00 A 78 78 0 0 1 178.00 100.00" className="viz-donut__track" />
            {units.map((u) => (
              <g key={u.id} className="hb-u" data-show={u.id === unit ? 'true' : 'false'}>
                {rings[u.id].map(
                  (s, i) =>
                    s.d && (
                      <path
                        key={teams[i]!.id}
                        d={s.d}
                        fill={colorOf(i)}
                        className="viz-donut__slice"
                        data-st={st(teams[i]!.id)}
                        onPointerEnter={() => setHov(teams[i]!.id)}
                        onClick={() => open(teams[i]!.id, teams[i]!.noTeam)}
                      />
                    ),
                )}
              </g>
            ))}
            <g className="viz-donut__center">
              <g className="viz-donut__fade">
                <foreignObject x="0" y="52" width="200" height="34">
                  <div className="viz-donut__big-html">{big}</div>
                </foreignObject>
                <foreignObject x="0" y="88" width="200" height="14">
                  <div className="hb-donut-cap">{cap}</div>
                </foreignObject>
              </g>
            </g>
          </svg>
          <dl className="viz-sheet-summary__rows viz-donut__rows">
            {teams.map((t, i) => (
              <div
                key={t.id}
                className="viz-sheet-summary__row viz-donut__row"
                data-st={st(t.id)}
                title={`${t.name}: ${ring[i]?.pct ?? 0}% всего расхода по прайс-листу провайдеров. Нажмите, чтобы открыть команду.`}
                role="button"
                tabIndex={0}
                onPointerEnter={() => setHov(t.id)}
                onFocus={() => setHov(t.id)}
                onBlur={() => setHov(null)}
                onClick={() => open(t.id, t.noTeam)}
                onKeyDown={(e) => rowKey(e, () => open(t.id, t.noTeam))}
              >
                <dt>
                  <span className="viz-donut__label">
                    <svg className="viz-swatch" viewBox="0 0 10 10" aria-hidden="true">
                      <circle cx="5" cy="5" r="4" fill={colorOf(i)} stroke={colorOf(i)} strokeWidth="1.5" />
                    </svg>
                    {t.name}
                  </span>
                </dt>
                <dd>
                  <span className="viz-donut__share">{ring[i]?.pct ?? 0}%</span>
                  <b>{unitValue(unit, t)}</b>
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </Card>
  );
};

// ── «Эффективность» ─────────────────────────────────────────────────────────────────────────────────────────────────

export const Efficiency = ({
  rows,
  base,
  period,
}: {
  rows: TeamEfficiency[];
  base: string;
  period: OverviewPeriod;
}) => {
  const order = rankDesc(rows.map((r) => r.tasksPerPerson));
  const widths = relativeWidths(order.map((i) => rows[i]!.tasksPerPerson));
  return (
    <Card
      label="Эффективность"
      className="dash-span-6"
      title={<TitleLink onClick={() => navigate(`${base}/analytics`)}>Эффективность</TitleLink>}
      description={`Закрытые задачи на человека ${forPeriod(period)}`}
      actions={<GoButton label="Подробнее" onClick={() => navigate(`${base}/analytics`)} />}
    >
      {rows.length === 0 ? (
        <p className="ov-empty">Закрытых задач за период ещё нет.</p>
      ) : (
        <ul className="dash-bars">
          {order.map((idx, n) => {
            const r = rows[idx]!;
            return (
              <li
                key={r.id}
                className={r.noTeam ? 'dash-bar dash-bar--muted' : 'dash-bar'}
                data-action="true"
              >
                <button
                  type="button"
                  className="dash-bar__hit"
                  title={`Закрытые задачи, делённые на число людей команды ${forPeriod(period)}. Процент у названия: сколько из задуманного команда закрыла. Нажмите, чтобы открыть команду.`}
                  onClick={() =>
                    navigate(r.noTeam ? `${base}/teams` : `${base}/team?t=${encodeURIComponent(r.id)}`)
                  }
                >
                  <span
                    className="dash-bar__fill"
                    aria-hidden="true"
                    style={css({ '--dash-w': widths[n]! })}
                  />
                  <span className="dash-bar__label">
                    {r.name}
                    <small>{n === 0 ? reachedLong(r.resultPct) : reachedShort(r.resultPct)}</small>
                  </span>
                  <span className="dash-bar__value">
                    {n === 0 ? perPersonLong(r.tasksPerPerson) : perPersonShort(r.tasksPerPerson)}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
};

// ── «Подписки» ──────────────────────────────────────────────────────────────────────────────────────────────────────

export const Subscriptions = ({ items, base }: { items: SubscriptionUse[]; base: string }) => {
  const order = rankDesc(items.map((s) => s.usedPct));
  const widths = relativeWidths(order.map((i) => items[i]!.usedPct));
  const go = () => navigate(`${base}/accounts`);
  const one = (id: string) => () => navigate(`${base}/accounts?a=${encodeURIComponent(id)}`);
  return (
    <div className="hb-one">
      <Card
        label="Подписки"
        className="dash-span-6"
        title={<TitleLink onClick={go}>Подписки</TitleLink>}
        description="Сколько недельного окна общих аккаунтов уже израсходовано"
        actions={<GoButton label="Подробнее" onClick={go} />}
      >
        {items.length === 0 ? (
          <p className="ov-empty">Общих аккаунтов нет.</p>
        ) : (
          <ul className="dash-bars">
            {order.map((idx, n) => {
              const s = items[idx]!;
              const sub = [s.teams.join(', '), s.resetsOn ? `сброс в ${s.resetsOn}` : '']
                .filter(Boolean)
                .join(' · ');
              return (
                <li
                  key={s.id}
                  className={windowIsHot(s.usedPct) ? 'dash-bar dash-bar--rose' : 'dash-bar'}
                  data-action="true"
                >
                  <button
                    type="button"
                    className="dash-bar__hit"
                    title="Какая часть недельного окна подписки уже израсходована всеми, кто ею пользуется. Нажмите, чтобы открыть аккаунт."
                    onClick={one(s.id)}
                  >
                    <span
                      className="dash-bar__fill"
                      aria-hidden="true"
                      style={css({ '--dash-w': widths[n]! })}
                    />
                    <span className="dash-bar__label">
                      {s.name}
                      {sub && <small>{sub}</small>}
                    </span>
                    <span className="dash-bar__value">{s.usedPct}%</span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </Card>
    </div>
  );
};

// ── «Расход по людям» of a lead ─────────────────────────────────────────────────────────────────────────────────────

export const PeopleSpend = ({
  data,
  base,
  period,
}: {
  data: NonNullable<OverviewData['byPerson']>;
  base: string;
  period: OverviewPeriod;
}) => {
  const [now] = useState(() => Date.now());
  const team = teamBar(data.spentRub, data.limitRub);
  const bars = personBars(
    data.people.map((p) => ({ userId: p.userId, spent: p.spentRub, limit: p.limitRub })),
    IDLE_BELOW,
  );
  const byId = new Map<string, PersonSpend>(data.people.map((p) => [p.userId, p]));
  return (
    <Card
      label="Расход по людям"
      className="dash-span-12 ld"
      title={<span className="mx-card-title">Расход по людям</span>}
      description={`Команда ${data.teamName} ${forPeriod(period)} · личные лимиты ставишь ты, нажми на человека`}
    >
      <div className="ld-team">
        <div className="ld-team__big">
          <b>
            {/* the board's total sits in a span, which `.ld-team__big span` draws small and grey; the 28px of the line stays */}
            <span>{rub(data.spentRub)}</span>
          </b>
          <span>
            {team && data.limitRub !== null
              ? `из ${rub(data.limitRub)} лимита команды · ${team.pct}%`
              : 'у команды нет своего лимита'}
          </span>
        </div>
        {team && (
          <div className="ld-team__bar" data-tone={team.tone}>
            <i style={css({ '--w': team.w })} />
          </div>
        )}
      </div>
      <div className="ld-head">
        <span>Человек</span>
        <span>Потрачено и личный лимит</span>
        <span />
      </div>
      {bars.length === 0 ? (
        <p className="ov-empty">В команде пока никого нет.</p>
      ) : (
        <ul className="ld-list">
          {bars.map((b) => {
            const p = byId.get(b.userId)!;
            const hot = b.hot ? '1' : '0';
            return (
              <li key={b.userId}>
                <button
                  type="button"
                  className="ld-row"
                  title="Потрачено за период по прайс-листу провайдеров и личный лимит на месяц. Нажмите, чтобы открыть человека и поменять лимит."
                  data-idle={b.idle ? '1' : '0'}
                  onClick={() => navigate(`${base}/teams?p=${encodeURIComponent(p.userId)}`)}
                >
                  <span className="ld-who">
                    <span className="hx-avatar ld-av">{initial(p.name)}</span>
                    <span>
                      <b>{p.name}</b>
                      <small>{whoSub(p, now)}</small>
                    </span>
                  </span>
                  <span className="ld-bar" data-hot={hot}>
                    <i style={css({ '--w': b.w })} />
                    {b.cap && <span className="ld-cap" style={css({ '--x': b.cap })} />}
                  </span>
                  <span className="ld-num" data-hot={hot}>
                    <b>{p.spentRub > 0 ? rub(p.spentRub) : '—'}</b>
                    <small>{limitLine(p.limitRub, b.share)}</small>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
};

// ── Loading and failure ─────────────────────────────────────────────────────────────────────────────────────────────

/** Where the tiles and cards will be: the shapes of the screen, so nothing jumps when the numbers arrive. */
export const Skeleton = ({ member }: { member: boolean }) => (
  <div className="ov-sk" role="status" aria-label="Загрузка">
    <div className="sx-grid">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="ov-sk__tile" />
      ))}
    </div>
    {member ? (
      <div className="me-grid">
        <div className="ov-sk__card" />
        <div className="ov-sk__card" />
      </div>
    ) : (
      <>
        <div className="ov-sk__card ov-sk__card--short" />
        <div className="ov-sk__card" />
      </>
    )}
  </div>
);

export const LoadFailed = ({ error, retry }: { error: unknown; retry: () => void }) => (
  <div className="ov-err" role="alert">
    <b>Не загрузилось</b>
    <span>{orgErrorText(error)}</span>
    <Btn size="sm" onClick={retry}>
      Повторить
    </Btn>
  </div>
);
