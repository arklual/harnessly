// The voice's look on made-up states (`?dev=voice`), beside the design boards: the head in its six states at
// 26/48/96 px, «Человечек» of the voice settings, and the line of a field over a board with the head while the voice is
// on. The voice is emulated: the field's state is set as a call would set it.
//
//   &head=face          the other head (default h)
//   &s=idle|listen|think|speak|barged|error   the field's head state (default speak)
//   &mood=neutral|joy|sad|thinking|surprised   the mood the shared mascot wears (voice-mood.ts; default neutral)
//   &full=1             the full-screen talk itself, with the mascot of &head= in the state &s=:
//     &stage=1          something shown on the stage (voice-stage.ts), the mascot small at the bottom
//     &text=1           the microphone is off: the feed of turns and a text field
//   &theme=light|dark&lang=ru|en
import { useEffect, useRef } from 'react';
import { Group } from '@/next/features/settings/rows';
import DigestLine from '@/next/features/voice-live/digest-line';
import DigestListen, { DIGEST_VOICE } from '@/next/features/voice-live/digest-listen';
import FieldHead from '@/next/features/voice-live/field-head';
import MascotPick from '@/next/features/voice-live/mascot-pick';
import MiniWave from '@/next/features/voice-live/mini-wave';
import {
  FIELD_FLOOR,
  setVoiceExpanded,
  setVoiceField,
  type IVoiceField,
  type IVoiceTurn,
  type TFieldFloor,
  VOICE_LOOK,
} from '@/next/features/voice-live/field';
import VoiceHead, { HEAD, type THead } from '@/next/features/voice-live/voice-head';
import { VoiceFullBody } from '@/next/features/voice-live/voice-full';
import { setVoiceMood, VOICE_MOOD, type VoiceMood } from '@/next/features/voice-live/voice-mood';
import { showOnVoiceStage } from '@/next/features/voice-live/voice-stage';
import { pinLocale, type TLang } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { PortalContext } from '@/next/ui/portal';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/settings.css';
import '@/next/styles/island.css';
import '@/next/styles/voice-layer.css';
import '@/next/dev/voice-gallery.css';

const params = new URLSearchParams(window.location.search);
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);
const DARK = params.get('theme') === 'dark';
const KIND = params.get('head') === 'face' ? 'face' : 'h';
const STATES: readonly THead[] = [HEAD.idle, HEAD.listen, HEAD.think, HEAD.speak, HEAD.barged, HEAD.error];
const S = (STATES as readonly string[]).includes(params.get('s') ?? '')
  ? (params.get('s') as THead)
  : HEAD.speak;
const MOODS = Object.values(VOICE_MOOD) as VoiceMood[];
const MOOD = MOODS.find((m) => m === params.get('mood')) ?? VOICE_MOOD.neutral;
const FULL = params.get('full') === '1';
const STAGE = params.get('stage') === '1';
const TEXT = params.get('text') === '1';
const FLOOR_OF: Record<THead, TFieldFloor> = {
  idle: FIELD_FLOOR.muted,
  listen: FIELD_FLOOR.listening,
  think: FIELD_FLOOR.thinking,
  speak: FIELD_FLOOR.speaking,
  barged: FIELD_FLOOR.listening,
  error: FIELD_FLOOR.failed,
};

const TURNS: IVoiceTurn[] =
  LANG === 'ru'
    ? [
        { role: 'user', text: 'Что у меня сегодня по релизу?', done: true },
        {
          role: 'assistant',
          text: 'С утра созвон по релизу в 10:30, потом два часа свободны. Хотите, поставлю туда разбор доски?',
          done: true,
        },
        { role: 'user', text: 'Да, поставь. И напомни про квоты.', done: true },
        {
          role: 'assistant',
          text: 'Поставил на 12:00. Квоты: осталось перенести две организации.',
          done: true,
        },
      ]
    : [
        { role: 'user', text: 'What do I have on the release today?', done: true },
        {
          role: 'assistant',
          text: 'The release call at 10:30, then two free hours. Shall I put the board review there?',
          done: true,
        },
        { role: 'user', text: 'Yes, please. And remind me about quotas.', done: true },
        { role: 'assistant', text: 'Done for 12:00. Quotas: two organisations left to move.', done: true },
      ];

const StageDemo = () => (
  <div className="hx-voice-gallery__stage">
    <h3>{LANG === 'ru' ? 'Миграция квот' : 'Quota migration'}</h3>
    <ul>
      <li>{LANG === 'ru' ? 'Организация «Север» — перенесена' : 'North — moved'}</li>
      <li>{LANG === 'ru' ? 'Организация «Юг» — ждёт' : 'South — waiting'}</li>
      <li>{LANG === 'ru' ? 'Организация «Восток» — ждёт' : 'East — waiting'}</li>
    </ul>
    <p>
      {LANG === 'ru'
        ? 'Перенести целиком за выходные или по одной организации до понедельника.'
        : 'Move everything over the weekend or one by one until Monday.'}
    </p>
  </div>
);

