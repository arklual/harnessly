// One voice call: the page's WebRTC peer connection (or the machine's, machine-sound.ts) with the microphone
// going out and the voice coming back, the machine's control socket followed for transcripts and tasks, and the loudness of whoever speaks for the orb.
import { journal } from '@/next/core/journal';
import { callStyle, styleFromWords } from '@/next/features/voice-live/style';
import { useCallback, useEffect, useRef, useState } from 'react';
import { refusalCode } from '@/next/features/chatgpt/api';
import {
  callAudioSocket,
  followCall,
  hangCall,
  loadVoiceState,
  sayInCall,
  startCall,
  VOICE_ROUTE,
  type IVoiceEvent,
  type TVoiceMode,
} from '@/next/features/voice-live/api';
import { BARGE_START, bargeBar, bargeStep } from '@/next/features/voice-live/barge';
import { dropEcho } from '@/next/features/voice-live/echo';
import {
  joinMachineSound,
  MACHINE_STATE,
  machineSoundWorks,
  preferMachine,
  prefersMachine,
  type IMachineSound,
} from '@/next/features/voice-live/machine-sound';
import type { MachineId } from '@/next/protocol/v2';

export const CALL = {
  idle: 'idle',
  connecting: 'connecting',
  live: 'live',
  ended: 'ended',
  failed: 'failed',
} as const;
export type TCall = (typeof CALL)[keyof typeof CALL];

/** Who has the floor now. */
export const FLOOR = { listening: 'listening', thinking: 'thinking', speaking: 'speaking' } as const;
export type TFloor = (typeof FLOOR)[keyof typeof FLOOR];

/** Why a call did not start or broke. */
export const VOICE_FAIL = {
  off: 'off',
  noLogin: 'noLogin',
  mic: 'mic',
  refused: 'refused',
  broken: 'broken',
  /** The sound did not come through in time: the device tried the voice server's addresses and none answered. */
  silent: 'silent',
  /** The voice server's answer named no address the device could try. */
  noRoute: 'noRoute',
} as const;
export type TVoiceFail = (typeof VOICE_FAIL)[keyof typeof VOICE_FAIL];

export interface IVoiceLine {
  role: 'user' | 'assistant';
  text: string;
  done: boolean;
}

export interface IVoiceTask {
  id: string;
  /** The voice's instruction for the task (its own words). */
  text: string;
  /** What the person said for it, word for word, as heard: the agent gets these, the voice's words only as a note. */
  said?: string;
}

/** The voice's words that promise work: «смотрю», «сделаю», «передаю», «сейчас поправлю». */
const PROMISE_RE =
  /(^|[^а-яё])(смотрю|посмотрю|гляну|передаю|передам|передал|сделаю|делаю|поправлю|исправлю|займусь|запускаю|проверю|сейчас покажу)([^а-яё]|$)/i;
/** Whether a line of the voice promises work. */
export const promises = (text: string): boolean => PROMISE_RE.test(text.toLowerCase());
/** How long a promise waits for the voice's own hand-over before the page hands the words over itself. */
export const PROMISE_WAIT_MS = 2000;

/** How many of the voice's last lines its echo is looked for in. */
const ECHO_LINES = 6;

/**
 * The person's words since the line `from`: what they said for the task the voice hands over now, without the voice's
 * own words the microphone heard back (echo.ts).
 */
export const saidSince = (lines: readonly { role: string; text: string }[], from: number): string => {
  const voice = lines
    .filter((l) => l.role === 'assistant')
    .slice(-ECHO_LINES)
    .map((l) => l.text);
  return lines
    .slice(from)
    .filter((l) => l.role === 'user')
    .map((l) => dropEcho(l.text, voice))
    .filter(Boolean)
    .join(' ');
};

