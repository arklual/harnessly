// The apps that are on in this chat, as a quiet row over the field: the app's name (it opens the app as the main thing)
// and a small «off». Nothing else: no counts, no badges. An app that goes leaves with a fade; turning one on is
// «+» → «Приложение».
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

export const CHIP_EXIT_MS = 200;

export interface IChatAppChip {
  id: string;
  name: string;
}

export interface IChatAppsRowProps {
  apps: readonly IChatAppChip[];
  /** The app whose change waits for the machine: its «off» is busy and ignores presses. */
  busy: string | null;
  onOpen: (id: string) => void;
  onOff: (id: string) => void;
}

const ChatAppsRow = ({ apps, busy, onOpen, onOff }: IChatAppsRowProps) => {
  const shown = useLeavingList(apps, (a) => a.id, CHIP_EXIT_MS);
  if (shown.length === 0) return null;
  return (
    <div className="hx-chatapps" role="group" aria-label={t('next.chatapps.label')}>
      {shown.map(({ item, leaving }) => (
        <span
          key={item.id}
          className="hx-chatapps__chip"
          data-leaving={leaving || undefined}
          data-busy={busy === item.id || undefined}
        >
          <button type="button" className="hx-chatapps__name" onClick={() => onOpen(item.id)}>
            <Icon name="layout-grid" size="sm" />
            <span>{item.name}</span>
          </button>
          <button
            type="button"
            className="hx-chatapps__off"
            aria-label={t('next.chatapps.off', { name: item.name })}
            disabled={busy !== null || leaving}
            onClick={() => onOff(item.id)}
          >
            <Icon name="x" size="sm" />
          </button>
        </span>
      ))}
    </div>
  );
};

export default ChatAppsRow;