/** The full-screen talk on a made-up call. */
const FullGallery = () => {
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = DARK ? 'dark' : 'light';
    root.classList.toggle('dark', DARK);
    root.style.colorScheme = DARK ? 'dark' : 'light';
    setVoiceMood(MOOD, 0);
    if (STAGE) showOnVoiceStage(<StageDemo />, LANG === 'ru' ? 'Квоты' : 'Quotas');
    return () => {
      showOnVoiceStage(null);
      setVoiceMood(VOICE_MOOD.neutral);
    };
  }, []);
  const voice: IVoiceField = {
    host: null,
    conv: 'c-board' as ConvId,
    machine: 'gallery' as MachineId,
    end: () => undefined,
    floor: TEXT ? FIELD_FLOOR.muted : FLOOR_OF[S],
    level: () => 0.35 + 0.3 * Math.sin(performance.now() / 120),
    heard: '',
    said: '',
    turns: TURNS,
    typed: () => undefined,
    head: KIND,
    interrupted: S === HEAD.barged,
    muted: TEXT,
    mute: () => undefined,
  };
  return (
    <div className="hx-root" data-testid="gallery">
      <VoiceFullBody voice={voice} title={LANG === 'ru' ? 'План на день' : 'Day plan'} />
    </div>
  );
};

const Gallery = () => {
  const portal = useRef<HTMLDivElement>(null);
  const island = useRef<HTMLDivElement>(null);
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = DARK ? 'dark' : 'light';
    root.classList.toggle('dark', DARK);
    root.style.colorScheme = DARK ? 'dark' : 'light';
    setVoiceField({
      host: host.current,
      conv: 'c-board' as ConvId,
      machine: 'gallery' as MachineId,
      end: () => undefined,
      floor: FLOOR_OF[S],
      level: () => 0.6,
      heard: '',
      said: '',
      typed: () => undefined,
      head: KIND,
      interrupted: S === HEAD.barged,
      muted: false,
      mute: () => undefined,
    });
    setVoiceExpanded(false);
    setVoiceMood(MOOD, 0);
    return () => {
      setVoiceField(null);
      setVoiceMood(VOICE_MOOD.neutral);
    };
  }, []);
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-voice-gallery" data-testid="gallery">
        <section className="hx-voice-gallery__row">
          {STATES.map((s) => (
            <figure key={s} className="hx-voice-gallery__cell">
              <span className="hx-voice-gallery__s96">
                <VoiceHead state={s} kind={KIND} />
              </span>
              <span className="hx-voice-gallery__s48">
                <VoiceHead state={s} kind={KIND} />
              </span>
              <VoiceHead state={s} kind={KIND} />
              <figcaption>{s}</figcaption>
            </figure>
          ))}
        </section>
        <section className="hx-voice-gallery__row">
          {MOODS.map((m) => (
            <figure key={m} className="hx-voice-gallery__cell">
              <span className="hx-voice-gallery__s96">
                <VoiceHead state={HEAD.idle} kind={KIND} mood={m} />
              </span>
              <span className="hx-voice-gallery__s48">
                <VoiceHead state={HEAD.speak} kind={KIND} mood={m} level={() => 0.5} />
              </span>
              <VoiceHead state={HEAD.listen} kind={KIND} mood={m} />
              <figcaption>{m}</figcaption>
            </figure>
          ))}
        </section>
        <section className="hx-voice-gallery__row">
          <MiniWave look={VOICE_LOOK.speak} />
          <MiniWave look={VOICE_LOOK.listen} />
          <MiniWave look={VOICE_LOOK.think} />
        </section>
        <section className="hx-voice-gallery__row">
          {Object.values(DIGEST_VOICE).map((st) => (
            <DigestListen
              key={st}
              state={st}
              onPlay={() => undefined}
              onPause={() => undefined}
              onEnd={() => undefined}
            />
          ))}
        </section>
        <section className="hx-voice-gallery__set">
          <DigestLine
            head={HEAD.speak}
            me="Давай с миграции — открой варианты"
            said="Открыл «Разобраться с миграцией квот». Вариантов два:"
            rest="перенести квоты целиком за выходные или по одной организации до понедельника. Какой берём?"
            onOpen={() => undefined}
            openLabel="Открыть пункт"
          />
          <DigestLine head={HEAD.speak} voiceOnly level={() => 0.6} />
        </section>
        <section className="hx-voice-gallery__set">
          <Group title={LANG === 'ru' ? 'Голос' : 'Voice'}>
            <MascotPick />
          </Group>
        </section>
        <section className="hx-voice-gallery__field">
          <div className="hx-island" ref={island}>
            <div className="hx-island__status" data-shown="true">
              <div className="hx-island__status-in">
                <div className="hx-island__now hx-island__line">
                  <FieldHead root={island} />
                  <span className="hx-island__step">Покрасил «Оплату» в красный</span>
                  <span className="hx-spacer" />
                  <button type="button" className="hx-island__act">
                    Отменить
                  </button>
                  <button type="button" className="hx-island__act">
                    В чат
                  </button>
                </div>
              </div>
            </div>
            <div ref={host} className="hx-composer hx-voice-gallery__composer" />
          </div>
        </section>
      </div>
      <div ref={portal} />
    </PortalContext>
  );
};

const VoiceGallery = () => (FULL ? <FullGallery /> : <Gallery />);

export default VoiceGallery;
