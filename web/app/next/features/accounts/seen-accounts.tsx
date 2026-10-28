// The accounts an agent was signed in with on this machine. One machine is one subscription account: a second account
// signing in on the same machine can get the subscription blocked, so the page always says which account this machine
// belongs to, and says it loudly once there were two.
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import '@/next/styles/providers.css';

interface ISeenAccountsProps {
  seen: readonly string[] | undefined;
}

/** «На этой машине входили как …», and the warning when that is more than one account. */
const SeenAccounts = ({ seen }: ISeenAccountsProps) => {
  if (!seen || seen.length === 0) return null;
  const mixed = seen.length > 1;
  return (
    <div className="hx-seen" data-tone={mixed ? 'warn' : undefined} role={mixed ? 'alert' : undefined}>
      <Icon name={mixed ? 'triangle-alert' : 'user'} size="sm" className="hx-seen__ic" />
      <span className="hx-seen__body">
        <span>
          {t('next.seen.lead')} <strong>{seen.join(', ')}</strong>
        </span>
        <span className="hx-seen__note">{t(mixed ? 'next.seen.mixed' : 'next.seen.keep')}</span>
      </span>
    </div>
  );
};

export default SeenAccounts;
