// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from 'react';

import { ChartSpline, TriangleAlert } from 'lucide-react';

import { VizEmpty } from '../internal/empty';

import '../viz.css';

export interface VizStatusProps {
  readonly kind: 'empty' | 'error';
  readonly title: string;
  readonly description?: string;
  readonly icon?: ReactNode;
  /** Только для ошибки: «Повторить». */
  readonly onRetry?: () => void;
  readonly retryLabel?: string;
}

/**
 * Пусто и ошибка поверх графика: EmptyState кита, а под ним — скелетон графика, чтобы блок
 * не схлопывался и было видно, что здесь будет.
 */
export function VizStatus({
  kind,
  title,
  description,
  icon,
  onRetry,
  retryLabel = 'Повторить',
}: VizStatusProps) {
  const error = kind === 'error';
  return (
    <div className="viz-status" data-kind={kind}>
      <VizEmpty
        icon={icon ?? (error ? <TriangleAlert /> : <ChartSpline />)}
        tone={error ? 'rose' : 'neutral'}
        title={title}
        description={description}
        action={error && onRetry ? { label: retryLabel, onClick: onRetry } : undefined}
      />
    </div>
  );
}
