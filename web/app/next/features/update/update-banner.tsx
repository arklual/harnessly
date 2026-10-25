// «Вышла новая версия · Обновить»: the same pill as «Нет связи» (.hx-net), at the top of the window, until the person
// reloads or puts it away. It never reloads by itself: a draft or an open dialog would be lost.
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/connection.css';

const EXIT_MS = 260;

interface IUpdateBannerProps {
  available: boolean;
  onReload: () => void;
  onDismiss: () => void;
}

const UpdateBanner = ({ available, onReload, onDismiss }: IUpdateBannerProps) => {
  const container = usePortalContainer();
  const { value, state } = usePresence(available || null, EXIT_MS);
  const host = container?.current;
  if (!value || !host) return null;

  return createPortal(
    <div
      className="hx-net"
      role="status"
      aria-live="polite"
      data-kind="update"
      data-state={state === 'closing' ? 'closing' : 'update'}
    >
      <Icon name="sparkles" size="sm" className="hx-net__icon" />
      <span className="hx-net__text">{t('next.update.title')}</span>
      <button type="button" className="hx-net__now" onClick={onReload}>
        {t('next.update.reload')}
      </button>
      <button type="button" className="hx-net__x" aria-label={t('next.update.later')} onClick={onDismiss}>
        <Icon name="x" size="sm" />
      </button>
    </div>,
    host,
  );
};

export default UpdateBanner;
