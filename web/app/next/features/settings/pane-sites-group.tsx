// «Ссылки» (Settings → General): the sites whose links from the chat open in the pane's browser; any other link opens in
// a new tab of the browser. The list starts as the installation's; the person adds a site, takes one off or returns to
// the installation's list. It is this browser's, as the look is (stores/pane-sites.ts).
import { useId, useState } from 'react';
import { siteOf } from '@/next/features/chat/pane-sites';
import { FormError, Group, Row, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

interface IPaneSitesGroupProps {
  sites: readonly string[];
  /** The person changed the installation's list here: «Вернуть как было» is offered. */
  changed: boolean;
  onAdd: (site: string) => void;
  onRemove: (site: string) => void;
  onReset: () => void;
}

export const PaneSitesGroup = ({ sites, changed, onAdd, onRemove, onReset }: IPaneSitesGroupProps) => {
  const id = useId();
  const [text, setText] = useState('');
  const [wrong, setWrong] = useState(false);

  const submit = () => {
    const site = siteOf(text);
    if (!site) {
      setWrong(true);
      return;
    }
    onAdd(site);
    setText('');
  };

  return (
    <Group
      title={t('next.settings.links')}
      foot={
        changed ? (
          <button type="button" className="hx-set__foot-link" onClick={onReset}>
            {t('next.settings.links.reset')}
          </button>
        ) : (
          t('next.settings.links.foot')
        )
      }
    >
      {sites.length === 0 && (
        <Row type="link" label={t('next.settings.links.none')} hint={t('next.settings.links.noneHint')}>
          {null}
        </Row>
      )}
      {sites.map((site) => (
        <Row key={site} type="link" label={site}>
          <SetButton tone="ghost" onClick={() => onRemove(site)}>
            {t('next.settings.links.remove')}
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
          <span className="hx-nb__label">{t('next.settings.links.add.label')}</span>
          <input
            id={id}
            className="hx-set__input"
            value={text}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            inputMode="url"
            placeholder="posthog.example.com, *.preview.example.com, example.com/admin"
            aria-invalid={wrong || undefined}
            onChange={(e) => {
              setText(e.target.value);
              setWrong(false);
            }}
          />
        </label>
        {wrong && <FormError>{t('next.settings.links.wrong')}</FormError>}
        <div className="hx-key-form__row">
          <SetButton tone="primary" disabled={text.trim() === ''} onClick={submit}>
            {t('next.settings.links.add')}
          </SetButton>
        </div>
      </form>
    </Group>
  );
};

export default PaneSitesGroup;
