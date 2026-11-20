// «All voice settings»: the link at the foot of the quick settings, to the section of Settings where the rest lives.
import { t } from '@/next/i18n';
import { goTo, ROUTE } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';

interface IAllSettingsProps {
  section: 'voice' | 'accounts';
  /** The panel closes before the page changes. */
  onNavigate: () => void;
}

const AllSettings = ({ section, onNavigate }: IAllSettingsProps) => (
  <button
    type="button"
    className="hx-vq__all"
    onClick={() => {
      onNavigate();
      goTo({ kind: ROUTE.settings, section });
    }}
  >
    <Icon name="settings" size="sm" />
    <span>{t('next.voice.quick.all')}</span>
    <Icon name="chevron-right" size="sm" />
  </button>
);

export default AllSettings;
