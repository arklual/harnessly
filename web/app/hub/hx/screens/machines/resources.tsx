// «Ресурсы» of the machine card: what the machine has, and for a rented server the sliders that resize it. The price of
// the new size comes from the hub (a quote), not from a formula in the page.
import {
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
  useEffect,
  useRef,
  useState,
} from 'react';

import type { MachineDetail, ResourceQuote } from '../../../org/api/machines';
import { plural } from '../../../org/screens/machines/format';
import { useOrg } from '../../../org/context';
import { Btn } from '../../ui';
import {
  deltaOf,
  gbLabel,
  ramFloor,
  rubOf,
  rubText,
  sameSize,
  snapStop,
  stepStop,
  stopLabel,
  stopPos,
  stopsOf,
  withValue,
} from './calc';

type Size = { cores: number; ramGb: number; diskGb: number };

const NEEDS: Record<string, string> = {
  org_admin: 'Может поменять: админ организации',
  tenant_admin: 'Может поменять: админ тенанта',
  team_lead: 'Может поменять: лид команды',
};

const pct = (n: number) => `${n.toFixed(2)}%`;

interface SliderProps {
  label: string;
  vals: readonly number[];
  firstOk: number;
  cur: number;
  value: number;
  unit: 'cores' | 'gb';
  full: (v: number) => string;
  hint: string;
  onPick: (v: number) => void;
}

const Slider = ({ label, vals, firstOk, cur, value, unit, full, hint, onPick }: SliderProps) => {
  const drag = useRef(false);
  const n = vals.length;
  const sel = Math.max(0, vals.indexOf(value));
  const at = (e: PointerEvent<HTMLDivElement>) => {
    const rc = e.currentTarget.getBoundingClientRect();
    const t = rc.width > 0 ? (e.clientX - rc.left) / rc.width : 0;
    const v = vals[snapStop(t, n, firstOk)];
    if (v !== undefined) onPick(v);
  };
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const dir = ({ ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 } as Record<string, 1 | -1>)[
      e.key
    ];
    if (!dir) return;
    e.preventDefault();
    const v = vals[stepStop(sel, dir, n, firstOk)];
    if (v !== undefined) onPick(v);
  };
  return (
    <div className="md-sl">
      <div className="md-sl__head">
        <span className="md-sl__k">{label}</span>
        <span className="md-sl__v">
          <s data-show={value !== cur}>{full(cur)}</s>
          <b>{full(value)}</b>
        </span>
      </div>
      <div
        className="md-sl__track"
        style={
          {
            '--fill': pct(stopPos(sel, n)),
            '--lock': firstOk > 0 ? pct(stopPos(firstOk, n)) : '0%',
          } as CSSProperties
        }
        role="slider"
        tabIndex={0}
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={Math.max(0, n - 1)}
        aria-valuenow={sel}
        aria-valuetext={full(value)}
        onPointerDown={(e) => {
          drag.current = true;
          e.currentTarget.setPointerCapture?.(e.pointerId);
          at(e);
        }}
        onPointerMove={(e) => drag.current && at(e)}
        onPointerUp={() => {
          drag.current = false;
        }}
        onPointerCancel={() => {
          drag.current = false;
        }}
        onKeyDown={onKey}
      >
        <span className="md-sl__rail" />
        <span className="md-sl__lock" />
        <span className="md-sl__fill" />
        {vals.map((v, i) => {
          const off = i < firstOk;
          return (
            <button
              key={v}
              type="button"
              className="md-sl__stop"
              style={{ '--x': pct(stopPos(i, n)) } as CSSProperties}
              aria-pressed={i === sel}
              data-done={i < sel && !off ? '1' : '0'}
              data-cur={cur === v ? '1' : '0'}
              aria-disabled={off}
              title={off ? hint : ''}
              tabIndex={-1}
              onClick={() => !off && onPick(v)}
            >
              <i />
              <span>{stopLabel(v, unit)}</span>
            </button>
          );
        })}
      </div>
      <small className="md-sl__hint">{hint}</small>
    </div>
  );
};

/** The new price of a size: asked from the hub a moment after the sliders stop. */
const useQuote = (id: string, size: Size, on: boolean): ResourceQuote | null => {
  const { api } = useOrg();
  const key = `${id}:${size.cores}:${size.ramGb}:${size.diskGb}`;
  const [got, setGot] = useState<{ key: string; quote: ResourceQuote | null } | null>(null);
  useEffect(() => {
    if (!on) return;
    let live = true;
    const t = window.setTimeout(() => {
      api.quoteResources(id, { cores: size.cores, ramGb: size.ramGb, diskGb: size.diskGb }).then(
        (quote) => live && setGot({ key, quote }),
        () => live && setGot({ key, quote: null }),
      );
    }, 250);
    return () => {
      live = false;
      window.clearTimeout(t);
    };
  }, [api, id, key, on, size.cores, size.ramGb, size.diskGb]);
  return got && got.key === key ? got.quote : null;
};

