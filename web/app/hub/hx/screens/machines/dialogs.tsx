// The two questions of the machine card: restart (the runner or the whole machine) and switch off.
import { useRef, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';

import type { MovePlan, OrgMachine, ResourcePlan, RestartWhat, RestartWhen } from '../../../org/api/machines';
import { useOrg } from '../../../org/context';
import { movePlanLines } from '../../../org/screens/machines/format';
import { Btn, Dialog, Icon } from '../../ui';
import { offItems, restartHint } from './calc';

const WHAT: readonly { id: RestartWhat; label: string }[] = [
  { id: 'runner', label: 'Раннер' },
  { id: 'machine', label: 'Всю машину' },
];

const Restart = ({
  m,
  onClose,
  onConfirm,
}: {
  m: OrgMachine;
  onClose: () => void;
  onConfirm: (what: RestartWhat, when: RestartWhen) => void;
}) => {
  const [what, setWhat] = useState<RestartWhat>('runner');
  return (
    <>
      <span className="md-dlg__ic">
        <Icon i="rotate-cw" />
      </span>
      <h4>Перезапустить {m.name}?</h4>
      <div className="hx-set__seg md-dlg__seg" role="radiogroup">
        {WHAT.map((o) => (
          <button
            key={o.id}
            type="button"
            className="hx-set__seg-item"
            role="radio"
            aria-checked={what === o.id}
            onClick={() => setWhat(o.id)}
          >
            {o.label}
          </button>
        ))}
      </div>
      <p className="md-dlg__lead">{restartHint(what, m)}</p>
      {m.runnerOutdated && what === 'runner' && (
        <p className="md-note">
          Раннер {m.runnerVersion} устарел. Хаб сам обновление не ставит: новая версия подхватывается, когда
          раннер перезапускается на машине.
        </p>
      )}
      <p className="md-note">
        Если на машине идут чаты, они встанут на паузу и продолжатся сами после перезапуска.
      </p>
      <div className="md-dlg__foot">
        <Btn variant="ghost" size="sm" onClick={onClose}>
          Отмена
        </Btn>
        <Btn variant="primary" size="sm" onClick={() => onConfirm(what, 'now')}>
          Перезапустить
        </Btn>
      </div>
    </>
  );
};

const Remove = ({ m, onClose, onConfirm }: { m: OrgMachine; onClose: () => void; onConfirm: () => void }) => (
  <>
    <span className="md-dlg__ic" data-tone="danger">
      <Icon i="trash-2" />
    </span>
    <h4>Убрать {m.name}?</h4>
    <p className="md-dlg__lead">
      Машина исчезнет из хаба насовсем. Вернуть её можно только заново, по новому коду.
    </p>
    <ul className="md-dlg__list">
      <li>
        <Icon i="key-round" size="sm" />
        <span>Ключ машины перестанет работать, связь с хабом закроется.</span>
      </li>
      <li>
        <Icon i="message-square" size="sm" />
        <span>
          {(m.chats ?? 0) > 0
            ? `Идущие чаты (${m.chats}) прервутся.`
            : 'Если на ней будут идти чаты, они прервутся.'}
        </span>
      </li>
      <li>
        <Icon i="server" size="sm" />
        <span>Сам сервер и файлы на нём не удаляются: хаб только отпускает машину.</span>
      </li>
    </ul>
    <div className="md-dlg__foot">
      <Btn variant="ghost" size="sm" onClick={onClose}>
        Отмена
      </Btn>
      <Btn variant="primary" size="sm" className="md-btn-danger" onClick={onConfirm}>
        Убрать машину
      </Btn>
    </div>
  </>
);

const Off = ({
  m,
  plan,
  onClose,
  onConfirm,
}: {
  m: OrgMachine;
  plan: ResourcePlan | null;
  onClose: () => void;
  onConfirm: () => void;
}) => (
  <>
    <span className="md-dlg__ic" data-tone="danger">
      <Icon i="circle-stop" />
    </span>
    <h4>Отключить {m.name}?</h4>
    <p className="md-dlg__lead">Машина останется в списке тенанта, её можно будет подключить обратно.</p>
    <ul className="md-dlg__list">
      {offItems(m, plan).map((l) => (
        <li key={l.icon}>
          <Icon i={l.icon} size="sm" />
          <span>{l.text}</span>
        </li>
      ))}
    </ul>
    <div className="md-dlg__foot">
      <Btn variant="ghost" size="sm" onClick={onClose}>
        Отмена
      </Btn>
      <Btn variant="primary" size="sm" className="md-btn-danger" onClick={onConfirm}>
        Отключить
      </Btn>
    </div>
  </>
);

/** Moving the machine to another tenant: the hub's dry run is shown first, the move itself waits for it. */
export const Move = ({
  m,
  onClose,
  onMoved,
}: {
  m: OrgMachine;
  onClose: () => void;
  onMoved: () => void;
}) => {
  const { api, shell } = useOrg();
  const targets = shell.tenants.filter((t) => t.id !== m.tenantId);
  const [tenantId, setTenantId] = useState('');
  const [plan, setPlan] = useState<MovePlan | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const seq = useRef(0);
  const pick = (id: string) => {
    const mine = ++seq.current;
    setTenantId(id);
    setPlan(null);
    setError(null);
    if (!id) return setLoading(false);
    setLoading(true);
    api.planMoveMachine(m.id, id).then(
      (p) => {
        if (seq.current !== mine) return;
        setPlan(p);
        setLoading(false);
      },
      (e: unknown) => {
        if (seq.current !== mine) return;
        setError(orgErrorText(e));
        setLoading(false);
      },
    );
  };
  const go = async () => {
    if (!plan || plan.noop || busy) return;
    setBusy(true);
    setError(null);
    try {
      await api.moveMachine(m.id, tenantId);
      onMoved();
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <span className="md-dlg__ic">
        <Icon i="arrow-right-left" />
      </span>
      <h4>Перенести {m.name} в другой тенант?</h4>
      <p className="md-dlg__lead">
        Машина сохранит id и ключ: ничего переустанавливать не нужно. Переносить может админ обоих тенантов.
      </p>
      <select
        className="hx-set__input"
        aria-label="Куда перенести"
        value={tenantId}
        disabled={busy}
        onChange={(e) => pick(e.target.value)}
      >
        <option value="">Выберите тенант</option>
        {targets.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </select>
      {loading && <p className="md-note">Считаем, что изменится…</p>}
      {plan && (
        <ul className="md-dlg__list">
          {movePlanLines(plan).map((l) => (
            <li key={l.text}>
              <Icon i="arrow-right-left" size="sm" />
              <span>{l.text}</span>
            </li>
          ))}
        </ul>
      )}
      {error && (
        <p className="md-note tm-err" role="alert">
          {error}
        </p>
      )}
      <div className="md-dlg__foot">
        <Btn variant="ghost" size="sm" disabled={busy} onClick={onClose}>
          Отмена
        </Btn>
        <Btn
          variant="primary"
          size="sm"
          busy={busy}
          disabled={!plan || plan.noop || loading}
          onClick={() => void go()}
        >
          Перенести
        </Btn>
      </div>
    </>
  );
};

export const MachineDialogs = ({
  dlg,
  m,
  plan,
  onClose,
  onRestart,
  onOff,
  onRemove,
  onMoved,
}: {
  dlg: 'restart' | 'off' | 'remove' | 'move' | null;
  m: OrgMachine;
  plan: ResourcePlan | null;
  onClose: () => void;
  onRestart: (what: RestartWhat, when: RestartWhen) => void;
  onOff: () => void;
  onRemove: () => void;
  onMoved: () => void;
}) => (
  <>
    <Dialog inline open={dlg === 'restart'} onClose={onClose} label={`Перезапустить ${m.name}`}>
      <Restart m={m} onClose={onClose} onConfirm={onRestart} />
    </Dialog>
    <Dialog inline open={dlg === 'remove'} onClose={onClose} label={`Убрать ${m.name}`}>
      <Remove m={m} onClose={onClose} onConfirm={onRemove} />
    </Dialog>
    <Dialog inline open={dlg === 'move'} onClose={onClose} label={`Перенести ${m.name}`}>
      <Move m={m} onClose={onClose} onMoved={onMoved} />
    </Dialog>
    <Dialog inline open={dlg === 'off'} onClose={onClose} label={`Отключить ${m.name}`}>
      <Off m={m} plan={plan} onClose={onClose} onConfirm={onOff} />
    </Dialog>
  </>
);
