// «Роль машины» of the machine card: chats' machine or a builder for heavy jobs (renders, builds), with its
// labels and slots, and the pool of the tenant's builders with their load. docs/hub/builders.md.
import { useEffect, useState } from 'react';

import { http } from '../../../org/api/live/http';
import { Btn } from '../../ui';

interface Role {
  role: 'agents' | 'builder';
  caps: string[];
  slots: number;
}

interface Builder {
  id: string;
  name: string;
  caps: string[];
  slots: number;
  running: number;
  waiting: number;
  online: boolean;
  ready: boolean;
  note?: string;
}

export const parseCaps = (s: string): string[] => [
  ...new Set(
    s
      .split(/[\s,]+/)
      .map((c) => c.trim().toLowerCase())
      .filter(Boolean),
  ),
];

const load = (b: Builder) => `${b.running} из ${b.slots}${b.waiting > 0 ? ` · ждут ${b.waiting}` : ''}`;

export const BuilderSection = ({
  id,
  tenantId,
  canAdmin,
}: {
  id: string;
  tenantId: string;
  canAdmin: boolean;
}) => {
  const [role, setRole] = useState<Role | null>(null);
  const [pool, setPool] = useState<Builder[]>([]);
  const [editing, setEditing] = useState(false);
  const [mode, setMode] = useState<Role['role']>('agents');
  const [caps, setCaps] = useState('');
  const [slots, setSlots] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [rev, setRev] = useState(0);
  useEffect(() => {
    let live = true;
    const read = () =>
      Promise.all([
        http.get<{ machine: Role }>(`/api/machines/${encodeURIComponent(id)}`),
        http.get<{ builders: Builder[] }>(`/api/spaces/${encodeURIComponent(tenantId)}/builders`),
      ]).then(
        ([m, p]) => {
          if (!live) return;
          setRole({
            role: m.machine.role ?? 'agents',
            caps: m.machine.caps ?? [],
            slots: m.machine.slots ?? 0,
          });
          setPool(p.builders);
        },
        () => live && setRole(null),
      );
    void read();
    const t = setInterval(() => void read(), 15_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [id, tenantId, rev]);
  if (!role) return null;
  const isBuilder = role.role === 'builder';
  const start = () => {
    setMode(role.role);
    setCaps(role.caps.join(', '));
    setSlots(role.slots > 0 ? String(role.slots) : '');
    setErr('');
    setEditing(true);
  };
  const save = async () => {
    setBusy(true);
    setErr('');
    try {
      await http.put(`/api/machines/${encodeURIComponent(id)}/builder`, {
        role: mode,
        caps: parseCaps(caps),
        slots: Number.parseInt(slots, 10) || 0,
      });
      setEditing(false);
      setRev((r) => r + 1);
    } catch (e) {
      setErr(e instanceof Error && e.message ? e.message : 'Не получилось сохранить роль');
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="md-sec" data-testid="builder-section">
      <div className="md-sec__head">
        <span className="md-sec__t">Роль машины</span>
        {canAdmin && !editing && (
          <button type="button" className="hb-add" onClick={start}>
            Изменить
          </button>
        )}
      </div>
      {!editing && (
        <p className="md-acc-sum">
          {isBuilder
            ? `Билдер · ${role.slots} ${role.slots === 1 ? 'слот' : 'слота'} · ${role.caps.join(', ') || 'без меток'}`
            : 'Чаты и агенты (по умолчанию)'}
        </p>
      )}
      {editing && (
        <div className="md-edit">
          <div className="md-radio" role="radiogroup" aria-label="Роль машины">
            {(
              [
                ['agents', 'Чаты и агенты', 'сюда хаб размещает чаты людей'],
                ['builder', 'Билдер', 'тяжёлые задачи (рендеры, сборки); чаты сюда не попадают'],
              ] as const
            ).map(([v, label, hint]) => (
              <button
                key={v}
                type="button"
                className="md-radio__o"
                role="radio"
                aria-checked={mode === v}
                onClick={() => setMode(v)}
              >
                <span className="md-radio__dot" />
                <span>
                  <b>{label}</b>
                  <small>{hint}</small>
                </span>
              </button>
            ))}
          </div>
          {mode === 'builder' && (
            <>
              <input
                className="hx-set__input"
                value={caps}
                aria-label="Метки возможностей"
                placeholder="blender, gpu, ffmpeg, docker (пусто — определит агент машины)"
                onChange={(e) => setCaps(e.target.value)}
              />
              <input
                className="hx-set__input"
                value={slots}
                inputMode="numeric"
                aria-label="Число тяжёлых слотов"
                placeholder="слотов (пусто — по ядрам машины)"
                onChange={(e) => setSlots(e.target.value.replace(/\D/g, '').slice(0, 2))}
              />
              <p className="md-acc-sum">
                На самой машине должно быть AGENT_HUB_JOBS=on: без согласия хозяина машины агент задачи не
                берёт.
              </p>
            </>
          )}
          {err && <span className="md-err">{err}</span>}
          <div className="hxa__actions">
            <Btn variant="primary" size="sm" busy={busy} onClick={() => void save()}>
              Сохранить
            </Btn>
            <Btn variant="ghost" size="sm" onClick={() => setEditing(false)}>
              Отмена
            </Btn>
          </div>
        </div>
      )}
      {pool.length > 0 && (
        <ul className="md-debug" aria-label="Пул билдеров">
          {pool.map((b) => (
            <li key={b.id}>
              <b>{b.name}</b> · {b.ready ? 'готов' : (b.note ?? 'не готов')} · слоты {load(b)}
              <p>{b.caps.join(', ') || 'без меток'}</p>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

/**
 * «Билдеры» of the machines screen: the pool of builders of every tenant in view with their load, refreshed
 * while the screen is open. Empty: how a machine becomes one.
 */
export const BuilderPool = ({ tenants }: { tenants: readonly { id: string; name: string }[] }) => {
  const [pool, setPool] = useState<(Builder & { tenant: string })[] | null>(null);
  const key = tenants.map((t) => t.id).join(',');
  useEffect(() => {
    let live = true;
    const read = () =>
      Promise.all(
        tenants.map((t) =>
          http
            .get<{ builders: Builder[] }>(`/api/spaces/${encodeURIComponent(t.id)}/builders`)
            .then((p) => p.builders.map((b) => ({ ...b, tenant: t.name })))
            .catch(() => [] as (Builder & { tenant: string })[]),
        ),
      ).then((all) => live && setPool(all.flat()));
    void read();
    const t = setInterval(() => void read(), 15_000);
    return () => {
      live = false;
      clearInterval(t);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  if (pool === null) return null;
  const many = tenants.length > 1;
  return (
    <section className="t-card hx-bp" data-hx-part="builders" aria-label="Билдеры">
      <header className="hx-bp__h">
        <b>Билдеры</b>
        <span>Машины для тяжёлых задач: рендеры, сборки. Чаты людей сюда не попадают</span>
      </header>
      {pool.length === 0 ? (
        <p className="hx-bp__empty">
          Билдеров пока нет. Откройте машину и в «Роль машины» выберите «Билдер»: хаб будет отдавать ей
          тяжёлые задачи по меткам и слотам.
        </p>
      ) : (
        <ul className="hx-bp__list">
          {pool.map((b) => {
            const pct = b.slots > 0 ? Math.min(100, Math.round((b.running / b.slots) * 100)) : 0;
            const st = !b.online ? 'off' : !b.ready ? 'warn' : b.waiting > 0 ? 'busy' : 'ok';
            return (
              <li key={`${b.tenant}:${b.id}`} className="hx-bp__row" data-st={st}>
                <span className="hx-bp__dot" aria-hidden="true" />
                <span className="hx-bp__name">
                  <b>{b.name}</b>
                  <small>
                    {many ? `${b.tenant} · ` : ''}
                    {!b.online ? 'не в сети' : b.ready ? 'готов' : (b.note ?? 'не готов')}
                  </small>
                </span>
                <span className="hx-bp__caps">
                  {b.caps.length ? b.caps.map((c) => <i key={c}>{c}</i>) : <small>без меток</small>}
                </span>
                <span className="hx-bp__load" title={`Занято слотов: ${load(b)}`}>
                  <span className="hx-bp__bar">
                    <i style={{ width: `${pct}%` }} />
                  </span>
                  <small>{load(b)}</small>
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
};
