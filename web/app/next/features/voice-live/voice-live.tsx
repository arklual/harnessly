// The voice mode («Голос — в поле»): one button in the field turns the voice on and off. While it is on, the
// field itself is the voice: the wave on its text line and its ring say who has the floor, what the person says is
// written on the line as it is heard, and what they type goes to the voice, which answers out loud. Nothing grows over
// the screen; a refusal says its reason in a toast with the way out. Esc or the same button ends it; ⌥ Space starts it
// from anywhere (the shell opens the last chat and asks its field).
import { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { t } from '@/next/i18n';
import { loadVoiceState, VOICE_TALK, type TVoiceMode } from '@/next/features/voice-live/api';
import {
  FIELD_FLOOR,
  setVoiceExpanded,
  setVoiceField,
  voiceExpanded,
  VOICE_MOVED_EVENT,
  takeVoiceStart,
  takeWakeStart,
  VOICE_START_EVENT,
  type IVoiceTurn,
  type TFieldFloor,
} from '@/next/features/voice-live/field';
import { useVoiceState } from '@/next/features/voice-live/use-voice-state';
import { useWakeEnd } from '@/next/features/voice-live/wake-end';
import { WAKE_SILENCE_DEFAULT } from '@/next/features/voice-live/wake';
import {
  CALL,
  FLOOR,
  useVoiceCall,
  VOICE_FAIL,
  type IVoiceLine,
  type IVoiceTask,
  type TVoiceFail,
} from '@/next/features/voice-live/use-voice-call';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { useVoiceMascot } from '@/next/stores/me';
import { goTo, ROUTE } from '@/next/routes/routes';
import { RoundButton } from '@/next/ui/button';
import { toast } from '@/next/ui/toast';
import { dropEcho } from '@/next/features/voice-live/echo';
import '@/next/styles/voice-live.css';
import EffortSteps from '@/next/features/voice-live/effort';
import { resetCallStyle } from '@/next/features/voice-live/style';

const failKey = (fail: TVoiceFail) => `next.chatgpt.voice.fail.${fail}`;

/** Asks the call that is on to end (controls.end). */
const VOICE_END_EVENT = 'hx-voice-end';

export interface IVoiceLiveControls {
  /** Tells the voice what a task came to, or a quiet note on how it goes. */
  say: (text: string, task?: string, speak?: boolean) => Promise<boolean>;
  /** A quiet line over the field on something done or asked («Заголовок крупнее · Отменить», «Удалить? · Да»). */
  note: (text: string, action?: IVoiceNoteAction) => void;
  /** Ends the call (the router's «stop»: the person said goodbye). */
  end?: () => void;
}

/** The one button of a quiet line; after it ran, the line says `after` (or stays) and has no button. */
export interface IVoiceNoteAction {
  label: string;
  run: () => Promise<unknown>;
  after?: string;
}

interface IVoiceNote {
  id: number;
  text: string;
  action?: IVoiceNoteAction;
}

/** The quiet lines kept on screen: the latest few. */
const NOTES_MAX = 3;

interface IVoiceLiveProps {
  machine: MachineId;
  mode: TVoiceMode;
  /** A few lines about where the voice was opened (the chat, the open design). */
  context: () => string;
  /** What the call starts knowing: the chat it was opened from and the screen (bindings/voice-screen.ts). */
  primer?: () => { conv?: string; screen?: string };
  /** Something the agent is doing for the voice now: the wave thinks meanwhile. */
  working?: boolean;
  onTask?: (task: IVoiceTask, controls: IVoiceLiveControls) => void;
  /** A line said and over (the person's or the voice's): the feed shows it at once. */
  onLine?: (line: IVoiceLine) => void;
  onEnd?: (lines: IVoiceLine[]) => void;
  /** The voice is being turned on (and may be used): what it may need soon starts ahead. */
  onBegin?: () => void;
  /** Lets the owner reach the call (to say results) while it is on. */
  controlsRef?: { current: IVoiceLiveControls | null };
  /** The chat the talk belongs to (an agent's chat): the sidebar and the bar away from it know it by this. */
  conv?: ConvId | null;
}

const floorOf = (call: string, floor: string, muted: boolean, working: boolean): TFieldFloor => {
  if (call === CALL.failed) return FIELD_FLOOR.failed;
  if (call !== CALL.live) return FIELD_FLOOR.starting;
  if (floor === FLOOR.speaking) return FIELD_FLOOR.speaking;
  if (muted) return FIELD_FLOOR.muted;
  if (floor === FLOOR.thinking || working) return FIELD_FLOOR.thinking;
  return FIELD_FLOOR.listening;
};

const VoiceLive = ({
  machine,
  mode,
  context,
  primer,
  working = false,
  onTask,
  onLine,
  onEnd,
  onBegin,
  controlsRef,
  conv = null,
}: IVoiceLiveProps) => {
  const mascot = useVoiceMascot();
  // The voice's place moved (voice-layer.tsx: its chat came on screen or left): the field it stands in is looked up anew.
  const [moved, setMoved] = useState(0);
  useEffect(() => {
    const onMoved = () => setMoved((n) => n + 1);
    window.addEventListener(VOICE_MOVED_EVENT, onMoved);
    return () => window.removeEventListener(VOICE_MOVED_EVENT, onMoved);
  }, []);
  const [on, setOn] = useState(false);
  // The button stands only where the person turned the voice mode on (use-voice-state.ts).
  const [state, setState] = useVoiceState(machine);
  const holder = useRef<HTMLSpanElement>(null);
  const [notes, setNotes] = useState<readonly IVoiceNote[]>([]);
  const noteSeq = useRef(0);
  const note = (text: string, action?: IVoiceNoteAction) => {
    noteSeq.current += 1;
    const id = noteSeq.current;
    setNotes((was) => [...was, { id, text, action }].slice(-NOTES_MAX));
  };
  const act = (n: IVoiceNote) => {
    const action = n.action;
    if (!action) return;
    setNotes((was) => was.map((x) => (x.id === n.id ? { id: x.id, text: action.after ?? x.text } : x)));
    action.run().catch(() => toast(t('next.chatgpt.voice.fail.broken'), { tone: 'error' }));
  };
  // The quiet lines stand just over the field, in a place of their own put before it.
  const [notesAt, setNotesAt] = useState<HTMLElement | null>(null);
  const {
    call,
    floor,
    fail,
    failText,
    soundHeld,
    playSound,
    lines,
    muted,
    start,
    stop,
    say,
    type,
    level,
    toggleMute,
    interrupted,
  } = useVoiceCall({
    machine,
    mode,
    voice: state?.voice ?? '',
    model: state?.model ?? '',
    context,
    primer,
    barge: state?.barge ?? true,
    onTask: (task, sayIt) => onTask?.(task, { say: sayIt, note }),
    onEnd,
  });
  useEffect(() => {
    if (controlsRef)
      controlsRef.current =
        call === CALL.live
          ? { say, note, end: () => window.dispatchEvent(new Event(VOICE_END_EVENT)) }
          : null;
  }, [controlsRef, call, say]);

  // The state is read when the voice is turned on: the switch may have changed in Settings since.
  // A voice the wake phrase started ends itself on silence or «пока» (wake-end.ts).
  const [byWake, setByWake] = useState(false);
  const begin = async () => {
    setByWake(takeWakeStart());
    setOn(true);
    const st = await loadVoiceState(machine).catch(() => null);
    setState(st);
    if (st) resetCallStyle(st);
    if (!st) {
      setOn(false);
      toast(t('next.chatgpt.voice.fail.broken'), { tone: 'error' });
      return;
    }
    if (!st.ready) {
      setOn(false);
      toast(t(failKey(st.subscription ? VOICE_FAIL.noLogin : VOICE_FAIL.off)), {
        tone: 'warning',
        action: {
          label: t(st.subscription ? 'next.chatgpt.voice.signIn' : 'next.chatgpt.voice.settings'),
          onClick: () => goTo({ kind: ROUTE.settings, section: 'accounts' }),
        },
      });
      return;
    }
    onBegin?.();
    void start();
  };
  const end = () => {
    stop();
    setOn(false);
    setByWake(false);
  };

  // ⌥ Space from anywhere: the shell opened this chat and asks its field to start.
  useEffect(() => {
    const onStart = () => {
      if (takeVoiceStart() && !on) void begin();
    };
    // A field that comes just after ⌥ Space (the chat was being opened) takes the ask too.
    onStart();
    window.addEventListener(VOICE_START_EVENT, onStart);
    return () => window.removeEventListener(VOICE_START_EVENT, onStart);
    // begin reads the state of the moment.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [on]);

  // The router's «stop» (controls.end): the call ends as by its button.
  useEffect(() => {
    if (!on) return;
    const onEnd = () => end();
    window.addEventListener(VOICE_END_EVENT, onEnd);
    return () => window.removeEventListener(VOICE_END_EVENT, onEnd);
    // end reads the call's own refs only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [on]);

  useEffect(() => {
    if (!on) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      // The full-screen talk folds into the chat; the talk goes on (voice-full.tsx).
      if (voiceExpanded()) setVoiceExpanded(false);
      else end();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // end reads the call's own refs only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [on]);

  // A refusal says its reason once, with the way out.
  useEffect(() => {
    if (!on || call !== CALL.failed || !fail) return;
    toast(t(failKey(fail)), {
      tone: 'error',
      action: { label: t('next.chatgpt.voice.again'), onClick: () => void begin() },
    });
    // begin reads the state of the moment.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [on, call, fail]);

  useEffect(() => {
    if (!on || !failText) return;
    toast(failText, { tone: 'warning' });
  }, [on, failText]);

  useEffect(() => {
    if (!on || !soundHeld) return;
    toast(t('next.chatgpt.voice.sound'), {
      tone: 'warning',
      action: { label: t('next.chatgpt.voice.sound'), onClick: playSound },
    });
  }, [on, soundHeld, playSound]);

  // Every line said and over goes to the feed once.
  const told = useRef(0);
  useEffect(() => {
    if (!on) {
      told.current = 0;
      return;
    }
    const done = lines.filter((l) => l.done);
    // The person's line without what the microphone heard of the voice itself (echo.ts).
    const voiceSaid = lines.filter((l) => l.role === 'assistant').map((l) => l.text);
    for (const line of done.slice(told.current)) {
      if (line.role !== 'user') {
        onLine?.(line);
        continue;
      }
      const text = dropEcho(line.text, voiceSaid.slice(-6));
      if (text) onLine?.({ ...line, text });
    }
    told.current = done.length;
  }, [on, lines, onLine]);

  // The field is the voice while it is on.
  const open = lines.findLast((l) => l.role === 'user' && !l.done);
  const fieldFloor = floorOf(call, floor, muted, working);
  useWakeEnd({
    active: on && byWake && call === CALL.live,
    busy: fieldFloor === FIELD_FLOOR.thinking || fieldFloor === FIELD_FLOOR.speaking,
    lines,
    level,
    silenceSec: state?.wake?.silence ?? WAKE_SILENCE_DEFAULT,
    end,
  });
  const heard = open?.text.trim() ?? '';
  const said = lines.findLast((l) => l.role === 'assistant' && !l.done)?.text.trim() ?? '';
  // Only voice (Settings): nothing is written on the field's line, no quiet notes; the wave alone shows who speaks.
  const voiceOnly = state?.talk === VOICE_TALK.voice;
  // The feed of the text mode: every line with words in it, the person's without what the microphone heard of the voice.
  const turns = useMemo<IVoiceTurn[]>(() => {
    if (voiceOnly) return [];
    const voiceSaid = lines.filter((l) => l.role === 'assistant').map((l) => l.text);
    return lines.flatMap((l) => {
      const text = l.role === 'user' && l.done ? dropEcho(l.text, voiceSaid.slice(-6)) : l.text.trim();
      return text.trim() ? [{ role: l.role, text: text.trim(), done: l.done }] : [];
    });
  }, [lines, voiceOnly]);
  const endRef = useRef<() => void>(() => undefined);
  useEffect(() => {
    endRef.current = end;
  });
  useEffect(() => {
    if (!on) return;
    // Away from its chat the voice has no field and goes on: the sidebar's row and the bar show it.
    const host = holder.current?.closest<HTMLElement>('.hx-composer') ?? null;
    setVoiceField({
      host,
      conv,
      machine,
      end: () => endRef.current(),
      floor: fieldFloor,
      level,
      heard: voiceOnly ? '' : heard,
      said: voiceOnly ? '' : said,
      turns,
      typed: type,
      head: mascot,
      interrupted,
      muted,
      mute: toggleMute,
    });
  }, [
    on,
    fieldFloor,
    level,
    heard,
    said,
    turns,
    type,
    voiceOnly,
    mascot,
    interrupted,
    conv,
    machine,
    moved,
    muted,
    toggleMute,
  ]);
  useEffect(() => {
    if (!on) return;
    return () => {
      setVoiceField(null);
      setNotes([]);
    };
  }, [on]);
  useEffect(() => {
    const host = holder.current?.closest<HTMLElement>('.hx-composer');
    if (!on || !host?.parentElement) return;
    const place = document.createElement('div');
    place.className = 'hx-voice-notes';
    host.parentElement.insertBefore(place, host);
    setNotesAt(place);
    return () => {
      place.remove();
      setNotesAt(null);
    };
  }, [on, moved]);

  if (!state?.ready && !state?.subscription && !on) return null;
  // Without the Codex sign-in the button stays and says what to do; a click opens the sign-in.
  let buttonLabel = 'next.chatgpt.voice.button';
  if (on) buttonLabel = 'next.chatgpt.voice.close';
  else if (state && !state.ready) buttonLabel = 'next.chatgpt.voice.noLogin';
  const quiet =
    notesAt && notes.length > 0 && !voiceOnly
      ? createPortal(
          notes.map((n) => (
            <p key={n.id} className="hx-voice-note">
              <span className="hx-voice-note__text">{n.text}</span>
              {n.action && (
                <button type="button" className="hx-voice-note__act" onClick={() => act(n)}>
                  {n.action.label}
                </button>
              )}
            </p>
          )),
          notesAt,
        )
      : null;
  return (
    <span ref={holder} className="hx-voice-holder">
      {quiet}
      {on && call === CALL.live && <EffortSteps tell={(n) => void say(n, undefined, false)} />}
      {on && call === CALL.live && (
        <RoundButton
          icon={muted ? 'mic-off' : 'mic'}
          className="hx-voice-mic"
          label={t(muted ? 'next.chatgpt.voice.unmute' : 'next.chatgpt.voice.mute')}
          aria-pressed={muted}
          onClick={toggleMute}
        />
      )}
      <RoundButton
        icon="audio-lines"
        className="hx-voice-btn"
        label={t(buttonLabel)}
        keys={on ? ['Esc'] : ['⌥', 'Space']}
        aria-pressed={on}
        onClick={() => (on ? end() : void begin())}
      />
    </span>
  );
};

export default VoiceLive;
