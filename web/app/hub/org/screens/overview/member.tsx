// The participant's Обзор (HRN-541): only their own work. No other people, no team money.
import { useState } from 'react';
import { GitPullRequest, MessagesSquare, Send, Server } from 'lucide-react';

import { Button, Spinner } from '@kit';

import { orgErrorText } from '../../api/errors';
import type { MyChat, MyOverview } from '../../api/member';
import { useOrg } from '../../context';
import { monthWord } from '../../dates';
import { useAction, useLoad } from '../../hooks';
import { Loading, LoadError, Page, PageHead, Seg } from '../../ui/page';
import { MiniBars, ProgressLine, Tile } from '../../ui/tile';
import { ago, num, resetText, rub, thousands, ESTIMATE_NOTE } from './format';
import { shownSymbol } from '../../money';
import { LimitBar } from './svg';

const PERIODS = [
  { value: 'week', label: 'Неделя' },
  { value: 'month', label: 'Месяц' },
] as const;

const END: Record<MyChat['end'], string> = {
  running: 'идёт',
  merged: 'смёржен',
  review: 'на ревью',
  abandoned: 'брошен без MR',
};

const endText = (c: MyChat): string => {
  if (c.mr && c.end === 'merged') return `MR ${c.mr} смёржен`;
  if (c.mr && c.end === 'review') return `MR ${c.mr} на ревью`;
  return END[c.end];
};

const Breakdown = ({ items }: { items: MyOverview['breakdown'] }) => {
  const total = items.reduce((s, b) => s + b.rub, 0) || 1;
  return (
    <ul className="ov-legend">
      {items.map((b, i) => (
        <li key={b.label}>
          <span className="ov-legend__dot" data-tone={i} aria-hidden="true" />
          <span className="ov-legend__name">{b.label}</span>
          <b className="ov-legend__val">{rub(b.rub)}</b>
          <span className="ov-legend__pct">{num((b.rub / total) * 100, 0)}%</span>
        </li>
      ))}
    </ul>
  );
};

/** No rule binds the person: only what they spent. */
const NoLimit = ({ data }: { data: MyOverview }) => (
  <section className="ov-card" aria-labelledby="mo-limit">
    <h2 id="mo-limit" className="ov-card__title">
      Мой расход за {monthWord()}
    </h2>
    <p className="ov-card__sub">Личного лимита нет</p>
    <p className="mo-big">
      <b>{rub(data.spentRub)}</b>
    </p>
    {data.breakdown.length > 0 && (
      <>
        <h3 className="mo-sub">На что ушло</h3>
        <Breakdown items={data.breakdown} />
      </>
    )}
  </section>
);

const Limit = ({ data }: { data: NonNullable<MyOverview['limit']> }) => {
  const [now] = useState(() => Date.now());
  const ask = useAction((api) => api.requestMoreLimit());
  const [sent, setSent] = useState(false);
  const pct = Math.round((data.spentRub / data.limitRub) * 100);
  const left = data.limitRub - data.spentRub;
  const request = async () => {
    const r = await ask.run();
    if (r?.sent) setSent(true);
  };
  return (
    <section className="ov-card" aria-labelledby="mo-limit">
      <h2 id="mo-limit" className="ov-card__title">
        Мой лимит на {monthWord()}
      </h2>
      <p className="ov-card__sub">
        Лимит поставил {data.setBy.name}. Сбросится {resetText(data.resetsAt, now)}
      </p>
      <p className="mo-big">
        <b>{rub(data.spentRub)}</b>
        <span>
          из {thousands(data.limitRub)} {shownSymbol()} · осталось {thousands(left)}
        </span>
      </p>
      <LimitBar
        spent={data.spentRub}
        limit={(data.limitRub * data.warnPct) / 100}
        scale={data.limitRub}
        tone={pct >= data.warnPct ? 'bad' : 'ok'}
      />
      <p className="mo-legend">
        <span className="mo-key mo-key--spent">потрачено</span>
        <span className="mo-key mo-key--forecast">к концу месяца ≈ {thousands(data.forecastRub)}</span>
        {data.mode !== 'off' && (
          <span className="mo-key mo-key--warn">на {data.warnPct}% придёт предупреждение</span>
        )}
      </p>
      {data.breakdown.length > 0 && (
        <>
          <h3 className="mo-sub">На что ушло</h3>
          <Breakdown items={data.breakdown} />
        </>
      )}
      {data.mode === 'stop' && (
        <p className="mo-note">
          Когда лимит кончится, агенты встанут до {new Date(data.resetsAt).getDate()}-го числа
        </p>
      )}
      {sent ? (
        <p className="mo-sent" role="status">
          Запрос отправлен: {data.setBy.name} увидит его в Обзоре.
        </p>
      ) : (
        <Button
          appearance="outline"
          size="s"
          onClick={() => void request()}
          disabled={ask.pending}
          icon={ask.pending ? <Spinner size="xs" /> : <Send size={14} />}
        >
          Попросить больше у лида
        </Button>
      )}
      {ask.error ? (
        <p className="mo-error" role="alert">
          {orgErrorText(ask.error)}
        </p>
      ) : null}
    </section>
  );
};

