// How hard the voice thinks, beside the microphone: five steps like ChatGPT's Instant … Pro. A step taken
// in a live call is told to the voice quietly (its instructions can not change once the call is made); the person can
// also say «думай лучше» / «быстрее» (style.ts).
import {
  callStyle,
  EFFORT_STEPS,
  effortNote,
  useCallStyle,
  type TEffort,
} from '@/next/features/voice-live/style';
import { t } from '@/next/i18n';
import '@/next/styles/voice-style.css';

interface IProps {
  /** Tells a live call the new step; absent before the call. */
  tell?: (note: string) => void;
}

const EffortSteps = ({ tell }: IProps) => {
  const { effort } = useCallStyle();
  const at = EFFORT_STEPS.indexOf(effort);
  const pick = (e: TEffort) => {
    if (e === effort) return;
    callStyle.set({ ...callStyle.get(), effort: e });
    tell?.(effortNote(e));
  };
  return (
    <span
      className="hx-vstyle__effort"
      role="radiogroup"
      aria-label={t('next.chatgpt.voice.effort')}
      title={t(`next.chatgpt.voice.effort.${effort}Sub`)}
      data-top={effort === 'max' || undefined}
    >
      {EFFORT_STEPS.map((e, i) => (
        <button
          key={e}
          type="button"
          role="radio"
          aria-checked={e === effort}
          aria-label={t(`next.chatgpt.voice.effort.${e}`)}
          className="hx-vstyle__step"
          data-on={i <= at || undefined}
          onClick={() => pick(e)}
        />
      ))}
      <span className="hx-vstyle__effort-label">{t(`next.chatgpt.voice.effort.${effort}`)}</span>
    </span>
  );
};

export default EffortSteps;
