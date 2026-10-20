import { useState } from 'react';
import { KeyRound } from 'lucide-react';
import { toast } from 'sonner';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwButton,
  TwMenuButton,
} from '@/components/agents/ui';
import useVaultEntries from '@/hooks/use-vault-entries';
import { mintFillToken } from '@/lib/hl-vault-client';
import { loginInPreview, REFUSAL_TEXT, type TVaultFillOutcome } from '@/lib/hl-vault-fill';
import { sameVaultOrigin } from '@/lib/vault/vault-origin';
import type { IVaultEntry } from '@/types/vault';
import { t } from '@/lib/i18n';

interface IVaultLoginButtonProps {
  /** The tab whose preview frame the login goes into. */
  previewKey: string;
  /** The site the frame shows. */
  origin: string;
}

const tell = (outcome: TVaultFillOutcome | null, entry: IVaultEntry) => {
  if (!outcome) return toast.error(t('vault.vaultLoginButton.previewClosed'));
  switch (outcome.kind) {
    case 'logged-in':
      return toast.success(t('vault.vaultLoginButton.loggedIn', { login: entry.login }));
    case 'still-login':
      return toast.warning(t('vault.vaultLoginButton.stillLogin'));
    case 'unverified':
      return toast.info(t('vault.vaultLoginButton.unverified'));
    case 'filled':
      return toast.info(t('vault.vaultLoginButton.filled'));
    case 'refused':
      return toast.error(t('vault.vaultLoginButton.refused', { reason: REFUSAL_TEXT[outcome.reason] }));
  }
};

/**
 * "Log in as …" in the browser of the right panel, for a site the safe has a password for. The press is the
 * person's consent: a one-time token is minted after the frame proved it shows that very site, and only the bridge of
 * that slot and document can trade it for the value. This page never holds the password.
 */
const VaultLoginButton = ({ previewKey, origin }: IVaultLoginButtonProps) => {
  const { view } = useVaultEntries();
  const [busy, setBusy] = useState(false);
  const entries =
    view.kind === 'ready' ? view.entries.filter((entry) => sameVaultOrigin(entry.origin, origin)) : [];
  if (!entries.length) return null;

  const login = async (entry: IVaultEntry) => {
    if (busy) return;
    setBusy(true);
    try {
      tell(
        await loginInPreview(previewKey, origin, (fill) =>
          mintFillToken(entry.id, origin, { for: 'person' }, fill),
        ),
        entry,
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('vault.vaultLoginButton.noAnswer'));
    } finally {
      setBusy(false);
    }
  };

  if (entries.length === 1) {
    const [entry] = entries;
    return (
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        icon={<KeyRound />}
        pending={busy}
        className="tw-vault-login"
        aria-label={t('vault.vaultLoginButton.logInAs', { login: entry.login })}
        title={t('vault.vaultLoginButton.logInTitle', { origin })}
        onClick={() => void login(entry)}
      >
        <span className="tw-vault-login-text">
          {t('vault.vaultLoginButton.logInAs', { login: entry.login })}
        </span>
      </TwButton>
    );
  }
  return (
    <TwMenuButton
      items={entries.map((entry) => ({
        kind: TW_MENU_KIND.action,
        id: entry.id,
        label: entry.label ? `${entry.login} · ${entry.label}` : entry.login,
        onSelect: () => void login(entry),
      }))}
      menuLabel={t('vault.vaultLoginButton.menuLabel')}
      align={LAYER_ALIGN.end}
      className="tw-ui-button tw-vault-login"
      data-variant={BUTTON_VARIANT.ghost}
      data-size={BUTTON_SIZE.sm}
      disabled={busy}
      aria-label={t('vault.vaultLoginButton.logInAsMenu')}
      title={t('vault.vaultLoginButton.logInTitle', { origin })}
    >
      <KeyRound />
      <span className="tw-ui-button-label tw-vault-login-text">
        {t('vault.vaultLoginButton.logInAsMenu')}
      </span>
    </TwMenuButton>
  );
};

export default VaultLoginButton;
