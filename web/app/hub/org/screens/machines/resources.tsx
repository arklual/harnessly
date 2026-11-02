// Changing a server's cores, memory and disk: three rows of steps (no native range), the price before and after, and
// «Применить и перезагрузить». The memory cannot go below what is used; the disk only grows.
import { useState } from 'react';

import { Button } from '@kit';

import type { OrgMachine, ResourcePlan } from '../../api/machines';
import { orgErrorText } from '../../api/errors';
import { gb, plural, priceOf, rub } from './format';

interface Draft {
  cores: number;
  ramGb: number;
  diskGb: number;
}

const Steps = ({
  label,
  steps,
  value,
  current,
  floor,
  fmtStop,
  fmtValue,
  hint,
  onPick,
}: {
  label: string;
  steps: number[];
  value: number;
  current: number;
  floor: number;
  fmtStop: (v: number) => string;
  fmtValue: (v: number) => string;
  hint: string | null;
  onPick: (v: number) => void;
}) => {
  const sel = steps.indexOf(value);
  return (
    <div className="hm-steps">
      <div className="hm-steps__head">
        <span>{label}</span>
        <span className="hm-steps__val">
          {value !== current && <s>{fmtValue(current)}</s>} <b>{fmtValue(value)}</b>
        </span>
      </div>
      <div className="hm-steps__row" role="radiogroup" aria-label={label}>
        {steps.map((v, i) => {
          const locked = v < floor;
          return (
            <button
              key={v}
              type="button"
              role="radio"
              aria-checked={i === sel}
              disabled={locked}
              className="hm-stop"
              data-before={i > 0 && i <= sel ? 'on' : undefined}
              data-after={i < steps.length - 1 && i < sel ? 'on' : undefined}
              data-cur={v === current || undefined}
              onClick={() => onPick(v)}
            >
              <i className="hm-stop__dot" />
              <span className="hm-stop__t">{fmtStop(v)}</span>
            </button>
          );
        })}
      </div>
      {hint && <p className="hm-steps__hint">{hint}</p>}
    </div>
  );
};

export const ResourceEditor = ({
  m,
  plan,
  pending,
  error,
  onCancel,
  onApply,
}: {
  m: OrgMachine;
  plan: ResourcePlan;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onApply: (to: Draft) => void;
}) => {
  const cur: Draft = { cores: m.hw.cores, ramGb: m.hw.ramGb, diskGb: m.hw.diskGb };
  const [d, setD] = useState<Draft>(cur);
  const ramFloor = plan.usedRamGb * 1.15;
  const ramShort = d.ramGb < ramFloor;
  const changed = d.cores !== cur.cores || d.ramGb !== cur.ramGb || d.diskGb !== cur.diskGb;
  const was = priceOf(plan.price, cur.cores, cur.ramGb, cur.diskGb);
  const now = priceOf(plan.price, d.cores, d.ramGb, d.diskGb);
  const delta = now - was;
  const cores = (v: number) => `${v} ${plural(v, 'ядро', 'ядра', 'ядер')}`;
  return (
    <div className="hm-edit">
      <Steps
        label="Ядра"
        steps={plan.cores}
        value={d.cores}
        current={cur.cores}
        floor={0}
        fmtStop={String}
        fmtValue={cores}
        hint={null}
        onPick={(v) => setD({ ...d, cores: v })}
      />
      <Steps
        label="Память"
        steps={plan.ramGb}
        value={d.ramGb}
        current={cur.ramGb}
        floor={ramFloor}
        fmtStop={String}
        fmtValue={gb}
        hint={`занято ${plan.usedRamGb} ГБ — меньше не выбрать`}
        onPick={(v) => setD({ ...d, ramGb: v })}
      />
      <Steps
        label="Диск"
        steps={plan.diskGb}
        value={d.diskGb}
        current={cur.diskGb}
        floor={cur.diskGb}
        fmtStop={gb}
        fmtValue={gb}
        hint={`занято ${plan.usedDiskGb} ГБ — диск можно только увеличить`}
        onPick={(v) => setD({ ...d, diskGb: v })}
      />
      <div className="hm-edit__price">
        <span>В месяц</span>
        <span className="hm-edit__sum">
          {changed && <s>{rub(was)}</s>} <b>{rub(now)}</b>
          {changed && delta !== 0 && (
            <i className="hm-edit__delta" data-up={delta > 0 || undefined}>
              {delta > 0 ? '+' : '−'}
              {rub(Math.abs(delta))}
            </i>
          )}
        </span>
      </div>
      <p className="hm-edit__note">
        Машина перезагрузится, это около 2 минут. Если идут чаты, они встанут на паузу и продолжатся сами.
      </p>
      {error != null && <p className="hm-dlg__err">{orgErrorText(error)}</p>}
      <div className="hm-edit__actions">
        <Button appearance="ghost" onClick={onCancel} disabled={pending}>
          Отмена
        </Button>
        <Button
          appearance="primary"
          loading={pending}
          disabled={!changed || ramShort}
          onClick={() => onApply(d)}
        >
          Применить и перезагрузить
        </Button>
      </div>
    </div>
  );
};