const Chats = ({ chats }: { chats: MyChat[] }) => {
  const [now] = useState(() => Date.now());
  return (
    <section className="ov-card" aria-labelledby="mo-chats">
      <h2 id="mo-chats" className="ov-card__title">
        Мои последние чаты
      </h2>
      <p className="ov-card__sub">Сколько стоил каждый и чем закончился</p>
      {chats.length === 0 ? (
        <p className="ov-empty">Чатов за период ещё не было.</p>
      ) : (
        <ul className="mo-chats">
          {chats.map((c) => (
            <li key={c.id} className="mo-chat" data-end={c.end}>
              <span className="mo-chat__dot" aria-hidden="true" />
              <span className="ov-row__text">
                <b>{c.title}</b>
                <small>
                  {c.machine} · {ago(c.end === 'running' && !c.endedAt ? null : c.endedAt, now)}
                </small>
              </span>
              <span className="mo-chat__end">{endText(c)}</span>
              <b className="mo-chat__cost">${num(c.costUsd)}</b>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
};

export const MemberOverview = () => {
  const { base } = useOrg();
  const [period, setPeriod] = useState<'week' | 'month'>('month');
  const { data, error, reload } = useLoad((api) => api.myOverview(period), [period]);
  return (
    <Page>
      <PageHead
        title="Обзор"
        sub={`Только твоя работа · ${period === 'week' ? 'неделя' : monthWord()}`}
        actions={<Seg label="Период" value={period} options={PERIODS} onChange={setPeriod} />}
      />
      {error ? <LoadError error={error} retry={reload} /> : null}
      {!error && !data && <Loading />}
      {data && (
        <>
          <div className="ho-tiles">
            <Tile
              tone="violet"
              icon={<span className="mo-ico">{shownSymbol()}</span>}
              title="Мой лимит"
              line={
                data.limit
                  ? `${thousands(data.limit.spentRub)} из ${thousands(data.limit.limitRub)} ${shownSymbol()} · сбросится ${new Date(data.limit.resetsAt).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })}`
                  : `${rub(data.spentRub)} · лимита нет`
              }
              viz={
                data.limit ? <ProgressLine value={data.limit.spentRub / data.limit.limitRub} /> : undefined
              }
            />
            <Tile
              tone="teal"
              icon={<MessagesSquare size={16} />}
              title="Чатов за неделю"
              line={`${data.tiles.chats.week} · ${data.tiles.chats.running} идут сейчас`}
              viz={<MiniBars values={data.tiles.chats.bars} />}
            />
            <Tile
              tone="blue"
              icon={<GitPullRequest size={16} />}
              title="MR за неделю"
              line={
                data.tiles.mr.merged === null
                  ? String(data.tiles.mr.week)
                  : `${data.tiles.mr.week} · ${data.tiles.mr.merged} уже смёржены`
              }
              viz={<MiniBars values={data.tiles.mr.bars} />}
              to={`${base}/analytics`}
            />
            {data.tiles.machine && (
              <Tile
                tone="gray"
                icon={<Server size={16} />}
                title="Моя машина"
                line={`${data.tiles.machine.name} ${data.tiles.machine.online ? 'на связи' : 'не на связи'} · загрузка ${data.tiles.machine.loadPct}%`}
                viz={<MiniBars values={data.tiles.machine.bars} />}
              />
            )}
          </div>
          <div className="mo-pair">
            {data.limit ? <Limit data={data.limit} /> : <NoLimit data={data} />}
            {data.estimated && <p className="mo-note">{ESTIMATE_NOTE}</p>}
            <Chats chats={data.chats} />
          </div>
        </>
      )}
    </Page>
  );
};
