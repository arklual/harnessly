import { type KeyboardEvent, type ReactNode, useRef } from 'react';

import { cx } from './cx';

export interface TabStripItem {
  value: string;
  label: ReactNode;
  /** A small number or mark after the label. */
  count?: ReactNode;
  disabled?: boolean;
}

export interface TabsProps {
  tabs: TabStripItem[];
  selected: string;
  onSelect: (value: string) => void;
  /** Accessible name of the strip. */
  label: string;
  /** Lets each tab name the panel it controls: `${panelId(value)}`. */
  panelId?: (value: string) => string;
  className?: string;
}

const STEP: Record<string, (at: number, n: number) => number> = {
  ArrowRight: (at, n) => (at + 1) % n,
  ArrowLeft: (at, n) => (at - 1 + n) % n,
  Home: () => 0,
  End: (_at, n) => n - 1,
};

/**
 * A row of mutually exclusive views. Only the selected tab is in the tab order; arrows, Home and End select another
 * tab and move the focus with it (disabled tabs are skipped).
 */
export function Tabs({ tabs, selected, onSelect, label, panelId, className }: TabsProps) {
  const strip = useRef<HTMLDivElement>(null);
  const usable = tabs.filter((t) => !t.disabled);

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const step = STEP[e.key];
    if (!step || usable.length === 0) return;
    e.preventDefault();
    const at = Math.max(
      0,
      usable.findIndex((t) => t.value === selected),
    );
    const next = usable[step(at, usable.length)];
    if (!next) return;
    onSelect(next.value);
    strip.current?.querySelector<HTMLButtonElement>(`[data-value="${CSS.escape(next.value)}"]`)?.focus();
  };

  return (
    <div ref={strip} role="tablist" aria-label={label} className={cx('k-tabstrip', className)}>
      {tabs.map((t) => {
        const on = t.value === selected;
        return (
          <button
            key={t.value}
            type="button"
            role="tab"
            data-value={t.value}
            aria-selected={on}
            aria-controls={panelId?.(t.value)}
            disabled={t.disabled}
            tabIndex={on ? 0 : -1}
            className="k-tabstrip__tab"
            onClick={() => onSelect(t.value)}
            onKeyDown={onKeyDown}
          >
            {t.label}
            {t.count != null && <span className="k-tabstrip__count">{t.count}</span>}
          </button>
        );
      })}
    </div>
  );
}
