// The microphone line of the quick settings: the device (the same choice as Settings → Voice input), a live level
// beside it while the panel is open, and «Allow access» while the browser still hides the devices' names.
import { useId, useRef, useState } from 'react';
import { allowMicrophone, useMicLevel, useMicrophonesRead } from '@/next/features/voice-quick/microphones';
import { t } from '@/next/i18n';
import { setVoicePrefs, useVoicePrefs } from '@/next/stores/voice';
import Icon from '@/next/ui/icon';

const MicPick = () => {
  const id = useId();
  const prefs = useVoicePrefs();
  const { list: microphones, read } = useMicrophonesRead();
  const [failed, setFailed] = useState(false);
  const [asking, setAsking] = useState(false);
  const meter = useRef<HTMLSpanElement>(null);
  // The picked device is gone (unplugged): the level listens to the system's one, as a recording would.
  const known = microphones?.some((m) => m.id === prefs.deviceId) ? prefs.deviceId : null;
  useMicLevel(microphones !== null, known, meter);

  const allow = () => {
    setAsking(true);
    void allowMicrophone().then((ok) => {
      setAsking(false);
      setFailed(!ok);
    });
  };

  return (
    <div className="hx-vq__row">
      {microphones ? (
        <label className="hx-vq__label" htmlFor={`${id}-mic`}>
          {t('next.settings.voice.mic')}
        </label>
      ) : (
        <span className="hx-vq__label">{t('next.settings.voice.mic')}</span>
      )}
      {microphones ? (
        <select
          id={`${id}-mic`}
          className="hx-vq__select"
          value={known ?? ''}
          onChange={(e) => setVoicePrefs({ deviceId: e.target.value || null })}
        >
          <option value="">{t('next.settings.voice.mic.default')}</option>
          {microphones.map((m) => (
            <option key={m.id} value={m.id}>
              {m.label}
            </option>
          ))}
        </select>
      ) : read ? (
        <button
          id={`${id}-mic`}
          type="button"
          className="hx-vq__allow"
          disabled={asking}
          aria-busy={asking || undefined}
          onClick={allow}
        >
          <Icon name="mic" size="sm" />
          {t('next.voice.quick.allow')}
        </button>
      ) : (
        <span className="hx-vq__select" aria-hidden="true" data-wait="" />
      )}
      {failed && !microphones && (
        <p className="hx-vq__note" role="status">
          {t('next.voice.quick.denied')}
        </p>
      )}
      {microphones && (
        <span ref={meter} className="hx-vq__meter" role="img" aria-label={t('next.voice.quick.level')}>
          <i />
        </span>
      )}
    </div>
  );
};

export default MicPick;
