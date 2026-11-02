import { ChevronRight, Settings } from 'lucide-react';

import { cx } from '@kit';

import type { Person, TeamBlock } from '../../api/teams';
import { ago, kRub, levelOf, ofLimit, people, sharePct, thousand } from './format';

/** The limit bar: the spent part in the colour of its level, the rest grey. SVG, so the width is an attribute. */
const LimitBar = ({ pct }: { pct: number | null }) => (
  <svg className="ht-bar" width="100%" height="6" aria-hidden="true">
    <rect className="ht-bar__track" width="100%" height="6" rx="3" />
    <rect className={`ht-bar__${levelOf(pct)}`} width={`${Math.min(100, pct ?? 0)}%`} height="6" rx="3" />
  </svg>
);

const Row = ({
  p,
  now,
  teamName,
  onOpen,
}: {
  p: Person;
  now: number;
  teamName: string | null;
  onOpen: (id: string) => void;
}) => {
  const pct = sharePct(p.spentRub, p.effective.amountRub);
  const quiet = !p.invited && p.lastActiveAt !== null && now - Date.parse(p.lastActiveAt) >= 7 * 86_400_000;
  return (
    <button type="button" className="ht-row" onClick={() => onOpen(p.id)}>
      <span className="ht-row__who">
        <span className="ho-mark ho-mark--dark">{p.name.slice(0, 1)}</span>
        <span className="ht-row__t">
          <b data-muted={p.invited || undefined}>{p.name}</b>
          <small data-warn={p.invited || quiet || undefined}>
            {p.invited ? 'приглашен(а)' : p.lastActiveAt ? ago(p.lastActiveAt, now) : 'ещё не заходил(а)'}
            {teamName ? ` · ${teamName}` : ''}
          </small>
        </span>
      </span>
      <span className="ht-row__role" data-lead={p.role === 'lead' || undefined}>
        {p.role === 'lead' ? 'лид' : 'участник'}
      </span>
      <span className="ht-row__limit" data-level={levelOf(pct)} data-label="Расход и лимит за месяц">
        <LimitBar pct={p.invited ? null : pct} />
        <span className="ht-row__amt">
          {p.invited
            ? `— из ${p.effective.amountRub === null ? 'без лимита' : kRub(p.effective.amountRub)}`
            : ofLimit(p.spentRub, p.effective.amountRub)}
          {p.effective.setBy && <small>поставил: {p.effective.setBy}</small>}
        </span>
      </span>
      <span className="ht-row__n" data-label="Чатов">
        {p.chatsWeek ?? '—'}
      </span>
      <span className="ht-row__n" data-label="MR">
        {p.mrsWeek ?? '—'}
      </span>
    </button>
  );
};

const Head = () => (
  <div className="ht-thead" aria-hidden="true">
    <span>Человек</span>
    <span>Роль</span>
    <span>Расход и лимит за месяц</span>
    <span>Чатов</span>
    <span>MR</span>
  </div>
);

export const TeamSection = ({
  t,
  open,
  now,
  onToggle,
  onOpenPerson,
  onOpenTeam,
}: {
  t: TeamBlock;
  open: boolean;
  now: number;
  onToggle: () => void;
  onOpenPerson: (id: string) => void;
  onOpenTeam: (id: string) => void;
}) => {
  const pct = sharePct(t.spentRub, t.limitRub);
  const lvl = levelOf(pct);
  return (
    <section className="ht-team">
      <div className="ht-team__head">
        <button type="button" className="ht-team__toggle" aria-expanded={open} onClick={onToggle}>
          <ChevronRight size={14} className={cx('ht-chev', open && 'ht-chev--open')} aria-hidden="true" />
          <span className="ho-mark ho-mark--soft">{t.name.slice(0, 1)}</span>
          <span className="ht-team__t">
            <b>{t.name}</b>
            <small>
              {t.id === null
                ? `${people(t.people.length)} · лимиты из тенанта`
                : `${t.leadName ? `лид ${t.leadName} · ` : ''}${people(t.people.length)}`}
              {t.handedOutPct !== null && t.handedOutPct > 100 ? ` · раздано ${t.handedOutPct}%` : ''}
            </small>
          </span>
          <span className="ht-team__pct" data-level={lvl}>
            {pct !== null && `${pct}% лимита`}
          </span>
          <span className="ht-team__bar">
            <LimitBar pct={pct} />
          </span>
          <span className="ht-team__amt">
            {t.limitRub === null ? kRub(t.spentRub) : `${thousand(t.spentRub)} из ${kRub(t.limitRub)}`}
          </span>
        </button>
        {t.id !== null ? (
          <button
            type="button"
            className="ht-gear"
            aria-label={`Лимит команды ${t.name}`}
            onClick={() => onOpenTeam(t.id ?? '')}
          >
            <Settings size={14} />
          </button>
        ) : (
          <span className="ht-gear ht-gear--none" />
        )}
      </div>
      {open && (
        <div className="ht-team__body">
          <Head />
          {t.people.map((p) => (
            <Row key={p.id} p={p} now={now} teamName={null} onOpen={onOpenPerson} />
          ))}
        </div>
      )}
    </section>
  );
};

export const FlatPeople = ({
  teams,
  now,
  onOpenPerson,
}: {
  teams: TeamBlock[];
  now: number;
  onOpenPerson: (id: string) => void;
}) => (
  <section className="ht-team">
    <div className="ht-team__body ht-team__body--flat">
      <Head />
      {teams.flatMap((t) =>
        t.people.map((p) => <Row key={p.id} p={p} now={now} teamName={t.name} onOpen={onOpenPerson} />),
      )}
    </div>
  </section>
);
