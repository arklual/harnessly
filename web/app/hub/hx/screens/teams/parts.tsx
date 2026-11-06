// The pieces both team screens draw: the loading skeleton, the failure row, the four tiles, the people table.
import { type CSSProperties, type DragEvent, type KeyboardEvent, type ReactNode, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type { Person, TeamsView } from '../../../org/api/teams';
import { MiniBars, SegmentStrip, StatTile, StatTiles } from '../../charts/stat-tile';
import { Btn, Icon, initial } from '../../ui';
import { canMove, DRAG_TYPE, type Manage, onDragStartOf, RowMenu, TeamFoot } from './manage';
import { type Group, rowOf, tilesOf } from './model';

/** The shimmering boxes of the screen while the hub answers. */
export const TeamsSkeleton = ({ rows = 6 }: { rows?: number }) => (
  <div className="tm-sk" aria-busy="true" aria-label="Загружаем">
    <div className="tm-sk__tiles">
      {[0, 1, 2, 3].map((i) => (
        <span key={i} className="hx-sk" style={{ height: 118 }} />
      ))}
    </div>
    <span className="hx-sk" style={{ height: 40, width: '60%' }} />
    <div className="tm-sk__rows">
      {Array.from({ length: rows }, (_, i) => (
        <span key={i} className="hx-sk" style={{ height: 52 }} />
      ))}
    </div>
  </div>
);

/** One row: what went wrong and the way to try again. */
export const LoadFailed = ({ error, onRetry }: { error: unknown; onRetry: () => void }) => (
  <div className="tm-fail" role="alert">
    <Icon i="circle-alert" />
    <span>{orgErrorText(error)}</span>
    <Btn size="sm" onClick={onRetry}>
      Повторить
    </Btn>
  </div>
);

/** An action the hub refused: the change is already taken back, this says why. */
export const Problem = ({ text, onClose }: { text: string; onClose: () => void }) => (
  <div className="tm-fail" role="alert">
    <Icon i="circle-alert" />
    <span>{text}</span>
    <Btn variant="ghost" size="sm" shape="icon" onClick={onClose} aria-label="Закрыть">
      <Icon i="x" />
    </Btn>
  </div>
);

export const Tiles = ({ view, lead }: { view: TeamsView; lead: boolean }) => {
  const t = tilesOf(view, lead);
  return (
    <StatTiles>
      <StatTile
        index={0}
        tone="#8968f4"
        icon="coins"
        chart={<SegmentStrip parts={t.spend.parts} />}
        label="Расход за месяц"
        value={t.spend.value}
      />
      <StatTile
        index={1}
        tone="#25a897"
        icon="users"
        chart={<MiniBars values={t.active.bars} />}
        label="Активны"
        value={t.active.value}
      />
      <StatTile
        index={2}
        tone="#c8830b"
        icon="gauge"
        chart={<SegmentStrip parts={t.near.parts} />}
        label="Близко к лимиту"
        value={t.near.value}
      />
      <StatTile
        index={3}
        tone="#7c7c86"
        icon="mail"
        chart={<SegmentStrip parts={t.idle.parts} />}
        label="Не работают"
        value={t.idle.value}
      />
    </StatTiles>
  );
};

const HEAD = (
  <div className="pt-row mt-head">
    <span>Человек</span>
    <span>Роль</span>
    <span>Расход и лимит за месяц</span>
    <span>Чатов</span>
    <span>MR</span>
  </div>
);

const PersonRowButton = ({
  p,
  now,
  team,
  onOpen,
  m,
}: {
  p: Person;
  now: number;
  team: string | null;
  onOpen: (id: string) => void;
  m?: Manage;
}) => {
  const r = rowOf(p, now, team);
  const drag = m && canMove(p);
  const open = () => onOpen(p.id);
  return (
    <div
      role="button"
      tabIndex={0}
      className="pt-row"
      data-idle={r.idle ? '1' : '0'}
      data-act={m ? '1' : undefined}
      draggable={drag || undefined}
      onDragStart={drag ? onDragStartOf(p) : undefined}
      onClick={open}
      onKeyDown={onKey(open)}
    >
      <span className="pt-name">
        <span className="hx-avatar pt-av">{r.ini}</span>
        <span className="mt-name__t">
          <b>{r.name}</b>
          <small data-warn={r.idle ? '1' : '0'}>{r.sub}</small>
        </span>
      </span>
      <span className="pt-role" data-lead={r.lead ? '1' : '0'}>
        {r.role}
      </span>
      <span
        className="pt-lim"
        data-hot={r.hot ? '1' : '0'}
        data-loose={r.limHint ? '1' : undefined}
        title={r.limHint}
      >
        <i style={{ '--w': r.w } as CSSProperties} />
        <b>{r.limTxt}</b>
      </span>
      <span className="mt-num">{r.chats}</span>
      <span className="mt-num">{r.mr}</span>
      {m && <RowMenu p={p} m={m} />}
    </div>
  );
};

export const PeopleTable = ({
  items,
  now,
  onOpen,
  inGroup,
  manage,
}: {
  items: { person: Person; team: string | null }[];
  now: number;
  onOpen: (id: string) => void;
  inGroup?: boolean;
  manage?: Manage;
}) => (
  <div className={inGroup ? 'mt mt--in' : 'mt'}>
    {HEAD}
    {items.map((x) => (
      <PersonRowButton key={x.person.id} p={x.person} now={now} team={x.team} onOpen={onOpen} m={manage} />
    ))}
  </div>
);

const onKey = (fn: () => void) => (e: KeyboardEvent) => {
  if (e.target !== e.currentTarget) return;
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    fn();
  }
};

