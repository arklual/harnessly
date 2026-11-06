// Managing people where they are listed: the menu of a row («Перенести в…», the role), the drop of a dragged row on a
// team, the footer of a team with its invitation, machines and environment. Shown with `ui.hx.live`.
import { type DragEvent, useRef } from 'react';

import type { MemberRole, Person, TeamBlock } from '../../../org/api/teams';
import { Link } from '../../../lib/router';
import { Btn, Icon, usePop } from '../../ui';

export interface ManageTeam {
  id: string | null;
  name: string;
}

/** What a row and a team card may do; absent: the lists are read-only as before. */
export interface Manage {
  teams: ManageTeam[];
  move: (personId: string, teamId: string | null) => void;
  role: (personId: string, role: MemberRole) => void;
  invite: (teamId: string) => void;
  base: string;
  /** The team's block, for the numbers in the footer. */
  block: (teamId: string) => TeamBlock | undefined;
}

export const DRAG_TYPE = 'application/x-hx-person';

export const canMove = (p: Person) => p.can.team && !p.invited;

export const onDragStartOf = (p: Person) => (e: DragEvent) => {
  e.dataTransfer.setData(DRAG_TYPE, p.id);
  e.dataTransfer.effectAllowed = 'move';
};

/** The «⋯» of a row: move to another team, make lead or member. */
export const RowMenu = ({ p, m }: { p: Person; m: Manage }) => {
  const root = useRef<HTMLSpanElement>(null);
  const pop = usePop(root);
  const moves = canMove(p) ? m.teams.filter((t) => t.id !== p.teamId) : [];
  const toRole: MemberRole | null = p.can.role ? (p.role === 'lead' ? 'member' : 'lead') : null;
  if (moves.length === 0 && !toRole) return null;
  return (
    <span
      className="pt-act"
      ref={root}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <Btn
        variant="ghost"
        size="sm"
        shape="icon"
        aria-label={`Действия: ${p.name}`}
        aria-haspopup="menu"
        aria-expanded={pop.open}
        onClick={pop.toggle}
      >
        <Icon i="ellipsis" size="sm" />
      </Btn>
      {pop.mounted && (
        <div className="hx-set-menu pt-menu" role="menu" data-state={pop.closing ? 'closing' : undefined}>
          {moves.length > 0 && <div className="pt-menu__t">Перенести в…</div>}
          {moves.map((t) => (
            <button
              key={t.id ?? 'none'}
              type="button"
              role="menuitem"
              className="hx-set-menu__item"
              onClick={() => {
                pop.close();
                m.move(p.id, t.id);
              }}
            >
              <span className="hx-set-menu__text">{t.name}</span>
            </button>
          ))}
          {toRole && (
            <>
              {moves.length > 0 && <div className="pt-menu__sep" />}
              <button
                type="button"
                role="menuitem"
                className="hx-set-menu__item"
                onClick={() => {
                  pop.close();
                  m.role(p.id, toRole);
                }}
              >
                <span className="hx-set-menu__text">
                  {toRole === 'lead' ? 'Сделать лидом команды' : 'Сделать участником'}
                </span>
              </button>
            </>
          )}
        </div>
      )}
    </span>
  );
};

/** Under the people of a team: invite into it, its machines, its environment. */
export const TeamFoot = ({ teamId, m }: { teamId: string; m: Manage }) => {
  const x = m.block(teamId)?.extras;
  const now = x?.machinesNow;
  return (
    <div className="tg-foot tg-links">
      <Btn size="sm" icon="mail" variant="ghost" onClick={() => m.invite(teamId)}>
        Пригласить в команду
      </Btn>
      <Link to={`${m.base}/machines`} className="tg-link">
        <Icon i="server" size="sm" />
        {now != null ? `Машины: ${now}` : 'Машины'}
      </Link>
      <Link to={`${m.base}/env`} className="tg-link">
        <Icon i="layers" size="sm" />
        Окружение команды
      </Link>
    </div>
  );
};
