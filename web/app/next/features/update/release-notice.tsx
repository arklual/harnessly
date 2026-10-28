// «Доступна версия X.Y.Z · Что нового · Как обновить»: the pill of «Вышла новая версия» (.hx-net), for a release of
// harnessly itself. It never updates anything: the links lead to the release notes and to the guide. Put away, it hides
// that version only.
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { UPDATE_GUIDE_URL, type IRelease } from './release';
import '@/next/styles/connection.css';

const EXIT_MS = 260;

interface IReleaseNoticeProps {
  /** The release to tell about; null: nothing to say (or put away). */
  release: IRelease | null;
  onDismiss: () => void;
}

const ReleaseNotice = ({ release, onDismiss }: IReleaseNoticeProps) => {
  const container = usePortalContainer();
  const { value, state } = usePresence(release, EXIT_MS);
  const host = container?.current;
  if (!value || !host) return null;

  return createPortal(
    <div
      className="hx-net"
      role="status"
      aria-live="polite"
      data-kind="release"
      data-state={state === 'closing' ? 'closing' : 'release'}
    >
      <Icon name="sparkles" size="sm" className="hx-net__icon" />
      <span className="hx-net__text">{t('next.release.title', { version: value.version })}</span>
      {value.url && (
        <a className="hx-net__link" href={value.url} target="_blank" rel="noopener noreferrer">
          {t('next.release.notes')}
        </a>
      )}
      <a className="hx-net__link" href={UPDATE_GUIDE_URL} target="_blank" rel="noopener noreferrer">
        {t('next.release.how')}
      </a>
      <button type="button" className="hx-net__x" aria-label={t('next.release.hide')} onClick={onDismiss}>
        <Icon name="x" size="sm" />
      </button>
    </div>,
    host,
  );
};

export default ReleaseNotice;
