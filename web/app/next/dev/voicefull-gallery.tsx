// The full-screen talk on made-up states (`?dev=voicefull`). The voice is emulated: the field is built as a
// call would build it, the loudness is a synthetic speech-like signal.
//
//   (nothing)             the orb in all states side by side, as it is in the layer
//   &full=listening|thinking|speaking|starting|muted|failed   one state as the layer itself (full screen)
//   &subs=0               without the caption (default: a phrase in the state)
//   &muted=1              the microphone button pressed
//   &theme=light|dark&lang=ru|en&head=face
import { FIELD_FLOOR, type IVoiceField, type TFieldFloor } from '@/next/features/voice-live/field';
import VoiceOrb from '@/next/features/voice-live/orb';
import { orbOf } from '@/next/features/voice-live/orb-model';
import { stateWord, VoiceFullBody } from '@/next/features/voice-live/voice-full';
import { pinLocale, type TLang } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/voice-layer.css';
import '@/next/styles/voice-head.css';
import '@/next/dev/voicefull-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const DARK = params.get('theme') === 'dark';
{
  // The theme is set before the first paint: the gallery has no state of its own.
  const root = document.documentElement;
  root.dataset.theme = DARK ? 'dark' : 'light';
  root.classList.toggle('dark', DARK);
  root.style.colorScheme = DARK ? 'dark' : 'light';
}
const FLOORS = Object.values(FIELD_FLOOR) as TFieldFloor[];
const FULL = FLOORS.find((f) => f === params.get('full')) ?? null;
const SUBS = params.get('subs') !== '0';
const MUTED = params.get('muted') === '1' || FULL === FIELD_FLOOR.muted;
if (!SUBS) {
  try {
    localStorage.setItem('hx-voice-subs', 'off');
  } catch {
    // The caption stays as it was.
  }
}

/** A speech-like loudness: words of a quarter second with pauses, never the same twice. */
const speech = (): number => {
  const s = performance.now() / 1000;
  const word = Math.max(0, Math.sin(s * 3.1) * Math.sin(s * 0.9 + 1));
  return Math.min(1, 0.08 + word * (0.7 + 0.3 * Math.sin(s * 11)));
};
const quiet = () => 0.02;
const levelOf = (floor: TFieldFloor) =>
  floor === FIELD_FLOOR.speaking || floor === FIELD_FLOOR.listening ? speech : quiet;

const PHRASE: Partial<Record<TFieldFloor, string>> = {
  listening:
    LANG === 'ru'
      ? 'Расскажи мне про план на завтра и что нужно успеть до обеда'
      : 'Tell me about tomorrow’s plan',
  speaking:
    LANG === 'ru'
      ? 'Конечно. С утра у вас созвон по релизу, потом два часа свободны — я бы поставил туда разбор доски.'
      : 'Sure. You have the release call in the morning, then two free hours.',
};

const fieldOf = (floor: TFieldFloor): IVoiceField => ({
  host: null,
  conv: 'c-gallery' as ConvId,
  machine: 'gallery' as MachineId,
  end: () => undefined,
  floor,
  level: levelOf(floor),
  heard: floor === FIELD_FLOOR.listening ? (PHRASE.listening ?? '') : '',
  said: floor === FIELD_FLOOR.speaking ? (PHRASE.speaking ?? '') : '',
  typed: () => undefined,
  head: params.get('head') === 'face' ? 'face' : 'h',
  interrupted: false,
  muted: MUTED,
  mute: () => undefined,
});

const Gallery = () => {
  if (FULL) {
    return (
      <div className="hx-root" data-testid="gallery">
        <VoiceFullBody voice={fieldOf(FULL)} title={LANG === 'ru' ? 'План на день' : 'Day plan'} />
      </div>
    );
  }
  return (
    <div className="hx-root hx-voicefull-gallery" data-testid="gallery">
      {FLOORS.map((floor) => (
        <figure key={floor} className="hx-voicefull-gallery__cell">
          <VoiceOrb mode={orbOf(floor)} level={levelOf(floor)} className="hx-voicefull-gallery__orb" />
          <figcaption>
            {floor} · {stateWord(floor)}
          </figcaption>
        </figure>
      ))}
    </div>
  );
};

const VoiceFullGallery = () => <Gallery />;

export default VoiceFullGallery;
