// The card of a person or a team (board `ps`): the month's numbers, role and team of a person, the limit on a slider.
// Saving applies the changes at once on the screen behind; the drawer closes when the hub has accepted them.
import { type CSSProperties, useState } from 'react';

import type { MemberRole, Person, TeamBlock, TeamsView } from '../../../org/api/teams';
import { kRub, needsText, people as peopleText } from '../../../org/screens/teams/format';
import { useOrg } from '../../../org/context';
import { navigate } from '../../../lib/router';
import { Btn, Drawer, initial } from '../../ui';
import { ConfirmDialog } from './confirm';
import { LimitSlider } from './slider';
import {
  colorOf,
  PERSON_STEPS,
  ROLE_HINT,
  roleWord,
  sliderOf,
  stopsOf,
  TEAM_STEPS,
  teamCeiling,
  teamLimitNote,
} from './model';
import { moveTo, personLimit, removePerson, setRole, teamLimit } from './ops';
import type { Op } from './use-teams';

export type Subject = { kind: 'person' | 'team'; id: string };

interface Common {
  view: TeamsView;
  onClose: () => void;
  apply: (op: Op) => Promise<boolean>;
  /** The last refusal of the hub, shown under the limit. */
  problem: string | null;
}

const find = (view: TeamsView, s: Subject): { person?: Person; block?: TeamBlock } => {
  if (s.kind === 'team') return { block: view.teams.find((t) => t.id === s.id) };
  for (const b of view.teams) {
    const person = b.people.find((p) => p.id === s.id);
    if (person) return { person, block: b };
  }
  return {};
};

export const Seg = <T,>({
  items,
  value,
  disabled,
  onPick,
  className,
}: {
  items: { id: T; label: string }[];
  value: T;
  disabled?: boolean;
  onPick: (id: T) => void;
  className?: string;
}) => (
  <div
    className={`hx-set__seg${className ? ` ${className}` : ''}`}
    role="radiogroup"
    data-off={disabled || undefined}
  >
    {items.map((o) => (
      <button
        key={String(o.id)}
        type="button"
        className="hx-set__seg-item"
        role="radio"
        aria-checked={o.id === value}
        aria-disabled={disabled || undefined}
        onClick={() => !disabled && onPick(o.id)}
      >
        {o.label}
      </button>
    ))}
  </div>
);

