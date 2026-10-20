import { RotateCcw, X } from 'lucide-react';
import { OFFLINE_STATE_LABELS, type IOfflinePrompt } from '@/hooks/use-offline-queue';
import { t } from '@/lib/i18n';

interface IOfflineQueueListProps {
  items: IOfflinePrompt[];
  onRetry: (item: IOfflinePrompt) => void;
  onDrop: (item: IOfflinePrompt) => void;
}

/** What waits for the machine, above the composer: each message with where it is on its way. */
const OfflineQueueList = ({ items, onRetry, onDrop }: IOfflineQueueListProps) => {
  const shown = items.filter((item) => item.state !== 'delivered');
  if (!shown.length) return null;
  return (
    <ul className="tw-offline-queue" aria-label={t('agents.offlineQueueList.label')}>
      {shown.map((item) => (
        <li key={item.id} className="tw-offline-item" data-state={item.state}>
          <p className="tw-offline-text">{item.text}</p>
          <div className="tw-offline-meta">
            <span>
              {item.state === 'failed' && item.error ? item.error : OFFLINE_STATE_LABELS[item.state]}
            </span>
            {item.state === 'failed' && (
              <button type="button" onClick={() => onRetry(item)}>
                <RotateCcw aria-hidden />
                {t('agents.offlineQueueList.resend')}
              </button>
            )}
            {(item.state === 'queued' || item.state === 'failed') && (
              <button
                type="button"
                aria-label={t('agents.offlineQueueList.drop')}
                onClick={() => onDrop(item)}
              >
                <X aria-hidden />
              </button>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
};

export default OfflineQueueList;
