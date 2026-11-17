// The form under «От организации»: the person's own skill (its SKILL.md text) or MCP server (an http address).
import { useState } from 'react';
import type { HubEnvKind, IOwnItem } from '@/next/features/hub-env/api';
import { FormError, SegControl, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import '@/next/styles/ext-providers.css';
import '@/next/styles/notify-bots.css';

export interface IHubEnvAddProps {
  kind: HubEnvKind;
  /** Resolves null when the item was added, else the words of why not. */
  onAdd: (item: IOwnItem) => Promise<string | null>;
}

const HubEnvAdd = ({ kind: initial, onAdd }: IHubEnvAddProps) => {
  const [kind, setKind] = useState<HubEnvKind>(initial);
  const [name, setName] = useState('');
  const [hint, setHint] = useState('');
  const [text, setText] = useState('');
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [seen, setSeen] = useState(initial);
  // The open tab moves the form's kind with it.
  if (seen !== initial) {
    setSeen(initial);
    setKind(initial);
  }

  const value = kind === 'skill' ? text : url;
  const ready = name.trim() !== '' && value.trim() !== '';
  const submit = async () => {
    if (!ready || busy) return;
    setBusy(true);
    setError(null);
    const source: IOwnItem['source'] =
      kind === 'skill' ? { body: text } : { transport: 'http', url: url.trim() };
    const refused = await onAdd({ kind, name: name.trim(), hint: hint.trim(), source });
    setBusy(false);
    setError(refused);
    if (refused === null) {
      setName('');
      setHint('');
      setText('');
      setUrl('');
    }
  };

  return (
    <form
      className="hx-extp__body"
      aria-busy={busy || undefined}
      aria-label={t('next.settings.hubEnv.add')}
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="hx-extp__fld">
        <span className="hx-extp__label">{t('next.settings.hubEnv.kind')}</span>
        <SegControl
          label={t('next.settings.hubEnv.kind')}
          value={kind}
          choices={[
            { value: 'skill', label: t('next.settings.hubEnv.kind.skill') },
            { value: 'mcp', label: t('next.settings.hubEnv.kind.mcp') },
          ]}
          onPick={setKind}
          disabled={busy}
        />
      </div>
      <div className="hx-extp__two">
        <label className="hx-extp__fld">
          <span className="hx-extp__label">{t('next.settings.hubEnv.name')}</span>
          <input
            className="hx-set__input"
            value={name}
            disabled={busy}
            maxLength={60}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label className="hx-extp__fld">
          <span className="hx-extp__label">{t('next.settings.hubEnv.hint')}</span>
          <input
            className="hx-set__input"
            value={hint}
            disabled={busy}
            maxLength={200}
            onChange={(event) => setHint(event.target.value)}
          />
        </label>
      </div>
      {kind === 'skill' ? (
        <label className="hx-extp__fld">
          <span className="hx-extp__label">{t('next.settings.hubEnv.skillText')}</span>
          <textarea
            className="hx-set__input hx-nb__textarea"
            value={text}
            disabled={busy}
            spellCheck={false}
            onChange={(event) => setText(event.target.value)}
          />
        </label>
      ) : (
        <label className="hx-extp__fld">
          <span className="hx-extp__label">{t('next.settings.hubEnv.url')}</span>
          <input
            className="hx-set__input hx-set__input--mono"
            value={url}
            disabled={busy}
            autoCapitalize="off"
            spellCheck={false}
            placeholder="https://mcp.example.ru/mcp"
            onChange={(event) => setUrl(event.target.value)}
          />
        </label>
      )}
      {error !== null && <FormError>{error}</FormError>}
      <SetButton tone="primary" icon="plus" busy={busy} disabled={!ready} onClick={() => void submit()}>
        {t('next.settings.hubEnv.save')}
      </SetButton>
    </form>
  );
};

export default HubEnvAdd;
