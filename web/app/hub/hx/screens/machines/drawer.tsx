// The machine card: what is going on with it, its load over a day, its resources, who may start chats, and the actions.
import { useNow } from '../../../lib/hooks';
import type { OrgMachine } from '../../../org/api/machines';
import { LoadRow } from '../../charts/spark';
import { Btn, Drawer, Icon } from '../../ui';
import { AccessSection } from './access';
import { cardSub, statusOf } from './calc';
import { MachineDialogs } from './dialogs';
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
}: {
  id: string;
  open: boolean;
  /** The machine as the list knows it: the head is drawn from it before the card has loaded. */
  hint: OrgMachine | null;
  mlUrl: string | null;
  url: Pick<DrawerState, 'tab' | 'dlg' | 'close' | 'setTab' | 'setDlg'>;
  onChanged: (m: OrgMachine) => void;
}) => {
  const card = useMachineCard(id, onChanged);
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

  const body = () => {
    if (card.loadError && !d) {
      return <ErrorRow text="Не удалось загрузить машину" onRetry={card.reload} />;
    }
    if (!d || !m || !st) return <Skeleton />;
    const off = m.state === 'offline' || m.state === 'disabled';
    return (
      <>
        {card.error && (
          <div className="md-err" role="alert">
            <span>{card.error}</span>
            <button type="button" className="md-link" onClick={card.clearError}>
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
            {m.runnerOutdated && (
              <Btn
                variant="primary"
                size="sm"
                icon="download"
                aria-disabled
                title="Раннер обновляется вместе с перезапуском на самой машине"
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
