// «Голосовой ввод»: how dictation stands on the machine (it works, ffmpeg is missing, the model is being downloaded),
// the microphone and the language of this browser, and the model the machine recognizes with.
import { Group, MenuRow, Row, SegRow, SetButton } from '@/next/features/settings/rows';
import type { IVoiceEngine, TVoiceModel } from '@/next/features/settings/voice-api';
import { t } from '@/next/i18n';
import { VOICE_LANGS, type IVoicePrefs, type TVoiceLang } from '@/next/stores/voice';

export interface IMicrophone {
  id: string;
  label: string;
}

export interface IVoiceSectionProps {
  /** What the machine says; null while it has not answered (or cannot). */
  engine: IVoiceEngine | null;
  prefs: IVoicePrefs;
  /** The browser's microphones; null when it does not list them (no permission yet). */
  microphones: readonly IMicrophone[] | null;
  modelBusy: boolean;
  onDevice: (id: string | null) => void;
  onLang: (lang: TVoiceLang) => void;
  onModel: (model: TVoiceModel) => void;
  onCopyInstall: () => void;
}

const stateText = (engine: IVoiceEngine): string => {
  if (!engine.decoder) return t('next.settings.voice.noDecoder');
  if (!engine.builtin) return t('next.settings.voice.external');
  if (engine.state === 'preparing')
    return t('next.settings.voice.preparing', { percent: String(engine.percent) });
  if (engine.state === 'failed') return t('next.settings.voice.failed', { error: engine.error });
  if (engine.state === 'ready') return t('next.settings.voice.ready');
  return t('next.settings.voice.idle');
};

const VoiceSection = ({
  engine,
  prefs,
  microphones,
  modelBusy,
  onDevice,
  onLang,
  onModel,
  onCopyInstall,
}: IVoiceSectionProps) => (
  <>
    {engine && (
      <Group title={t('next.settings.voice.machine')}>
        <Row type="link" label={stateText(engine)} hint={engine.decoder ? null : engine.install}>
          {!engine.decoder && (
            <SetButton tone="ghost" icon="copy" onClick={onCopyInstall}>
              {t('next.settings.voice.copy')}
            </SetButton>
          )}
        </Row>
        {engine.builtin && (
          <SegRow
            label={t('next.settings.voice.model')}
            hint={t('next.settings.voice.model.hint')}
            value={engine.model}
            choices={engine.models.map((value) => ({
              value,
              label: t(`next.settings.voice.model.${value}`),
            }))}
            busy={modelBusy}
            onPick={onModel}
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

export default VoiceSection;
