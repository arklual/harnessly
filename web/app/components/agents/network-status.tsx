import { Loader2, WifiLow } from 'lucide-react';
import { cn } from '@/lib/utils';
import { selectNetworkQuality, useNetworkHealthStore } from '@/lib/network-health';
import { t } from '@/lib/i18n';

const COPY = {
  machine: {
    label: t('agents.networkStatus.machineLabel'),
    hint: t('agents.networkStatus.machineHint'),
  },
  slow: {
    label: t('agents.networkStatus.slowLabel'),
    hint: t('agents.networkStatus.slowHint'),
  },
  reconnecting: {
    label: t('agents.networkStatus.reconnectingLabel'),
    hint: t('agents.networkStatus.reconnectingHint'),
  },
} as const;

/** Says out loud why the page lags — a busy machine, a slow link or a dropped socket — instead of leaving stale data unexplained. */
const NetworkStatus = () => {
  const quality = useNetworkHealthStore(selectNetworkQuality);
  if (quality === 'ok') return null;
  return (
    <span className={cn('tw-net', `tw-net-${quality}`)} role="status" title={COPY[quality].hint}>
      {quality === 'reconnecting' ? <Loader2 className="tw-inline-spin" /> : <WifiLow />}
      <span className="tw-net-label">{COPY[quality].label}</span>
    </span>
  );
};

export default NetworkStatus;
