// The machine card: what is going on with it, its load over a day, its resources, who may start chats, and the actions.
import { useState } from 'react';

import { useNow } from '../../../lib/hooks';
import type { OrgMachine } from '../../../org/api/machines';
import { LoadRow } from '../../charts/spark';
import { Btn, Drawer, Icon } from '../../ui';
import { AccessSection } from './access';
import { canOpen, cardSub, cleanName, statusOf } from './calc';
import { MachineDialogs } from './dialogs';
import { useOpenMachine } from './open';
import { ErrorRow } from './parts';
import { ResourcesSection } from './resources';
import type { DrawerState } from './url';
import { useMachineCard } from './use-machines';

const NEEDS: Record<string, string> = {
  org_admin: 'Может поменять: админ организации',
  tenant_admin: 'Может поменять: админ тенанта',
  team_lead: 'Может поменять: лид команды',
};

const Skeleton = () => (
  <div className="mx-skel-card" role="status" aria-label="Загрузка">
    {[0, 1, 2, 3].map((i) => (
      <span key={i} className="mx-skel" />
    ))}
  </div>
);

export const MachineDrawer = ({
  id,
  open,
  hint,
  mlUrl,
  url,
  onChanged,
  onRemoved,
}: {
  id: string;
  open: boolean;
  /** The machine as the list knows it: the head is drawn from it before the card has loaded. */
  hint: OrgMachine | null;
  mlUrl: string | null;
  url: Pick<DrawerState, 'tab' | 'dlg' | 'close' | 'setTab' | 'setDlg'>;
  onChanged: (m: OrgMachine) => void;
  /** The machine was taken away: the list is read again and the card closes. */
  onRemoved: () => void;
}) => {
  const card = useMachineCard(id, onChanged);
  const ws = useOpenMachine();
  const [renaming, setRenaming] = useState<string | null>(null);
  const now = useNow(30_000, open);
  const d = card.detail;
  const m = d?.machine ?? hint;
  const busy = d?.machine.busy ?? null;
  const disabled = d?.machine.state === 'disabled';
  const st = d && m ? statusOf(m, busy?.kind ?? null, card.done, now) : null;
  const locked = busy != null || disabled === true;

  const apply = (to: { cores: number; ramGb: number; diskGb: number }) => {
    url.setTab(null);
    void card.resize(to);
  };
  const saveAccess = async (mode: Parameters<typeof card.saveAccess>[0], ids: string[]) => {
    const ok = await card.saveAccess(mode, ids);
    if (ok) url.setTab(null);
    return ok;
  };

  const saveName = async () => {
    const name = cleanName(renaming ?? '');
    if (name === null) return;
    if (name === m?.name) return setRenaming(null);
    if (await card.rename(name)) setRenaming(null);
  };

  const body = () => {
    if (card.loadError && !d) {
      return <ErrorRow text="Не удалось загрузить машину" onRetry={card.reload} />;
    }
    if (!d || !m || !st) return <Skeleton />;
    const off = m.state === 'offline' || m.state === 'disabled';
    return (
      <>
        {(card.error || ws.error?.id === id) && (
          <div className="md-err" role="alert">
            <span>{card.error ?? ws.error?.text}</span>
            <button
              type="button"
              className="md-link"
              onClick={() => {
                card.clearError();
                ws.clear();
              }}
            >
              Закрыть
            </button>
          </div>
        )}
        <div className="md-status" data-st={st.st}>
          <span className="mo-dot" data-st={st.st} />
          <span className="md-spin" aria-hidden="true" />
          <b>{st.label}</b>
          <span className="md-status__hint">{st.hint}</span>
          {busy?.kind === 'drain' && (
            <button type="button" className="md-link" onClick={() => void card.cancelRestart()}>
              Отменить
            </button>
          )}
        </div>
        {mlUrl && (
          <a className="md-ext" href={mlUrl} target="_blank" rel="noopener noreferrer">
            <span className="md-ext__ic">
              <Icon i="activity" size="sm" />
            </span>
            <span className="md-ext__t">
              <b>Подробнее в ML Core</b>
              <small>процессы, логи и графики машины по минутам</small>
            </span>
            <Icon i="arrow-up-right" size="sm" className="md-ext__go" />
          </a>
        )}
        <div className="md-sec">
          <div className="md-sec__t">Нагрузка за сутки</div>
          <div className="md-load">
            <LoadRow label="Процессор" values={d.series.cpu} off={off} />
            <LoadRow label="Память" values={d.series.ram} off={off} />
            <LoadRow label="Диск" values={d.series.disk} off={off} />
          </div>
        </div>
        <ResourcesSection
          d={d}
          editing={url.tab === 'resources'}
          locked={locked}
          onEdit={() => url.setTab('resources')}
          onCancel={() => url.setTab(null)}
          onApply={apply}
        />
        <AccessSection
          d={d}
          editing={url.tab === 'access'}
          locked={locked}
          onEdit={() => url.setTab('access')}
          onCancel={() => url.setTab(null)}
          onSave={saveAccess}
        />
        {m.canAdmin && (
          <div className="md-sec">
            <div className="md-sec__head">
              <span className="md-sec__t">Название</span>
              {renaming === null && busy == null && (
                <button type="button" className="ad-add" onClick={() => setRenaming(m.name)}>
                  Переименовать
                </button>
              )}
            </div>
            {renaming === null ? (
              <p className="md-acc-sum">{m.name}</p>
            ) : (
              <form
                className="md-rename"
                onSubmit={(e) => {
                  e.preventDefault();
                  void saveName();
                }}
              >
                <input
                  className="hx-set__input"
                  value={renaming}
                  maxLength={60}
                  autoFocus
                  aria-label="Название машины"
                  aria-invalid={cleanName(renaming) === null || undefined}
                  onChange={(e) => setRenaming(e.target.value)}
                />
                <Btn variant="ghost" size="sm" onClick={() => setRenaming(null)}>
                  Отмена
                </Btn>
                <Btn variant="primary" size="sm" type="submit" disabled={cleanName(renaming) === null}>
                  Сохранить
                </Btn>
              </form>
            )}
            <p className="md-note">
              Название от 1 до 60 символов. Так машину видят все, у кого есть к ней доступ.
            </p>
          </div>
        )}
        {m.canAdmin && (
          <div className="md-sec md-remove">
            <div className="md-sec__t">Убрать машину</div>
            <p className="md-note">
              Машина пропадёт из хаба, её ключ перестанет работать. Сам сервер не удаляется.
            </p>
            <Btn
              variant="outline"
              size="sm"
              icon="trash-2"
              className="md-danger"
              disabled={busy != null}
              onClick={() => url.setDlg('remove')}
            >
              Убрать машину…
            </Btn>
          </div>
        )}
      </>
    );
  };

  const canRestart = m != null && m.canManage && m.state !== 'offline' && busy == null;
  const actions =
    d && m ? (
      <div className="md-actions">
        {disabled ? (
          busy == null && (
            <Btn variant="primary" size="sm" icon="play" onClick={() => void card.enable()}>
              Подключить обратно
            </Btn>
          )
        ) : (
          <>
            {canOpen(m) && (
              <Btn
                variant="primary"
                size="sm"
                icon="external-link"
                busy={ws.busyId === m.id}
                disabled={busy != null}
                onClick={() => void ws.open(m.id)}
              >
                Открыть
              </Btn>
            )}
            {m.runnerOutdated && (
              <Btn
                variant="outline"
                size="sm"
                icon="download"
                aria-disabled={!canRestart}
                title={
                  !m.canManage
                    ? NEEDS.team_lead
                    : 'Раннер подхватит новую версию при перезапуске: выберем «Раннер» и подтвердим'
                }
                onClick={() => canRestart && url.setDlg('restart')}
              >
                Обновить раннер
              </Btn>
            )}
            <Btn
              variant="outline"
              size="sm"
              icon="rotate-cw"
              aria-disabled={!canRestart}
              title={
                !m.canManage ? NEEDS.team_lead : m.state === 'offline' ? 'Машина не на связи' : undefined
              }
              onClick={() => canRestart && url.setDlg('restart')}
            >
              Перезапустить
            </Btn>
            <span className="hx-spacer" />
            <Btn
              variant="ghost"
              size="sm"
              className="md-danger"
              aria-disabled={!m.canAdmin || busy != null}
              title={!m.canAdmin && m.needs ? NEEDS[m.needs] : undefined}
              onClick={() => m.canAdmin && busy == null && url.setDlg('off')}
            >
              Отключить
            </Btn>
          </>
        )}
      </div>
    ) : null;

  const dialogs =
    d && m ? (
      <MachineDialogs
        dlg={url.dlg}
        m={m}
        plan={d.plan}
        onClose={() => url.setDlg(null)}
        onRestart={(what, when) => {
          url.setDlg(null);
          void card.restart(what, when);
        }}
        onOff={() => {
          url.setDlg(null);
          void card.disable();
        }}
        onRemove={() => {
          url.setDlg(null);
          void card.remove().then((ok) => ok && onRemoved());
        }}
      />
    ) : null;

  return (
    <Drawer
      open={open}
      onClose={url.close}
      className="md"
      label="Машина"
      lead={
        <span className="md-ic">
          <Icon i="server" />
        </span>
      }
      title={m?.name ?? ''}
      sub={m ? cardSub(m) : undefined}
      over={
        <>
          {actions}
          {dialogs}
        </>
      }
    >
      {body()}
    </Drawer>
  );
};
