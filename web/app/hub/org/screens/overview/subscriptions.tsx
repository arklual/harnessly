import type { SubscriptionUse } from '../../api/overview';
import { FillRow } from './svg';
import { toneOf } from './format';

/** «Подписки»: how much of the weekly window of each shared account is used. */
export const Subscriptions = ({ items }: { items: SubscriptionUse[] }) => (
  <section className="ov-card" aria-labelledby="ov-subs">
    <h2 id="ov-subs" className="ov-card__title">
      Подписки
    </h2>
    <p className="ov-card__sub">Сколько недельного окна общих аккаунтов уже израсходовано</p>
    <div className="ov-rows">
      {items.map((s) => (
        <FillRow key={s.id} pct={s.usedPct} tone={toneOf(s.usedPct) === 'bad' ? 'bad' : 'plain'}>
          <span className="ov-row__text">
            <b>{s.name}</b>
            <small>
              {s.teams.join(', ')} · сброс в {s.resetsOn}
            </small>
          </span>
          <span className="ov-row__val">{s.usedPct}%</span>
        </FillRow>
      ))}
    </div>
  </section>
);
