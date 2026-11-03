// The limit slider of the board: stops on a rail, a locked part below what is already spent, drag and arrow keys snap
// to the nearest stop that can still be picked.
import { type CSSProperties, type KeyboardEvent, type PointerEvent, useRef } from 'react';

import { kRub } from '../../../org/screens/teams/format';
import { type Slider, snapStop, sliderUnit, stepStop } from './model';

export const LimitSlider = ({
  slider,
  value,
  base,
  spent,
  disabled,
  hint,
  onPick,
}: {
  slider: Slider;
  /** The value picked now (null: nothing). */
  value: number | null;
  /** The value that is set at the hub. */
  base: number | null;
  spent: number;
  disabled?: boolean;
  hint?: string;
  onPick: (v: number) => void;
}) => {
  const dragging = useRef(false);
  const at = (e: PointerEvent<HTMLDivElement>) => {
    const rc = e.currentTarget.getBoundingClientRect();
    return snapStop(slider, rc.width > 0 ? (e.clientX - rc.left) / rc.width : 0);
  };
  const changed = value !== null && value !== base;
  const unit = sliderUnit();
  return (
    <div className="md-sl" aria-disabled={disabled || undefined}>
      <div className="md-sl__head">
        <span className="md-sl__k">{unit}</span>
        <span className="md-sl__v">
          <s data-show={changed ? 'true' : 'false'}>{base === null ? 'не задан' : kRub(base)}</s>
          <b>{value === null ? 'не задан' : kRub(value)}</b>
        </span>
      </div>
      <div
        className="md-sl__track"
        style={{ '--fill': slider.fill, '--lock': slider.lock } as CSSProperties}
        role="slider"
        tabIndex={disabled ? -1 : 0}
        aria-label={unit}
        aria-valuetext={value === null ? 'не задан' : kRub(value)}
        aria-disabled={disabled || undefined}
        onPointerDown={(e) => {
          if (disabled) return;
          dragging.current = true;
          e.currentTarget.setPointerCapture?.(e.pointerId);
          onPick(at(e));
        }}
        onPointerMove={(e) => {
          if (!disabled && dragging.current) onPick(at(e));
        }}
        onPointerUp={() => {
          dragging.current = false;
        }}
        onPointerCancel={() => {
          dragging.current = false;
        }}
        onKeyDown={(e: KeyboardEvent) => {
          if (disabled) return;
          const dir = ({ ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 } as Record<string, 1 | -1>)[
            e.key
          ];
          if (!dir) return;
          e.preventDefault();
          onPick(stepStop(slider, dir));
        }}
      >
        <span className="md-sl__rail" />
        <span className="md-sl__lock" />
        <span className="md-sl__fill" />
        {slider.opts.map((o) => (
          <button
            key={o.value}
            type="button"
            tabIndex={-1}
            className="md-sl__stop"
            style={{ '--x': o.x } as CSSProperties}
            aria-pressed={o.on}
            data-done={o.done ? '1' : '0'}
            data-cur={o.cur ? '1' : '0'}
            aria-disabled={o.off}
            title={o.why || undefined}
            onClick={() => {
              if (!o.off && !disabled) onPick(o.value);
            }}
          >
            <i />
            <span>{o.label}</span>
          </button>
        ))}
      </div>
      <small className="md-sl__hint">{hint ?? `уже потрачено ${kRub(spent)} — меньше не выбрать`}</small>
    </div>
  );
};
