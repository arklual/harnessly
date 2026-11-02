import { ArrowUpRight } from 'lucide-react';

import { Link } from '../../../lib/router';
import type { Signal } from '../../api/overview';

/** «Требует внимания»: each signal says what is wrong and carries the action that opens the right screen. */
export const Attention = ({ signals, base }: { signals: Signal[]; base: string }) => (
  <section className="ov-card" aria-labelledby="ov-attn">
    <h2 id="ov-attn" className="ov-card__title">
      Требует внимания
    </h2>
    {signals.length === 0 ? (
      <p className="ov-empty">Всё в порядке: ничего не требует вашего внимания.</p>
    ) : (
      <ul className="ov-signals">
        {signals.map((s) => (
          <li key={s.id} className="ov-signal" data-sev={s.severity}>
            <span className="ov-signal__dot" aria-hidden="true" />
            <div className="ov-signal__text">
              <b>{s.title}</b>
              <span>{s.detail}</span>
            </div>
            <Link to={`${base}/${s.action.section}`} className="ov-signal__go">
              {s.action.label}
              <ArrowUpRight size={13} aria-hidden="true" />
            </Link>
          </li>
        ))}
      </ul>
    )}
  </section>
);
