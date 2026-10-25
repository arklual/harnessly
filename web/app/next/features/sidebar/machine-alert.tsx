import { useState } from 'react';
import '@/next/styles/health.css';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { usePresence } from '@/next/ui/presence';
import type { IMachineAlert } from '@/next/features/sidebar/types';

const EXIT_MS = 200;

export interface IMachineAlertProps {
  /** The banner of a critical machine; null while it is not critical. */
  alert: IMachineAlert | null;
  /** Opens the machine's card. */
  onMore: () => void;
}

/**
 * The one line at the top of the sidebar while the machine is about to fail its work: what runs out and how much is
 * left, «Подробнее» opens the machine's card. Hidden by the person until it worsens (another key) or recovers.
 */
const MachineAlert = ({ alert, onMore }: IMachineAlertProps) => {
  const [dismissed, setDismissed] = useState<string | null>(null);
  // A machine that recovered shows its next trouble again.
  if (alert === null && dismissed !== null) setDismissed(null);
  const presence = usePresence(alert && alert.key !== dismissed ? alert : null, EXIT_MS);
  const shown = presence.value;
  if (!shown) return null;

  return (
    <div className="hx-health" role="status" data-state={presence.state}>
      <div className="hx-health__in">
        <div className="hx-health__box">
          <Icon name={shown.icon} size="sm" className="hx-health__ic" />
          <div className="hx-health__main">
            <span className="hx-health__text">{shown.text}</span>
            <button type="button" className="hx-health__more" onClick={onMore}>
              {t('next.health.more')}
            </button>
          </div>
          <button
            type="button"
            className="hx-health__close"
            aria-label={t('next.health.dismiss')}
            onClick={() => setDismissed(shown.key)}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
      </div>
    </div>
  );
};

export default MachineAlert;
