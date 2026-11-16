// The field under the feed (the design's .hx-composer): it grows with the text up to 40% of the screen, «+» attaches
// (a sheet on a phone), the pill shows the agent and model, the microphone dictates into the field, and the round
// button sends, or stops the agent while it works. Keys are in keys.ts; ↑ in an empty field opens the earlier prompts
// and the quick ones (prompt-history.tsx), «/» the commands, skills and settings of the agent (slash-palette.tsx). On a phone the dock rides over the keyboard.
import AttachmentThumb from '@/next/ui/attachment-thumb';
import { formatProgress, percentOf } from '@/next/ui/transfer';
import { distinctFiles, pastedFiles } from '@/next/features/composer/distinct-files';
import { useFileDrop } from '@/next/features/composer/use-file-drop';
import ContextRing, { type IContextRingProps } from '@/next/features/composer/context-ring';
import {
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
  type RefObject,
} from 'react';
import { isStashKey, KEY_ACTION, keyAction } from '@/next/features/composer/keys';
import LimitsChip, { paysBySpend } from '@/next/features/limits/limits-chip';
import LimitsMini from '@/next/features/limits/limits-mini';
import { useHeld } from '@/next/ui/use-held';
import type { LimitsView } from '@/next/features/limits/model';
import ModelPicker, { type IModelPickerProps } from '@/next/features/composer/model-picker';
import type { IPlusApps } from '@/next/features/composer/apps-pane';
import PlusMenu, {
  type IPlusAttach,
  type IPlusMenuHandle,
  type IPlusTakes,
} from '@/next/features/composer/plus-menu';
import type { IStash } from '@/next/features/composer/stash-list';
import { RefChips, type IRefChip } from '@/next/features/chat/ref-chips';
import { AnnotationChips } from '@/next/features/composer/annotation-chips';
import { NoteChips, type INoteChipGroup } from '@/next/ui/notes/note-chips';
import type { IAnnotation } from '@/next/features/pane/annotations';
import { usePromptHistory } from '@/next/features/composer/prompt-history';
import { useSlashPalette, type ISlashSource } from '@/next/features/composer/slash-palette';
import type {
  ComposerAttachment,
  ComposerContext,
  ComposerMode,
  Recording,
} from '@/next/features/composer/types';
import useKeyboardInset from '@/next/features/composer/use-keyboard-inset';
import { RoundButton } from '@/next/ui/button';
import { useFieldValue } from '@/next/ui/use-field-value';
import { useFieldEdges, useFitField } from '@/next/ui/use-fit-field';
import { boxOf, canAnimate, claimEdit, FLIGHT_EASE, FLIGHT_MS, launchFrom } from '@/next/ui/flight';
import Icon, { type TIconName } from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { useMedia } from '@/next/ui/use-media';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import { usePresence } from '@/next/ui/presence';
import { DictationMirror, RecordingTime, RecordingWave } from '@/next/features/composer/recording';
import { FIELD_FLOOR, useVoiceField } from '@/next/features/voice-live/field';
import VoiceHead, { HEAD, headOf } from '@/next/features/voice-live/voice-head';
import { chordOf, isMac } from '@/next/features/keys/chord';
import { KEY_SCOPE } from '@/next/features/keys/commands';
import { useKeyHint } from '@/next/features/keys/keys-help';
import { commandsOfChord, isDefault } from '@/next/features/keys/keymap';
import { keyOverrides, useKeys } from '@/next/features/keys/use-keys';
import '@/next/styles/ui.css';
import '@/next/styles/composer.css';
import { countRender } from '@/next/core/perf-journal';

export type TSendMode = 'queue' | 'now';

const PLACEHOLDER_KEYS: Record<ComposerMode, string> = {
  new: 'next.composer.placeholderNew',
  idle: 'next.composer.placeholder',
  stopped: 'next.composer.placeholderStopped',
  working: 'next.composer.placeholderWorking',
  answer: 'next.composer.placeholderAnswer',
};
/** A narrow field (a design's chat column, the side chat) says the same in a few words; the whole hint is its tooltip. */
const SHORT_PLACEHOLDER_KEYS: Partial<Record<ComposerMode, string>> = {
  idle: 'next.composer.placeholderShort',
  stopped: 'next.composer.placeholderStoppedShort',
  working: 'next.composer.placeholderWorkingShort',
};
/** The key that sends into the running turn, as the hints write it: ⌘ on a Mac, ⌃ elsewhere (keys.ts takes both). */
const QUEUE_MOD = isMac() ? '⌥' : 'Alt+';
/** Narrower than this, the field's own text box takes the short placeholder. */
const NARROW_FIELD_PX = 480;
/** A file this big shows its percent, speed and time left on its chip; a small one only its bar. */
const BIG_UPLOAD_BYTES = 8 * 1024 * 1024;

