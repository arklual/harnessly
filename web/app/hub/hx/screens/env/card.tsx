// The drawer of one entry: its numbers, who it is for, whether it is pinned. Saving sends only what changed; the drawer
// stays (and says why) when the hub refuses, and closes when it takes the change.
import { type CSSProperties, useEffect, useState } from 'react';

import type {
  EnvCard,
  EnvEntryRow,
  EnvLayer,
  EnvResponse,
  LockedReason,
  PatchEntryRequest,
} from '../../../../hub-api/env';
import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Btn, Drawer, Icon } from '../../ui';
import {
  type Draft,
  dayBars,
  draftChanged,
  draftOf,
  draftPatch,
  draftValid,
  findRow,
  fmtNum,
  hasPatch,
  KIND_ICON,
  KIND_WORD,
  type Mode,
  withAdded,
  withoutEntry,
  withPatch,
  withRow,
} from './helpers';
import type { EnvView } from './state';

type Vars = CSSProperties & Record<`--${string}`, string | number>;

const LOCKED_TEXT: Record<LockedReason, [string, string]> = {
  pinned_by_org: [
    'Закреплено организацией',
    'Есть у всех во всех тенантах. Изменить может админ организации.',
  ],
  pinned_by_tenant: ['Закреплено тенантом', 'Есть у всех команд тенанта. Изменить может админ.'],
  no_rights: ['Менять нельзя', 'Изменить может руководитель слоя или админ организации.'],
};

const modesOf = (layer: EnvLayer, teamName: string): { id: Mode; label: string; hint: string }[] =>
  layer.layer === 'team'
    ? [
        { id: 'all', label: `Команде ${teamName}`, hint: 'Всем, кто в команде' },
        { id: 'off', label: 'Выключено', hint: 'Останется в окружении, но в чатах не появится' },
      ]
    : [
        { id: 'all', label: 'Всему тенанту', hint: 'Всем командам и людям без команды' },
        { id: 'teams', label: 'Только командам', hint: 'Выберите, каким' },
        { id: 'off', label: 'Выключено', hint: 'Останется в окружении, но в чатах не появится' },
      ];

const inversePatch = (
  row: Pick<EnvEntryRow, 'enabled' | 'audience' | 'pinned'>,
  patch: PatchEntryRequest,
): PatchEntryRequest => {
  const back: PatchEntryRequest = {};
  if (patch.enabled !== undefined) back.enabled = row.enabled;
  if (patch.audience) back.audience = { mode: row.audience.mode, teams: [...row.audience.teams] };
  if (patch.pinned !== undefined) back.pinned = row.pinned;
  return back;
};

