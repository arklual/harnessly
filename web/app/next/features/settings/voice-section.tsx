// «Голосовой ввод»: how dictation stands on the machine (it works, ffmpeg is missing, the model is being downloaded),
// what recognizes there — the built-in whisper with its model, or a server of the person's own that speaks OpenAI's
// API — and the microphone and the language of this browser.
import { useId, useState } from 'react';
import { FormError, Group, MenuRow, Row, SegRow, SetButton } from '@/next/features/settings/rows';
import {
  VOICE_ENGINES,
  type IVoiceEngine,
  type IVoiceRemote,
  type TVoiceEngineKind,
  type TVoiceModel,
  needsNewKey,
  originOf,
} from '@/next/features/settings/voice-api';
import { t } from '@/next/i18n';
import { VOICE_LANGS, type IVoicePrefs, type TVoiceLang } from '@/next/stores/voice';

export interface IMicrophone {
  id: string;
  label: string;
}

/** What the server form asks the machine to keep; `key` is left out to keep the one it has. */
export interface IRemoteForm {
  url: string;
  model: string;
  key?: string;
}

export interface IVoiceSectionProps {
  /** What the machine says; null while it has not answered (or cannot). */
  engine: IVoiceEngine | null;
  prefs: IVoicePrefs;
  /** The browser's microphones; null when it does not list them (no permission yet). */
  microphones: readonly IMicrophone[] | null;
  modelBusy: boolean;
  engineBusy: boolean;
  onDevice: (id: string | null) => void;
  onLang: (lang: TVoiceLang) => void;
  onModel: (model: TVoiceModel) => void;
  onBuiltin: () => void;
  /** Resolves with the machine's refusal in words, null when it kept the server. */
  onRemote: (form: IRemoteForm) => Promise<string | null>;
  onCopyInstall: () => void;
}

const stateText = (engine: IVoiceEngine): string => {
  if (!engine.decoder) return t('next.settings.voice.noDecoder');
  if (engine.engine === 'remote' && engine.remote) return t('next.settings.voice.remote.on');
  if (!engine.builtin) return t('next.settings.voice.external');
  if (engine.state === 'preparing')
    return t('next.settings.voice.preparing', { percent: String(engine.percent) });
  if (engine.state === 'failed') return t('next.settings.voice.failed', { error: engine.error });
  if (engine.state === 'ready') return t('next.settings.voice.ready');
  return t('next.settings.voice.idle');
};

/** A download size as the person reads it: «190 МБ», «0,6 ГБ». */
const sizeText = (bytes: number | undefined): string | null => {
  if (!bytes) return null;
  const mb = bytes / 1_000_000;
  if (mb < 1000) return t('next.settings.voice.size.mb', { n: String(Math.round(mb)) });
  return t('next.settings.voice.size.gb', {
    n: (mb / 1000).toLocaleString(undefined, { maximumFractionDigits: 1 }),
  });
};

/** The engine the section shows: the machine's, or «Свой сервер» while its form is being filled. */
const shownEngine = (engine: IVoiceEngine | null, drafting: boolean): TVoiceEngineKind | null => {
  if (!engine) return null;
  if (drafting) return 'remote';
  return engine.engine;
};

const keyPlaceholder = (remote: IVoiceRemote | null, newOrigin: boolean): string => {
  if (newOrigin) return t('next.settings.voice.remote.keyNew');
  if (remote?.keyMask) return remote.keyMask;
  return t('next.settings.voice.remote.keyNone');
};

interface IServerFormProps {
  remote: IVoiceRemote | null;
  busy: boolean;
  onSave: (form: IRemoteForm) => Promise<string | null>;
}

