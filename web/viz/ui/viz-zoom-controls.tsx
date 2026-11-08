// SPDX-License-Identifier: Apache-2.0
import { Maximize2, Minus, Plus } from 'lucide-react';

import { classNames } from '../internal/cx';
import { VizIconButton } from '../internal/icon-button';
import type { VizViewport } from '../model/viewport';
import { useVizViewportTarget } from './hooks';

import '../viz.css';

export interface VizZoomLabels {
  readonly zoomIn: string;
  readonly zoomOut: string;
  readonly reset: string;
}

export const DEFAULT_VIZ_ZOOM_LABELS: VizZoomLabels = {
  zoomIn: 'Приблизить',
  zoomOut: 'Отдалить',
  reset: 'Весь период',
};

export interface VizZoomControlsProps {
  readonly viewport: VizViewport;
  /** Кнопка «весь период» — появляется, когда окно уже всей истории. */
  readonly reset?: boolean;
  readonly disabled?: boolean;
  readonly labels?: Partial<VizZoomLabels>;
  /** Нажали кнопку — например, закрыть закреплённую подсказку. */
  readonly onInteract?: () => void;
  readonly className?: string;
}

const ZOOM_IN = 0.6;
const ZOOM_OUT = 1 / ZOOM_IN;

/** Кнопки масштаба: на телефоне и для тех, кто не пользуется колесом и щипком. */
export function VizZoomControls({
  viewport,
  reset = true,
  disabled = false,
  labels,
  onInteract,
  className,
}: VizZoomControlsProps) {
  const l = { ...DEFAULT_VIZ_ZOOM_LABELS, ...labels };
  const target = useVizViewportTarget(viewport);
  const extent = viewport.extent;
  const full = target.end - target.start >= extent.end - extent.start - 1;
  const zoom = (factor: number) => {
    onInteract?.();
    viewport.zoomAround((target.start + target.end) / 2, factor);
  };
  return (
    <div className={classNames('viz-zoom', className)} role="group" aria-label={l.reset}>
      {reset && !full ? (
        <VizIconButton
          size="s"

          icon={<Maximize2 />}
          aria-label={l.reset}
          disabled={disabled}
          onClick={() => {
            onInteract?.();
            viewport.setRange(extent);
          }}
        />
      ) : null}
      <VizIconButton
        size="s"
        icon={<Minus />}
        aria-label={l.zoomOut}
        disabled={disabled || full}
        onClick={() => zoom(ZOOM_OUT)}
      />
      <VizIconButton
        size="s"
        icon={<Plus />}
        aria-label={l.zoomIn}
        disabled={disabled}
        onClick={() => zoom(ZOOM_IN)}
      />
    </div>
  );
}
