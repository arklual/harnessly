import { shownSymbol } from '../../money';
import { monthWord } from '../../dates';
import { useState } from 'react';

import { Link } from '../../../lib/router';
import type { OverviewData } from '../../api/overview';
import { ago, awayFor, rub, thousands } from './format';
import { LimitBar } from './svg';

/** «Расход по людям» (a lead's): the team's budget, then each person's spend against their personal limit. */
export const PeopleSpend = ({
  data,
  base,
}: {
  data: NonNullable<OverviewData['byPerson']>;
  base: string;
}) => {
  const [now] = useState(() => Date.now());
  const scale = Math.max(...data.people.map((p) => Math.max(p.spentRub, p.limitRub ?? 0)), 1);
  const teamPct = data.limitRub ? Math.round((data.spentRub / data.limitRub) * 100) : null;
  const people = [...data.people].sort((a, b) => (b.spentRub > 0 ? 1 : 0) - (a.spentRub > 0 ? 1 : 0));
  return (
    <section className="ov-card" aria-labelledby="ov-people">
      <h2 id="ov-people" className="ov-card__title">
        Расход по людям
      </h2>
      <p className="ov-card__sub">
        Команда {data.teamName} за {monthWord()} · личные лимиты ставишь ты, нажми на человека
      </p>
      <p className="ov-teamline">
        <b>{rub(data.spentRub)}</b>
        {data.limitRub ? ` из ${rub(data.limitRub)} лимита команды · ${teamPct}%` : ' · лимита команды нет'}
      </p>
      {data.limitRub !== null && (
        <LimitBar
          spent={data.spentRub}
          limit={null}
          scale={data.limitRub}
          tone={(teamPct ?? 0) >= 80 ? 'bad' : 'ok'}
        />
      )}
      <div className="ov-people" role="table" aria-label="Расход по людям">
        <div className="ov-people__head" role="row">
          <span>Человек</span>
          <span>Потрачено и личный лимит</span>
        </div>
        {people.map((p) => {
          const pct = p.limitRub ? Math.round((p.spentRub / p.limitRub) * 100) : 0;
          const away = p.lastActiveAt ? awayFor(p.lastActiveAt, now) : null;
          const sub = p.invited
            ? 'приглашена, ещё не зашла'
            : `${p.isLead ? 'лид · ' : ''}${away ?? ago(p.lastActiveAt, now)}`;
          return (
            <Link key={p.userId} to={`${base}/teams?person=${p.userId}`} className="ov-person" role="row">
              <span className="ov-person__who">
                <span className="ho-mark ho-mark--dark">{p.name.slice(0, 1)}</span>
                <span className="ov-row__text">
                  <b>{p.name}</b>
                  <small>{sub}</small>
                </span>
              </span>
              <LimitBar spent={p.spentRub} limit={p.limitRub} scale={scale} tone={pct >= 80 ? 'bad' : 'ok'} />
              <span className="ov-person__sum" data-tone={pct >= 80 ? 'bad' : 'ok'}>
                <b>{p.spentRub > 0 ? rub(p.spentRub) : '—'}</b>
                <small>
                  {p.limitRub
                    ? `из ${thousands(p.limitRub)} ${shownSymbol()} · ${pct}%`
                    : 'без личного лимита'}
                </small>
              </span>
            </Link>
          );
        })}
      </div>
    </section>
  );
};
