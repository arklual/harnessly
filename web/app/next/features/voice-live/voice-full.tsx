// The full-screen talk (like the voice mode of ChatGPT): while a live voice is on, a layer over the app shows
// the orb in the middle — it listens, thinks, speaks — a word for the state, the last phrase as a caption, and three
// buttons: microphone, «Чат» (folds the layer; the talk goes on) and «Завершить». Pulling the layer down does the same
// as «Чат»; the bar at the top («Вернуться») opens it again. Esc folds (voice-live.tsx), Space turns the microphone.
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import VoiceHead from '@/next/features/voice-live/voice-head';
import { headOf } from '@/next/features/voice-live/voice-head';
import {
  setVoiceExpanded,
  useVoiceExpanded,
  useVoiceNow,
  type IVoiceField,
} from '@/next/features/voice-live/field';
import VoiceOrb from '@/next/features/voice-live/orb';
import { orbOf, tailOf } from '@/next/features/voice-live/orb-model';
import { t } from '@/next/i18n';
import type { ConvId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import '@/next/styles/voice-full.css';

/** The layer leaves in this long (hx-voice-full-out). */
const EXIT_MS = 320;
/** Pulled down this far (px), or this fast (px/ms) and at least FOLD_MIN, the layer folds. */
const FOLD_AT = 120;
const FOLD_SPEED = 0.6;
const FOLD_MIN = 40;
const SUBS_KEY = 'hx-voice-subs';

const readSubs = (): boolean => {
  try {
    return localStorage.getItem(SUBS_KEY) !== 'off';
  } catch {
    return true;
  }
};

interface IVoiceFullProps {
  /** The chat's name, null while the list does not know it. */
  titleOf: (conv: ConvId) => string | null;
}

const VoiceFull = ({ titleOf }: IVoiceFullProps) => {
  const now = useVoiceNow();
  const open = useVoiceExpanded();
  const { value: voice, state } = usePresence<IVoiceField>(open ? now : null, EXIT_MS);
  if (!voice) return null;
  const title = voice.conv ? titleOf(voice.conv) : null;
  return <VoiceFullBody voice={voice} title={title} closing={state === PRESENCE.closing} />;
};

/** The word under the orb. */
export const stateWord = (floor: IVoiceField['floor']): string => t(`next.chatgpt.voice.full.s.${floor}`);

export const VoiceFullBody = ({
  voice,
  title,
  closing = false,
  onFold = () => setVoiceExpanded(false),
}: {
  voice: IVoiceField;
  title: string | null;
  closing?: boolean;
  onFold?: () => void;
}) => {
  const root = useRef<HTMLDivElement>(null);
  const [subs, setSubs] = useState(readSubs);
  const mode = orbOf(voice.floor);
  const head = voice.interrupted ? 'barged' : headOf(voice.floor);

  // The caption: the phrase being said now, kept until the other side starts one (state set while drawing, not in an effect).
  const [line, setLine] = useState('');
  const spoken = voice.floor === 'speaking' ? voice.said : voice.heard;
  if (spoken.trim() && spoken !== line) setLine(spoken);
  else if ((voice.floor === 'starting' || voice.floor === 'failed') && line) setLine('');

  // Focus: the layer takes it when it opens and gives it back when it folds.
  useEffect(() => {
    const was = document.activeElement as HTMLElement | null;
    root.current?.focus({ preventScroll: true });
    return () => {
      if (was?.isConnected) was.focus({ preventScroll: true });
    };
  }, []);

  // Space turns the microphone (a button in focus keeps its own Space).
  const mute = voice.mute;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== ' ' || e.repeat) return;
      const el = e.target instanceof Element ? e.target : null;
      if (el?.closest('input, textarea, select, button, a, [contenteditable="true"]')) return;
      e.preventDefault();
      mute();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [mute]);

  // Pulling down: the layer follows the pointer, a release far or fast enough folds it, else it springs back.
  const drag = useRef<{ id: number; y0: number; at: number; y: number; v: number; pulling: boolean } | null>(
    null,
  );
  const move = (dy: number) => {
    const el = root.current;
    if (!el) return;
    // Up is held by a rubber band, down follows.
    const y = dy < 0 ? dy / 6 : dy;
    el.style.transition = 'none';
    el.style.transform = `translateY(${y}px) scale(${1 - Math.min(0.06, Math.max(0, y) / 4000)})`;
  };
  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0 || (e.target as HTMLElement).closest('button')) return;
    drag.current = {
      id: e.pointerId,
      y0: e.clientY,
      at: performance.now(),
      y: e.clientY,
      v: 0,
      pulling: false,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    const t1 = performance.now();
    const dt = Math.max(1, t1 - d.at);
    d.v = d.v * 0.6 + ((e.clientY - d.y) / dt) * 0.4;
    d.y = e.clientY;
    d.at = t1;
    if (!d.pulling && Math.abs(e.clientY - d.y0) > 6) d.pulling = true;
    if (d.pulling) move(e.clientY - d.y0);
  };
  const onUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    drag.current = null;
    const el = root.current;
    if (!d || d.id !== e.pointerId || !el || !d.pulling) return;
    const dy = e.clientY - d.y0;
    if (dy > FOLD_AT || (d.v > FOLD_SPEED && dy > FOLD_MIN)) {
      // Inertia: it goes on down at the speed it was let go with.
      const left = Math.max(0, window.innerHeight - dy);
      const ms = Math.min(320, Math.max(140, left / Math.max(0.8, d.v)));
      el.dataset.gone = 'true';
      el.style.transition = `transform ${Math.round(ms)}ms cubic-bezier(0.32, 0.72, 0, 1)`;
      el.style.transform = `translateY(${window.innerHeight}px)`;
      window.setTimeout(onFold, Math.round(ms) - 20);
    } else {
      el.style.transition = 'transform 460ms cubic-bezier(0.34, 1.56, 0.64, 1)';
      el.style.transform = '';
    }
  };

  const toggleSubs = () => {
    setSubs((was) => {
      try {
        localStorage.setItem(SUBS_KEY, was ? 'off' : 'on');
      } catch {
        // Not kept: the choice lasts until the page is closed.
      }
      return !was;
    });
  };
  const word = stateWord(voice.floor);
  return (
    <div
      ref={root}
      className="hx-voice-full"
      role="dialog"
      aria-label={t('next.chatgpt.voice.full.label')}
      tabIndex={-1}
      data-s={mode}
      data-closing={closing || undefined}
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerCancel={onUp}
    >
      <header className="hx-voice-full__top">
        <button
          type="button"
          className="hx-voice-full__i"
          aria-label={t('next.chatgpt.voice.full.fold')}
          onClick={onFold}
        >
          <Icon name="chevron-down" size="md" />
        </button>
        <span className="hx-voice-full__who">
          <VoiceHead state={head} kind={voice.head} level={voice.level} />
          <span className="hx-voice-full__name">{title ?? t('next.chatgpt.voice.bar.untitled')}</span>
        </span>
        <button
          type="button"
          className="hx-voice-full__i"
          aria-label={t(subs ? 'next.chatgpt.voice.full.subsOff' : 'next.chatgpt.voice.full.subsOn')}
          aria-pressed={subs}
          onClick={toggleSubs}
        >
          <Icon name="captions" size="md" />
        </button>
      </header>
      <div className="hx-voice-full__grab" aria-hidden="true" />
      <main className="hx-voice-full__stage">
        <VoiceOrb mode={mode} level={voice.level} className="hx-voice-full__orb" />
        <p className="hx-voice-full__state" role="status" aria-live="polite">
          {word}
        </p>
        <p
          className="hx-voice-full__line"
          key={line}
          data-on={(subs && !!line) || undefined}
          aria-hidden={!subs}
        >
          {subs ? tailOf(line) : ''}
        </p>
      </main>
      <footer className="hx-voice-full__bar">
        <button
          type="button"
          className="hx-voice-full__b"
          aria-pressed={voice.muted}
          data-on={voice.muted || undefined}
          onClick={voice.mute}
        >
          <span className="hx-voice-full__c">
            <Icon name={voice.muted ? 'mic-off' : 'mic'} size="md" />
          </span>
          <span>{t(voice.muted ? 'next.chatgpt.voice.full.unmute' : 'next.chatgpt.voice.full.mute')}</span>
        </button>
        <button type="button" className="hx-voice-full__b" onClick={onFold}>
          <span className="hx-voice-full__c">
            <Icon name="message-square" size="md" />
          </span>
          <span>{t('next.chatgpt.voice.full.chat')}</span>
        </button>
        <button type="button" className="hx-voice-full__b hx-voice-full__b--end" onClick={voice.end}>
          <span className="hx-voice-full__c">
            <Icon name="x" size="md" />
          </span>
          <span>{t('next.chatgpt.voice.bar.end')}</span>
        </button>
      </footer>
    </div>
  );
};

export default VoiceFull;
