// The two confirmations of a machine's card: restart and disable, with the texts of the board. They sit over the
// sheet, play their closing animation and keep a spinner on the button while the hub works (nothing changes before
// the answer).
import { type ReactNode, useState } from 'react';
import { HardDrive, CircleStop, Coins, RotateCw, Users } from 'lucide-react';

import { Button } from '@kit';

import type { OrgMachine, RestartWhat, RestartWhen } from '../../api/machines';
import { orgErrorText } from '../../api/errors';
import { Seg } from '../../ui/page';
import { useClosing } from '../../ui/closing';
import { rub } from './format';

const Card = ({
  open,
  onClose,
  icon,
  title,
  children,
  actions,
  tone,
}: {
  open: boolean;
  onClose: () => void;
  icon: ReactNode;
  title: string;
  children: ReactNode;
  actions: ReactNode;
  tone: 'accent' | 'danger';
}) => {
  const { mounted, closing } = useClosing(open, 180);
  if (!mounted) return null;
  return (
    <div className="hm-dlg" data-closing={closing || undefined}>
      <div className="hm-dlg__scrim" onClick={onClose} />
      <div className="hm-dlg__card" role="alertdialog" aria-label={title}>
        <span className="hm-dlg__icon" data-tone={tone}>
          {icon}
        </span>
        <h3 className="hm-dlg__title">{title}</h3>
        {children}
        <div className="hm-dlg__actions">{actions}</div>
      </div>
    </div>
  );
};

export const RestartDialog = ({
  open,
  m,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  open: boolean;
  m: OrgMachine;
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onConfirm: (what: RestartWhat, when: RestartWhen) => void;
}) => {
  const [what, setWhat] = useState<RestartWhat>('runner');
  const [when, setWhen] = useState<RestartWhen>('after_chats');
  const hasChats = (m.chats ?? 0) > 0;
  const now = !hasChats || when === 'now';
  return (
    <Card
      open={open}
      onClose={pending ? () => undefined : onClose}
      icon={<RotateCw size={18} />}
      title={`Перезапустить ${m.name}?`}
      tone="accent"
      actions={
        <>
          <Button appearance="ghost" onClick={onClose} disabled={pending}>
            Отмена
          </Button>
          <Button
            appearance="primary"
            loading={pending}
            onClick={() => onConfirm(what, now ? 'now' : 'after_chats')}
          >
            {now ? 'Перезапустить' : 'Перезапустить после чатов'}
          </Button>
        </>
      }
    >
      {m.cloud && (
        <Seg
          label="Что перезапустить"
          value={what}
          onChange={setWhat}
          options={[
            { value: 'runner', label: 'Раннер' },
            { value: 'machine', label: 'Всю машину' },
          ]}
        />
      )}
      <p className="hm-dlg__text">
        {what === 'runner'
          ? 'Перезапустится только раннер harnessly — займёт около 30 секунд. Помогает, когда чаты зависают или не стартуют.'
          : `Сервер перезагрузится целиком — около 2 минут.${m.cloud ? ' Если машина не отвечает, перезагрузим её через Selectel.' : ''}`}
      </p>
      {hasChats ? (
        <div className="hm-radios" role="radiogroup" aria-label="Когда">
          {(
            [
              [
                'after_chats',
                'Дождаться, пока чаты закончатся',
                'новые чаты на машину не пойдут, перезапуск — сам после последнего',
              ],
              ['now', 'Перезапустить сейчас', 'чаты встанут на паузу и продолжатся после перезапуска'],
            ] as const
          ).map(([id, label, hint]) => (
            <button
              key={id}
              type="button"
              role="radio"
              aria-checked={when === id}
              className="hm-radio"
              onClick={() => setWhen(id)}
            >
              <i className="hm-radio__dot" aria-hidden="true" />
              <span>
                <b>{label}</b>
                <small>{hint}</small>
              </span>
            </button>
          ))}
        </div>
      ) : (
        <p className="hm-dlg__note">
          Если на машине идут чаты, они встанут на паузу и продолжатся сами после перезапуска.
        </p>
      )}
      {error != null && <p className="hm-dlg__err">{orgErrorText(error)}</p>}
    </Card>
  );
};

export const OffDialog = ({
  open,
  m,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  open: boolean;
  m: OrgMachine;
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onConfirm: () => void;
}) => (
  <Card
    open={open}
    onClose={pending ? () => undefined : onClose}
    icon={<CircleStop size={18} />}
    title={`Отключить ${m.name}?`}
    tone="danger"
    actions={
      <>
        <Button appearance="ghost" onClick={onClose} disabled={pending}>
          Отмена
        </Button>
        <Button appearance="danger" loading={pending} onClick={onConfirm}>
          Отключить
        </Button>
      </>
    }
  >
    <p className="hm-dlg__text">Машина останется в списке тенанта, её можно будет подключить обратно.</p>
    <ul className="hm-offs">
      <li>
        <CircleStop size={14} />
        Раннер остановится. Если на машине идут чаты, они прервутся.
      </li>
      <li>
        <Users size={14} />
        Люди с доступом не смогут запускать на ней чаты.
      </li>
      <li>
        <HardDrive size={14} />
        Файлы и репозитории на диске останутся — после подключения всё на месте.
      </li>
      {m.cloud && (
        <li>
          <Coins size={14} />
          Сервер выключим: за ядра и память платить перестанем, за диск — {rub(m.hw.diskGb * 8)} в месяц.
        </li>
      )}
    </ul>
    {error != null && <p className="hm-dlg__err">{orgErrorText(error)}</p>}
  </Card>
);
