// The lower sections of Аналитика: changes and their effect, what needs attention, where money is wasted, who is next
// to whom. Each takes what the API gave and draws it; none invents a number.
import { Cpu, Layers, Plug } from 'lucide-react';

import { Link } from '../../../lib/router';
import type { Change, ChangeKind, Effect, Peers, Signal, Verdict, Waste } from '../../api/analytics';
import { dayOfMonth, monthShort } from '../../dates';
import { formatValue } from './format';

const KIND_ICON: Record<ChangeKind, typeof Plug> = { tool: Plug, model: Cpu, plugins: Layers };

const VERDICT_TEXT: Record<Verdict, string> = {
  better: 'лучше',
  worse: 'хуже',
  noise: 'не отличить от шума',
};

const EffectRow = ({ e }: { e: Effect }) => {
  const top = Math.max(e.before, e.after, 1e-9);
  return (
    <li className="an-eff">
      <span className="an-eff__name">{e.title}</span>
      <svg className="an-eff__bars" data-verdict={e.verdict} width="100%" height="14" aria-hidden="true">
        <rect className="an-eff__before" width={`${(e.before / top) * 100}%`} height="5" rx="2.5" />
        <rect className="an-eff__after" y="8" width={`${(e.after / top) * 100}%`} height="5" rx="2.5" />
      </svg>
      <span className="an-eff__nums">
        <span className="an-eff__from">{formatValue(e.format, e.before)}</span>
        <span aria-hidden="true">→</span>
        <b>{formatValue(e.format, e.after)}</b>
      </span>
      <span className="an-verdict" data-verdict={e.verdict}>
        {VERDICT_TEXT[e.verdict]}
      </span>
    </li>
  );
};

export const Changes = ({ changes }: { changes: Change[] }) => (
  <section className="an-card" aria-labelledby="an-changes">
    <h2 id="an-changes" className="an-card__title">
      Изменения и их эффект
    </h2>
    <p className="an-card__sub">
      Что меняли в окружении и как сдвинулись метрики: неделя до против недели после
    </p>
    {changes.length === 0 ? (
      <p className="an-empty">За период в окружении ничего не меняли.</p>
    ) : (
      <ol className="an-changes">
        {changes.map((c) => {
          const Icon = KIND_ICON[c.kind];
          return (
            <li key={c.id} className="an-change">
              <span className="an-change__icon" data-kind={c.kind}>
                <Icon size={14} />
              </span>
              <div className="an-change__body">
                <p className="an-change__head">
                  <b>{c.title}</b>
                  <small>
                    {dayOfMonth(c.at)} {monthShort(c.at)} · {c.meta}
                  </small>
                </p>
                <ul className="an-effs">
                  {c.effects.map((e) => (
                    <EffectRow key={e.kpi} e={e} />
                  ))}
                </ul>
              </div>
            </li>
          );
        })}
      </ol>
    )}
  </section>
);

export const Signals = ({ signals, base }: { signals: Signal[]; base: string }) => (
  <section className="an-card" aria-labelledby="an-signals">
    <h2 id="an-signals" className="an-card__title">
      На что обратить внимание
    </h2>
    <ul className="an-signals">
      {signals.map((s) => (
        <li key={s.id} className="an-signal" data-tone={s.tone}>
          <i className="an-signal__dot" aria-hidden="true" />
          <div className="an-signal__text">
            <b>{s.title}</b>
            <span>{s.text}</span>
          </div>
          {s.action && (
            <Link to={`${base}/${s.action.section}`} className="an-signal__go">
              {s.action.label}
            </Link>
          )}
        </li>
      ))}
    </ul>
  </section>
);

const money = (n: number) => `$${new Intl.NumberFormat('ru-RU').format(n)}`;

export const WasteCard = ({ waste }: { waste: Waste }) => (
  <section className="an-card" aria-labelledby="an-waste">
    <h2 id="an-waste" className="an-card__title">
      Куда уходят деньги впустую
    </h2>
    <p className="an-card__sub">
      {money(waste.wasteUsd)} из {money(waste.totalUsd)} за период ·{' '}
      {Math.round((waste.wasteUsd / waste.totalUsd) * 100)}% расхода. Наведите на строку — откуда цифра
    </p>
    <svg className="an-waste__bar" width="100%" height="8" aria-hidden="true">
      {waste.items.map((w, i) => {
        const before = waste.items.slice(0, i).reduce((s, x) => s + x.usd, 0);
        return (
          <rect
            key={w.id}
            data-tone={w.tone}
            x={`${(before / waste.wasteUsd) * 100}%`}
            width={`${(w.usd / waste.wasteUsd) * 100 - 0.4}%`}
            height="8"
            rx="2"
          />
        );
      })}
    </svg>
    <ul className="an-waste">
      {waste.items.map((w) => (
        <li key={w.id} className="an-waste__row" title={w.text}>
          <i className="an-waste__dot" data-tone={w.tone} aria-hidden="true" />
          <div className="an-waste__text">
            <b>{w.title}</b>
            <span>{w.text}</span>
          </div>
          <div className="an-waste__sum">
            <b>{money(w.usd)}</b>
            <span>{Math.round((w.usd / waste.totalUsd) * 100)}% расхода</span>
          </div>
        </li>
      ))}
    </ul>
  </section>
);

export const PeersCard = ({ peers }: { peers: Peers }) => (
  <section className="an-card" aria-labelledby="an-peers">
    <h2 id="an-peers" className="an-card__title">
      {peers.title}
    </h2>
    <p className="an-card__sub">{peers.sub}</p>
    <div className="an-peers__scroll">
      <table className="an-peers">
        <thead>
          <tr>
            <th scope="col">
              <span className="an-sr">Строка</span>
            </th>
            {peers.columns.map((c) => (
              <th key={c} scope="col">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {peers.rows.map((r) => (
            <tr key={r.id}>
              <th scope="row">{r.name}</th>
              {r.cells.map((c, i) => (
                <td key={i} data-lv={Math.round(c.score * 2)}>
                  {c.text}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
    <p className="an-scale" aria-hidden="true">
      хуже среднего
      <i data-lv="-2" />
      <i data-lv="-1" />
      <i data-lv="0" />
      <i data-lv="1" />
      <i data-lv="2" />
      лучше среднего
    </p>
  </section>
);