const Inner = ({
  subject,
  open,
  view,
  onClose,
  apply,
  problem,
}: Common & { subject: Subject; open: boolean }) => {
  const { person, block } = find(view, subject);
  const isPerson = subject.kind === 'person';
  const [role, setRoleDraft] = useState<MemberRole | null>(null);
  const [team, setTeam] = useState<string | null | undefined>(undefined);
  const [limit, setLimit] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [removeFailed, setRemoveFailed] = useState(false);
  const [failed, setFailed] = useState(false);
  const { shell, base } = useOrg();
  if (isPerson ? !person : !block) return null;

  const subj = isPerson ? person! : block!;
  const name = subj.name;
  const baseLimit = isPerson ? person!.limitRub : block!.limitRub;
  const spent = subj.spentRub;
  const shownBase = baseLimit ?? (isPerson ? person!.effective.amountRub : null);
  const steps = stopsOf(isPerson ? PERSON_STEPS : TEAM_STEPS, baseLimit, limit);
  const ceiling = isPerson ? null : teamCeiling(view, block!);
  const slider = sliderOf(steps, limit ?? shownBase, baseLimit, spent, ceiling);

  const canLimit = isPerson ? person!.can.limit : block!.can.limit;
  const needLimit = isPerson ? person!.needs.limit : block!.needs.limit;
  const self = isPerson && person!.self;

  const roleNow = role ?? person?.role ?? 'member';
  const teamNow = team === undefined ? (person?.teamId ?? null) : team;
  const limitNow = limit ?? baseLimit;
  const changed =
    (isPerson && (roleNow !== person!.role || teamNow !== person!.teamId)) ||
    (limit !== null && limit !== baseLimit);

  const stats = isPerson
    ? [
        { v: person!.invited ? '—' : kRub(spent), k: 'потрачено в этом месяце' },
        {
          v: person!.invited || person!.chatsWeek === null ? '—' : String(person!.chatsWeek),
          k: 'чатов за неделю',
        },
        { v: person!.invited || person!.mrsWeek === null ? '—' : String(person!.mrsWeek), k: 'MR за неделю' },
      ]
    : [
        { v: kRub(spent), k: 'потрачено в этом месяце' },
        { v: String(block!.people.length), k: 'человек' },
        {
          v: block!.limitRub ? `${Math.round((spent / block!.limitRub) * 100)}%` : '—',
          k: 'лимита',
        },
      ];

  const teamName = isPerson ? (view.teams.find((t) => t.id === person!.teamId)?.name ?? 'Без команды') : '';
  const sub = isPerson
    ? `${teamName} · ${roleWord(person!.role)}${person!.invited ? ' · не принял приглашение' : ''}`
    : `${block!.leadName ? `лид ${block!.leadName} · ` : ''}${peopleText(block!.people.length)}`;
  const color = colorOf(view.teams, isPerson ? person!.teamId : block!.id);

  // The hub is asked first and the drawer waits: it closes when the person is gone, else it says why not.
  const remove = async () => {
    if (!person || removing) return;
    setRemoving(true);
    setRemoveFailed(false);
    const ok = await apply(removePerson(person.id));
    setRemoving(false);
    if (ok) {
      setConfirm(false);
      onClose();
    } else setRemoveFailed(true);
  };

  const save = async () => {
    if (!changed || saving) return;
    setSaving(true);
    setFailed(false);
    const jobs: Promise<boolean>[] = [];
    if (isPerson) {
      if (roleNow !== person!.role) jobs.push(apply(setRole(person!.id, roleNow)));
      if (teamNow !== person!.teamId) jobs.push(apply(moveTo(person!.id, teamNow)));
      if (limit !== null && limit !== baseLimit) jobs.push(apply(personLimit(person!.id, limit)));
    } else if (limit !== null && limit !== baseLimit) {
      jobs.push(apply(teamLimit(block!.id!, limit)));
    }
    const ok = (await Promise.all(jobs)).every(Boolean);
    setSaving(false);
    if (ok) onClose();
    else setFailed(true);
  };

  const teamItems = [
    ...view.teams.filter((t) => t.id !== null).map((t) => ({ id: t.id as string | null, label: t.name })),
    ...(person?.teamId === null ? [{ id: null as string | null, label: 'Без команды' }] : []),
  ];

  return (
    <Drawer
      open={open}
      onClose={onClose}
      className="md ps"
      label={name}
      lead={
        <span className="hx-avatar ps-av" style={{ '--c': color } as CSSProperties}>
          {initial(name)}
        </span>
      }
      title={name}
      sub={sub}
      over={
        <>
          {isPerson && (
            <ConfirmDialog
              open={confirm}
              label="Убрать из тенанта"
              title={`Убрать ${name} из тенанта?`}
              lead="Человек потеряет доступ к машинам и чатам тенанта. Приглашение можно отправить заново."
              yes="Убрать"
              busy={removing}
              error={removeFailed ? problem : null}
              onNo={() => {
                setConfirm(false);
                setRemoveFailed(false);
              }}
              onYes={() => void remove()}
            />
          )}
          <div className="md-actions tm-actions">
            {isPerson && person!.can.remove && (
              <Btn variant="ghost" size="sm" className="md-danger" onClick={() => setConfirm(true)}>
                Убрать из тенанта
              </Btn>
            )}
            <span className="hx-spacer" />
            <Btn variant="ghost" size="sm" onClick={onClose}>
              Отмена
            </Btn>
            <button
              type="button"
              className="hx-btn md-save"
              data-variant="primary"
              data-size="sm"
              data-busy={saving ? '1' : undefined}
              aria-disabled={!changed}
              onClick={() => void save()}
            >
              <span className="md-spin" aria-hidden="true" />
              {saving ? 'Сохраняем' : 'Сохранить'}
            </button>
          </div>
        </>
      }
    >
      <div className="md-res ps-stats">
        {stats.map((x) => (
          <div key={x.k}>
            <b>{x.v}</b>
            <small>{x.k}</small>
          </div>
        ))}
      </div>

      {isPerson && (
        <>
          <div className="md-sec">
            <div className="md-sec__t">Роль</div>
            <Seg
              items={[
                { id: 'member' as MemberRole, label: 'Участник' },
                { id: 'lead' as MemberRole, label: 'Лид команды' },
              ]}
              value={roleNow}
              disabled={!person!.can.role}
              onPick={setRoleDraft}
            />
            <p className="md-note">
              {!person!.can.role
                ? self
                  ? 'Себе менять нельзя'
                  : needLabel(person!.needs.role)
                : ROLE_HINT[roleNow]}
            </p>
          </div>
          {person!.openIn && (
            <div className="md-sec">
              <div className="md-sec__t">Тенант</div>
              <p className="md-note tm-first">
                Человек состоит в нескольких тенантах и не входит ни в одну команду, поэтому лимит и удаление
                относятся к тенанту. Откройте нужный:
              </p>
              <div className="tm-inline">
                {person!.openIn.map((t) => (
                  <Btn
                    key={t.slug}
                    size="sm"
                    onClick={() => navigate(`/o/${shell.org.slug}/${t.slug}/teams?p=${person!.id}`)}
                  >
                    {t.name}
                  </Btn>
                ))}
              </div>
            </div>
          )}
          <div className="md-sec">
            <div className="md-sec__t">Команда</div>
            <Seg
              className="ps-teams"
              items={teamItems}
              value={teamNow}
              disabled={!person!.can.team}
              onPick={setTeam}
            />
            {!person!.can.team && (self || !person!.openIn) && (
              <p className="md-note">{self ? 'Себе менять нельзя' : needLabel(person!.needs.team)}</p>
            )}
          </div>
        </>
      )}

      <div className="md-sec">
        <div className="md-sec__t">{isPerson ? 'Лимит в месяц' : 'Лимит команды в месяц'}</div>
        <LimitSlider
          slider={slider}
          value={limitNow ?? shownBase}
          base={baseLimit}
          spent={spent}
          disabled={!canLimit}
          onPick={setLimit}
        />
        <p className="md-note">
          {!canLimit
            ? self
              ? 'Себе менять нельзя: лимит ставит другой администратор'
              : person?.openIn
                ? 'Лимит ставится в тенанте человека: откройте его выше'
                : needLabel(needLimit)
            : isPerson
              ? 'Когда лимит кончится, агенты встанут до 1-го числа — или пока лимит не поднимут. На 80% придёт предупреждение.'
              : teamLimitNote(view, block!, limitNow ?? 0)}
        </p>
        {failed && problem && (
          <p className="md-note tm-err" role="alert">
            {problem}
          </p>
        )}
      </div>
      {!isPerson && block!.id && shell.viewer.role === 'admin' && (
        <div className="md-sec">
          <div className="md-sec__t">Название и удаление</div>
          <div className="tm-inline">
            <Btn size="sm" icon="pencil" onClick={() => navigate(`${base}/team?t=${block!.id}&dlg=edit`)}>
              Переименовать или удалить
            </Btn>
          </div>
        </div>
      )}
    </Drawer>
  );
};

const needLabel = (n: Parameters<typeof needsText>[0] | null): string => (n ? needsText(n) : '');

/** Keeps the last card while the drawer plays its closing animation. */
export const PersonOrTeamSheet = ({ subject, ...rest }: Common & { subject: Subject | null }) => {
  const [kept, setKept] = useState<Subject | null>(subject);
  if (subject && (!kept || kept.kind !== subject.kind || kept.id !== subject.id)) setKept(subject);
  const shown = subject ?? kept;
  if (!shown) return null;
  return <Inner key={`${shown.kind}:${shown.id}`} subject={shown} open={subject !== null} {...rest} />;
};
