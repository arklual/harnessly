// «Куда ушли деньги»: the spend of the scope cut by team, person, model or account, for today, the week or the month.
// Opens from the «Расход» tile and the donut's title; the cut, the span and the highlighted row live in the address
// (`?spend=team&span=day&hl=…`), so a link from a signal lands on the same view and Back closes it.
import { useState } from 'react';

import { navigate } from '../../../lib/router';
import { orgErrorText } from '../../../org/api/errors';
import type { SpendCut, SpendDetail, SpendLine, SpendSpan } from '../../../org/api/overview';
import { useOrg } from '../../../org/context';
import { useLoad } from '../../../org/hooks';
import { Btn, Drawer } from '../../ui';
import { useUrl } from '../teams/url';
import { relativeWidths } from './calc';
import { css } from './parts';
import { kRub, plural, rub, tokens } from './fmt';

export const SPEND_CARDS = ['spend', 'span', 'hl'] as const;

const CUTS: { id: SpendCut; label: string; lead: boolean }[] = [
  { id: 'team', label: 'Команды', lead: false },
  { id: 'person', label: 'Люди', lead: true },
  { id: 'model', label: 'Модели', lead: true },
  { id: 'account', label: 'Аккаунты', lead: true },
];

const SPANS: { id: SpendSpan; label: string }[] = [
  { id: 'day', label: 'Сегодня' },
  { id: 'week', label: 'Неделя' },
  { id: 'month', label: 'Месяц' },
];

const SPAN_WORD: Record<SpendSpan, string> = { day: 'сегодня', week: 'за неделю', month: 'за месяц' };

const isCut = (v: string | null): v is SpendCut => CUTS.some((c) => c.id === v);
const isSpan = (v: string | null): v is SpendSpan => SPANS.some((s) => s.id === v);

const Seg = <T extends string>({
  items,
  value,
  label,
  onPick,
}: {
  items: { id: T; label: string }[];
  value: T;
  label: string;
  onPick: (v: T) => void;
}) => (
  <div className="hx-set__seg" role="radiogroup" aria-label={label}>
    {items.map((o) => (
      <button
        key={o.id}
        type="button"
        className="hx-set__seg-item"
        role="radio"
        aria-checked={o.id === value}
        onClick={() => onPick(o.id)}
      >
        {o.label}
      </button>
    ))}
  </div>
);

const callsText = (n: number) => `${n.toLocaleString('ru-RU')} ${plural(n, 'вызов', 'вызова', 'вызовов')}`;

/** The second line of a row: what the amount is made of and how close the limit is. */
const rowSub = (r: SpendLine, span: SpendSpan): string =>
  [
    `${tokens(r.tokens)} токенов`,
    callsText(r.calls),
    r.limit ? `${r.limit.pct}% лимита ${kRub(r.limit.rub)}` : '',
    r.forecastRub !== null && span !== 'day' ? `к концу месяца ≈ ${rub(r.forecastRub)}` : '',
    r.unpricedCalls > 0 ? `${r.unpricedCalls} без цены` : '',
  ]
    .filter(Boolean)
    .join(' · ');

/** Where a row leads: the team's page, the person's card, the account's card; a model has no page. */
const rowHref = (base: string, by: SpendCut, key: string): string | null => {
  if (!key) return null;
  const k = encodeURIComponent(key);
  if (by === 'team') return `${base}/team?t=${k}`;
  if (by === 'person') return `${base}/teams?p=${k}`;
  if (by === 'account') return `${base}/accounts?a=${k}`;
  return null;
};

const timeOf = (iso: string | null): string =>
  iso ? new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }) : '';

