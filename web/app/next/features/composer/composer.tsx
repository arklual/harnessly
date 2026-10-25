// The field under the feed (the design's .hx-composer): it grows with the text up to 40% of the screen, «+» attaches
// (a sheet on a phone), the pill shows the agent and model, the microphone dictates into the field, and the round
// button sends, or stops the agent while it works. Keys are in keys.ts; ↑ in an empty field opens the earlier prompts
// and the quick ones (prompt-history.tsx), «/» the commands, skills and settings of the agent (slash-palette.tsx). On a phone the dock rides over the keyboard.
import AttachmentThumb from '@/next/ui/attachment-thumb';
import ContextRing, { type IContextRingProps } from '@/next/features/composer/context-ring';
import {
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from 'react';
import { isStashKey, KEY_ACTION, keyAction } from '@/next/features/composer/keys';
import LimitsChip from '@/next/features/limits/limits-chip';
import type { LimitsView } from '@/next/features/limits/model';
import ModelPicker, { type IModelPickerProps } from '@/next/features/composer/model-picker';
import PlusMenu, { type IPlusAttach } from '@/next/features/composer/plus-menu';
import type { IStash } from '@/next/features/composer/stash-list';
import { RefChips, type IRefChip } from '@/next/features/chat/ref-chips';
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
import { boxOf, canAnimate, claimEdit, FLIGHT_EASE, FLIGHT_MS, launchFrom } from '@/next/ui/flight';
import Icon, { type TIconName } from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { useMedia } from '@/next/ui/use-media';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import { usePresence } from '@/next/ui/presence';
import { DictationMirror, RecordingTime, RecordingWave } from '@/next/features/composer/recording';
import '@/next/styles/ui.css';
import '@/next/styles/composer.css';

export type TSendMode = 'queue' | 'now';

const PLACEHOLDER_KEYS: Record<ComposerMode, string> = {
  new: 'next.composer.placeholderNew',
  idle: 'next.composer.placeholder',
  stopped: 'next.composer.placeholderStopped',
  working: 'next.composer.placeholderWorking',
  answer: 'next.composer.placeholderAnswer',
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
  /** The places attached to the message (a design, a document, a board, a file, an MR, a task): chips before the files. */
  refs?: readonly IRefChip[];
  onRemoveRef?: (ref: IRefChip) => void;
  /** What «+» can attach besides files; without it only files, prompts and contexts. */
  attach?: IPlusAttach | null;
  /** Drafts put aside («Отложить черновик», ⌘⇧S); without it the field has no such thing. */
  stash?: IStash | null;
  recording: Recording | null;
  /** Voice input; without it the microphone is not offered. */
  onMic?: () => void;
  /** What is left of the agent's subscription, by the pill; no chip until the machine has answered. */
  limits?: LimitsView | null;
  /** How full the agent's window is; no ring until it is known. */
  contextRing?: IContextRingProps | null;
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
}

/** The recording bar's exit, as long as hx-recbar-out in composer.css. */
const REC_EXIT_MS = 180;

export { RecordingTime, RecordingWave };

/** The text that left the field fades up and away over this long, while the bubble takes it over. */
const LIFT_MS = 200;
const LIFT_PX = 10;
/** The field eases to its new height as the text grows or goes (sent, cleared): ease-out, this long. */
const GROW_MS = 180;

/**
 * Sets the field's height from its text and eases it there from the height it shows now. A height animation still running
 * (the text grew a moment ago: dictation that ends with «send», a paste sent at once) is cancelled before the text is
 * measured: while it runs it holds the field at its in-between height, scrollHeight reads that height and an emptied
 * field would keep it. Returns the new animation, or null where the field jumps (no change, no motion, `animate` off).
 */
const fitHeight = (
  field: HTMLTextAreaElement,
  running: Animation | null,
  animate: boolean,
): Animation | null => {
  const was = field.offsetHeight;
  running?.cancel();
  field.style.height = 'auto';
  field.style.height = `${field.scrollHeight}px`;
  const target = field.offsetHeight;
  if (!animate || was <= 0 || Math.abs(target - was) <= 1 || !canAnimate(field)) return null;
  return field.animate([{ height: `${was}px` }, { height: `${target}px` }], {
    duration: GROW_MS,
    easing: FLIGHT_EASE,
  });
};

const Composer = (props: IComposerProps) => {
  const { value, onChange, onSubmit, mode, picker, attachments, recording, maxText } = props;
  const rootRef = useRef<HTMLDivElement>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const fieldRef = useRef<HTMLTextAreaElement | null>(null);
  const mirrorRef = useRef<HTMLDivElement | null>(null);
  /** What was sent from the field a moment ago: the text and where it stood, until the field shows it is empty. */
  const sentRef = useRef<{ text: string; at: number } | null>(null);
  const touch = useMedia('(pointer: coarse)');
  const stop = usePending<'stop'>();
  // The recording bar leaves with an animation and keeps what it showed meanwhile.
  const { value: bar, state: barState } = usePresence(recording, REC_EXIT_MS);
  // The controls come back with a soft entrance once a recording has ended; on the page's first draw they just stand.
  const [recorded, setRecorded] = useState(false);
  if (bar && !recorded) setRecorded(true);
  // «Stop and send»: the text is still being recognized; it goes when the recording is over and the field has it.
  const [sendAfter, setSendAfter] = useState(false);
  const [dropping, setDropping] = useState(false);
  useKeyboardInset(rootRef);
  useImperativeHandle(props.inputRef, () => fieldRef.current as HTMLTextAreaElement, []);

  // The field's height follows its text; CSS caps it at 40% of the screen and scrolls past that.
  // It grows by easing from the old height to the new, and a text that is set from outside (dictation, a message taken
  // back from the queue) shows its end: the caret stays in sight.
  // It shrinks the same way when the text leaves (into the queue, into the agent's turn, deleted): the measure never
  // reads a height an earlier animation still holds (fitHeight).
  const growRef = useRef<Animation | null>(null);
  useLayoutEffect(() => {
    const field = fieldRef.current;
    if (!field) return;
    growRef.current = fitHeight(field, growRef.current, true);
    if (field.selectionStart === field.value.length) field.scrollTop = field.scrollHeight;
  }, [value]);

  // The field can be laid out narrow first (a column that is still opening) and wide later: the text then wraps
  // otherwise, and the height computed for the old width is wrong. Recalculate on a width change too, once per frame.
  useEffect(() => {
    const field = fieldRef.current;
    if (!field || typeof ResizeObserver === 'undefined') return;
    let width = field.clientWidth;
    let frame = 0;
    // A width change mid-ease goes on easing to the height of the new width; otherwise the field just takes it.
    const fit = () => {
      frame = 0;
      const easing = growRef.current?.playState === 'running';
      growRef.current = fitHeight(field, growRef.current, easing);
    };
    const watcher = new ResizeObserver(() => {
      if (field.clientWidth === width) return;
      width = field.clientWidth;
      if (frame === 0) frame = requestAnimationFrame(fit);
    });
    watcher.observe(field);
    return () => {
      watcher.disconnect();
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, []);

  // While it listens the layer over the field shows the same text: the newest words are in view, the layer scrolls with
  // the field and wraps where the field's scrollbar leaves room.
  const listening = bar !== null;
  useLayoutEffect(() => {
    const input = fieldRef.current;
    const layer = mirrorRef.current;
    if (!listening || !input || !layer) return;
    input.scrollTop = input.scrollHeight;
    layer.style.setProperty('--hx-sb', `${input.offsetWidth - input.clientWidth}px`);
    layer.scrollTop = input.scrollTop;
  }, [listening, value]);
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
    value.trim().length > 0 ||
    (props.quotes?.length ?? 0) > 0 ||
    (props.refs?.length ?? 0) > 0 ||
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

  // The dictation counts every text it fixed in the field; «stop and send» goes once the next one is there. A recording
  // that ends otherwise (cancelled, failed, silent) sends nothing.
  const recognizedSeen = useRef(props.recognized ?? 0);
  useEffect(() => {
    const now = props.recognized ?? 0;
    if (now === recognizedSeen.current) return;
    recognizedSeen.current = now;
    if (sendAfter && canSend) send('queue');
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

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (props.stash && isStashKey(event.nativeEvent)) {
      event.preventDefault();
      if (props.stash.canStash && !recording) props.stash.onStash();
      return;
    }
    if (palette.onKeyDown(event) || history.onKeyDown(event)) return;
    const action = keyAction(event.nativeEvent, touch, props.enterSends ?? true);
    if (action === KEY_ACTION.send) {
      event.preventDefault();
      if (!recording) submit('queue');
      else if (!recording.busy) stopAndSend();
    }
    if (action === KEY_ACTION.sendNow) {
      event.preventDefault();
      submit('now');
    }
  };

  const { onFiles } = props;
  // Files dragged anywhere over the window go to the field (the design's dragDepth): enter and leave come in pairs
  // for every element crossed, so the depth says whether the drag is still over the page.
  useEffect(() => {
    let depth = 0;
    const carries = (event: globalThis.DragEvent) => !!event.dataTransfer?.types.includes('Files');
    const enter = (event: globalThis.DragEvent) => {
      if (!carries(event)) return;
      depth += 1;
      setDropping(true);
    };
    const over = (event: globalThis.DragEvent) => {
      if (carries(event)) event.preventDefault();
    };
    const leave = (event: globalThis.DragEvent) => {
      if (!carries(event)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDropping(false);
    };
    const drop = (event: globalThis.DragEvent) => {
      if (!carries(event)) return;
      event.preventDefault();
      depth = 0;
      setDropping(false);
      onFiles([...(event.dataTransfer?.files ?? [])]);
    };
    window.addEventListener('dragenter', enter);
    window.addEventListener('dragover', over);
    window.addEventListener('dragleave', leave);
    window.addEventListener('drop', drop);
    return () => {
      window.removeEventListener('dragenter', enter);
      window.removeEventListener('dragover', over);
      window.removeEventListener('dragleave', leave);
      window.removeEventListener('drop', drop);
    };
  }, [onFiles]);
  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = [...event.clipboardData.files];
    if (files.length === 0) return;
    event.preventDefault();
    onFiles(files);
  };

  return (
    <div
      ref={rootRef}
      className="hx-composer"
      data-rec={bar ? 'true' : undefined}
      data-mode={dropping ? 'drop' : undefined}
      data-quiet={props.quiet || undefined}
    >
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
          {attachments.map((file) => (
            <span
              key={file.id}
              className="hx-attach"
              data-kind={file.kind}
              data-status={file.status}
              title={file.name}
            >
              <AttachmentThumb item={file} className="hx-attach__thumb" />
              <span className="hx-attach__name">{file.name}</span>
              {file.error && <span className="hx-attach__err">{file.error}</span>}
              <span className="hx-attach__bar" aria-hidden="true" />
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
              value={value}
              placeholder={bar ? t('next.voice.listening') : (props.placeholder ?? t(PLACEHOLDER_KEYS[mode]))}
              aria-label={t('next.composer.label')}
              aria-invalid={tooLong || undefined}
              onChange={(event) => {
                onChange(event.target.value);
                history.onInput(event.target.value);
                palette.sync(event.target.value, event.target.selectionStart);
              }}
              onSelect={(event) =>
                palette.sync(event.currentTarget.value, event.currentTarget.selectionStart)
              }
              onKeyDown={onKeyDown}
              onBlur={(event) => {
                if (rootRef.current?.contains(event.relatedTarget)) return;
                history.close();
                palette.close();
              }}
              onPaste={onPaste}
            />
            {bar && <DictationMirror text={value} ref={mirrorRef} />}
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
            <PlusMenu
              contexts={props.contexts ?? []}
              onFiles={props.onFiles}
              onContext={(id) => props.onContext?.(id)}
              onPrompts={history.show}
              attach={props.attach}
              stash={props.stash}
            />
            {picker && <ModelPicker {...picker} />}
            {props.limits && <LimitsChip view={props.limits} />}
            <span className="hx-spacer" />
            {props.contextRing && <ContextRing {...props.contextRing} />}
            {props.tools}
            {props.onMic && <RoundButton icon="mic" label={t('next.composer.voice')} onClick={props.onMic} />}
            {/* While the agent works the field shows one button: stop with an empty field, the arrow with a message
                (↵ into the queue, ⌃↵ straight into its turn). */}
            {working && !hasContent && (
              <RoundButton
                icon="square"
                iconSize="sm"
                variant="accent"
                label={props.waiting ? t('next.composer.stopWaiting') : t('next.composer.stop')}
                keys={['⌃', 'C']}
                busy={isBusy(stop.phase)}
                onClick={() => stop.run('stop', props.onStop)}
              />
            )}
            {(!working || hasContent) && (
              <RoundButton
                icon="arrow-up"
                variant="accent"
                label={working ? t('next.composer.queueTip') : t('next.composer.send')}
                keys={['↵']}
                disabled={!canSend}
                onClick={() => submit('queue')}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default Composer;