interface IOptions {
  machine: MachineId;
  mode: TVoiceMode;
  voice: string;
  /** The model to call with; '' leaves it to the machine's settings. */
  model: string;
  /** A few lines about where the voice was opened. */
  context: () => string;
  /** What the call starts knowing beside the context: the chat it was opened from and the screen (voice-screen.ts). */
  primer?: () => { conv?: string; screen?: string };
  /** Speaking over the voice stops it (barge.ts); off in Settings, the voice talks on. */
  barge?: boolean;
  onTask?: (
    task: IVoiceTask,
    say: (text: string, task?: string, speak?: boolean) => Promise<boolean>,
  ) => void;
  /** The call is over: what was said, in order. */
  onEnd?: (lines: IVoiceLine[]) => void;
  /** The call is ready: in place of the greeting (the digest's reader starts reading). */
  onReady?: () => void;
  /** A line is over: who said it and what. */
  onTurn?: (role: IVoiceLine['role'], text: string) => void;
}

const failOf = (error: unknown): TVoiceFail => {
  if (error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'NotFoundError'))
    return VOICE_FAIL.mic;
  const code = refusalCode(error);
  if (code === 'voice_off') return VOICE_FAIL.off;
  if (code === 'voice_no_login') return VOICE_FAIL.noLogin;
  if (code === 'voice_refused' || code === 'voice_unreached') return VOICE_FAIL.refused;
  return VOICE_FAIL.broken;
};

/** How long a call may connect before the person is told it did not. */
const CONNECT_MS = 15_000;
/** How long a direct call may connect before it is made again over the machine: the direct way connects in a second
 *  where it works at all. */
const DIRECT_MS = 8_000;

/** The candidates of an SDP as kinds only, «udp host ×2, tcp host» — no address leaves the page. */
export const candidateKinds = (sdp: string | undefined): string => {
  const kinds = new Map<string, number>();
  for (const m of (sdp ?? '').matchAll(/^a=candidate:\S+ \d+ (\S+) \d+ \S+ \d+ typ (\S+)/gm)) {
    const k = `${m[1].toLowerCase()} ${m[2]}`;
    kinds.set(k, (kinds.get(k) ?? 0) + 1);
  }
  return [...kinds].map(([k, n]) => (n > 1 ? `${k} ×${n}` : k)).join(', ') || 'none';
};

/** Why a call that did not connect in time did not: the voice server named no address, or none of them answered. */
export const silentFail = (remoteSdp: string | undefined): TVoiceFail =>
  candidateKinds(remoteSdp) === 'none' ? VOICE_FAIL.noRoute : VOICE_FAIL.silent;

/** What the voice is asked to say first: it proves the sound goes both ways. */
const GREETING =
  'Greet the person in one short sentence in Russian and say you are listening. Do not ask anything else.';

const gathered = (pc: RTCPeerConnection) =>
  new Promise<void>((resolve) => {
    if (pc.iceGatheringState === 'complete') return resolve();
    const timer = setTimeout(resolve, 2500);
    pc.addEventListener('icegatheringstatechange', () => {
      if (pc.iceGatheringState !== 'complete') return;
      clearTimeout(timer);
      resolve();
    });
  });

/** The loudness (0…1) of a stream, read on demand. */
const meter = (ctx: AudioContext, stream: MediaStream) => {
  const analyser = ctx.createAnalyser();
  analyser.fftSize = 1024;
  ctx.createMediaStreamSource(stream).connect(analyser);
  const buf = new Float32Array(analyser.fftSize);
  return () => {
    analyser.getFloatTimeDomainData(buf);
    let sum = 0;
    for (const v of buf) sum += v * v;
    return Math.min(1, Math.sqrt(sum / buf.length) * 4);
  };
};

/** The lines with a transcript piece added: to the speaker's open line, else as a new one. */
export const addPiece = (
  all: readonly IVoiceLine[],
  role: IVoiceLine['role'],
  text: string,
  done: boolean,
): IVoiceLine[] => {
  const at = all.findLastIndex((l) => l.role === role && !l.done);
  if (at >= 0) {
    const was = all[at];
    const merged = done ? text || was.text : was.text + text;
    return [...all.slice(0, at), { role, text: merged, done }, ...all.slice(at + 1)];
  }
  return text.trim() ? [...all, { role, text, done }] : [...all];
};

/** How a call's sound goes: straight between the page and the voice, or over the machine (machine-sound.ts). */
export const VIA = { direct: 'direct', machine: 'machine' } as const;
export type TVia = (typeof VIA)[keyof typeof VIA];