const Rows = ({ d, base, hl }: { d: SpendDetail; base: string; hl: string | null }) => {
  if (d.rows.length === 0) return <p className="md-note">{`Расхода ${SPAN_WORD[d.span]} ещё нет.`}</p>;
  const widths = relativeWidths(d.rows.map((r) => r.rub || r.tokens / 1e6));
  return (
    <ul className="dash-bars sp-rows">
      {d.rows.map((r, i) => {
        const href = rowHref(base, d.by, r.key);
        const tone = r.limit?.status === 'stop' ? ' dash-bar--rose' : !r.key ? ' dash-bar--muted' : '';
        return (
          <li
            key={r.key || `none-${i}`}
            className={`dash-bar${tone}`}
            data-action="true"
            data-hl={hl && hl === r.key ? '1' : undefined}
          >
            <button
              type="button"
              className="dash-bar__hit"
              disabled={!href}
              onClick={() => href && navigate(href)}
            >
              <span className="dash-bar__fill" aria-hidden="true" style={css({ '--dash-w': widths[i]! })} />
              <span className="dash-bar__label">
                {r.label}
                <small>{rowSub(r, d.span)}</small>
              </span>
              <span className="dash-bar__value">
                {r.rub > 0 || r.unpricedCalls === 0 ? rub(r.rub) : '—'}
                {r.estimated && <small> оценка</small>}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
};

const Body = ({ by, span, hl }: { by: SpendCut; span: SpendSpan; hl: string | null }) => {
  const { base } = useOrg();
  const { data, error, reload } = useLoad((api) => api.spendDetail(by, span), [by, span]);
  const [kept, setKept] = useState<SpendDetail | null>(data);
  if (data && data !== kept) setKept(data);
  const d = data ?? kept;
  if (error && !data)
    return (
      <div className="md-sec">
        <p className="md-note tm-err" role="alert">
          {orgErrorText(error)}
        </p>
        <Btn variant="outline" size="sm" onClick={reload}>
          Повторить
        </Btn>
      </div>
    );
  if (!d)
    return (
      <div className="md-sec" aria-busy="true">
        <div className="hx-sk" style={{ height: 56, marginBottom: 12 }} />
        <div className="hx-sk" style={{ height: 220 }} />
      </div>
    );
  const unpriced = d.rows.reduce((n, r) => n + r.unpricedCalls, 0);
  return (
    <div data-busy={data ? undefined : '1'}>
      <div className="md-res ps-stats">
        <div>
          <b>{rub(d.total.rub)}</b>
          <small>{`по прайсу API ${SPAN_WORD[d.span]}`}</small>
        </div>
        <div>
          <b>{d.total.calls.toLocaleString('ru-RU')}</b>
          <small>{plural(d.total.calls, 'вызов модели', 'вызова модели', 'вызовов модели')}</small>
        </div>
        <div>
          <b>
            {d.limit
              ? `${d.limit.pct}%`
              : d.forecastRub !== null
                ? `≈ ${kRub(d.forecastRub)}`
                : tokens(d.total.tokens)}
          </b>
          <small>
            {d.limit ? `лимита ${kRub(d.limit.rub)}` : d.forecastRub !== null ? 'к концу месяца' : 'токенов'}
          </small>
        </div>
      </div>
      <div className="md-sec">
        <Rows d={d} base={base} hl={hl} />
        <p className="md-note">
          {[
            unpriced > 0
              ? `${callsText(unpriced)} без цены модели — их стоимость не посчитана, цену задаёт администратор хаба.`
              : '',
            d.accountedUntil ? `Учтено по ${timeOf(d.accountedUntil)}.` : '',
            'Всё считается по прайсу API: токены × цена модели. Абонентская плата подписок — в «Аккаунтах».',
          ]
            .filter(Boolean)
            .join(' ')}
        </p>
      </div>
    </div>
  );
};

/** The drawer itself; mounted by «Обзор» and open while `?spend=` is in the address. */
export const SpendDrawer = ({ lead, defaultSpan }: { lead: boolean; defaultSpan: SpendSpan }) => {
  const url = useUrl(SPEND_CARDS);
  const raw = url.get('spend');
  const cuts = CUTS.filter((c) => !lead || c.lead);
  const open = raw !== null;
  const by: SpendCut = isCut(raw) && cuts.some((c) => c.id === raw) ? raw : cuts[0]!.id;
  const s = url.get('span');
  const span: SpendSpan = isSpan(s) ? s : defaultSpan;
  return (
    <Drawer
      open={open}
      onClose={url.close}
      className="md sp"
      label="Расход"
      title="Куда ушли деньги"
      sub={`${cuts.find((c) => c.id === by)!.label.toLowerCase()} · ${SPAN_WORD[span]}`}
    >
      <div className="md-sec sp-tools">
        <Seg items={cuts} value={by} label="Разрез" onPick={(v) => url.put({ spend: v, hl: null })} />
        <Seg items={SPANS} value={span} label="Период" onPick={(v) => url.put({ span: v })} />
      </div>
      {open && <Body by={by} span={span} hl={url.get('hl')} />}
    </Drawer>
  );
};

/** Opens the drawer on a cut (the tile opens the first one). */
export const useOpenSpend = () => {
  const url = useUrl(SPEND_CARDS);
  return (by?: SpendCut) => url.open({ spend: by ?? 'team' });
};
