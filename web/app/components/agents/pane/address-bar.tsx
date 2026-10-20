import { useState, type FormEvent, type ReactNode } from 'react';
import { ArrowLeft, ArrowRight, RefreshCw } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import { t } from '@/lib/i18n';

interface IAddressFieldProps {
  /** What the browser shows; the field returns to it when the person leaves without opening anything. */
  value: string;
  /** Shown instead of the address until the field is focused: a design board by its name. */
  label?: string;
  onSubmit: (address: string) => void;
  disabled?: boolean;
}

/** «Search or enter a URL» of Codex: a port or a site; Enter opens it, Escape gives the shown address back. */
export const AddressField = ({ value, label, onSubmit, disabled }: IAddressFieldProps) => {
  const [draft, setDraft] = useState<string | null>(null);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const address = (draft ?? value).trim();
    if (!address) return;
    setDraft(null);
    (document.activeElement as HTMLElement | null)?.blur();
    onSubmit(address);
  };
  return (
    <form className="tw-rpane-address" onSubmit={submit} role="search">
      <input
        className="tw-rpane-address-input"
        value={draft ?? label ?? value}
        title={label ? value : undefined}
        disabled={disabled}
        placeholder={t('pane.addressBar.address')}
        aria-label={t('pane.addressBar.address')}
        spellCheck={false}
        autoComplete="off"
        onFocus={(event) => {
          setDraft(draft ?? value);
          event.currentTarget.select();
        }}
        onBlur={() => {
          if (draft === value) setDraft(null);
        }}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== 'Escape') return;
          event.preventDefault();
          setDraft(null);
          event.currentTarget.blur();
        }}
      />
    </form>
  );
};

interface IBrowserBarProps {
  onBack?: () => void;
  onForward?: () => void;
  onReload?: () => void;
  children: ReactNode;
}

/** Back, forward and reload before the address; without a page to drive they stay disabled, as on Codex's new tab. */
export const BrowserBar = ({ onBack, onForward, onReload, children }: IBrowserBarProps) => (
  <div className="tw-rpane-browser-bar" role="toolbar" aria-label={t('pane.addressBar.navigation')}>
    <span className="tw-rpane-nav">
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        aria-label={t('pane.addressBar.back')}
        title={t('pane.addressBar.back')}
        disabled={!onBack}
        onClick={onBack}
      >
        <ArrowLeft />
      </TwButton>
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        aria-label={t('pane.addressBar.forward')}
        title={t('pane.addressBar.forward')}
        disabled={!onForward}
        onClick={onForward}
      >
        <ArrowRight />
      </TwButton>
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        aria-label={t('pane.addressBar.reload')}
        title={t('pane.addressBar.reload')}
        disabled={!onReload}
        onClick={onReload}
      >
        <RefreshCw />
      </TwButton>
    </span>
    {children}
  </div>
);
