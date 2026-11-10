// «Агент в браузере» (Settings → General): the sites the person blocked for the agent in the pane's browser. Anywhere
// else the agent acts without asking, and the person sees its cursor (HRN-681). Kept in this browser
// (stores/browser-agent.ts).
import { useId, useState } from 'react';
import { FormError, Group, Row, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

interface IBrowserAgentGroupProps {
  blocked: readonly string[];
  onBlock: (host: string) => void;
  onUnblock: (host: string) => void;
}

/** A host as typed, with or without a scheme and a path, the way the page's address names it (a single label and an
 * IP address included); null when it is not one. */
export const hostOf = (text: string): string | null => {
  const raw = text.trim().toLowerCase();
  if (!raw || /\s/.test(raw)) return null;
  try {
    const host = new URL(/^[a-z][a-z0-9+.-]*:\/\//.test(raw) ? raw : `https://${raw}`).hostname.replace(
      /\.$/,
      '',
    );
    return host || null;
  } catch {
    return null;
  }
};

export const BrowserAgentGroup = ({ blocked, onBlock, onUnblock }: IBrowserAgentGroupProps) => {
  const id = useId();
  const [text, setText] = useState('');
  const [wrong, setWrong] = useState(false);

  const submit = () => {
    const host = hostOf(text);
    if (!host) {
      setWrong(true);
      return;
    }
    onBlock(host);
    setText('');
  };

  return (
    <Group title={t('next.settings.browserAgent')} foot={t('next.settings.browserAgent.foot')}>
      {blocked.length === 0 && (
        <Row
          type="link"
          label={t('next.settings.browserAgent.none')}
          hint={t('next.settings.browserAgent.noneHint')}
        >
          {null}
        </Row>
      )}
      {blocked.map((host) => (
        <Row key={host} type="link" label={host} hint={t('next.settings.browserAgent.denied')}>
          <SetButton tone="ghost" onClick={() => onUnblock(host)}>
            {t('next.settings.browserAgent.unblock')}
          </SetButton>
        </Row>
      ))}
      <form
        className="hx-key-form hx-voice-words"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <label className="hx-voice-words__field" htmlFor={id}>
          <span className="hx-nb__label">{t('next.settings.browserAgent.add.label')}</span>
          <input
            id={id}
            className="hx-set__input"
            value={text}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            inputMode="url"
            placeholder="bank.example.com"
            aria-invalid={wrong || undefined}
            onChange={(e) => {
              setText(e.target.value);
              setWrong(false);
            }}
          />
        </label>
        {wrong && <FormError>{t('next.settings.browserAgent.wrong')}</FormError>}
        <div className="hx-key-form__row">
          <SetButton tone="primary" disabled={text.trim() === ''} onClick={submit}>
            {t('next.settings.browserAgent.block')}
          </SetButton>
        </div>
      </form>
    </Group>
  );
};

export default BrowserAgentGroup;
