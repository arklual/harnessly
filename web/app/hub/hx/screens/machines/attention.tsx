// «Нужен шаг» of the machines screen, as the board draws it (`attCard`): one line per machine that needs a person, the
// button on the right opens the card of the machine, where the step is made.
import type { CSSProperties } from 'react';

import { useNow } from '../../../lib/hooks';
import type { MachineGroup } from '../../../org/api/machines';
import { machineSignals } from './calc';

const MAX = 8;

export const MachineAttention = ({
  groups,
  onOpen,
}: {
  groups: readonly MachineGroup[];
  onOpen: (id: string) => void;
}) => {
  const now = useNow(60_000);
  const all = machineSignals(groups, now);
  if (all.length === 0) return null;
  const shown = all.slice(0, MAX);
  return (
    <div className="t-card viz-card dash-tile mx-attention hd-card" role="region" aria-label="Нужен шаг">
      <header className="viz-card__header">
        <div className="viz-card__heading">
          <div className="viz-card__title-row">
            <h3 className="viz-card__title">
              <span className="at-title">
                Нужен шаг <span className="at-title__count">{all.length}</span>
              </span>
            </h3>
          </div>
        </div>
      </header>
      <div className="viz-card__body">
        <div className="at-grid" style={{ '--at-cols': 1 } as CSSProperties}>
          {shown.map((s, i) => (
            <div
              key={s.id}
              className="at-cell hd-att"
              data-tone={s.tone}
              style={{ '--i': i, '--at-span': 1, '--j': 0, '--jr': 0 } as CSSProperties}
            >
              <span className="at-cell__dot" aria-hidden="true" />
              <span className="at-cell__text">
                <b className="at-cell__title">{s.title}</b>
                <span className="at-cell__detail">{s.detail}</span>
              </span>
              <span className="at-cell__action hd-att__acts">
                <button
                  type="button"
                  className="hx-btn hd-btn"
                  data-size="sm"
                  data-variant={i === 0 && s.tone === 'risk' ? 'primary' : 'outline'}
                  onClick={() => onOpen(s.machineId)}
                >
                  {s.action}
                </button>
                <button type="button" className="md-link hd-link" onClick={() => onOpen(s.machineId)}>
                  Карточка машины
                </button>
              </span>
            </div>
          ))}
        </div>
        {all.length > MAX && <p className="mut sm">И ещё {all.length - MAX} — они в списке ниже.</p>}
      </div>
    </div>
  );
};