/** A team of the list: its head with the bar and the gear, and under it the people (opens and closes smoothly). */
export const GroupCard = ({
  g,
  i,
  open,
  now,
  onToggle,
  onGear,
  onOpenPerson,
  manage,
}: {
  g: Group;
  i: number;
  open: boolean;
  now: number;
  onToggle: () => void;
  onGear?: () => void;
  onOpenPerson: (id: string) => void;
  manage?: Manage;
}) => {
  const [over, setOver] = useState(false);
  // A row dragged here goes to this team (a card with no team takes it out of its team).
  const drop = manage
    ? {
        onDragOver: (e: DragEvent) => {
          if (!e.dataTransfer.types.includes(DRAG_TYPE)) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
          setOver(true);
        },
        onDragLeave: (e: DragEvent) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false);
        },
        onDrop: (e: DragEvent) => {
          setOver(false);
          const id = e.dataTransfer.getData(DRAG_TYPE);
          if (!id) return;
          e.preventDefault();
          if (!g.people.some((x) => x.id === id)) manage.move(id, g.teamId);
        },
      }
    : {};
  return (
    <div
      className="mo-g tg"
      aria-expanded={open || over}
      data-drop={over ? '1' : undefined}
      style={{ '--i': i } as CSSProperties}
      {...drop}
    >
      <div
        className="mo-g__head tg-head"
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={onToggle}
        onKeyDown={onKey(onToggle)}
      >
        <Icon i="chevron-right" size="sm" className="mo-g__chev" />
        <span className="ad-dot tg-dot" style={{ '--c': g.color } as CSSProperties}>
          {g.initial}
        </span>
        <span className="mo-g__t">
          <b>{g.name}</b>
          <small>{g.sub}</small>
        </span>
        <span className="mo-g__flag tg-flag" data-show={g.hasFlag} data-tone={g.tone}>
          {g.flag}
        </span>
        <span className="tg-bar" data-tone={g.tone}>
          <i style={{ '--w': g.w } as CSSProperties} />
        </span>
        <span className="mo-g__s tg-sum">{g.sum}</span>
        {g.hasLimit && onGear ? (
          <Btn
            variant="ghost"
            size="sm"
            shape="icon"
            className="tg-gear"
            aria-label="Настройки команды"
            title="Настройки команды"
            onClick={(e) => {
              e.stopPropagation();
              onGear();
            }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <Icon i="settings" size="sm" />
          </Btn>
        ) : (
          <span />
        )}
      </div>
      <div className="mo-g__body">
        <div>
          <PeopleTable
            inGroup
            items={g.people.map((person) => ({ person, team: null }))}
            now={now}
            onOpen={onOpenPerson}
            manage={manage}
          />
          {manage && g.teamId && <TeamFoot teamId={g.teamId} m={manage} />}
        </div>
      </div>
    </div>
  );
};

export const Empty = ({ children }: { children: ReactNode }) => <div className="mo-empty">{children}</div>;

export { initial };
