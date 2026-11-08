// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from 'react';

export type VizEmptyTone = 'green' | 'amber' | 'purple' | 'rose' | 'teal' | 'neutral';

export interface VizEmptyAction {
  readonly label: string;
  readonly onClick: () => void;
}

export interface VizEmptyProps {
  readonly icon: ReactNode;
  readonly title: string;
  readonly description?: string;
  readonly tone?: VizEmptyTone;
  readonly action?: VizEmptyAction;
}

/** Empty and error placeholder laid over the chart skeleton. */
export function VizEmpty({ icon, title, description, tone = 'neutral', action }: VizEmptyProps) {
  return (
    <div className="viz-empty viz-empty--default" role="status" aria-live="polite">
      <div className={`viz-empty__icon viz-empty__icon--${tone}`}>
        <span className="viz-empty__icon-content">{icon}</span>
      </div>
      <h3 className="viz-empty__title">{title}</h3>
      {description ? <p className="viz-empty__description">{description}</p> : null}
      {action ? (
        <button
          type="button"
          className="viz-empty__action viz-empty__action--contrast"
          onClick={action.onClick}
        >
          {action.label}
        </button>
      ) : null}
    </div>
  );
}