interface ILive {
  id: string | null;
  /** The page's own peer connection; null when the sound goes over the machine. */
  pc: RTCPeerConnection | null;
  /** The sound over the machine; null on the direct way. */
  sound: IMachineSound | null;
  mic: MediaStream;
  ctx: AudioContext;
  audio: HTMLAudioElement;
  unfollow: () => void;
  timer: ReturnType<typeof setTimeout> | undefined;
  frame: number;
  /** The route the call took: the key route takes cancels on its data channel. */
  route: string;
  via: TVia;
  /** The machine could carry the sound itself: a direct call that does not connect is made again over it. */
  relay?: boolean;
}

export const useVoiceCall = ({
  machine,
  mode,
  voice,
  model,
  context,
  primer,
  barge: bargeOn = true,
  onTask,
  onEnd,
  onReady,
  onTurn,
}: IOptions) => {
  const [call, setCall] = useState<TCall>(CALL.idle);
  const [floor, setFloor] = useState<TFloor>(FLOOR.listening);
  const [fail, setFail] = useState<TVoiceFail | null>(null);
  // What the voice's side said when it refused mid-call.
  const [failText, setFailText] = useState('');
  // The browser held the sound back: one click on the page lets it play.
  const [soundHeld, setSoundHeld] = useState(false);
  // How loud whoever speaks is now, 0…1: the field's wave reads it every frame.
  const loudness = useRef(0);
  const [lines, setLines] = useState<IVoiceLine[]>([]);
  const [muted, setMuted] = useState(false);
  // The person spoke over the voice and it is silenced (barge.ts) until its cut answer is over.
  const [interrupted, setInterrupted] = useState(false);
  const live = useRef<ILive | null>(null);
  // Cuts the voice's answer on the routes that can (the key route): set when a call starts.
  const cut = useRef<() => void>(() => undefined);
  const linesRef = useRef<IVoiceLine[]>([]);
  // The first line not yet handed over with a task.
  const taskFrom = useRef(0);
  // The wait after the voice promised something, for its hand-over (promises()).
  const promiseTimer = useRef(0);
  const orb = useRef<HTMLSpanElement | null>(null);
  const handlers = useRef({ onTask, onEnd, onReady, onTurn, context, primer, bargeOn });
  useEffect(() => {
    handlers.current = { onTask, onEnd, onReady, onTurn, context, primer, bargeOn };
  });
  // The voice's sound held back by the page (the digest paused): muted whatever barge-in says.
  const held = useRef(false);

  const putLines = (next: IVoiceLine[]) => {
    linesRef.current = next;
    setLines(next);
  };

  /** Adds a transcript piece: to the open line of that speaker (the voice may start answering before the person's
   * line is final), or as a new line. */
  const piece = (role: IVoiceLine['role'], text: string, done: boolean) => {
    putLines(addPiece(linesRef.current, role, text, done));
  };

  const teardown = useCallback(
    (ended: boolean) => {
      const l = live.current;
      live.current = null;
      window.clearTimeout(promiseTimer.current);
      if (!l) return;
      cancelAnimationFrame(l.frame);
      clearTimeout(l.timer);
      l.unfollow();
      l.pc?.close();
      l.sound?.close();
      for (const track of l.mic.getTracks()) track.stop();
      l.audio.srcObject = null;
      void l.ctx.close();
      if (l.id) void hangCall(machine, l.id).catch(() => undefined);
      if (ended) handlers.current.onEnd?.(linesRef.current.filter((x) => x.text.trim()));
    },
    [machine],
  );

  /** Gives the voice a task's result or a note; speak false keeps it quiet. */
  const say = useCallback(
    (text: string, task?: string, speak = true) => {
      const id = live.current?.id;
      if (!id) return Promise.resolve(false);
      return sayInCall(machine, id, { text, task, speak }).then(
        () => true,
        () => false,
      );
    },
    [machine],
  );

  const onEvent = (e: IVoiceEvent) => {
    switch (e.type) {
      case 'user':
        // The person speaks again: the voice will hand over the whole of it, the earlier promise's wait is off.
        window.clearTimeout(promiseTimer.current);
        setFloor(FLOOR.listening);
        piece('user', e.text ?? '', false);
        break;
      case 'assistant':
        piece('assistant', e.text ?? '', false);
        break;
      case 'turn':
        setFailText('');
        if (e.role) piece(e.role, e.text ?? '', true);
        if (e.role) handlers.current.onTurn?.(e.role, e.text ?? '');
        // The voice said it does something («смотрю», «сделаю») but handed nothing over: after PROMISE_WAIT_MS the
        // person's own words go to the agent from here, so a promise is never left empty (15.11, «обещает и не делает»).
        if (e.role === 'assistant' && promises(e.text ?? '')) {
          const from = taskFrom.current;
          window.clearTimeout(promiseTimer.current);
          promiseTimer.current = window.setTimeout(() => {
            if (taskFrom.current !== from || !live.current) return;
            const said = saidSince(linesRef.current, from);
            if (!said) return;
            taskFrom.current = linesRef.current.length;
            handlers.current.onTask?.({ id: '', text: said, said }, say);
          }, PROMISE_WAIT_MS);
        }
        if (e.role === 'user') {
          setFloor(FLOOR.thinking);
          // «Помолчи, скажи просто результат», «думай лучше»: the call's style switches, and the voice is told quietly.
          const change = styleFromWords(e.text ?? '', callStyle.get());
          if (change) {
            callStyle.set(change.style);
            void say(change.note, undefined, false);
          }
        }
        break;
      case 'task':
        if (e.id) {
          const said = saidSince(linesRef.current, taskFrom.current);
          taskFrom.current = linesRef.current.length;
          handlers.current.onTask?.({ id: e.id, text: e.text ?? '', said }, say);
        }
        break;
      case 'error':
        // Not every refusal ends the call: its words stand under the status until the next turn.
        setFailText(e.message ?? '');
        break;
      case 'ready':
        if (handlers.current.onReady) handlers.current.onReady();
        else void say(GREETING, undefined, true);
        break;
      case 'ended':
        if (live.current) {
          teardown(true);
          setCall(CALL.ended);
        }
        break;
      default:
        break;
    }
  };

  // The call made again another way (start's own fallback): the start of the moment.
  const startRef = useRef<(withModel?: string, via?: TVia) => Promise<void>>(async () => undefined);
  /** Starts a call; `withModel` overrides the model of the options (a model picked a moment ago). `via` is how its
   * sound goes: straight between the page and the voice, or over the machine (machine-sound.ts) for a network that
   * carries no UDP there; left out, the device's last need decides. A direct call that never connects is made again
   * over the machine. */
  const start = useCallback(
    async (withModel?: string, via?: TVia) => {
      if (live.current) return;
      const how: TVia = via ?? (prefersMachine() && machineSoundWorks() ? VIA.machine : VIA.direct);
      held.current = false;
      setFail(null);
      setFailText('');
      setSoundHeld(false);
      setMuted(false);
      putLines([]);
      taskFrom.current = 0;
      setFloor(FLOOR.listening);
      setCall(CALL.connecting);
      let mic: MediaStream | null = null;
      const pc = how === VIA.direct ? new RTCPeerConnection() : null;
      const ctx = new AudioContext();
      const audio = new Audio();
      audio.autoplay = true;
      try {
        mic = await navigator.mediaDevices.getUserMedia({
          audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
        });
        let events: { open: () => boolean; send: (s: string) => void } = {
          open: () => false,
          send: () => undefined,
        };
        if (pc) {
          for (const track of mic.getTracks()) pc.addTrack(track, mic);
          const dc = pc.createDataChannel('oai-events', { ordered: true });
          dc.addEventListener('message', (m: MessageEvent<string>) => onServer(m.data));
          events = { open: () => dc.readyState === 'open', send: (s) => dc.send(s) };
        }
        const l: ILive = {
          id: null,
          pc,
          sound: null,
          mic,
          ctx,
          audio,
          unfollow: () => undefined,
          frame: 0,
          timer: undefined,
          route: '',
          via: how,
        };
        live.current = l;
        const own = meter(ctx, mic);
        let theirs: (() => number) | null = null;
        let loudAt = 0;
        const hear = (stream: MediaStream) => {
          audio.srcObject = stream;
          audio.play().then(
            () => setSoundHeld(false),
            () => setSoundHeld(true),
          );
          theirs = meter(ctx, stream);
        };
        let up = false;
        const connected = () => {
          if (live.current !== l) return;
          up = true;
          clearTimeout(l.timer);
          setCall(CALL.live);
        };
        /** The sound never came through: a direct call is made again over the machine, where it can be. */
        const broke = (ev: string, text: string, why: TVoiceFail) => {
          if (live.current !== l) return;
          const again = l.via === VIA.direct && machineSoundWorks() && l.relay;
          journal(
            { level: 'warn', src: 'web', ev, text: `${text}${again ? ' → over the machine' : ''}` },
            machine,
          );
          if (again) {
            teardown(false);
            preferMachine(true);
            void startRef.current(withModel, VIA.machine);
            return;
          }
          // Over the machine failed too: the next call tries the direct way first again.
          if (l.via === VIA.machine) preferMachine(false);
          teardown(true);
          setFail(why);
          setCall(CALL.failed);
        };
        const diag = () =>
          pc
            ? `ice=${pc.iceConnectionState} conn=${pc.connectionState} gather=${pc.iceGatheringState} route=${l.route} local=[${candidateKinds(pc.localDescription?.sdp)}] remote=[${candidateKinds(pc.remoteDescription?.sdp)}]`
            : `via=machine route=${l.route} socket=${l.sound ? `${l.sound.open() ? 'open' : 'shut'} ${l.sound.seen()}` : 'joining'}`;
        if (pc) {
          pc.addEventListener('track', (e) => {
            const [stream] = e.streams;
            if (stream) hear(stream);
          });
          pc.addEventListener('connectionstatechange', () => {
            if (pc.connectionState === 'connected') connected();
            if (pc.connectionState === 'failed') broke('voice.connect-failed', diag(), VOICE_FAIL.broken);
          });
        }
        let shown = 0;
        // Barge-in (barge.ts): the person speaking over the voice silences its sound on the next frame; the key route
        // also cancels the answer and drops what the server still has to send.
        let barge = BARGE_START;
        cut.current = () => cutAnswer();
        const cutAnswer = () => {
          if (l.route !== VOICE_ROUTE.key || !events.open()) return;
          events.send(JSON.stringify({ type: 'response.cancel' }));
          events.send(JSON.stringify({ type: 'output_audio_buffer.clear' }));
        };
        function onServer(data: string) {
          // The server's own turn detection, when it is quicker than the page's.
          if (barge.ducked !== null || performance.now() - loudAt >= 350) return;
          const type = (() => {
            try {
              return (JSON.parse(data) as { type?: string }).type;
            } catch {
              return undefined;
            }
          })();
          if (type !== 'input_audio_buffer.speech_started' || !handlers.current.bargeOn) return;
          // The server hears speech in a noise too: the page's own bar for this room has the last word (barge.ts).
          if (own() < bargeBar(barge) * 0.8) return;
          barge = { ...barge, over: null, ducked: performance.now(), loudAt };
          audio.muted = true;
          setInterrupted(true);
          cutAnswer();
        }
        const tick = () => {
          const them = theirs ? theirs() : 0;
          const me = own();
          const now = performance.now();
          if (them > 0.04) loudAt = now;
          const speaking = now - loudAt < 350;
          const [next, cut] = bargeStep(barge, me, them, now, speaking && handlers.current.bargeOn);
          barge = next;
          audio.muted = barge.ducked !== null || held.current;
          setInterrupted(barge.ducked !== null);
          if (cut) cutAnswer();
          setFloor((f) => {
            if (speaking) return FLOOR.speaking;
            return f === FLOOR.speaking ? FLOOR.listening : f;
          });
          const level = speaking ? them : me;
          shown += (level - shown) * 0.25;
          loudness.current = shown;
          orb.current?.style.setProperty('--hx-voice-level', shown.toFixed(3));
          l.frame = requestAnimationFrame(tick);
        };
        l.frame = requestAnimationFrame(tick);
        if (pc) {
          await pc.setLocalDescription();
          await gathered(pc);
        }
        const answer = await startCall(machine, {
          sdp: pc?.localDescription?.sdp ?? '',
          mode,
          voice,
          model: withModel ?? model,
          context: handlers.current.context(),
          ...handlers.current.primer?.(),
          ...callStyle.get(),
          ...(pc ? {} : { relay: true }),
        });
        if (live.current !== l) {
          void hangCall(machine, answer.id).catch(() => undefined);
          return;
        }
        l.id = answer.id;
        l.route = answer.route;
        if (pc) {
          // Whether this machine could carry the sound itself, should this way not come through.
          void loadVoiceState(machine).then(
            (st) => {
              l.relay = st.relay === true;
            },
            () => undefined,
          );
          await pc.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
        } else {
          const sound = await joinMachineSound(callAudioSocket(machine, answer.id), mic, {
            state: (s) => {
              if (s === MACHINE_STATE.connected) connected();
              // The machine closes the sound of a call that ended: an end, not a break.
              if (s === MACHINE_STATE.closed && up) {
                if (live.current !== l) return;
                teardown(true);
                setCall(CALL.ended);
                return;
              }
              if (s === MACHINE_STATE.failed || s === MACHINE_STATE.closed)
                broke('voice.machine-sound', `${diag()} state=${s}`, VOICE_FAIL.broken);
            },
            event: onServer,
          });
          if (live.current !== l) {
            sound.close();
            return;
          }
          l.sound = sound;
          hear(sound.stream);
        }
        l.unfollow = followCall(machine, answer.id, onEvent);
        // Live only when the sound flows; a call that never connects says so instead of listening to nothing.
        if (pc?.connectionState === 'connected') setCall(CALL.live);
        l.timer = setTimeout(
          () => {
            // Over the machine the sound may have come before this timer was set: the machine says connected as soon
            // as the page's socket joins a call it has already connected.
            if (live.current !== l || up || pc?.connectionState === 'connected') return;
            // What the machine's journal needs to tell a blocked network from a server that named nowhere to go.
            broke(
              'voice.connect-timeout',
              diag(),
              pc ? silentFail(pc.remoteDescription?.sdp) : VOICE_FAIL.silent,
            );
          },
          pc && machineSoundWorks() ? DIRECT_MS : CONNECT_MS,
        );
      } catch (error) {
        if (live.current) teardown(false);
        else {
          pc?.close();
          for (const track of mic?.getTracks() ?? []) track.stop();
          void ctx.close();
        }
        // A machine that can not carry the sound yet: the device goes the direct way again.
        if (how === VIA.machine && refusalCode(error) === 'voice_bad_request') {
          preferMachine(false);
          if (via === undefined) {
            void startRef.current(withModel, VIA.direct);
            return;
          }
        }
        setFail(failOf(error));
        setCall(CALL.failed);
      }
    },
    // onEvent reads refs only; the call is made with the options of the moment it starts.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [machine, mode, voice, model, teardown],
  );
  useEffect(() => {
    startRef.current = start;
  });

  const stop = useCallback(() => {
    if (!live.current) return;
    teardown(true);
    setCall(CALL.ended);
  }, [teardown]);

  const toggleMute = useCallback(() => {
    const l = live.current;
    if (!l) return;
    setMuted((m) => {
      for (const track of l.mic.getAudioTracks()) track.enabled = m;
      return !m;
    });
  }, []);

  useEffect(() => () => teardown(false), [teardown]);

  /** Words the person typed in place of saying them: the voice answers out loud. */
  const type = useCallback(
    (text: string) => {
      const id = live.current?.id;
      if (!id) return;
      putLines([...linesRef.current, { role: 'user', text, done: true }]);
      setFloor(FLOOR.thinking);
      void sayInCall(machine, id, { text, typed: true }).catch(() => undefined);
    },
    [machine],
  );
  const level = useCallback(() => loudness.current, []);

  /** Holds the voice's sound back (on) or lets it go again; holding also cuts the answer where the route can. */
  const hold = useCallback((on: boolean) => {
    held.current = on;
    const l = live.current;
    if (l) l.audio.muted = on;
    if (on) cut.current();
  }, []);

  const playSound = useCallback(() => {
    live.current?.sound?.resume();
    const audio = live.current?.audio;
    if (audio)
      audio.play().then(
        () => setSoundHeld(false),
        () => undefined,
      );
  }, []);

  return {
    call,
    floor,
    fail,
    failText,
    soundHeld,
    playSound,
    lines,
    muted,
    interrupted,
    start,
    stop,
    toggleMute,
    say,
    type,
    level,
    hold,
    orb,
  };
};
