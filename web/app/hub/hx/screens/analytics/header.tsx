// The period and its comparison: two buttons in the screen's head and a popover with the presets and the strip of
// weeks. The state is the picker's (period.ts); a click changes it at once and the screen asks for the new numbers.
import { type CSSProperties, useEffect, useMemo, useRef } from 'react';

import type { ChangeRef, FrameWeek } from '../../../org/api/analytics';
import { Icon, usePop } from '../../ui';
import {
  type PickerState,
  hintOf,
  labelOf,
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
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const presets = useMemo(() => presetsOf(weeks, change), [weeks, change]);
  const cells = useMemo(() => weekCells(weeks, state), [weeks, state]);
  const strip = useRef<HTMLDivElement>(null);
  // On a narrow screen the strip scrolls: it opens on the newest weeks.
  useEffect(() => {
    if (pop.mounted && strip.current) strip.current.scrollLeft = strip.current.scrollWidth;
  }, [pop.mounted]);
  const now = labelOf(weeks, state.a);
  const prev = labelOf(weeks, state.b);

  const openTab = (k: 'a' | 'b') => {
    if (pop.open && state.edit === k) {
      pop.close();
      return;
    }
    if (state.edit !== k) onChange(setEdit(state, k));
    pop.setOpen(true);
  };

  return (
    <div className="ad-tools">
      <div className="anx-per" ref={root}>
        <div className="anx-per__row">
          <label className="anx-per__f">
            <span className="anx-per__cap">Период</span>
            <button
              type="button"
              className="an-period anx-per__btn"
              data-set
              data-k="a"
              aria-haspopup="dialog"
              aria-expanded={pop.open && state.edit === 'a'}
              onClick={() => openTab('a')}
            >
              <span className="anx-per__dot" data-k="a" />
              <span className="an-period__label">
                <b>{now}</b>
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
              aria-haspopup="dialog"
              aria-expanded={pop.open && state.edit === 'b'}
              onClick={() => openTab('b')}
            >
              <span className="anx-per__dot" data-k="b" />
              <span className="an-period__label">
                <b>{prev}</b>
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
                  <b>{now}</b>
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
                  <b>{prev}</b>
                </span>
              </button>
            </div>
            <div className="anx-wp__presets">
              {presets.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className="anx-wp__preset"
                  aria-pressed={sameRange(p.a, state.a) && sameRange(p.b, state.b)}
                  onClick={() => onChange(pickPreset(state, p))}
                >
                  {p.label}
                </button>
              ))}
            </div>
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
