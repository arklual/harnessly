// The two questions of the machine card: restart (the runner or the whole machine) and switch off.
import { useState } from 'react';

import type { OrgMachine, ResourcePlan, RestartWhat, RestartWhen } from '../../../org/api/machines';
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

export const MachineDialogs = ({
  dlg,
  m,
  plan,
  onClose,
  onRestart,
  onOff,
  onRemove,
}: {
  dlg: 'restart' | 'off' | 'remove' | null;
  m: OrgMachine;
  plan: ResourcePlan | null;
  onClose: () => void;
  onRestart: (what: RestartWhat, when: RestartWhen) => void;
  onOff: () => void;
  onRemove: () => void;
}) => (
  <>
    <Dialog inline open={dlg === 'restart'} onClose={onClose} label={`Перезапустить ${m.name}`}>
      <Restart m={m} onClose={onClose} onConfirm={onRestart} />
    </Dialog>
    <Dialog inline open={dlg === 'remove'} onClose={onClose} label={`Убрать ${m.name}`}>
      <Remove m={m} onClose={onClose} onConfirm={onRemove} />
    </Dialog>
    <Dialog inline open={dlg === 'off'} onClose={onClose} label={`Отключить ${m.name}`}>
      <Off m={m} plan={plan} onClose={onClose} onConfirm={onOff} />
    </Dialog>
  </>
);
