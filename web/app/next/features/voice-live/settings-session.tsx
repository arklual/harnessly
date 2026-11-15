// Settings → Voice, what the voice knows and how it talks: what the person tells it about themselves (like
// ChatGPT's custom instructions), whether it also knows their agents' notes about them, how it tells the work by
// default and how hard it thinks by default. All of it goes only into the person's own calls.
import { useId, useState } from 'react';
import type { IVoiceSettings, IVoiceState } from '@/next/features/voice-live/api';
import { DELIVERY, EFFORT_STEPS, effortOf } from '@/next/features/voice-live/style';
import { MenuRow, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import '@/next/styles/voice-style.css';

const ABOUT_MAX = 2000;

interface IProps {
  state: IVoiceState;
  busy: boolean;
  save: (change: Partial<IVoiceSettings>) => void;
}

const VoiceSessionSettings = ({ state, busy, save }: IProps) => {
  const id = useId();
  // What the person is typing; null while untouched, so the saved text shows.
  const [draft, setDraft] = useState<string | null>(null);
  const text = draft ?? state.about;
  const keep = () => {
    if (draft !== null && draft.trim() !== state.about.trim()) save({ about: draft.trim() });
    setDraft(null);
  };
  return (
    <>
      <div className="hx-vstyle__about">
        <label className="hx-vstyle__label" htmlFor={id}>
          {t('next.chatgpt.voice.about')}
        </label>
        <textarea
          id={id}
          className="hx-set__input hx-vstyle__textarea"
          value={text}
          rows={3}
          maxLength={ABOUT_MAX}
          placeholder={t('next.chatgpt.voice.aboutPlaceholder')}
          disabled={busy}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={keep}
        />
        <p className="hx-vstyle__hint">{t('next.chatgpt.voice.aboutHint')}</p>
      </div>
      <SwitchRow
        label={t('next.chatgpt.voice.notes')}
        hint={t('next.chatgpt.voice.notesHint')}
        on={state.notes}
        busy={busy}
        onChange={(on) => save({ notes: on })}
      />
      <MenuRow
        label={t('next.chatgpt.voice.delivery')}
        value={state.delivery === DELIVERY.result ? DELIVERY.result : DELIVERY.narrate}
        choices={[
          {
            value: DELIVERY.narrate,
            label: t('next.chatgpt.voice.deliveryNarrate'),
            sub: t('next.chatgpt.voice.deliveryNarrateSub'),
          },
          {
            value: DELIVERY.result,
            label: t('next.chatgpt.voice.deliveryResult'),
            sub: t('next.chatgpt.voice.deliveryResultSub'),
          },
        ]}
        busy={busy}
        onPick={(v) => save({ delivery: v === DELIVERY.result ? DELIVERY.result : DELIVERY.narrate })}
      />
      <MenuRow
        label={t('next.chatgpt.voice.effort')}
        hint={t('next.chatgpt.voice.effortHint')}
        value={effortOf(state.effort)}
        choices={EFFORT_STEPS.map((e) => ({
          value: e,
          label: t(`next.chatgpt.voice.effort.${e}`),
          sub: t(`next.chatgpt.voice.effort.${e}Sub`),
        }))}
        busy={busy}
        onPick={(v) => save({ effort: effortOf(String(v)) })}
      />
    </>
  );
};

export default VoiceSessionSettings;