/** The address, model and key of the person's server. The key field starts empty: the saved key is not sent back. */
const ServerForm = ({ remote, busy, onSave }: IServerFormProps) => {
  const ids = useId();
  const [url, setUrl] = useState(remote?.url ?? '');
  const [model, setModel] = useState(remote?.model ?? 'whisper-1');
  const [key, setKey] = useState('');
  const [error, setError] = useState<string | null>(null);
  const newOrigin = needsNewKey(remote, url);
  const ready = url.trim() !== '' && model.trim() !== '' && (!newOrigin || key.trim() !== '');
  const onUrl = (next: string) => {
    // Another server: what was typed as a key for the old one is not sent to it.
    if (originOf(next) !== originOf(url)) setKey('');
    setUrl(next);
  };
  const save = () => {
    if (!ready) return;
    const form: IRemoteForm = { url: url.trim(), model: model.trim() };
    if (key.trim() !== '') form.key = key.trim();
    void onSave(form).then((refusal) => {
      setError(refusal);
      if (refusal === null) setKey('');
    });
  };
  return (
    <div className="hx-key-form hx-voice-server" data-state={busy ? 'busy' : undefined}>
      <label className="hx-voice-server__field" htmlFor={`${ids}-url`}>
        <span className="hx-nb__label">{t('next.settings.voice.remote.url')}</span>
        <input
          id={`${ids}-url`}
          className="hx-set__input hx-set__input--mono"
          value={url}
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          placeholder="https://api.openai.com/v1"
          onChange={(e) => onUrl(e.target.value)}
        />
      </label>
      <label className="hx-voice-server__field" htmlFor={`${ids}-model`}>
        <span className="hx-nb__label">{t('next.settings.voice.remote.model')}</span>
        <input
          id={`${ids}-model`}
          className="hx-set__input hx-set__input--mono"
          value={model}
          autoComplete="off"
          spellCheck={false}
          placeholder="whisper-1"
          onChange={(e) => setModel(e.target.value)}
        />
      </label>
      <label className="hx-voice-server__field" htmlFor={`${ids}-key`}>
        <span className="hx-nb__label">{t('next.settings.voice.remote.key')}</span>
        <input
          id={`${ids}-key`}
          className="hx-set__input hx-set__input--mono"
          type="password"
          value={key}
          autoComplete="new-password"
          spellCheck={false}
          required={newOrigin}
          aria-invalid={newOrigin && key.trim() === '' ? true : undefined}
          placeholder={keyPlaceholder(remote, newOrigin)}
          onChange={(e) => setKey(e.target.value)}
        />
      </label>
      {error && <FormError>{error}</FormError>}
      <div className="hx-key-form__row">
        <SetButton tone="primary" busy={busy} disabled={!ready || busy} onClick={save}>
          {t('next.settings.voice.remote.save')}
        </SetButton>
      </div>
    </div>
  );
};

const VoiceSection = ({
  engine,
  prefs,
  microphones,
  modelBusy,
  engineBusy,
  onDevice,
  onLang,
  onModel,
  onBuiltin,
  onRemote,
  onCopyInstall,
}: IVoiceSectionProps) => {
  // «Свой сервер» picked here, before the machine has a server to switch to.
  const [drafting, setDrafting] = useState(false);
  const shown = shownEngine(engine, drafting);
  const pickEngine = (kind: TVoiceEngineKind) => {
    if (!engine) return;
    if (kind === 'builtin') {
      setDrafting(false);
      if (engine.engine !== 'builtin') onBuiltin();
      return;
    }
    setDrafting(true);
  };
  const saveRemote = (form: IRemoteForm) =>
    onRemote(form).then((refusal) => {
      if (refusal === null) setDrafting(false);
      return refusal;
    });
  return (
    <>
      {engine && (
        <Group
          title={t('next.settings.voice.machine')}
          foot={shown === 'remote' ? t('next.settings.voice.remote.foot') : null}
        >
          <Row type="link" label={stateText(engine)} hint={engine.decoder ? null : engine.install}>
            {!engine.decoder && (
              <SetButton tone="ghost" icon="copy" onClick={onCopyInstall}>
                {t('next.settings.voice.copy')}
              </SetButton>
            )}
          </Row>
          {engine.choosable && (
            <SegRow
              label={t('next.settings.voice.engine')}
              hint={t('next.settings.voice.engine.hint')}
              value={shown}
              choices={VOICE_ENGINES.map((value) => ({
                value,
                label: t(`next.settings.voice.engine.${value}`),
              }))}
              busy={engineBusy}
              onPick={pickEngine}
            />
          )}
          {shown === 'builtin' && engine.builtin && (
            <MenuRow
              label={t('next.settings.voice.model')}
              hint={t('next.settings.voice.model.hint')}
              value={engine.model}
              choices={engine.models.map((value) => ({
                value,
                label: t(`next.settings.voice.model.${value}`),
                sub:
                  value === 'auto' ? t('next.settings.voice.model.auto.sub') : sizeText(engine.sizes[value]),
              }))}
              busy={modelBusy}
              onPick={(value) => onModel(value as TVoiceModel)}
            />
          )}
          {shown === 'remote' && engine.choosable && (
            <ServerForm
              key={engine.remote?.url ?? ''}
              remote={engine.remote}
              busy={engineBusy}
              onSave={saveRemote}
            />
          )}
        </Group>
      )}
      <Group title={t('next.settings.voice.browser')} foot={t('next.settings.voice.browser.foot')}>
        <MenuRow
          label={t('next.settings.voice.mic')}
          hint={microphones ? null : t('next.settings.voice.mic.hint')}
          value={prefs.deviceId}
          choices={[
            { value: null, label: t('next.settings.voice.mic.default'), sub: null },
            ...(microphones ?? []).map((m) => ({ value: m.id, label: m.label, sub: null })),
          ]}
          onPick={(value) => onDevice(typeof value === 'string' ? value : null)}
        />
        <SegRow
          label={t('next.settings.voice.lang')}
          hint={t('next.settings.voice.lang.hint')}
          value={prefs.lang}
          choices={VOICE_LANGS.map((value) => ({ value, label: t(`next.settings.voice.lang.${value}`) }))}
          onPick={onLang}
        />
      </Group>
    </>
  );
};

export default VoiceSection;
