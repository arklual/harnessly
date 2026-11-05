// «Переименовать команду» and «Удалить команду» in one drawer: the name is saved on the spot, the deletion asks first.
import { type FormEvent, useState } from 'react';

import { people as peopleText } from '../../../org/screens/teams/format';
import { Btn, Drawer } from '../../ui';
import { ConfirmDialog } from './confirm';
import { deleteTeam, renameTeam } from './ops';
import type { Op } from './use-teams';

const NAME_MAX = 60;

export const TeamEditSheet = ({
  open,
  teamId,
  name: current,
  peopleCount,
  taken,
  canDelete,
  apply,
  problem,
  onClose,
  onDeleted,
}: {
  open: boolean;
  teamId: string;
  name: string;
  peopleCount: number;
  /** The names of the other teams. */
  taken: string[];
  canDelete: boolean;
  apply: (op: Op) => Promise<boolean>;
  problem: string | null;
  onClose: () => void;
  onDeleted: () => void;
}) => {
  const [name, setName] = useState(current);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [delFailed, setDelFailed] = useState(false);
  const [was, setWas] = useState(open);
  if (open !== was) {
    setWas(open);
    if (open) {
      setName(current);
      setFailed(false);
      setConfirm(false);
      setDelFailed(false);
    }
  }
  const clean = name.trim();
  const dup = taken.some((t) => t.trim().toLowerCase() === clean.toLowerCase());
  const same = clean === current.trim();
  const bad = clean === '' || clean.length > NAME_MAX || dup || same;

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (bad || busy) return;
    setBusy(true);
    setFailed(false);
    const ok = await apply(renameTeam(teamId, clean));
    setBusy(false);
    if (ok) onClose();
    else setFailed(true);
  };
  const remove = async () => {
    if (deleting) return;
    setDeleting(true);
    setDelFailed(false);
    const ok = await apply(deleteTeam(teamId));
    setDeleting(false);
    if (ok) {
      setConfirm(false);
      onDeleted();
    } else setDelFailed(true);
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      className="md"
      label="Команда"
      title="Переименовать команду"
      sub={`${current} · ${peopleText(peopleCount)}`}
      over={
        <>
          <ConfirmDialog
            open={confirm}
            label="Удалить команду"
            title={`Удалить команду ${current}?`}
            lead={
              peopleCount > 0
                ? `Люди (${peopleText(peopleCount)}) останутся в тенанте без команды, лимиты команды и роль лида пропадут. Машины останутся в тенанте. Отменить нельзя.`
                : 'Лимиты команды и роль лида пропадут. Машины останутся в тенанте. Отменить нельзя.'
            }
            yes="Удалить"
            busy={deleting}
            error={delFailed ? problem : null}
            onNo={() => {
              setConfirm(false);
              setDelFailed(false);
            }}
            onYes={() => void remove()}
          />
          <div className="md-actions tm-actions">
            {canDelete && (
              <Btn variant="ghost" size="sm" className="md-danger" onClick={() => setConfirm(true)}>
                Удалить команду
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
              data-busy={busy ? '1' : undefined}
              aria-disabled={bad}
              onClick={() => void submit()}
            >
              <span className="md-spin" aria-hidden="true" />
              {busy ? 'Сохраняем' : 'Сохранить'}
            </button>
          </div>
        </>
      }
    >
      <form className="md-sec" onSubmit={(e) => void submit(e)}>
        <div className="md-sec__t">Название</div>
        <input
          className="hx-set__input"
          value={name}
          maxLength={NAME_MAX}
          autoFocus
          aria-label="Название команды"
          onChange={(e) => {
            setName(e.target.value);
            setFailed(false);
          }}
        />
        <p className="md-note" data-bad={dup || undefined}>
          {dup ? 'Команда с таким названием уже есть' : 'Название видят все в тенанте'}
        </p>
        {failed && problem && (
          <p className="md-note tm-err" role="alert">
            {problem}
          </p>
        )}
      </form>
    </Drawer>
  );
};