const Editor = ({
  d,
  onCancel,
  onApply,
}: {
  d: MachineDetail;
  onCancel: () => void;
  onApply: (to: Size) => void;
}) => {
  const m = d.machine;
  const plan = d.plan;
  const cur: Size = { cores: m.hw.cores, ramGb: m.hw.ramGb, diskGb: m.hw.diskGb };
  const [draft, setDraft] = useState<Size>(cur);
  const changed = !sameSize(draft, cur);
  const quote = useQuote(m.id, draft, changed);
  if (!plan) return null;

  const usedRam = Math.round(plan.usedRamGb);
  const usedDisk = Math.round(plan.usedDiskGb);
  const cores = stopsOf(withValue(plan.cores, cur.cores), 0);
  const ram = stopsOf(withValue(plan.ramGb, cur.ramGb), ramFloor(plan.usedRamGb));
  const disk = stopsOf(withValue(plan.diskGb, cur.diskGb), cur.diskGb);
  const coresText = (v: number) => `${v} ${plural(v, 'ядро', 'ядра', 'ядер')}`;

  const from = rubOf(quote?.current ?? m.rent?.price) ?? m.monthPriceRub;
  const to = rubOf(quote?.price);
  const delta = deltaOf(quote?.delta);

  return (
    <div className="md-edit">
      <Slider
        label="Ядра"
        vals={cores.vals}
        firstOk={cores.firstOk}
        cur={cur.cores}
        value={draft.cores}
        unit="cores"
        full={coresText}
        hint=""
        onPick={(v) => setDraft((s) => ({ ...s, cores: v }))}
      />
      <Slider
        label="Память"
        vals={ram.vals}
        firstOk={ram.firstOk}
        cur={cur.ramGb}
        value={draft.ramGb}
        unit="gb"
        full={(v) => `${v} ГБ`}
        hint={`занято ${usedRam} ГБ — меньше не выбрать`}
        onPick={(v) => setDraft((s) => ({ ...s, ramGb: v }))}
      />
      <Slider
        label="Диск"
        vals={disk.vals}
        firstOk={disk.firstOk}
        cur={cur.diskGb}
        value={draft.diskGb}
        unit="gb"
        full={gbLabel}
        hint={`занято ${usedDisk} ГБ — диск можно только увеличить`}
        onPick={(v) => setDraft((s) => ({ ...s, diskGb: v }))}
      />
      <div className="md-sum" data-show={changed}>
        <span>В месяц</span>
        <s>{from != null ? rubText(from) : '—'}</s>
        <b>{to != null ? rubText(to) : '—'}</b>
        <em data-dir={delta?.dir ?? 'down'}>{delta?.text ?? ''}</em>
      </div>
      <p className="md-note">
        Машина перезагрузится, это около 2 минут. Если идут чаты, они встанут на паузу и продолжатся сами.
      </p>
      <div className="md-edit__foot">
        <Btn variant="ghost" size="sm" onClick={onCancel}>
          Отмена
        </Btn>
        <Btn variant="primary" size="sm" aria-disabled={!changed} onClick={() => changed && onApply(draft)}>
          Применить и перезагрузить
        </Btn>
      </div>
    </div>
  );
};

export const ResourcesSection = ({
  d,
  editing,
  locked,
  onEdit,
  onCancel,
  onApply,
}: {
  d: MachineDetail;
  editing: boolean;
  /** An action is running or the machine is switched off: nothing to change now. */
  locked: boolean;
  onEdit: () => void;
  onCancel: () => void;
  onApply: (to: Size) => void;
}) => {
  const m = d.machine;
  const { cores, ramGb, diskGb } = m.hw;
  const canOpen = m.cloud && d.plan != null && !locked;
  const rent = m.rent?.price ? rubOf(m.rent.price) : m.monthPriceRub;
  const note = m.cloud
    ? `Сервер в ${m.rent?.provider ?? 'облаке'}${rent != null ? ` · ${rubText(rent)} в месяц` : ''}`
    : 'Ноутбук — ресурсы меняются только на самой машине';
  const showEdit = editing && d.plan != null;
  return (
    <div className="md-sec">
      <div className="md-sec__head">
        <span className="md-sec__t">Ресурсы</span>
        {canOpen && !showEdit && (
          <button
            type="button"
            className="ad-add"
            aria-disabled={!m.canAdmin}
            title={!m.canAdmin && m.needs ? NEEDS[m.needs] : undefined}
            onClick={() => m.canAdmin && onEdit()}
          >
            Изменить
          </button>
        )}
      </div>
      {!showEdit && (
        <>
          <div className="md-res">
            <div>
              <b>{cores > 0 ? cores : '—'}</b>
              <small>{plural(cores, 'ядро', 'ядра', 'ядер')}</small>
            </div>
            <div>
              <b>{ramGb > 0 ? `${ramGb} ГБ` : '—'}</b>
              <small>память</small>
            </div>
            <div>
              <b>{diskGb > 0 ? gbLabel(diskGb) : '—'}</b>
              <small>диск</small>
            </div>
          </div>
          <p className="md-note">{note}</p>
          {m.cloud && d.providerError && <p className="md-note">{d.providerError}</p>}
        </>
      )}
      {showEdit && <Editor d={d} onCancel={onCancel} onApply={onApply} />}
    </div>
  );
};
