// The microphone of the quick settings: its card head (the device's short name as the big figure, the live level as a
// thin bar under it, the way the limits' card shows what is left) and the choices of the list. The same choice as
// Settings → Voice input keeps (stores/voice).
import { useRef, useState, type RefObject } from 'react';
import { allowMicrophone, useMicLevel, useMicrophonesRead } from '@/next/features/voice-quick/microphones';
import type { IOption } from '@/next/features/voice-quick/parts';
import { t } from '@/next/i18n';
import { setVoicePrefs, useVoicePrefs } from '@/next/stores/voice';
import Icon from '@/next/ui/icon';

const DEFAULT_PREFIX = /^(default|communications|по умолчанию)\s*[-–—]\s*/i;
const TRAILING_KIND = /\s+(microphone|mic|микрофон)$/i;
const TRAILING_NOTE = /\s*\([^)]*\)\s*$/;

/** «Default - MacBook Pro Microphone (Built-in)» as a person says it: «MacBook Pro». */
export const shortMic = (label: string): string => {
  const short = label
    .replace(DEFAULT_PREFIX, '')
    .replace(TRAILING_NOTE, '')
    .replace(TRAILING_KIND, '')
    .trim();
  return short || label;
};

export interface IMicState {
  /** The names are known (the browser was allowed a microphone) and there is a device. */
  named: boolean;
  /** The browser has answered the question once. */
  read: boolean;
  /** The short name of the picked device; «as in the system» for none. */
  name: string;
  options: IOption[];
  /** The picked device's id; '' for the system's. */
  value: string;
  pick: (value: string) => void;
  allow: () => void;
  asking: boolean;
  failed: boolean;
  deviceId: string | null;
}

export const useMic = (): IMicState => {
  const prefs = useVoicePrefs();
  const { list, read } = useMicrophonesRead();
  const [asking, setAsking] = useState(false);
  const [failed, setFailed] = useState(false);
  // The picked device is gone (unplugged): the system's one is used, as a recording would.
  const picked = list?.find((m) => m.id === prefs.deviceId) ?? null;
  return {
    named: list !== null,
    read,
    name: picked ? shortMic(picked.label) : t('next.settings.voice.mic.default'),
    options: [
      { value: '', label: t('next.settings.voice.mic.default') },
      ...(list ?? []).map((m) => {
        const label = shortMic(m.label);
        return { value: m.id, label, sub: label === m.label ? null : m.label };
      }),
    ],
    value: picked?.id ?? '',
    pick: (value) => setVoicePrefs({ deviceId: value || null }),
    allow: () => {
      setAsking(true);
      void allowMicrophone().then((ok) => {
        setAsking(false);
        setFailed(!ok);
      });
    },
    asking,
    failed,
    deviceId: picked?.id ?? null,
  };
};

interface IMicHeadProps {
  mic: IMicState;
  title: string;
  /** Opens the list of devices: the head is its button once the devices have names. */
  onOpen: () => void;
}

/**
 * «Микрофон  MacBook Pro ›», what it hears under it as a thin bar; the head opens the devices. Without the permission,
 * the way to give it.
 */
export const MicHead = ({ mic, title, onOpen }: IMicHeadProps) => {
  const bar: RefObject<HTMLDivElement | null> = useRef(null);
  useMicLevel(mic.named, mic.deviceId, bar);
  const name = <span className="hx-ctx__pct hx-vq__big">{mic.read ? mic.name : '\u00a0'}</span>;
  return (
    <>
      {mic.named ? (
        <button type="button" className="hx-ctx__head hx-vq__headbtn" data-row="mic" onClick={onOpen}>
          <span className="hx-ctx__title">{title}</span>
          {name}
          <Icon name="chevron-right" size="sm" />
        </button>
      ) : (
        <div className="hx-ctx__head">
          <span className="hx-ctx__title">{title}</span>
          {name}
        </div>
      )}
      {mic.named ? (
        <div ref={bar} className="hx-vq__bar" role="img" aria-label={t('next.voice.quick.level')}>
          <i />
        </div>
      ) : (
        mic.read && (
          <>
            <button
              type="button"
              className="hx-vq__allow"
              disabled={mic.asking}
              aria-busy={mic.asking || undefined}
              onClick={mic.allow}
            >
              <Icon name="mic" size="sm" />
              {t('next.voice.quick.allow')}
            </button>
            {mic.failed && (
              <p className="hx-ctx__used" role="status">
                {t('next.voice.quick.denied')}
              </p>
            )}
          </>
        )
      )}
    </>
  );
};
