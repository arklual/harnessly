// The participant's «Обзор»: only their own work, in the board's markup — four tiles, «Мой лимит» and «Мои последние чаты».
import { useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type { MyChat, MyLimit, MyOverview } from '../../../org/api/member';
import type { OverviewPeriod } from '../../../org/api/overview';
import { navigate } from '../../../lib/router';
import { useAction } from '../../../org/hooks';
import { monthWord } from '../../../org/dates';
import { resetText } from '../../../org/screens/overview/format';
import { Icon } from '../../ui';
import { limitView, splitRows } from './calc';
import {
  ago,
  CHAT_MARK,
  chatCost,
  chatsLine,
  chatTag,
  dayMonth,
  forPeriod,
  genitive,
  kRub,
  machineLine,
  mrLine,
  rub,
  thousand,
  thousandBare,
} from './fmt';
import { Bars, Card, css, NO_TEAM_COLOR, Strip, SxTile, TEAM_COLORS } from './parts';

const askMore = (api: { requestMoreLimit: () => Promise<{ sent: true }> }) => api.requestMoreLimit();

const Tiles = ({ data, period, base }: { data: MyOverview; period: OverviewPeriod; base: string }) => {
  const { limit, tiles } = data;
  const when = forPeriod(period);
  const m = tiles.machine;
  return (
    <div className="sx-wrap">
      <div className="sx-grid">
        <SxTile
          member
          i={0}
          tone="#8968f4"
          icon="coins"
          label="Мой лимит"
          hint="Сколько вы потратили из личного лимита за месяц, по прайс-листу провайдеров. Лимит ставит лид команды."
          value={
            limit
              ? `${thousandBare(limit.spentRub)} из ${rub(limit.limitRub)} · сбросится ${dayMonth(limit.resetsAt)}`
              : `${rub(data.spentRub)} · лимита нет`
          }
          chart={
            limit ? (
              <Strip used={limit.spentRub / 100} whole={limit.limitRub / 100} color="#8968f4" />
            ) : (
              <span className="sx-strip sx-strip--empty" aria-hidden="true" />
            )
          }
        />
        <SxTile
          member
          i={1}
          tone="#25a897"
          icon="messages-square"
          label={`Чатов ${when}`}
          hint={`Сколько чатов с агентом вы вели ${when}; в скобках те, что идут сейчас.`}
          value={chatsLine(tiles.chats.week, tiles.chats.running)}
          chart={<Bars values={tiles.chats.bars} />}
        />
        <SxTile
          member
          i={2}
          tone="#3b82f6"
          icon="git-pull-request"
          label={`MR ${when}`}
          hint={`Сколько merge request создали ваши чаты ${when}.`}
          value={mrLine(tiles.mr.week, tiles.mr.merged)}
          chart={<Bars values={tiles.mr.bars} />}
        />
        <SxTile
          member
          i={3}
          tone="#7c7c86"
          icon="server"
          label="Моя машина"
          value={m ? machineLine(m) : 'пока нет'}
          chart={<Bars values={m?.bars ?? []} />}
          hint="Машина, на которой шёл ваш последний чат. Нажмите, чтобы открыть её в разделе «Машины»."
          onClick={
            m
              ? () => navigate(m.id ? `${base}/machines?m=${encodeURIComponent(m.id)}` : `${base}/machines`)
              : undefined
          }
        />
      </div>
    </div>
  );
};

/** «На что ушло»: the parts of the spend as one stacked bar and a list. */
const Split = ({ items }: { items: MyOverview['breakdown'] }) => {
  const rows = splitRows(items.map((b) => b.rub));
  let model = 0;
  const colors = items.map((b) =>
    b.kind === 'machine' ? NO_TEAM_COLOR : TEAM_COLORS[model++ % TEAM_COLORS.length],
  );
  return (
    <>
      <div className="me-sec">На что ушло</div>
      <div className="me-split__bar" aria-hidden="true">
        {items.map((b, i) => (
          <i key={b.label} style={css({ '--w': rows[i]!.w, '--c': colors[i] })} />
        ))}
      </div>
      <ul className="me-split">
        {items.map((b, i) => (
          <li key={b.label} className="me-split__row">
            <span className="me-split__dot" style={css({ '--c': colors[i] })} />
            <span>{b.label}</span>
            <b>{kRub(b.rub)}</b>
            <small>{rows[i]!.pct}%</small>
          </li>
        ))}
      </ul>
    </>
  );
};

const LimitCard = ({ data, now }: { data: MyOverview; now: number }) => {
  const l: MyLimit | null = data.limit;
  const ask = useAction(askMore);
  const [sent, setSent] = useState(false);
  const month = monthWord();
  const request = async () => {
    const r = await ask.run();
    if (r?.sent) setSent(true);
  };
  if (!l) {
    return (
      <Card
        label={`Мой расход за ${month}`}
        className="me-lim"
        title={<span className="mx-card-title">Мой расход за {month}</span>}
        description="Личного лимита нет"
      >
        <div className="me-lim__big">
          <b>{rub(data.spentRub)}</b>
        </div>
        {data.breakdown.length > 0 && <Split items={data.breakdown} />}
      </Card>
    );
  }
  const v = limitView({ spent: l.spentRub, limit: l.limitRub, forecast: l.forecastRub, warnPct: l.warnPct });
  const lead = l.setBy.name;
  const leadOf = genitive(lead) ?? 'лида команды';
  return (
    <Card
      label={`Мой лимит на ${month}`}
      className="me-lim"
      title={<span className="mx-card-title">Мой лимит на {month}</span>}
      description={`Лимит ставит лид команды ${lead}. Сбросится ${resetText(l.resetsAt, now)}`}
    >
      <div className="me-lim__big">
        <b>{rub(l.spentRub)}</b>
        <span>
          из {rub(l.limitRub)} · {v.over ? 'лимит превышен' : `осталось ${thousand(v.left)}`}
        </span>
      </div>
      <div className="me-lim__bar" aria-hidden="true">
        <i className="me-lim__used" style={css({ '--w': v.usedW })} />
        <i className="me-lim__fc" style={css({ '--w': v.forecastW })} />
        {l.mode !== 'off' && <span className="me-lim__warn" style={css({ '--x': v.warnX })} />}
      </div>
      <div className="me-lim__legend">
        <span>
          <i className="me-k me-k--used" />
          потрачено
        </span>
        <span>
          <i className="me-k me-k--fc" />к концу месяца ≈ {thousand(l.forecastRub)}
        </span>
        {l.mode !== 'off' && (
          <span>
            <i className="me-k me-k--warn" />
            на {l.warnPct}% придёт предупреждение
          </span>
        )}
      </div>
      {data.breakdown.length > 0 && <Split items={data.breakdown} />}
      <div className="me-lim__foot">
        <span>
          {l.mode === 'stop'
            ? `Когда лимит кончится, агенты встанут до ${new Date(l.resetsAt).getDate()}-го числа`
            : l.mode === 'warn'
              ? 'Лимит агентов не останавливает: когда он кончится, придёт предупреждение'
              : 'Лимит только считается, агентов он не останавливает'}
        </span>
        <button
          type="button"
          className="hx-btn"
          data-variant="outline"
          data-size="sm"
          data-sent={sent ? '1' : '0'}
          disabled={ask.pending}
          onClick={() => void request()}
        >
          <Icon i={sent ? 'check' : 'send'} size="sm" />
          {sent ? `Запрос у ${leadOf}` : `Попросить больше у ${leadOf}`}
        </button>
      </div>
      {ask.error ? (
        <p className="ov-error-line" role="alert">
          {orgErrorText(ask.error)}
        </p>
      ) : null}
    </Card>
  );
};

const ChatsCard = ({ chats, now }: { chats: MyChat[]; now: number }) => (
  <Card
    label="Мои последние чаты"
    className="me-chats"
    title={<span className="mx-card-title">Мои последние чаты</span>}
    description="Сколько стоил каждый и чем закончился"
  >
    {chats.length === 0 ? (
      <p className="ov-empty">Чатов за период ещё не было.</p>
    ) : (
      <ul className="me-chatlist">
        {chats.map((c) => (
          <li key={c.id} className="me-chat" data-link={c.machineId ? '1' : undefined}>
            <span className="me-chat__st" data-st={CHAT_MARK[c.end]} />
            <span className="me-chat__t">
              {c.machineId ? (
                <a
                  className="me-chat__a"
                  href={`/api/machines/${encodeURIComponent(c.machineId)}/open?redirect=1`}
                  target="_blank"
                  rel="noopener"
                  title={`Открыть рабочее пространство ${c.machine}`}
                >
                  <b>{c.title}</b>
                </a>
              ) : (
                <b>{c.title}</b>
              )}
              <small>
                {c.machine} · {ago(c.endedAt, now)}
              </small>
            </span>
            <span className="me-chat__tag" data-st={CHAT_MARK[c.end]}>
              {chatTag(c.end, c.mr)}
            </span>
            <span className="me-chat__c">{chatCost(c.costUsd)}</span>
          </li>
        ))}
      </ul>
    )}
  </Card>
);

export const MemberView = ({
  data,
  period,
  base,
}: {
  data: MyOverview;
  period: OverviewPeriod;
  base: string;
}) => {
  const [now] = useState(() => Date.now());
  return (
    <>
      <Tiles data={data} period={period} base={base} />
      <div className="me-grid">
        <LimitCard data={data} now={now} />
        <ChatsCard chats={data.chats} now={now} />
      </div>
    </>
  );
};
