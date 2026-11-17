// The full-screen talk (like the voice mode of ChatGPT): while a live voice is on, a layer over the app shows
// the mascot of the person's choice big in the middle — it listens, thinks, speaks, makes a face (voice-mood.ts)
// — a word for the state, the last phrase as a caption, and three buttons: microphone, «Чат» (folds the layer; the talk
// goes on) and «Завершить». Pulling the layer down does the same as «Чат»; the bar at the top («Вернуться») opens it
// again. Esc folds (voice-live.tsx), Space turns the microphone. Three layouts, one mascot that moves between them:
//   voice  the mascot in the middle, the caption under it;
//   stage  something shown by showOnVoiceStage (voice-stage.ts) takes the area, the mascot is small at the bottom;
//   text   the microphone is off: the turns of the call as a feed, a text field at the bottom, the mascot small at the top.
import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react';
import VoiceHead from '@/next/features/voice-live/voice-head';
import { headOf } from '@/next/features/voice-live/voice-head';
import {
  setVoiceExpanded,
  useVoiceExpanded,
  useVoiceNow,
  type IVoiceField,
  type IVoiceTurn,
} from '@/next/features/voice-live/field';
import VoiceOrb from '@/next/features/voice-live/orb';
import { orbOf, tailOf } from '@/next/features/voice-live/orb-model';
import { t } from '@/next/i18n';
import type { ConvId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence, usePresentFlag } from '@/next/ui/presence';
import { showOnVoiceStage, useVoiceStage } from '@/next/features/voice-live/voice-stage';
import VoiceActs from '@/next/features/voice-live/voice-acts-list';
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
  const stage = useVoiceStage();
  const shown = usePresence(stage, EXIT_MS);
  // The text mode: the microphone is off, the call goes on in words. A stage in the way takes the area first.
  const typing = voice.muted;
  const feedOn = usePresentFlag(typing && !stage, EXIT_MS);
  const fieldOn = usePresentFlag(typing, EXIT_MS);
  const view = stage ? 'stage' : typing ? 'text' : 'voice';

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
    if (e.button !== 0 || (e.target as HTMLElement).closest('button, input, textarea, [data-nodrag]')) return;
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
      data-view={view}
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
      <main className="hx-voice-full__stage" data-view={view}>
        {shown.value && (
          <section
            className="hx-voice-full__panel"
            data-nodrag
            data-closing={shown.state === PRESENCE.closing || undefined}
            aria-label={shown.value.title ?? t('next.chatgpt.voice.full.stage.label')}
          >
            <header className="hx-voice-full__panel-top">
              <span className="hx-voice-full__panel-title">{shown.value.title}</span>
              <button
                type="button"
                className="hx-voice-full__i"
                aria-label={t('next.chatgpt.voice.full.stage.close')}
                onClick={() => showOnVoiceStage(null)}
              >
                <Icon name="x" size="md" />
              </button>
            </header>
            <div className="hx-voice-full__panel-body">{shown.value.node}</div>
          </section>
        )}
        {feedOn.mounted && (
          <VoiceFeed turns={voice.turns ?? []} closing={feedOn.state === PRESENCE.closing} />
        )}
        <div className="hx-voice-full__mascot" data-view={view} data-s={mode}>
          {view === 'voice' && <VoiceOrb mode={mode} level={voice.level} className="hx-voice-full__orb" />}
          <VoiceHead state={head} kind={voice.head} level={voice.level} />
        </div>
        <p className="hx-voice-full__state" role="status" aria-live="polite">
          {word}
        </p>
        <p
          className="hx-voice-full__line"
          key={line}
          data-on={(subs && !!line && view === 'voice') || undefined}
          aria-hidden={!subs || view !== 'voice'}
        >
          {subs ? tailOf(line) : ''}
        </p>
        {view !== 'text' && <VoiceActs />}
      </main>
      {fieldOn.mounted && (
        <VoiceTextField onSend={voice.typed} closing={fieldOn.state === PRESENCE.closing} />
      )}
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

/** The call's turns as a chat feed (the text mode): the person's on the right, the voice's on the left, the newest at the bottom. */
const VoiceFeed = ({ turns, closing }: { turns: readonly IVoiceTurn[]; closing: boolean }) => {
  const box = useRef<HTMLDivElement>(null);
  const last = turns[turns.length - 1];
  // Follows the newest words, unless the person scrolled up to read.
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    if (near || el.dataset.fresh !== 'false') el.scrollTop = el.scrollHeight;
    el.dataset.fresh = 'false';
  }, [turns.length, last?.text]);
  return (
    <div
      ref={box}
      className="hx-voice-full__feed"
      data-nodrag
      data-closing={closing || undefined}
      data-fresh="true"
      role="log"
      aria-live="polite"
      aria-label={t('next.chatgpt.voice.full.text.feed')}
    >
      {turns.length === 0 ? (
        <p className="hx-voice-full__empty">{t('next.chatgpt.voice.full.text.empty')}</p>
      ) : (
        turns.map((turn, i) => (
          <p
            key={i}
            className="hx-voice-full__turn"
            data-role={turn.role}
            data-live={!turn.done || undefined}
          >
            {turn.text}
          </p>
        ))
      )}
    </div>
  );
};

/** The text field of the text mode: Enter sends the words into the call, Shift+Enter breaks the line. */
const VoiceTextField = ({ onSend, closing }: { onSend: (text: string) => void; closing: boolean }) => {
  const [text, setText] = useState('');
  const area = useRef<HTMLTextAreaElement>(null);
  const send = () => {
    const words = text.trim();
    if (!words) return;
    onSend(words);
    setText('');
    if (area.current) area.current.style.height = '';
  };
  const onKey = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Enter' || e.shiftKey || e.nativeEvent.isComposing) return;
    e.preventDefault();
    send();
  };
  const grow = (el: HTMLTextAreaElement) => {
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 132)}px`;
  };
  return (
    <form
      className="hx-voice-full__text"
      data-nodrag
      data-closing={closing || undefined}
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        send();
      }}
    >
      <textarea
        ref={area}
        className="hx-voice-full__input"
        rows={1}
        value={text}
        placeholder={t('next.chatgpt.voice.full.text.placeholder')}
        aria-label={t('next.chatgpt.voice.full.text.placeholder')}
        enterKeyHint="send"
        onChange={(e) => {
          setText(e.target.value);
          grow(e.target);
        }}
        onKeyDown={onKey}
      />
      <button
        type="submit"
        className="hx-voice-full__send"
        disabled={!text.trim()}
        aria-label={t('next.chatgpt.voice.full.text.send')}
      >
        <Icon name="arrow-up" size="md" />
      </button>
    </form>
  );
};

export default VoiceFull;
