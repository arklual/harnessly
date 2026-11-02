// «Период · против · Сравнить с»: one popover with the two fields, the ready choices and a strip of weeks. A click on a
// week takes it; a click on a first and a last week takes the weeks between. Closes with an animation.
import { useRef, useState } from 'react';
import { ChevronDown } from 'lucide-react';

import { Popup } from '@kit';

import type { AnalyticsFrame, Week } from '../../api/analytics';
import { monthShort, parseDay, rangeLabel, weekShort } from '../../dates';
import { useClosing } from '../../ui/closing';
import { defaultCompare, presetsOf, sameWeek, spanOf } from './period';

type Target = 'period' | 'compare';

export interface PeriodValue {
  period: Week;
  compare: Week;
}

const inside = (w: Week, span: Week): boolean => w.from >= span.from && w.to <= span.to;

export const PeriodPicker = ({
  frame,
  value,
  onChange,
}: {
  frame: AnalyticsFrame;
  value: PeriodValue;
  onChange: (v: PeriodValue) => void;
}) => {
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<Target>('period');
  const [start, setStart] = useState<number | null>(null);
  const [manual, setManual] = useState(false);
  const anchor = useRef<HTMLDivElement>(null);
  const { mounted, closing } = useClosing(open, 140);
  const presets = presetsOf(frame);
  const { weeks } = frame;

  const close = () => {
    setOpen(false);
    setStart(null);
  };
  const pick = (i: number) => {
    const span = spanOf(weeks, start ?? i, i);
    setStart(start === null ? i : null);
    if (target === 'period') {
      onChange({ period: span, compare: manual ? value.compare : defaultCompare(span) });
    } else {
      setManual(true);
      onChange({ period: value.period, compare: span });
    }
  };
  const open_ = (t: Target) => {
    setTarget(t);
    setStart(null);
    setOpen(true);
  };

  return (
    <div className="an-per" ref={anchor}>
      <div className="an-per__field">
        <span className="an-per__label">Период</span>
        <button
          type="button"
          className="an-per__btn"
          data-kind="period"
          aria-expanded={open && target === 'period'}
          onClick={() => open_('period')}
        >
          <i className="an-per__sw" />
          {rangeLabel(value.period.from, value.period.to)}
          <ChevronDown size={14} aria-hidden="true" />
        </button>
      </div>
      <span className="an-per__vs">против</span>
      <div className="an-per__field">
        <span className="an-per__label">Сравнить с</span>
        <button
          type="button"
          className="an-per__btn"
          data-kind="compare"
          aria-expanded={open && target === 'compare'}
          onClick={() => open_('compare')}
        >
          <i className="an-per__sw" />
          {rangeLabel(value.compare.from, value.compare.to)}
          <ChevronDown size={14} aria-hidden="true" />
        </button>
      </div>
      <Popup
        anchor={anchor}
        open={mounted}
        onClose={close}
        className="an-pop"
        role="dialog"
        placement={{ side: 'bottom', align: 'end' }}
      >
        <div className="an-pop__in" data-closing={closing || undefined}>
          <div className="an-pop__fields">
            {(['period', 'compare'] as const).map((t) => (
              <button
                key={t}
                type="button"
                className="an-pop__field"
                data-kind={t}
                aria-pressed={target === t}
                onClick={() => {
                  setTarget(t);
                  setStart(null);
                }}
              >
                <i className="an-per__sw" />
                <span>
                  <small>{t === 'period' ? 'Период' : 'Сравнить с'}</small>
                  <b>{rangeLabel(value[t].from, value[t].to)}</b>
                </span>
              </button>
            ))}
          </div>
          {presets.length > 0 && (
            <div className="an-pop__presets">
              {presets.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className="an-chip"
                  aria-pressed={sameWeek(p.period, value.period) && sameWeek(p.compare, value.compare)}
                  onClick={() => {
                    setManual(true);
                    setStart(null);
                    onChange({ period: p.period, compare: p.compare });
                  }}
                >
                  {p.label}
                </button>
              ))}
            </div>
          )}
          <div className="an-weeks" role="group" aria-label="Недели">
            {weeks.map((w, i) => {
              const prev = weeks[i - 1];
              const month =
                !prev || parseDay(prev.to).getUTCMonth() !== parseDay(w.to).getUTCMonth()
                  ? monthShort(w.to)
                  : '';
              return (
                <button
                  key={w.from}
                  type="button"
                  className="an-week"
                  data-period={inside(w, value.period) || undefined}
                  data-compare={inside(w, value.compare) || undefined}
                  data-start={start === i || undefined}
                  aria-pressed={inside(w, target === 'period' ? value.period : value.compare)}
                  aria-label={rangeLabel(w.from, w.to)}
                  onClick={() => pick(i)}
                >
                  <span className="an-week__d">{weekShort(w.from, w.to)}</span>
                  <span className="an-week__m">{month}</span>
                </button>
              );
            })}
          </div>
          <p className="an-pop__hint">
            Нажмите на неделю. Чтобы взять несколько недель подряд — на первую и последнюю.
          </p>
        </div>
      </Popup>
    </div>
  );
};