export const CardDrawer = ({
  entryId,
  view,
  state,
  canPin,
  onClose,
  onProblem,
}: {
  entryId: string | null;
  view: EnvResponse;
  state: EnvView;
  /** An admin pins; a lead does not. */
  canPin: boolean;
  onClose: () => void;
  onProblem: (text: string) => void;
}) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  // Keep the last entry on screen while the drawer slides out.
  const [shown, setShown] = useState<string | null>(entryId);
  if (entryId && entryId !== shown) setShown(entryId);
  const open = entryId !== null;
  const found = shown ? findRow(view, shown) : null;

  const [card, setCard] = useState<{ id: string; data: EnvCard | null; error: unknown }>({
    id: '',
    data: null,
    error: null,
  });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!entryId) return;
    let alive = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCard({ id: entryId, data: null, error: null });
    api.envCard(org, entryId).then(
      (data) => alive && setCard({ id: entryId, data, error: null }),
      (error: unknown) => alive && setCard({ id: entryId, data: null, error }),
    );
    return () => {
      alive = false;
    };
  }, [api, org, entryId, tick]);

  const [draft, setDraft] = useState<Draft>({ mode: 'all', teams: [], pinned: false });
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [wasOpen, setWasOpen] = useState(false);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open && found) {
      setDraft(draftOf(found.row));
      setErr(null);
      setSaving(false);
    }
  }

  if (!found)
    return (
      <Drawer open={false} onClose={onClose} label="Элемент окружения" title="">
        {null}
      </Drawer>
    );
  const { layer, row } = found;
  const mine = card.id === shown ? card : { id: '', data: null, error: null };
  const data = mine.data;
  const locked = row.locked && row.lockedReason !== null;
  const teamName =
    view.teams.find((t) => t.id === layer.scopeId)?.name ?? layer.title.replace(/^Команда\s+/, '');
  const teamsHere =
    layer.layer === 'org' ? view.teams : view.teams.filter((t) => t.tenantId === layer.tenantId);
  const base = draftOf(row);
  const changed = draftChanged(base, draft);
  const disabled = !changed || !draftValid(draft);
  const callsWeek = data ? data.callsWeek : row.callsWeek;

  const save = async () => {
    if (disabled || saving) return;
    const patch = draftPatch(row, draft, layer.layer);
    if (!hasPatch(patch)) {
      onClose();
      return;
    }
    const back = inversePatch(row, patch);
    setSaving(true);
    setErr(null);
    const res = await state.write({
      apply: (v) => withPatch(v, row.entryId, patch),
      undo: (v) => withPatch(v, row.entryId, back),
      run: () => api.envPatch(org, row.entryId, patch),
      settle: (v, answer) => withRow(v, answer),
    });
    setSaving(false);
    if (res.ok) onClose();
    else setErr(orgErrorText(res.error));
  };

  const remove = () => {
    const at = { layer: layer.layer, scopeId: layer.scopeId };
    onClose();
    void state
      .write({
        apply: (v) => withoutEntry(v, row.entryId),
        undo: (v) => withAdded(v, at, row),
        run: () => api.envRemove(org, row.entryId),
      })
      .then((res) => {
        if (!res.ok) onProblem(orgErrorText(res.error));
      });
  };

  const stats: [string, string][] = [
    [callsWeek === null ? '—' : fmtNum(callsWeek), 'вызовов за неделю'],
    [data?.people == null ? '—' : fmtNum(data.people), 'человек пользовались'],
    [data?.perDay == null ? '—' : fmtNum(data.perDay), 'в среднем за день'],
  ];
  const unused = data ? data.unused === true : row.unused === true;
  const days = data ? dayBars(data.bars14, data.callsWeek) : [];
  const [lockTitle, lockText] = row.lockedReason ? LOCKED_TEXT[row.lockedReason] : ['', ''];
  const pinnable = canPin && layer.layer !== 'team';

  return (
    <Drawer
      open={open}
      onClose={onClose}
      className="md es"
      label={row.name}
      lead={
        <span className="ad-ic es-ic">
          <Icon i={KIND_ICON[row.kind]} />
        </span>
      }
      title={row.name}
      sub={`${KIND_WORD[row.kind]} · ${row.hint} · ${layer.title}`}
      over={
        <>
          {err && (
            <p className="es-err" role="alert">
              {err}
            </p>
          )}
          <div className="md-actions">
            {!locked && (
              <Btn variant="ghost" size="sm" className="md-danger" onClick={remove} disabled={saving}>
                Убрать из окружения
              </Btn>
            )}
            <span className="hx-spacer" />
            <Btn variant="ghost" size="sm" onClick={onClose}>
              {locked ? 'Закрыть' : 'Отмена'}
            </Btn>
            {!locked && (
              <button
                type="button"
                className="hx-btn md-save"
                data-variant="primary"
                data-size="sm"
                data-busy={saving ? '1' : '0'}
                aria-disabled={disabled ? 'true' : 'false'}
                onClick={save}
              >
                <span className="md-spin" aria-hidden="true" />
                {saving ? 'Сохраняем' : 'Сохранить'}
              </button>
            )}
          </div>
        </>
      }
    >
      <div className="md-res ps-stats">
        {stats.map(([v, k]) => (
          <div key={k}>
            <b>{v}</b>
            <small>{k}</small>
          </div>
        ))}
      </div>
      <div className="md-sec">
        <div className="md-sec__t">Вызовы за две недели</div>
        {mine.error != null ? (
          <p className="es-err" role="alert" style={{ padding: 0 }}>
            {orgErrorText(mine.error)}{' '}
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setTick((n) => n + 1)}
            >
              Повторить
            </button>
          </p>
        ) : !data ? (
          <span className="hx-sk" style={{ display: 'block', height: 110 }} aria-busy="true" />
        ) : (
          <div className="es-chart" data-zero={data.callsWeek ? '0' : '1'}>
            {days.map((d, i) => (
              <span key={i} className="es-bar" title={d.title}>
                <i style={{ '--h': d.h } as Vars} />
                <small>{d.label}</small>
              </span>
            ))}
          </div>
        )}
        {unused && (
          <p className="md-note">Месяц без вызовов. Если не нужно — уберите, агенту меньше читать.</p>
        )}
      </div>
      {locked ? (
        <div className="md-sec">
          <div className="md-sec__t">Кому доступно</div>
          <div className="es-locked">
            <Icon i="lock" size="sm" />
            <span>
              <b>{lockTitle}</b>
              <small>{lockText}</small>
            </span>
          </div>
        </div>
      ) : (
        <>
          <div className="md-sec">
            <div className="md-sec__t">Кому доступно</div>
            <div className="md-radio" role="radiogroup" aria-label="Кому доступно">
              {modesOf(layer, teamName).map((m) => (
                <button
                  key={m.id}
                  type="button"
                  className="md-radio__o"
                  role="radio"
                  aria-checked={draft.mode === m.id ? 'true' : 'false'}
                  disabled={saving}
                  onClick={() => setDraft((d) => ({ ...d, mode: m.id }))}
                >
                  <span className="md-radio__dot" />
                  <span>
                    <b>{m.label}</b>
                    <small>{m.hint}</small>
                  </span>
                </button>
              ))}
            </div>
            {draft.mode === 'teams' && layer.layer !== 'team' && (
              <div className="md-people es-teams">
                {teamsHere.map((t) => {
                  const on = draft.teams.includes(t.id);
                  return (
                    <button
                      key={t.id}
                      type="button"
                      className="hx-chip md-person md-person--pick"
                      aria-pressed={on ? 'true' : 'false'}
                      disabled={saving}
                      onClick={() =>
                        setDraft((d) => ({
                          ...d,
                          teams: on ? d.teams.filter((x) => x !== t.id) : [...d.teams, t.id],
                        }))
                      }
                    >
                      <Icon i={on ? 'check' : 'plus'} size="sm" />
                      {t.name}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
          {pinnable && (
            <div className="md-sec">
              <div className="es-pin">
                <span>
                  <b>Закрепить</b>
                  <small>Команды и люди не смогут выключить у себя</small>
                </span>
                <button
                  type="button"
                  className="hx-switch"
                  role="switch"
                  aria-checked={draft.pinned ? 'true' : 'false'}
                  aria-label="Закрепить"
                  disabled={saving}
                  onClick={() => setDraft((d) => ({ ...d, pinned: !d.pinned }))}
                >
                  <i />
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </Drawer>
  );
};
