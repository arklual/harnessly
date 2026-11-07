// The period and its comparison: two buttons in the screen's head and a popover with the presets and the strip of
// weeks. The state is the picker's (period.ts); a click changes it at once and the screen asks for the new numbers.
import { type CSSProperties, useEffect, useMemo, useRef } from 'react';

import type { ChangeRef, FrameWeek } from '../../../org/api/analytics';
import { useLive } from '../../live';
import { Icon, usePop } from '../../ui';
import {
  type PickerState,
  compareNameOf,
  hintOf,
  periodNameOf,
  lastDays,
  pickFree,
  QUICK,
  quickOn,
  setFreeDay,
  pickPreset,
  pickWeek,
  presetsOf,
  sameRange,
  setEdit,
  weekCells,
} from './period';

export interface PeriodControlsProps {
  weeks: readonly FrameWeek[];
  change: ChangeRef | null;
  state: PickerState;
  onChange: (next: PickerState) => void;
}

export const PeriodControls = ({ weeks, change, state, onChange }: PeriodControlsProps) => {
  const live = useLive();
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const presets = useMemo(() => presetsOf(weeks, change), [weeks, change]);
  const cells = useMemo(() => weekCells(weeks, state), [weeks, state]);
  const strip = useRef<HTMLDivElement>(null);
  // On a narrow screen the strip scrolls: it opens on the newest weeks.
  useEffect(() => {
    if (pop.mounted && strip.current) strip.current.scrollLeft = strip.current.scrollWidth;
  }, [pop.mounted]);
  const now = periodNameOf(weeks, state);
  const prev = compareNameOf(weeks, state);

  const openTab = (k: 'a' | 'b') => {
    if (pop.open && state.edit === k) {
      pop.close();
      return;
    }
    if (state.edit !== k) onChange(setEdit(state, k));
    pop.setOpen(true);
  };

  return (
    <div className="hb-tools">
      {live && (
        <div className="hx-set__seg an-quick" role="group" aria-label="Быстрый период">
          {QUICK.map((q) => (
            <button
              key={q.id}
              type="button"
              className="hx-set__seg-item"
              aria-pressed={quickOn(state) === q.id}
              onClick={() => onChange(pickFree(state, lastDays(q.days), true))}
            >
              {q.label}
            </button>
          ))}
        </div>
      )}
      <div className="anx-per" ref={root}>
        <div className="anx-per__row">
          <label className="anx-per__f">
            <span className="anx-per__cap">Период</span>
            <button
              type="button"
              className="an-period anx-per__btn"
              data-set
              data-k="a"
              title="Период, который смотрим: недели целиком, с понедельника по воскресенье"
              aria-haspopup="dialog"
              aria-expanded={pop.open && state.edit === 'a'}
              onClick={() => openTab('a')}
            >
              <span className="anx-per__dot" data-k="a" />
              <span className="an-period__label">
                <b>{now.title}</b>
                <span className="anx-per__d">{now.dates}</span>
              </span>
              <Icon i="chevron-down" size="sm" />
            </button>
          </label>
          <span className="anx-per__vs">против</span>
          <label className="anx-per__f">
            <span className="anx-per__cap">Сравнить с</span>
            <button
              type="button"
              className="an-period anx-per__btn"
              data-set
              data-k="b"
              title="С чем сравниваем: по умолчанию столько же недель сразу перед периодом"
              aria-haspopup="dialog"
              aria-expanded={pop.open && state.edit === 'b'}
              onClick={() => openTab('b')}
            >
              <span className="anx-per__dot" data-k="b" />
              <span className="an-period__label">
                <b>{prev.title}</b>
                <span className="anx-per__d">{prev.dates}</span>
              </span>
              <Icon i="chevron-down" size="sm" />
            </button>
          </label>
        </div>
        {pop.mounted && (
          <div
            className="anx-wp"
            data-closing={pop.closing ? '1' : undefined}
            role="dialog"
            aria-label="Период и сравнение"
          >
            <div className="anx-wp__tabs" role="tablist">
              <button
                type="button"
                className="anx-wp__tab"
                data-k="a"
                role="tab"
                aria-selected={state.edit === 'a'}
                onClick={() => onChange(setEdit(state, 'a'))}
              >
                <span className="anx-per__dot" data-k="a" />
                <span>
                  <small>Период</small>
                  <b>{`${now.title} · ${now.dates}`}</b>
                </span>
              </button>
              <button
                type="button"
                className="anx-wp__tab"
                data-k="b"
                role="tab"
                aria-selected={state.edit === 'b'}
                onClick={() => onChange(setEdit(state, 'b'))}
              >
                <span className="anx-per__dot" data-k="b" />
                <span>
                  <small>Сравнить с</small>
                  <b>{`${prev.title} · ${prev.dates}`}</b>
                </span>
              </button>
            </div>
            <div className="anx-wp__presets">
              {presets.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className="anx-wp__preset"
                  aria-pressed={!state.free && sameRange(p.a, state.a) && sameRange(p.b, state.b)}
                  onClick={() => onChange(pickPreset(state, p))}
                >
                  {p.label}
                </button>
              ))}
            </div>
            {live && (
              <div className="an-dates" role="group" aria-label="Даты">
                {(['from', 'to'] as const).map((end) => {
                  const w = state.free?.[state.edit] ?? null;
                  return (
                    <label key={end} className="an-dates__f">
                      <span>{end === 'from' ? 'С' : 'По'}</span>
                      <input
                        type="date"
                        className="hx-set__input"
                        value={w?.[end] ?? ''}
                        onChange={(e) =>
                          e.target.value && onChange(setFreeDay(state, state.edit, end, e.target.value))
                        }
                      />
                    </label>
                  );
                })}
              </div>
            )}
            <div className="anx-wp__weeks" role="group" aria-label="Недели" ref={strip}>
              {cells.map((w) => (
                <button
                  key={w.index}
                  type="button"
                  className="anx-wk"
                  data-a={w.inA ? '1' : undefined}
                  data-b={w.inB ? '1' : undefined}
                  data-now={w.now ? '1' : undefined}
                  data-anchor={w.anchor ? '1' : undefined}
                  title={w.title}
                  onClick={() => onChange(pickWeek(state, w.index))}
                >
                  <span className="anx-wk__bar">
                    <i style={{ '--h': w.h } as CSSProperties} />
                  </span>
                  <span className="anx-wk__d">{w.day}</span>
                  <span className="anx-wk__m">{w.month}</span>
                </button>
              ))}
            </div>
            <p className="anx-wp__hint">{hintOf(state)}</p>
          </div>
        )}
      </div>
    </div>
  );
};
