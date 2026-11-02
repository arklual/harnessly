import { ArrowRight, ChevronRight } from 'lucide-react';

import { cx } from '@kit';

import type { MachineGroup, OrgMachine } from '../../api/machines';
import { Link } from '../../../lib/router';
import { ago, stateOf, usd, type Tone } from './format';
import { LoadBar, StateStrip } from './parts';

const rowState = (m: OrgMachine, st: { tone: Tone; label: string }, now: number): string => {
  if (st.tone === 'ok') return 'на связи';
  if (st.tone === 'bad') return `не на связи ${ago(m.since, now)}`;
  if (st.tone === 'warn') return `простаивает ${ago(m.since, now)}`;
  return st.label.toLowerCase();
};

const MachineRow = ({ m, now, onOpen }: { m: OrgMachine; now: number; onOpen: (id: string) => void }) => {
  const st = stateOf(m, now);
  const sub = [m.ownerName ?? m.purpose, m.hw.cores ? `${m.hw.cores} ядер · ${m.hw.ramGb} ГБ` : null]
    .filter(Boolean)
    .join(' · ');
  return (
    <button type="button" className="hm-row" onClick={() => onOpen(m.id)}>
      <span className="hm-row__name">
        <i className="hm-dot" data-tone={st.tone} aria-hidden="true" />
        <span className="hm-row__t">
          <b>{m.name}</b>
          <small>{sub}</small>
        </span>
      </span>
      <span className="hm-row__state" data-tone={st.tone}>
        {rowState(m, st, now)}
      </span>
      <span className="hm-row__chats" data-label="Чаты сейчас">
        {m.chats ?? '—'}
      </span>
      <span className="hm-row__bar" data-label="Процессор">
        <LoadBar pct={m.cpu} />
      </span>
      <span className="hm-row__bar" data-label="Память">
        <LoadBar pct={m.ram} />
      </span>
      <span className="hm-row__bar" data-label="Диск">
        <LoadBar pct={m.disk} />
      </span>
      <span className="hm-row__ver" data-label="Раннер" data-old={m.runnerOutdated || undefined}>
        {m.runnerVersion}
        {m.runnerOutdated && ' · старый'}
      </span>
      <span className="hm-row__cost" data-label="За неделю">
        {m.weekCostUsd === null ? '—' : usd(m.weekCostUsd)}
      </span>
    </button>
  );
};

const HEAD = ['Машина', 'Состояние', 'Чаты сейчас', 'Процессор', 'Память', 'Диск', 'Раннер', 'За неделю'];

const Head = () => (
  <div className="hm-thead" aria-hidden="true">
    {HEAD.map((h) => (
      <span key={h}>{h}</span>
    ))}
  </div>
);

export const GroupBlock = ({
  g,
  now,
  base,
  open,
  onToggle,
  onOpen,
}: {
  g: MachineGroup;
  now: number;
  base: string;
  open: boolean;
  onToggle: () => void;
  onOpen: (id: string) => void;
}) => {
  const orgPrefix = base.split('/').slice(0, 3).join('/');
  return (
    <section className="hm-group">
      <button type="button" className="hm-group__head" aria-expanded={open} onClick={onToggle}>
        <ChevronRight size={14} className={cx('hm-chev', open && 'hm-chev--open')} aria-hidden="true" />
        <span className="ho-mark ho-mark--soft">{g.name.slice(0, 1)}</span>
        <span className="hm-group__t">
          <b>{g.name}</b>
          <small>{g.sub}</small>
        </span>
        {g.offline > 0 && <span className="hm-group__bad">{g.offline} не на связи</span>}
        <span className="hm-group__strip">
          <StateStrip online={g.online} idle={g.idle} offline={g.offline} />
        </span>
        <span className="hm-group__n">
          <b>{g.total}</b> <small>{g.total === 1 ? 'машина' : 'машин'}</small>
        </span>
        <span className="hm-group__cost">
          {g.weekCostUsd === null ? '' : <b>{usd(g.weekCostUsd)}</b>}{' '}
          {g.weekCostUsd !== null && <small>за неделю</small>}
        </span>
      </button>
      {open && (
        <div className="hm-group__body">
          <Head />
          {g.machines.map((m) => (
            <MachineRow key={m.id} m={m} now={now} onOpen={onOpen} />
          ))}
          {g.tenantSlug && g.total > g.machines.length && (
            <Link to={`${orgPrefix}/${g.tenantSlug}/machines`} className="hm-all">
              Все {g.total} машин тенанта <ArrowRight size={14} />
            </Link>
          )}
        </div>
      )}
    </section>
  );
};

export const FlatList = ({
  machines,
  now,
  onOpen,
}: {
  machines: OrgMachine[];
  now: number;
  onOpen: (id: string) => void;
}) => (
  <section className="hm-group">
    <div className="hm-group__body">
      <Head />
      {machines.map((m) => (
        <MachineRow key={m.id} m={m} now={now} onOpen={onOpen} />
      ))}
    </div>
  </section>
);
