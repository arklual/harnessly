// A machine's card in the sheet: state, load of the day, resources, who may start chats, restart and disable.
// Every action is pessimistic: the button spins, the card changes after the hub answered.
import { useCallback, useEffect, useState } from 'react';
import { Activity, ArrowUpRight, Play, RotateCw, Server } from 'lucide-react';

import { Button, Spinner } from '@kit';

import type { MachineBusyKind, MachineDetail } from '../../api/machines';
import { orgErrorText } from '../../api/errors';
import { useNow } from '../../../lib/hooks';
import { useOrg } from '../../context';
import { useAction } from '../../hooks';
import { Sheet } from '../../ui/sheet';
import { AccessEditor, accessSummary, Chip } from './access';
import { MoveDialog, OffDialog, RestartDialog } from './dialogs';
import { BUSY_TEXT, gb, plural, rub, stateOf } from './format';
import { Spark } from './parts';
import { ResourceEditor } from './resources';

type Edit = 'resources' | 'access' | null;
type Dlg = 'restart' | 'off' | 'move' | null;

const Body = ({
  id,
  mlUrl,
  onChanged,
  onClose,
}: {
  id: string;
  mlUrl: string | null;
  onChanged: () => void;
  onClose: () => void;
}) => {
  const { api } = useOrg();
  const now = useNow(60_000);
  const [d, setD] = useState<MachineDetail | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [edit, setEdit] = useState<Edit>(null);
  const [dlg, setDlg] = useState<Dlg>(null);
  const [running, setRunning] = useState<MachineBusyKind | null>(null);

  const reload = useCallback(() => {
    setLoadError(null);
    api.machineDetail(id).then(setD, setLoadError);
  }, [api, id]);
  useEffect(() => {
    let alive = true;
    api.machineDetail(id).then(
      (v) => alive && setD(v),
      (e: unknown) => alive && setLoadError(e),
    );
    return () => {
      alive = false;
    };
  }, [api, id]);
  // A restart that waits for chats goes on in the hub: look again until it is done.
  const waiting = d?.machine.busy?.kind === 'drain';
  useEffect(() => {
    if (!waiting) return;
    const t = window.setInterval(reload, 3000);
    return () => window.clearInterval(t);
  }, [waiting, reload]);

  const done = (next: MachineDetail | undefined) => {
    if (!next) return false;
    setD(next);
    onChanged();
    return true;
  };
  const resize = useAction((a, to: { cores: number; ramGb: number; diskGb: number }) =>
    a.resizeMachine(id, to),
  );
  const restart = useAction((a, what: 'runner' | 'machine', when: 'now' | 'after_chats') =>
    a.restartMachine(id, what, when),
  );
  const cancel = useAction((a) => a.cancelRestart(id));
  const off = useAction((a) => a.disableMachine(id));
  const on = useAction((a) => a.enableMachine(id));
  const access = useAction((a, mode: 'owner' | 'team' | 'tenant' | 'pick', personIds: string[]) =>
    a.setMachineAccess(id, { mode, personIds }),
  );

  const work = async (
    kind: MachineBusyKind,
    run: () => Promise<MachineDetail | undefined>,
  ): Promise<boolean> => {
    setRunning(kind);
    const next = await run();
    setRunning(null);
    return done(next);
  };

  if (loadError) {
    return (
      <div className="hm-state">
        <p>{orgErrorText(loadError)}</p>
        <Button onClick={reload}>Повторить</Button>
      </div>
    );
  }
  if (!d) {
    return (
      <div className="hm-state" role="status" aria-label="Загрузка">
        <Spinner size="m" />
      </div>
    );
  }
  const m = d.machine;

  const busyKind = running ?? m.busy?.kind ?? null;
  const st = busyKind
    ? {
        tone: 'busy' as const,
        label: BUSY_TEXT[busyKind](m.busy?.chats ?? m.chats ?? 0)[0],
        hint: BUSY_TEXT[busyKind](m.busy?.chats ?? m.chats ?? 0)[1],
      }
    : stateOf(m, now);
  const disabled = m.state === 'disabled' && !busyKind;
  const locked = busyKind !== null;
  const hint = (who: 'tenant_admin' | 'team_lead' | 'org_admin' | null) =>
    who === 'tenant_admin'
      ? 'Может поменять: админ тенанта'
      : who === 'org_admin'
        ? 'Может поменять: админ организации'
        : 'Может поменять: лид команды';
  const restartNote = m.canManage ? undefined : hint('team_lead');
  const restartTitle = m.state === 'offline' ? 'Машина не на связи' : restartNote;
  const sub = [
    m.tenantName,
    m.kind === 'shared' ? `общая${m.purpose ? `, ${m.purpose}` : ''}` : (m.ownerName ?? ''),
  ]
    .filter(Boolean)
    .join(' · ');
  const load = [
    { k: 'Процессор', v: m.cpu, s: d.series.cpu },
    { k: 'Память', v: m.ram, s: d.series.ram },
    { k: 'Диск', v: m.disk, s: d.series.disk },
  ];
  return (
    <div className="hm-card">
      <p className="hm-sub">{sub}</p>
      <div className="hm-stat" data-tone={st.tone}>
        {st.tone === 'busy' ? (
          <Spinner size="xs" />
        ) : (
          <i className="hm-dot" data-tone={st.tone} aria-hidden="true" />
        )}
        <b>{st.label}</b>
        <span>{st.hint}</span>
        {busyKind === 'drain' && (
          <Button
            size="xs"
            appearance="ghost"
            loading={cancel.pending}
            onClick={() => void cancel.run().then(done)}
          >
            Отменить
          </Button>
        )}
      </div>
      {mlUrl && (
        <a className="hm-ext" href={mlUrl} target="_blank" rel="noreferrer noopener">
          <span className="hm-ext__ic">
            <Activity size={14} />
          </span>
          <span className="hm-ext__t">
            <b>Подробнее в ML Core</b>
            <small>процессы, логи и графики машины по минутам</small>
          </span>
          <ArrowUpRight size={14} aria-hidden="true" />
        </a>
      )}

      {load.some((r) => r.v !== null || r.s.length > 0) && (
        <>
          <h3 className="hm-h">Нагрузка за сутки</h3>
          <div className="hm-loads">
            {load.map((r) => (
              <div key={r.k} className="hm-loads__row">
                <span>{r.k}</span>
                <Spark values={r.s} hot={(r.v ?? 0) >= 85} />
                <b data-hot={(r.v ?? 0) >= 85 || undefined}>{r.v === null ? '—' : `${r.v}%`}</b>
              </div>
            ))}
          </div>
        </>
      )}

      {(m.cloud || m.hw.cores > 0) && (
        <section className="hm-sec">
          <div className="hm-sec__head">
            <h3 className="hm-h">Ресурсы</h3>
            {edit !== 'resources' && m.cloud && !disabled && (
              <Button
                size="xs"
                disabled={!m.canAdmin || locked}
                title={!m.canAdmin ? hint(m.needs) : undefined}
                onClick={() => setEdit('resources')}
              >
                Изменить
              </Button>
            )}
          </div>
          {edit === 'resources' && d.plan ? (
            <ResourceEditor
              m={m}
              plan={d.plan}
              pending={resize.pending}
              error={resize.error}
              onCancel={() => {
                resize.reset();
                setEdit(null);
              }}
              onApply={(to) =>
                void work('resize', () => resize.run(to)).then((ok) => {
                  if (ok) setEdit(null);
                })
              }
            />
          ) : (
            <>
              <div className="hm-res">
                <div>
                  <b>{m.hw.cores}</b>
                  <small>{plural(m.hw.cores, 'ядро', 'ядра', 'ядер')}</small>
                </div>
                <div>
                  <b>{gb(m.hw.ramGb)}</b>
                  <small>память</small>
                </div>
                <div>
                  <b>{gb(m.hw.diskGb)}</b>
                  <small>диск</small>
                </div>
              </div>
              <p className="hm-note">
                {m.cloud
                  ? `Сервер в Selectel${m.monthPriceRub !== null ? ` · ${rub(m.monthPriceRub)} в месяц` : ''}`
                  : 'Ноутбук — ресурсы меняются только на самой машине'}
              </p>
            </>
          )}
        </section>
      )}

      <section className="hm-sec">
        <div className="hm-sec__head">
          <h3 className="hm-h">Кто может запускать чаты</h3>
          {edit !== 'access' && !disabled && (
            <Button
              size="xs"
              disabled={!m.canManage || locked}
              title={!m.canManage ? hint('team_lead') : undefined}
              onClick={() => setEdit('access')}
            >
              Изменить
            </Button>
          )}
        </div>
        {disabled ? (
          <p className="hm-note">Машина отключена</p>
        ) : edit === 'access' ? (
          <AccessEditor
            d={d}
            pending={access.pending}
            error={access.error}
            onCancel={() => {
              access.reset();
              setEdit(null);
            }}
            onSave={(mode, ids) =>
              void access.run(mode, ids).then((next) => {
                if (done(next)) setEdit(null);
              })
            }
          />
        ) : (
          <>
            <p className="hm-note">{accessSummary(d)}</p>
            <p className="hm-note">Пока это только запись: открыть машину может любой в тенанте.</p>
            {d.access.mode === 'pick' && (
              <div className="hm-chips">
                {d.access.people.map((p) => (
                  <Chip key={p.id} name={p.name} />
                ))}
              </div>
            )}
          </>
        )}
      </section>

      <footer className="hm-foot">
        <Button
          icon={<RotateCw size={14} />}
          disabled={!m.canManage || locked || disabled || m.state === 'offline'}
          title={restartTitle}
          onClick={() => {
            restart.reset();
            setDlg('restart');
          }}
        >
          Перезапустить
        </Button>
        <Button
          appearance="ghost"
          disabled={!m.canAdmin || locked}
          title={!m.canAdmin ? hint(m.needs) : undefined}
          onClick={() => setDlg('move')}
        >
          Перенести
        </Button>
        {disabled ? (
          <Button
            appearance="primary"
            icon={<Play size={14} />}
            loading={on.pending}
            disabled={!m.canAdmin}
            title={!m.canAdmin ? hint(m.needs) : undefined}
            onClick={() => void work('on', () => on.run())}
          >
            Подключить обратно
          </Button>
        ) : (
          <Button
            appearance="ghost"
            className="hm-danger"
            disabled={!m.canAdmin || locked}
            title={!m.canAdmin ? hint(m.needs) : undefined}
            onClick={() => {
              off.reset();
              setDlg('off');
            }}
          >
            Отключить
          </Button>
        )}
      </footer>

      <RestartDialog
        open={dlg === 'restart'}
        m={m}
        pending={restart.pending}
        error={restart.error}
        onClose={() => setDlg(null)}
        onConfirm={(what, when) =>
          void work(when === 'after_chats' ? 'drain' : what === 'runner' ? 'restart' : 'reboot', () =>
            restart.run(what, when),
          ).then((ok) => ok && setDlg(null))
        }
      />
      <MoveDialog
        open={dlg === 'move'}
        m={m}
        onClose={() => setDlg(null)}
        onMoved={() => {
          setDlg(null);
          onChanged();
          onClose();
        }}
      />
      <OffDialog
        open={dlg === 'off'}
        m={m}
        pending={off.pending}
        error={off.error}
        onClose={() => setDlg(null)}
        onConfirm={() => void work('off', () => off.run()).then((ok) => ok && setDlg(null))}
      />
    </div>
  );
};

export const MachineSheet = ({
  id,
  name,
  open,
  mlUrl,
  onClose,
  onChanged,
}: {
  id: string | null;
  name: string;
  open: boolean;
  mlUrl: string | null;
  onClose: () => void;
  onChanged: () => void;
}) => (
  <Sheet
    open={open}
    onClose={onClose}
    title={
      <span className="hm-title">
        <Server size={16} aria-hidden="true" /> {name || 'Машина'}
      </span>
    }
  >
    {id && <Body key={id} id={id} mlUrl={mlUrl} onChanged={onChanged} onClose={onClose} />}
  </Sheet>
);
