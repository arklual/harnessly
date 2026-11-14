// A machine's card opened in place, in the stack under the chat's column: what is wrong in a sentence and
// what weighs on the machine, a row each, with «Остановить» where a program may be stopped. The same detail the smart
// notice's open card shows (bindings/smart-notice.tsx, machineDetail), without leaving the card's spot.
import { t } from '@/next/i18n';
import type { TSmartDetail } from './smart-notice';

export interface IMachineMoreProps {
  detail: Extract<TSmartDetail, { k: 'machine' }>;
  /** The row whose stop was asked and not answered yet; null: none. */
  stopping: string | null;
  onStop: (heavyKey: string) => void;
}

const MachineMore = ({ detail, stopping, onStop }: IMachineMoreProps) => (
  <>
    <p className="hx-ns__what">{detail.text}</p>
    {detail.hint && <p className="hx-ns__hint">{detail.hint}</p>}
    {detail.heavy.length > 0 && (
      <ul className="hx-ns__heavy" aria-label={t('next.smart.machine.heaviest')}>
        {detail.heavy.map((heavy) => (
          <li key={heavy.key}>
            <span className="hx-ns__hname">{heavy.name}</span>
            <span className="hx-ns__htext">{heavy.text}</span>
            {heavy.stoppable && (
              <button
                type="button"
                className="hx-ns__btn"
                disabled={stopping !== null}
                onClick={() => onStop(heavy.key)}
              >
                {t('next.smart.machine.stop')}
              </button>
            )}
          </li>
        ))}
      </ul>
    )}
  </>
);

export default MachineMore;