/** Whether the text box is narrow, kept current as its column changes (the window, the pane, a design's chat). */
const useNarrowField = (field: RefObject<HTMLTextAreaElement | null>): boolean => {
  const [narrow, setNarrow] = useState(false);
  useEffect(() => {
    const node = field.current;
    if (!node || typeof ResizeObserver === 'undefined') return undefined;
    // A box not laid out yet (width 0) is not narrow.
    const observer = new ResizeObserver(() =>
      setNarrow(node.clientWidth > 0 && node.clientWidth < NARROW_FIELD_PX),
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [field]);
  return narrow;
};

export interface IComposerQuote {
  id: string;
  label: string;
  /** The chip's hint when it is hovered: the whole quote and the remark under it; the label when there is none. */
  title?: string;
  /** With the person's remark, not a bare quote. */
  remark: boolean;
  /** The chip's own icon (a task, a merge request taken from the Apps screen); a quote's follow `remark`. */
  icon?: TIconName;
}

export interface IComposerProps {
  value: string;
  onChange: (value: string) => void;
  /**
   * Enter: send, or into the queue while the agent works (it goes when the agent finishes its turn); Ctrl+Enter or the
   * «Now» button: into the working agent's turn (it reads the message after its current step).
   */
  onSubmit: (mode: TSendMode) => void;
  mode: ComposerMode;
  /** Words for the empty field in place of the mode's own (a chat held on its usage limit says when a message goes). */
  placeholder?: string;
  /** Stops the agent; the button waits for the runtime's answer. */
  onStop: () => Promise<unknown>;
  /** The agent only waits for its background agents: the stop button says it ends the wait. */
  waiting?: boolean;
  /** The agent, model and depth pill with its popover; null where the machine has no agent to pick. */
  picker: IModelPickerProps | null;
  /** The pill, the limits and the ring are put away (a «Разговор» of the new chat, kind-switch.tsx): they fold to
   *  nothing by width and fade, and unfold the same way back, instead of standing a moment and vanishing at once. */
  tucked?: boolean;
  /**
   * What «/» lists over the field (commands, skills, MCP servers, settings, files); without it «/» only finds the quick
   * prompts, as before.
   */
  slash?: ISlashSource | null;
  attachments: ComposerAttachment[];
  /** Files picked with «+», dropped on the field or pasted into it. */
  onFiles: (files: File[]) => void;
  onRemoveAttachment: (id: string) => void;
  /** Uploads a failed file again. */
  onRetryAttachment: (id: string) => void;
  /** Parts of the agent's answers picked to answer to: chips before the files, they go with the message. */
  quotes?: readonly IComposerQuote[];
  onRemoveQuote?: (id: string) => void;
  contexts?: ComposerContext[];
  onContext?: (id: string) => void;
  /** A chat carried from the sidebar can be dropped on the field: its next message points at it. */
  acceptsChats?: boolean;
  /** The places attached to the message (a design, a document, a board, a file, an MR, a task): chips before the files. */
  refs?: readonly IRefChip[];
  onRemoveRef?: (ref: IRefChip) => void;
  /** The marks sent from the pane's browser: a chip each, after the places; their words go with the message. */
  notes?: readonly IAnnotation[];
  onEditNote?: (id: string, comment: string) => void;
  onRemoveNote?: (id: string) => void;
  /** Annotations as numbered chips, a group each (the feed's words, a design's blocks); a press shows the annotated. */
  noteGroups?: readonly INoteChipGroup[];
  /** Filled with the field's own send (Enter's), for a host's button that sends what the field holds. */
  sendRef?: RefObject<(() => void) | null>;
  /** What «+» can attach besides files; without it only files, prompts and contexts. */
  attach?: IPlusAttach | null;
  /** Drafts put aside («Отложить черновик», ⌘⇧S); without it the field has no such thing. */
  stash?: IStash | null;
  /** The person's recordings in «+»; without it there is no «Записи». */
  takes?: IPlusTakes | null;
  /** The installed apps «+» can turn on in this chat; without it there is no «Приложение». */
  apps?: IPlusApps | null;
  recording: Recording | null;
  /** Voice input; without it the microphone is not offered. `at`: the caret in the field, where the words go. */
  onMic?: (at?: number) => void;
  /** Where the dictation wants the caret: after the words it put in. */
  caret?: number | null;
  /** What is left of the agent's subscription, by the pill; no chip until the machine has answered. */
  limits?: LimitsView | null;
  /** How full the agent's window is; no ring until it is known. */
  contextRing?: IContextRingProps | null;
  /** What this chat cost by the agent's own count, USD: the spend card of a machine that pays by the API. */
  chatCost?: number;
  onCancelRecording: () => void;
  onFinishRecording: () => void;
  /** How many dictated texts the voice input has fixed in the field so far (see useVoice). */
  recognized?: number;
  /** The longest text the runtime takes (Limits.maxText). */
  maxText: number;
  /** Enter sends (the default); off: Enter is a new line and Ctrl/Cmd+Enter sends (Settings). */
  enterSends?: boolean;
  inputRef?: Ref<HTMLTextAreaElement>;
  /** The field stands in its place before the chat is known (a page just loaded): quiet, nothing in it can be used yet. */
  quiet?: boolean;
  /** The host's own buttons on the bar, before the microphone (a design's «pick a block»). */
  tools?: ReactNode;
  /** The host takes words only (the side chat): no «+», and a file dropped or pasted is not taken. */
  plain?: boolean;
  /** The host has something to send without words (a picture's marks in its editor). */
  ready?: boolean;
  /** What the send button says where it is not a message (a review's «Спросить агента»). */
  sendLabel?: string;
}

/** The recording bar's exit, as long as hx-recbar-out in composer.css. */
const REC_EXIT_MS = 180;

export { RecordingTime, RecordingWave };

/** A line said aloud, as long as the field shows: its latest words. */
const LINE_SHOWN = 90;
const lineTail = (text: string) => (text.length > LINE_SHOWN ? `…${text.slice(-LINE_SHOWN)}` : text);

/** The text that left the field fades up and away over this long, while the bubble takes it over. */
/** The pastes a field has taken. */
const pasted = new WeakSet<Event>();

/** What the bar's pill, limits and ring mean, for holding them over a change of chat (ui/use-held.ts). */
const pickerKey = (p: IModelPickerProps): string => JSON.stringify(p.value);
const limitsKey = (view: LimitsView): string =>
  view.state === 'ready'
    ? JSON.stringify([view.provider, view.profile, view.limits, view.spend])
    : view.state;
const ringKey = (ring: IContextRingProps): string => `${ring.used}/${ring.limit}/${ring.compacting === true}`;

const LIFT_MS = 200;

/** How long the old hint fades before the new one takes its place and fades in (composer.css, .hx-composer__input). */
const HINT_OUT_MS = 140;

/** The field's hint changes (a «Задача» becomes a «Разговор»): the old words fade out, the new ones fade in, instead
 *  of one replacing the other at once. Reduced motion swaps them at once. */
const useHintSwap = (hint: string, calm: boolean): { text: string; out: boolean } => {
  const [shown, setShown] = useState(hint);
  const [out, setOut] = useState(false);
  const swap = !calm && hint !== shown;
  if (swap && !out) setOut(true);
  useEffect(() => {
    if (!out) return;
    const timer = setTimeout(() => {
      setShown(hint);
      setOut(false);
    }, HINT_OUT_MS);
    return () => clearTimeout(timer);
  }, [out, hint]);
  if (calm) return { text: hint, out: false };
  return { text: shown, out };
};

/** A control of the bar that can be put away (IComposerProps.tucked): it folds by width and fades, and takes no click
 *  or focus while it is away. The box is always there, so the way back is the same motion. */
const Tuck = ({ tucked, children }: { tucked: boolean; children: ReactNode }) => (
  <span className="hx-tuck" data-tucked={tucked || undefined} inert={tucked || undefined}>
    <span className="hx-tuck__in">{children}</span>
  </span>
);
const LIFT_PX = 10;
const Composer = (props: IComposerProps) => {
  countRender('field');
  const { value, onChange, onSubmit, mode, picker, attachments, recording, maxText } = props;
  const tucked = props.tucked === true;
  const rootRef = useRef<HTMLDivElement>(null);
  const voice = useVoiceField(rootRef);
  const rowRef = useRef<HTMLDivElement>(null);
  const fieldRef = useRef<HTMLTextAreaElement | null>(null);
  // ⇧Esc (features/keys): the cursor to the end of the latest field on screen.
  useKeys(KEY_SCOPE.global, {
    'global.focusField': () => {
      const field = fieldRef.current;
      if (!field) return;
      field.focus();
      field.setSelectionRange(field.value.length, field.value.length);
    },
  });
  const mirrorRef = useRef<HTMLDivElement | null>(null);
  /** What was sent from the field a moment ago: the text and where it stood, until the field shows it is empty. */
  const sentRef = useRef<{ text: string; at: number } | null>(null);
  const touch = useMedia('(pointer: coarse)');
  const calm = useMedia('(prefers-reduced-motion: reduce)');
  const stop = usePending<'stop'>();
  // The recording bar leaves with an animation and keeps what it showed meanwhile.
  const { value: bar, state: barState } = usePresence(recording, REC_EXIT_MS);
  // Over a change of chat or screen the bar's pill, limits and ring keep what they showed until the next one's come
  // (ui/use-held.ts): nothing vanishes for a frame. A held pill is the last chat's: it takes no click meanwhile.
  const shownPicker = useHeld(picker, pickerKey);
  const shownLimits = useHeld(props.limits ?? null, limitsKey);
  const shownRing = useHeld(props.contextRing ?? null, ringKey);
  // The controls come back with a soft entrance once a recording has ended; on the page's first draw they just stand.
  const [recorded, setRecorded] = useState(false);
  if (bar && !recorded) setRecorded(true);
  // «Stop and send»: the text is still being recognized; it goes when the recording is over and the field has it.
  const [sendAfter, setSendAfter] = useState(false);
  useKeyboardInset(rootRef);
  useImperativeHandle(props.inputRef, () => fieldRef.current as HTMLTextAreaElement, []);

  const narrowField = useNarrowField(fieldRef);
  // Before the fitting: it measures the text this puts in.
  useFieldValue(fieldRef, value);
  useFitField(fieldRef, value);
  useFieldEdges(fieldRef, value);

  // While it listens the layer over the field shows the same text: the newest words are in view, the layer scrolls with
  // the field and wraps where the field's scrollbar leaves room.
  const listening = bar !== null;
  // The caret follows the dictated words until the person puts it somewhere with a key or the pointer; then it
  // stays where they put it (the browser would move it to the end with every new text). On a desktop the field takes
  // the focus when it starts listening, so the person can edit while speaking.
  const ownCaret = useRef<{ moved: boolean; start: number; end: number }>({ moved: false, start: 0, end: 0 });
  const startedListening = useRef(false);
  useLayoutEffect(() => {
    const input = fieldRef.current;
    const layer = mirrorRef.current;
    if (!listening || !input || !layer) {
      startedListening.current = false;
      return;
    }
    if (!startedListening.current) {
      startedListening.current = true;
      ownCaret.current.moved = false;
      if (window.matchMedia?.('(pointer: fine)').matches) input.focus({ preventScroll: true });
    }
    const own = ownCaret.current;
    const at = props.caret ?? value.length;
    if (document.activeElement === input) {
      if (own.moved) input.setSelectionRange(own.start, own.end);
      else input.setSelectionRange(at, at);
    }
    if (own.moved || at >= value.length) input.scrollTop = input.scrollHeight;
    layer.style.setProperty('--hx-sb', `${input.offsetWidth - input.clientWidth}px`);
    layer.scrollTop = input.scrollTop;
  }, [listening, value, props.caret]);
  // Where the caret stood when the field lost the focus to the microphone: a dictation started from there goes in there.
  const blurCaret = useRef<{ value: string; at: number } | null>(null);
  const onMicClick = () => {
    const field = fieldRef.current;
    const kept = blurCaret.current;
    let at: number | undefined;
    if (field && document.activeElement === field) at = field.selectionStart;
    else if (kept && kept.value === value) at = kept.at;
    props.onMic?.(at);
  };
  useEffect(() => {
    const input = fieldRef.current;
    const layer = mirrorRef.current;
    if (!listening || !input || !layer) return;
    const follow = () => {
      layer.scrollTop = input.scrollTop;
    };
    input.addEventListener('scroll', follow);
    return () => input.removeEventListener('scroll', follow);
  }, [listening]);

  // The text that was sent leaves softly: the field is empty at once, a copy of the words rises and fades over it.
  useLayoutEffect(() => {
    const sent = sentRef.current;
    const field = fieldRef.current;
    const root = rootRef.current;
    if (!sent) return;
    sentRef.current = null;
    // The send was refused and the person goes on writing: nothing left the field.
    if (value !== '' || !field || !root) return;
    if (Date.now() - sent.at > 1500 || !sent.text.trim() || !canAnimate(root)) return;
    const ghost = document.createElement('div');
    ghost.className = 'hx-composer__ghost';
    ghost.textContent = sent.text;
    const at = boxOf(field);
    const within = boxOf(root);
    ghost.style.setProperty('--ghost-x', `${at.left - within.left}px`);
    ghost.style.setProperty('--ghost-y', `${at.top - within.top}px`);
    ghost.style.setProperty('--ghost-w', `${at.width}px`);
    root.appendChild(ghost);
    // The empty field's hint waits until the words have gone, so the two do not overlap.
    root.dataset.lifting = 'true';
    const gone = () => {
      ghost.remove();
      delete root.dataset.lifting;
    };
    ghost
      .animate(
        [
          { opacity: 1, transform: 'none' },
          { opacity: 0, transform: `translateY(-${LIFT_PX}px)` },
        ],
        { duration: LIFT_MS, easing: 'ease-out' },
      )
      .finished.then(gone, gone);
  }, [value]);

  // A text that came back from the queue (the pencil) is not typed in: it comes down from the row it left.
  const arrived = useRef({ length: 0, files: 0 });
  useLayoutEffect(() => {
    const before = arrived.current;
    arrived.current = { length: value.length, files: attachments.length };
    const row = rowRef.current;
    if (!row || (value.length <= before.length && attachments.length <= before.files)) return;
    const from = claimEdit(value);
    if (!from || !canAnimate(row)) return;
    row.animate(
      [
        { opacity: 0, transform: `translateY(${from.top - boxOf(row).top}px)` },
        { opacity: 1, offset: 0.4 },
        { opacity: 1, transform: 'none' },
      ],
      { duration: FLIGHT_MS, easing: FLIGHT_EASE },
    );
  }, [value, attachments.length]);

  const { phase: stopPhase, retry: retryStop, reset: resetStop } = stop;
  useEffect(() => {
    if (stopPhase !== PENDING.failed) return;
    toast(t('next.composer.stopFailed'), {
      tone: 'error',
      action: { label: t('next.composer.retry'), onClick: retryStop },
    });
    resetStop();
  }, [stopPhase, retryStop, resetStop]);

  const tooLong = value.length > maxText;
  const failedFile = attachments.some((file) => file.status === 'error');
  const hasContent =
    props.ready === true ||
    value.trim().length > 0 ||
    (props.quotes?.length ?? 0) > 0 ||
    (props.refs?.length ?? 0) > 0 ||
    (props.notes?.length ?? 0) > 0 ||
    (props.noteGroups ?? []).some((group) => group.notes.length > 0) ||
    attachments.some((file) => file.status !== 'error');
  const canSend = hasContent && !tooLong && !failedFile;
  const working = mode === 'working';

  // What goes out joins the prompts of the field's history.
  const history = usePromptHistory({
    value,
    onChange,
    focusField: () => fieldRef.current?.focus(),
    enabled: !recording,
    slashPalette: !!props.slash,
  });
  const palette = useSlashPalette({
    value,
    onChange,
    fieldRef,
    picker,
    slash: props.slash ?? null,
    quick: history.quick,
    enabled: !recording,
  });
  const { remember: rememberPrompt } = history;
  const send = (sendMode: TSendMode) => {
    // While the voice is on, the typed words are said to it: it answers out loud (and in the feed).
    if (voice && value.trim()) {
      voice.typed(value.trim());
      onChange('');
      return;
    }
    rememberPrompt(value);
    const row = rowRef.current;
    // Where the text stands, for the bubble that grows out of it; a send that is refused leaves the text and the place unused.
    if (row) launchFrom(boxOf(row), value);
    sentRef.current = { text: value, at: Date.now() };
    onSubmit(sendMode);
  };
  const submit = (sendMode: TSendMode) => {
    if (canSend) send(sendMode);
  };
  const { sendRef } = props;
  useEffect(() => {
    if (!sendRef) return;
    sendRef.current = () => submit('now');
    return () => {
      sendRef.current = null;
    };
  });

  // The dictation counts every text it fixed in the field; «stop and send» goes once the next one is there. A recording
  // that ends otherwise (cancelled, failed, silent) sends nothing.
  const recognizedSeen = useRef(props.recognized ?? 0);
  useEffect(() => {
    const now = props.recognized ?? 0;
    if (now === recognizedSeen.current) return;
    recognizedSeen.current = now;
    if (sendAfter && canSend) send('now');
  });
  // Every recording starts without the wish to send (state adjusted during render, not in an effect).
  const [recordingNow, setRecordingNow] = useState(false);
  if (recordingNow !== !!recording) {
    setRecordingNow(!!recording);
    if (recording) setSendAfter(false);
  }

  const stopAndSend = () => {
    setSendAfter(true);
    props.onFinishRecording();
  };

  // The field's shortcuts (features/keys). A field out of sight takes none; of several on a screen the latest does.
  const onScreen = () => {
    const root = rootRef.current;
    return root !== null && root.isConnected && (root.checkVisibility?.() ?? true);
  };
  const plusRef = useRef<IPlusMenuHandle>(null);
  useKeys(KEY_SCOPE.composer, {
    'composer.dictate': () => {
      if (!onScreen() || !props.onMic) return false;
      if (!recording) onMicClick();
      else if (!recording.busy) props.onFinishRecording();
    },
    'composer.cancelDictation': () => {
      if (!onScreen() || !recording || recording.busy) return false;
      setSendAfter(false);
      props.onCancelRecording();
    },
    'composer.stopAgent': () => {
      // Nothing to stop, or text selected (⌃C may be the copy): the press is not ours.
      const selected = (window.getSelection()?.toString() ?? '') !== '';
      if (!onScreen() || !working || selected || isBusy(stop.phase)) return false;
      void stop.run('stop', props.onStop);
    },
    'composer.attach': () => {
      if (!onScreen() || props.quiet || recording) return false;
      plusRef.current?.openFiles();
    },
  });
  const dictateHint = useKeyHint('composer.dictate');
  const cancelHint = useKeyHint('composer.cancelDictation');
  const stopHint = useKeyHint('composer.stopAgent');

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (props.stash && isStashKey(event.nativeEvent)) {
      event.preventDefault();
      if (props.stash.canStash && !recording) props.stash.onStash();
      return;
    }
    if (palette.onKeyDown(event) || history.onKeyDown(event)) return;
    let action = keyAction(event.nativeEvent, touch, props.enterSends ?? true);
    // «After the turn» is the action of the shortcuts list (composer.send): whatever it is bound to queues, and ⌥↵ no
    // longer does once it is bound elsewhere.
    const chord = chordOf(event.nativeEvent);
    const overrides = keyOverrides();
    const queues =
      chord !== null &&
      !event.nativeEvent.isComposing &&
      commandsOfChord(chord, overrides).some((c) => c.id === 'composer.send');
    if (action === KEY_ACTION.queue && !queues && !isDefault('composer.send', overrides)) action = null;
    else if (queues && action !== KEY_ACTION.send) action = KEY_ACTION.queue;
    if (action === KEY_ACTION.send) {
      event.preventDefault();
      if (!recording) submit('now');
      else if (!recording.busy) stopAndSend();
    }
    if (action === KEY_ACTION.queue) {
      event.preventDefault();
      submit('queue');
    }
  };

  const { onFiles, plain } = props;
  const dropping = useFileDrop(onFiles, rootRef) && !plain;
  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = [...event.clipboardData.files];
    if (files.length === 0 || plain) return;
    event.preventDefault();
    // One paste is taken once, by the field it went to; the same picture twice in the clipboard (its file and its
    // pixels, or the same bytes twice) is one.
    if (pasted.has(event.nativeEvent)) return;
    pasted.add(event.nativeEvent);
    void distinctFiles(pastedFiles(files)).then(onFiles);
  };

  let fullPlaceholder = props.placeholder ?? t(PLACEHOLDER_KEYS[mode], { mod: QUEUE_MOD });
  if (bar) fullPlaceholder = t(bar.preparing ? 'next.voice.preparing' : 'next.voice.listening');
  const shortKey = SHORT_PLACEHOLDER_KEYS[mode];
  let placeholder =
    narrowField && !bar && props.placeholder === undefined && shortKey ? t(shortKey) : fullPlaceholder;
  // The voice's wave stands where the placeholder would.
  if (voice) placeholder = '';
  // A recording's own words (getting ready, listen) come at once: they tell a state, not a kind of chat.
  const hint = useHintSwap(placeholder, calm || !!bar || voice !== null);

  return (
    <div
      ref={rootRef}
      className="hx-composer"
      data-rec={bar ? 'true' : undefined}
      data-live={voice?.floor}
      data-mode={dropping ? 'drop' : undefined}
      data-quiet={props.quiet || undefined}
      data-chat-drop={props.acceptsChats || undefined}
    >
      {props.acceptsChats && (
        <div className="hx-drop hx-drop--chat" aria-hidden="true">
          <Icon name="message-square" />
          <span>{t('next.composer.dropChat')}</span>
        </div>
      )}
      {dropping && (
        <div className="hx-drop" aria-hidden="true">
          <Icon name="upload" />
          <span>{t('next.composer.dropHere')}</span>
        </div>
      )}
      {history.panel}
      {palette.panel}
      <div className="hx-composer__layer">
        <div ref={rowRef} className="hx-composer__row">
          {props.quotes?.map((quote) => (
            <span key={quote.id} className="hx-attach" data-kind="quote" title={quote.title ?? quote.label}>
              <span className="hx-attach__thumb">
                <Icon name={quote.icon ?? (quote.remark ? 'message-square-text' : 'text-quote')} size="sm" />
              </span>
              <span className="hx-attach__name">{quote.label}</span>
              <button
                type="button"
                className="hx-attach__x"
                aria-label={t('next.composer.removeAttachment', { name: quote.label })}
                onClick={() => props.onRemoveQuote?.(quote.id)}
              >
                <Icon name="x" size="sm" />
              </button>
            </span>
          ))}
          {props.refs && <RefChips refs={props.refs} onRemove={props.onRemoveRef} />}
          {props.noteGroups?.map((group) => (
            <NoteChips
              key={group.key}
              notes={group.notes}
              onEdit={group.onEdit}
              onRemove={group.onRemove}
              onOpen={group.onOpen}
            />
          ))}
          {props.notes && props.onEditNote && props.onRemoveNote && (
            <AnnotationChips notes={props.notes} onEdit={props.onEditNote} onRemove={props.onRemoveNote} />
          )}
          {attachments.map((file) => (
            <span
              key={file.id}
              className="hx-attach"
              data-kind={file.kind}
              data-status={file.status}
              data-big={(file.progress && file.progress.size >= BIG_UPLOAD_BYTES) || undefined}
              title={file.name}
            >
              <AttachmentThumb item={file} className="hx-attach__thumb" inField />
              <span className="hx-attach__text">
                <span className="hx-attach__name">{file.name}</span>
                {file.progress && file.progress.size >= BIG_UPLOAD_BYTES && (
                  <span className="hx-attach__meta">{formatProgress(file.progress)}</span>
                )}
              </span>
              {file.error && <span className="hx-attach__err">{file.error}</span>}
              <span
                className="hx-attach__bar"
                aria-hidden="true"
                data-real={file.progress ? 'true' : undefined}
                style={file.progress ? ({ '--p': percentOf(file.progress) } as CSSProperties) : undefined}
              />
              {file.status === 'error' && (
                <button
                  type="button"
                  className="hx-attach__x"
                  aria-label={t('next.composer.retryAttachment', { name: file.name })}
                  onClick={() => props.onRetryAttachment(file.id)}
                >
                  <Icon name="rotate-ccw" size="sm" />
                </button>
              )}
              <button
                type="button"
                className="hx-attach__x"
                aria-label={t('next.composer.removeAttachment', { name: file.name })}
                onClick={() => props.onRemoveAttachment(file.id)}
              >
                <Icon name="x" size="sm" />
              </button>
            </span>
          ))}
          <div className="hx-composer__field">
            <textarea
              ref={fieldRef}
              className="hx-composer__input"
              rows={1}
              disabled={props.quiet}
              placeholder={hint.text}
              data-hint={hint.out ? 'out' : undefined}
              title={placeholder === fullPlaceholder ? undefined : fullPlaceholder}
              aria-label={t('next.composer.label')}
              aria-invalid={tooLong || undefined}
              onChange={(event) => {
                ownCaret.current.start = event.target.selectionStart;
                ownCaret.current.end = event.target.selectionEnd;
                onChange(event.target.value);
                history.onInput(event.target.value);
                palette.sync(event.target.value, event.target.selectionStart);
              }}
              onSelect={(event) => {
                const field = event.currentTarget;
                palette.sync(field.value, field.selectionStart);
                ownCaret.current.start = field.selectionStart;
                ownCaret.current.end = field.selectionEnd;
              }}
              onKeyDown={(event) => {
                if (listening && event.key !== 'Enter' && event.key !== 'Escape')
                  ownCaret.current.moved = true;
                onKeyDown(event);
              }}
              onPointerDown={() => {
                if (listening) ownCaret.current.moved = true;
              }}
              onBlur={(event) => {
                blurCaret.current = {
                  value: event.currentTarget.value,
                  at: event.currentTarget.selectionStart,
                };
                if (rootRef.current?.contains(event.relatedTarget)) return;
                history.close();
                palette.close();
              }}
              onPaste={onPaste}
            />
            {bar && <DictationMirror text={value} ref={mirrorRef} />}
            {voice && (
              <div
                className="hx-composer__voice"
                data-floor={voice.floor}
                data-typed={value ? 'true' : undefined}
              >
                <VoiceHead
                  state={voice.interrupted ? HEAD.barged : headOf(voice.floor)}
                  kind={voice.head}
                  level={voice.level}
                />
                {!value && (voice.heard || voice.said) && (
                  <span className="hx-composer__heard" data-said={voice.heard ? undefined : 'true'}>
                    {lineTail(voice.heard || voice.said)}
                  </span>
                )}
                <RecordingWave
                  level={voice.level}
                  listening={voice.floor !== FIELD_FLOOR.thinking && voice.floor !== FIELD_FLOOR.starting}
                />
              </div>
            )}
          </div>
        </div>
        {tooLong && (
          <div className="hx-composer__limit" role="alert">
            {t('next.composer.tooLong', { n: value.length, max: maxText })}
          </div>
        )}

        {bar ? (
          <div className="hx-composer__bar hx-recbar" data-state={barState}>
            <RoundButton
              icon="x"
              label={t('next.composer.cancelRecording')}
              keys={cancelHint ? [cancelHint] : undefined}
              variant="ghost"
              size="sm"
              onClick={() => {
                setSendAfter(false);
                props.onCancelRecording();
              }}
            />
            <span className="hx-recbar__live">
              <span className="hx-recbar__dot" />
              {bar.busy ? (
                <span className="hx-recbar__time">{t('next.voice.recognizing')}</span>
              ) : (
                <RecordingTime startedAt={bar.startedAt} />
              )}
            </span>
            <RecordingWave level={bar.level} listening={!bar.busy} />
            <RoundButton
              icon="square"
              iconSize="sm"
              label={t('next.composer.finishRecording')}
              keys={['Esc']}
              className="hx-recbar__stop"
              busy={bar.busy && !sendAfter}
              disabled={bar.busy}
              onClick={props.onFinishRecording}
            />
            <RoundButton
              icon="arrow-up"
              variant="accent"
              label={t('next.composer.sendRecording')}
              keys={['↵']}
              busy={bar.busy && sendAfter}
              disabled={bar.busy}
              onClick={stopAndSend}
            />
          </div>
        ) : (
          <div className="hx-composer__bar" data-back={recorded || undefined}>
            {!plain && (
              <PlusMenu
                ref={plusRef}
                contexts={props.contexts ?? []}
                onFiles={props.onFiles}
                onContext={(id) => props.onContext?.(id)}
                onPrompts={history.show}
                attach={props.attach}
                stash={props.stash}
                takes={props.takes}
                apps={props.apps}
              />
            )}
            {shownPicker.value && (
              <Tuck tucked={tucked}>
                <span className="hx-composer__held" inert={shownPicker.held || undefined}>
                  <ModelPicker
                    {...shownPicker.value}
                    limits={shownLimits.value && !paysBySpend(shownLimits.value) ? shownLimits.value : null}
                  />
                </span>
              </Tuck>
            )}
            {/* The limit is the bars by the context ring and the pill popover's card (HRN-1496); a chip of its own
                only for a machine that pays by the API (the day's spend), or where no pill stands. */}
            {shownLimits.value && (!shownPicker.value || paysBySpend(shownLimits.value)) && (
              <Tuck tucked={tucked}>
                <LimitsChip view={shownLimits.value} chatUsd={props.chatCost} />
              </Tuck>
            )}
            <span className="hx-spacer" />
            {/* The chat's limits in the field itself (HRN-1496): the five hours and the week, quiet bars; on hover the
                card of every window (HRN-1552). */}
            {shownLimits.value && shownPicker.value && !paysBySpend(shownLimits.value) && (
              <Tuck tucked={tucked}>
                <LimitsMini view={shownLimits.value} model={shownPicker.value.value.model ?? null} />
              </Tuck>
            )}
            {shownRing.value && (
              <Tuck tucked={tucked}>
                <ContextRing {...shownRing.value} />
              </Tuck>
            )}
            {props.tools}
            {/* Dictation (the mic) always stays; the live voice is its own button beside it (the wave). Only during a
                call the call's own mic (mute) stands here instead, so the field never shows two mics. */}
            {props.onMic && !voice && (
              <RoundButton
                className="hx-dictate-btn"
                icon="mic"
                label={t('next.composer.voice')}
                keys={dictateHint ? [dictateHint] : undefined}
                onClick={onMicClick}
              />
            )}
            {/* While the agent works the field shows one button: stop with an empty field, the arrow with a message
                (↵ into the queue, ⌃↵ straight into its turn). */}
            {working && !hasContent && (
              <RoundButton
                icon="square"
                iconSize="sm"
                variant="accent"
                label={props.waiting ? t('next.composer.stopWaiting') : t('next.composer.stop')}
                keys={stopHint ? [stopHint] : undefined}
                busy={isBusy(stop.phase)}
                onClick={() => stop.run('stop', props.onStop)}
              />
            )}
            {(!working || hasContent) && (
              <RoundButton
                icon="arrow-up"
                variant="accent"
                label={
                  working
                    ? t('next.composer.queueTip', { mod: QUEUE_MOD })
                    : (props.sendLabel ?? t('next.composer.send'))
                }
                keys={['↵']}
                disabled={!canSend}
                onClick={() => submit('now')}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default Composer;
