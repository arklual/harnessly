// The machine card: what is going on with it, its load over a day, its resources, who may start chats, and the actions.
import { type CSSProperties, useState } from 'react';

import { useNow } from '../../../lib/hooks';
import { useOrg } from '../../../org/context';
import type { OrgMachine } from '../../../org/api/machines';
import { plural } from '../../../org/format/machines';
import { LoadRow } from '../../charts/spark';
import { Btn, Drawer, Icon } from '../../ui';
import { AccessSection } from './access';
import { BuilderSection } from './builder';
import { canOpen, cardSub, cleanName, readiness, readyHeadline, statusOf } from './calc';
import { MachineDialogs } from './dialogs';
import { DebugLog, DebugPanel } from './debug';
import { type OpenMachine, useOpenMachine } from './open';
import { ErrorRow } from './parts';
import { ResourcesSection } from './resources';
import type { DrawerState } from './url';
import { useMachineCard } from './use-machines';

const NEEDS: Record<string, string> = {
  org_admin: 'Может поменять: админ организации',
  tenant_admin: 'Может поменять: админ тенанта',
  team_lead: 'Может поменять: лид команды',
};

const CARD = 't-card viz-card dash-tile hd-card';
const AGENT_NAME = { claude: 'Claude Code', codex: 'Codex' } as const;
const START_CMD = 'harnessly agent start --autostart';
const LIFE = ['Связь', 'Подтверждение', 'Подготовка', 'Готова к чатам'];

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
  opener,
  onChanged,
  onRemoved,
}: {
  /** The list's opener, so that a refusal met in the list is answered in the card. */
  opener?: OpenMachine;
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
  const own = useOpenMachine();
  const ws = opener ?? own;
  const [renaming, setRenaming] = useState<string | null>(null);
  const { api, scope } = useOrg();
  const [copied, setCopied] = useState(false);
  const [reapply, setReapply] = useState<'idle' | 'busy' | 'sent' | 'fail'>('idle');
  const now = useNow(30_000, open);
  const d = card.detail;
  const m = d?.machine ?? hint;
  const busy = d?.machine.busy ?? null;
  const disabled = d?.machine.state === 'disabled';
  const st = d && m ? statusOf(m, busy?.kind ?? null, card.done, now) : null;
  const locked = busy != null || disabled === true;
  const canRestart = m != null && m.canManage && m.state !== 'offline' && busy == null;

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
    // The hub knows only whether the machine answers: before that it is at the first step, after it all are done.
    const setup = d.setup ?? null;
    const needsTrust = !off && setup?.runtime === 'needs_trust';
    const preparing = !off && !needsTrust && setup?.env != null && setup.env.applied < setup.env.total;
    const li = off ? 0 : needsTrust ? 1 : preparing ? 2 : 4;
    const copyCmd = () => {
      void navigator.clipboard?.writeText(START_CMD).then(
        () => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 2000);
        },
        () => undefined,
      );
    };
    const applyAgain = () => {
      setReapply('busy');
      api
        .meRun({ mode: 'update', machineId: m.id }, scope.kind === 'tenant' ? scope.tenant.slug : undefined)
        .then(
          () => {
            setReapply('sent');
            window.setTimeout(() => card.reload(), 4000);
          },
          () => setReapply('fail'),
        );
    };
    const opened = ws.error?.id === id && !ws.error.ownWork ? ws.error : null;
    const ready = readiness(m, setup, opened, now);
    const headline = readyHeadline(ready);
    const pill =
      st.st === 'busy'
        ? { cls: 'info', label: st.label }
        : st.st === 'on'
          ? m.runnerOutdated
            ? { cls: 'warn', label: 'Нужно обновить' }
            : {
                cls: headline === 'Готова к чатам' ? 'ok' : 'warn',
                label: headline === 'Готова к чатам' ? headline : 'Агент на связи',
              }
          : { cls: st.st === 'off' ? 'bad' : '', label: st.label };
    const chatsLine = m.chats
      ? `${m.chats} ${plural(m.chats, 'чат идёт', 'чата идут', 'чатов идут')} сейчас`
      : 'Сейчас свободна';
    const fix =
      st.st === 'busy' ? (
        <div className="fixbox ok">
          <div className="row sp">
            <div>
              <b>{st.label}</b>
              <div className="mut sm">{st.hint}</div>
            </div>
            {busy?.kind === 'drain' && (
              <button type="button" className="md-link hd-link" onClick={() => void card.cancelRestart()}>
                Отменить
              </button>
            )}
          </div>
        </div>
      ) : disabled ? (
        <div className="fixbox">
          <b>{st.label}</b>
          <div className="mut sm" style={{ margin: '6px 0 10px' }}>
            {st.hint}
          </div>
          <Btn variant="primary" size="sm" icon="play" onClick={() => void card.enable()}>
            Подключить обратно
          </Btn>
        </div>
      ) : m.state === 'offline' ? (
        <div className="fixbox bad">
          <b>{st.label}</b>
          <div className="mut sm" style={{ marginTop: 6 }}>
            {st.hint}. Чаты на ней недоступны. Проверьте, что машина включена и у неё есть сеть: хаб увидит её
            сам. Если машина включена, а агент на ней не запущен, запустите его на самой машине:
          </div>
          <div className="row" style={{ margin: '8px 0 10px', gap: 8 }}>
            <code className="hd-code" style={{ flex: 1, overflowX: 'auto', whiteSpace: 'nowrap' }}>
              {START_CMD}
            </code>
            <Btn variant="ghost" size="sm" icon="copy" onClick={copyCmd}>
              {copied ? 'Скопировано' : 'Копировать'}
            </Btn>
          </div>
          <Btn variant="primary" size="sm" icon="refresh-cw" onClick={() => card.reload()}>
            Проверить снова
          </Btn>
        </div>
      ) : needsTrust ? (
        <div className="fixbox">
          <b>Машина ждёт подтверждения</b>
          <div className="mut sm" style={{ margin: '6px 0 10px' }}>
            Хаб открывает страницу машины на своём адресе, поэтому пускает только подтверждённые машины.{' '}
            {m.canAdmin
              ? 'Если вы подключили её сами, подтвердите, что она ваша.'
              : 'Подтвердить её может владелец тенанта: попросите его.'}
          </div>
          {m.canAdmin && (
            <Btn
              variant="primary"
              size="sm"
              icon="shield-check"
              busy={ws.busyId === m.id}
              onClick={() => void ws.trust(m.id).then(() => card.reload())}
            >
              Это моя машина — подтвердить
            </Btn>
          )}
        </div>
      ) : preparing && setup?.env ? (
        <div className="fixbox">
          <b>Машина готовится</b>
          <div className="mut sm" style={{ marginTop: 6 }}>
            Окружение команды стоит на {setup.env.applied} из {setup.env.total}: чаты уже можно открывать,
            остальное ставится при следующем обновлении.
          </div>
        </div>
      ) : m.runnerOutdated ? (
        <div className="fixbox">
          <b>Раннер устарел: {m.runnerVersion}</b>
          <div className="mut sm" style={{ margin: '6px 0 10px' }}>
            Раннер подхватит новую версию при перезапуске: выберем «Раннер» и подтвердим.
          </div>
          <Btn
            variant="primary"
            size="sm"
            icon="download"
            aria-disabled={!canRestart}
            title={!m.canManage ? NEEDS.team_lead : undefined}
            onClick={() => canRestart && url.setDlg('restart')}
          >
            Обновить раннер
          </Btn>
        </div>
      ) : (
        <div className="fixbox ok">
          <div className="row sp">
            <div>
              <b>{st.label === 'Простаивает' ? 'Простаивает' : headline}</b>
              <div className="mut sm">{card.done ?? chatsLine}</div>
            </div>
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
          </div>
        </div>
      );
    return (
      <>
        {ws.error?.id === id && ws.error.untrusted && !card.error && (
          <div className="hxa__msg hxa__trust" data-tone="warn" role="alert">
            <Icon i="shield-check" />
            <div className="hxa__stack">
              <strong>Машина ждёт доверия</strong>
              <span>
                Хаб открывает страницу машины на своём адресе, поэтому пускает только доверенные машины.{' '}
                {m.canAdmin
                  ? 'Если вы подключили её сами и это ваша машина, доверьтесь ей: после этого откроются чаты.'
                  : 'Довериться ей может владелец тенанта: попросите его, и чаты на машине откроются.'}
              </span>
              {m.canAdmin && (
                <div className="hxa__actions">
                  <Btn variant="primary" size="sm" busy={ws.busyId === id} onClick={() => void ws.trust(id)}>
                    Доверять машине
                  </Btn>
                  <Btn variant="ghost" size="sm" onClick={() => ws.clear()}>
                    Закрыть
                  </Btn>
                </div>
              )}
            </div>
          </div>
        )}
        {ws.error?.id === id && ws.error.ownWork && !card.error && (
          <DebugPanel
            busy={ws.busyId === id}
            failed={ws.error.debugFailed ?? null}
            onOpen={(reason) => void ws.debug(id, reason)}
            onClose={() => ws.clear()}
          />
        )}
        {(card.error || (ws.error?.id === id && !ws.error.untrusted && !ws.error.ownWork)) && (
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
        <div className="dh">
          <div>
            <h2>{m.name}</h2>
            <div className="mut sm">
              {cardSub(m)}
              {m.runnerVersion ? ` · раннер ${m.runnerVersion}` : ''}
            </div>
          </div>
          <div className="hd-x row">
            <span className={`pill ${pill.cls}`}>
              <span className="hd-d" />
              {pill.label}
            </span>
            <Btn variant="ghost" size="sm" shape="icon" aria-label="Закрыть" onClick={url.close}>
              <Icon i="x" />
            </Btn>
          </div>
        </div>
        <div className="life">
          {LIFE.map((s, i) => {
            const done = i < li || li === 4;
            return (
              <div key={s} className={`st ${done ? 'done' : i === li ? 'cur' : ''}`}>
                <b>{s}</b>
                {done ? 'готово' : i === li ? 'сейчас здесь' : 'дальше'}
              </div>
            );
          })}
        </div>
        <div style={{ height: 14 }} />
        {fix}
        <div className="sec">
          <h4>Готовность по частям</h4>
          <div className={CARD}>
            {ready.map((r) => (
              <div key={r.key} style={{ marginBottom: 8 }}>
                <div className="row">
                  <b>{r.label}</b>
                  <span className={`pill ${r.tone === 'unknown' ? '' : r.tone}`}>
                    <span className="hd-d" />
                    {r.tone === 'ok' ? 'в порядке' : r.tone === 'unknown' ? 'не известно' : 'есть проблема'}
                  </span>
                </div>
                <div className="mut sm" style={{ marginTop: 2 }}>
                  {r.text}
                </div>
              </div>
            ))}
          </div>
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
        <div className="sec">
          <h4>Чья машина</h4>
          <div className={CARD}>
            <div className="set" style={{ padding: '0 0 12px' }}>
              <div>
                <div className="hd-t">
                  {m.ownerName
                    ? `Владелец — ${m.ownerName}`
                    : `Общая машина${m.teamName ? ` команды ${m.teamName}` : ''}`}
                </div>
                <div className="hd-d">
                  Машина, подготовленная админом для сотрудника, становится его: с тем же id, ключом и чатами.
                </div>
              </div>
              {m.canAdmin && (
                <Btn
                  variant="outline"
                  size="sm"
                  aria-disabled={locked}
                  onClick={() => {
                    if (locked) return;
                    card.clearError();
                    url.setDlg('owner');
                  }}
                >
                  Передать
                </Btn>
              )}
            </div>
            <div className="set" style={{ padding: '12px 0 0' }}>
              <div>
                <div className="hd-t">
                  Тенант — {m.tenantName}
                  {m.teamName ? ` · команда ${m.teamName}` : ''}
                </div>
                <div className="hd-d">
                  Машина сохранит id и ключ. Перед переносом покажем, что изменится: команда, доступ и лимиты.
                </div>
              </div>
              {m.canAdmin && (
                <Btn variant="outline" size="sm" disabled={locked} onClick={() => url.setDlg('move')}>
                  Перенести
                </Btn>
              )}
            </div>
          </div>
        </div>
        <AccessSection
          d={d}
          editing={url.tab === 'access'}
          locked={locked}
          onEdit={() => url.setTab('access')}
          onCancel={() => url.setTab(null)}
          onSave={saveAccess}
        />
        {(m.runnerVersion || (setup && setup.agents.length > 0)) && (
          <div className="sec">
            <h4>Агенты на машине</h4>
            <div className={CARD}>
              {setup?.agents.map((a) => {
                const acc = setup.accounts.find((c) => c.provider === a.provider);
                const login = !a.installed
                  ? 'не поставлен'
                  : a.login === 'out'
                    ? 'вход не выполнен'
                    : acc
                      ? `работает на подписке «${acc.label}»`
                      : a.login === 'in'
                        ? 'вход выполнен'
                        : null;
                return (
                  <div key={a.provider} style={{ marginBottom: 8 }}>
                    <div className="row">
                      <b>{AGENT_NAME[a.provider]}</b>
                      <span className={`pill ${!a.installed ? '' : a.login === 'out' ? 'warn' : 'ok'}`}>
                        <span className="hd-d" />
                        {a.installed ? (a.version ?? 'поставлен') : 'не поставлен'}
                      </span>
                    </div>
                    {login && (
                      <div className="mut sm" style={{ marginTop: 2 }}>
                        {login}
                      </div>
                    )}
                  </div>
                );
              })}
              {m.runnerVersion && (
                <div className="row sp">
                  <div className="row">
                    <b>Раннер harnessly</b>
                    <span className={`pill ${m.runnerOutdated ? 'warn' : 'ok'}`}>
                      <span className="hd-d" />
                      {m.runnerVersion}
                      {m.runnerOutdated ? ' · устарел' : ''}
                    </span>
                  </div>
                </div>
              )}
              {m.os && (
                <div className="mut sm" style={{ marginTop: 4 }}>
                  {m.os}
                </div>
              )}
            </div>
          </div>
        )}
        {setup && (
          <div className="sec">
            <h4>Окружение и подписка</h4>
            <div className={CARD}>
              <div className="row sp">
                <div>
                  {setup.env ? (
                    <div>
                      Окружение команды применено{' '}
                      <b>
                        {setup.env.applied} из {setup.env.total}
                      </b>
                    </div>
                  ) : (
                    <div className="mut">Окружение: машина ещё не отчиталась</div>
                  )}
                  <div style={{ marginTop: 4 }}>
                    {setup.accounts.length > 0 ? (
                      <>
                        Подписка —{' '}
                        <b>
                          {setup.accounts
                            .map((c) => (c.plan ? `${c.label} (${c.plan})` : c.label))
                            .join(', ')}
                        </b>
                      </>
                    ) : (
                      <span className="mut">Подписка: чатов на машине ещё не было</span>
                    )}
                  </div>
                </div>
                {m.mine && !disabled && m.state !== 'offline' && (
                  <Btn
                    variant="ghost"
                    size="sm"
                    icon="refresh-cw"
                    busy={reapply === 'busy'}
                    disabled={busy != null}
                    onClick={applyAgain}
                  >
                    {reapply === 'sent'
                      ? 'Запущено'
                      : reapply === 'fail'
                        ? 'Не вышло, ещё раз'
                        : 'Применить заново'}
                  </Btn>
                )}
              </div>
            </div>
          </div>
        )}
        <ResourcesSection
          d={d}
          editing={url.tab === 'resources'}
          locked={locked}
          onEdit={() => url.setTab('resources')}
          onCancel={() => url.setTab(null)}
          onApply={apply}
          meters={
            m.cpu != null ? (
              (
                [
                  ['Процессор', m.cpu],
                  ['Память', m.ram],
                  ['Диск', m.disk],
                ] as const
              ).map(([l, v]) =>
                v == null ? null : (
                  <div key={l} className="row" style={{ margin: '6px 0' }}>
                    <span className="mut" style={{ width: 90 }}>
                      {l}
                    </span>
                    <div style={{ flex: 1 }}>
                      <span
                        className="hd-meter"
                        data-t={v > 85 ? 'bad' : v > 65 ? 'warn' : 'ok'}
                        style={{ '--w': `${Math.max(2, Math.min(100, v))}%` } as CSSProperties}
                      >
                        <i />
                      </span>
                    </div>
                    <span className="num" style={{ width: 44 }}>
                      {Math.round(v)}%
                    </span>
                  </div>
                ),
              )
            ) : (
              <div className="mut sm">Появятся, когда машина будет на связи.</div>
            )
          }
        />
        <div className="sec">
          <h4>Нагрузка за сутки</h4>
          <div className="md-load">
            <LoadRow label="Процессор" values={d.series.cpu} off={off} />
            <LoadRow label="Память" values={d.series.ram} off={off} />
            <LoadRow label="Диск" values={d.series.disk} off={off} />
          </div>
        </div>
        <BuilderSection id={id} tenantId={m.tenantId} canAdmin={m.canAdmin} />
        {/* The hub says whom it lists the openings to (canSeeDebug): the creator and who manages the tenant. */}
        {m.canSeeDebug === true && <DebugLog id={id} />}
        <div className="sec">
          <h4>Действия</h4>
          <div className="row" style={{ flexWrap: 'wrap' }}>
            {m.canAdmin && renaming === null && busy == null && (
              <Btn variant="outline" size="sm" onClick={() => setRenaming(m.name)}>
                Переименовать
              </Btn>
            )}
            {!disabled && (
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
            )}
            {!disabled && (
              <Btn
                variant="outline"
                size="sm"
                className="md-danger"
                aria-disabled={!m.canAdmin || busy != null}
                title={!m.canAdmin && m.needs ? NEEDS[m.needs] : undefined}
                onClick={() => m.canAdmin && busy == null && url.setDlg('off')}
              >
                Отключить
              </Btn>
            )}
            {m.canAdmin && (
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
            )}
          </div>
          {m.canAdmin && renaming !== null && (
            <>
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
              <p className="md-note">
                Название от 1 до 60 символов. Так машину видят все, у кого есть к ней доступ.
              </p>
            </>
          )}
          {m.canAdmin && (
            <p className="md-note">
              «Убрать машину»: она пропадёт из хаба, её ключ перестанет работать. Сам сервер не удаляется.
            </p>
          )}
        </div>
      </>
    );
  };

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
        onMoved={() => {
          url.setDlg(null);
          onRemoved();
        }}
        people={d.candidates}
        ownerError={card.error}
        onOwner={async (userId, history) => {
          const ok = await card.assignOwner(userId, history);
          if (ok) url.setDlg(null);
          return ok;
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
      over={<>{dialogs}</>}
    >
      {body()}
    </Drawer>
  );
};
