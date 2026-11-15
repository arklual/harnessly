// The voice's look on made-up states (`?dev=voice`), beside the design boards: the head in its six states at
// 26/48/96 px, «Человечек» of the voice settings, and the line of a field over a board with the head while the voice is
// on. The voice is emulated: the field's state is set as a call would set it.
//
//   &head=face          the other head (default h)
//   &s=idle|listen|think|speak|barged|error   the field's head state (default speak)
//   &theme=light|dark&lang=ru|en
import { useEffect, useRef } from 'react';
import { Group } from '@/next/features/settings/rows';
import DigestLine from '@/next/features/voice-live/digest-line';
import DigestListen, { DIGEST_VOICE } from '@/next/features/voice-live/digest-listen';
import FieldHead from '@/next/features/voice-live/field-head';
import MascotPick from '@/next/features/voice-live/mascot-pick';
import MiniWave from '@/next/features/voice-live/mini-wave';
import { FIELD_FLOOR, setVoiceField, type TFieldFloor, VOICE_LOOK } from '@/next/features/voice-live/field';
import VoiceHead, { HEAD, type THead } from '@/next/features/voice-live/voice-head';
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
const FLOOR_OF: Record<THead, TFieldFloor> = {
  idle: FIELD_FLOOR.muted,
  listen: FIELD_FLOOR.listening,
  think: FIELD_FLOOR.thinking,
  speak: FIELD_FLOOR.speaking,
  barged: FIELD_FLOOR.listening,
  error: FIELD_FLOOR.failed,
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
    });
    return () => setVoiceField(null);
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

const VoiceGallery = () => <Gallery />;

export default VoiceGallery;
