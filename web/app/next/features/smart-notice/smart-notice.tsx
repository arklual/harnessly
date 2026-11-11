// The smart notice: a black plate that shows up only while something waits for the person (a question of an agent,
// a permission, a plan, a one-time code) and for a few seconds when a turn ends or breaks. Folded it is one line —
// the chat and what waits — with plates peeking from under it while more waits; open it is a card where the thing is
// done in place: an option or words for a question, allow or deny, the code's cells. One black shape grows from a strip
// into the line and into the card and back: opening springs, closing eases out. Where it stands is the person's
// choice (stores/notices.ts); the middle of the bottom edge is over the field of the chat on screen. One
// implementation for every width: on the phone it is as wide as the screen less a margin.
import '@/next/styles/smart-notice.css';

import {
  memo,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type RefObject,
} from 'react';
import { t } from '@/next/i18n';
import CodeField, { CODE_FIELD, type TCodeFieldState } from '@/next/ui/code-field';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence, usePresentFlag } from '@/next/ui/presence';
import type { TNoticePlace, TNoticeSound } from '@/next/stores/notices';
import useGestures, { rise } from './use-gestures';
import { STACK_DEPTH } from './pager';
import { NOTICE_ACTION, NOTICE_INPUT, noteInput, noticeEvent } from './metrics';
import { RecordingTime, RecordingWave } from '@/next/features/composer/recording';
import type { Recording } from '@/next/features/composer/types';
import SlotBody, { sourceLabel, type ISlotNotice, type ISlotSource, type TSlotAnswer } from './slot-body';

export const SMART_KIND = {
  code: 'code',
  permission: 'permission',
  question: 'question',
  plan: 'plan',
  machine: 'machine',
  failed: 'failed',
  limit: 'limit',
  done: 'done',
  /** An app's or an agent's question through the open slot. */
  ask: 'ask',
  /** An app's or an agent's status or progress through the open slot. */
  info: 'info',
} as const;
export type TSmartKind = (typeof SMART_KIND)[keyof typeof SMART_KIND];

/** Kinds that wait for the person: they stay until done and cannot be taken away. */
export const WAITING_KINDS: ReadonlySet<TSmartKind> = new Set([
  SMART_KIND.code,
  SMART_KIND.permission,
  SMART_KIND.question,
  SMART_KIND.plan,
  SMART_KIND.ask,
]);

export interface ISmartItem {
  key: string;
  kind: TSmartKind;
  chatId: string | null;
  icon: string;
  /** The chat's title. */
  title: string;
  /** What waits, in a line. */
  text: string;
  /** When it first showed, by this page's clock. */
  at: number;
  /** A code's deadline by this page's clock; null without one. */
  until: number | null;
  /** A finished chat's short recap, when the machine made one. */
  summary?: string | null;
  /** Who asks through the open slot (an app or an agent): the label over the card. */
  source?: ISlotSource | null;
}

/** What the open card shows for its main item, read by the binding. */
export type TSmartDetail =
  | { k: 'plain' }
  | { k: 'loading' }
  | { k: 'question'; text: string; options: readonly string[] }
  | { k: 'permission'; command: string }
  | { k: 'code'; why: string; digits: number }
  | { k: 'machine'; text: string; level: number; heavy: readonly ISmartHeavy[] }
  /** A finished turn: the chat's short recap (when the machine made one) and a field to write on to the agent. */
  | { k: 'done'; summary: string | null; canReply: boolean }
  /** What an app or an agent asks through the open slot. */
  | { k: 'slot'; notice: ISlotNotice };

/** The words written in the card's field and the voice that dictates into them (the chat field's own dictation). */
export interface ISmartReply {
  text: string;
  setText: (text: string) => void;
  recording: Recording | null;
  /** Without it the microphone is not offered. */
  onMic?: () => void;
  onCancelRecording: () => void;
  onFinishRecording: () => void;
}

/** What weighs on a machine, in words: the agents together, or one program (a build, a browser). */
export interface ISmartHeavy {
  key: string;
  icon: string;
  name: string;
  text: string;
  /** It can be stopped from here (the platform's own services cannot). */
  stoppable: boolean;
}

/** What became of a sent code (features/safe/code-asks.tsx says the same). */
export const SMART_CODE = { ok: 'ok', bad: 'bad', gone: 'gone', failed: 'failed' } as const;
export type TSmartCode = (typeof SMART_CODE)[keyof typeof SMART_CODE];

export interface ISmartNoticeProps {
  /** Most urgent first; empty: nothing waits and the plate is gone. */
  items: readonly ISmartItem[];
  /** The key of the item open on top; null: the first. */
  mainKey: string | null;
  detail: TSmartDetail;
  open: boolean;
  /** The plate's place; the middle of the bottom edge stands at `anchor` (the field of the chat) when there is one. */
  place: TNoticePlace;
  anchor: { x: number; bottom: number } | null;
  /** Something opened from the field (the model's picker, the «+» menu) rises where the plate stands over it. */
  covered?: boolean;
  now: number;
  onOpen: (open: boolean) => void;
  onMain: (key: string) => void;
  onOpenChat: (item: ISmartItem) => void;
  onAnswer: (item: ISmartItem, answer: { choice: number } | { text: string }) => Promise<void>;
  onDecide: (item: ISmartItem, allow: boolean) => Promise<void>;
  onCode: (item: ISmartItem, code: string) => Promise<TSmartCode>;
  onCancelCode: (item: ISmartItem) => Promise<void>;
  /** Stops a heavy program of a machine's item. */
  onStop: (item: ISmartItem, heavyKey: string) => Promise<void>;
  /** Opens the machine's card with its figures. */
  onOpenMachine: (item: ISmartItem) => void;
  /** Takes the item off the plate (swept aside, «Скрыть», ⌫): the chat still waits, the centre keeps its row. */
  /** Put away: `from` is where the plate stood when it was put away by hand (null when thrown off by a gesture). */
  onHide: (item: ISmartItem, from: DOMRect | null) => void;
  /** «Закрыть все»: everything on the plate at once (what waits into its drops, news gone), with «Вернуть». */
  onCloseAll: () => void;
  onMute: (sound: TNoticeSound) => void;
  /** Answers a notice of the open slot; the machine checks it (a text that looks like a key comes back as secret). */
  onSlot: (notice: ISlotNotice, value: unknown) => Promise<TSlotAnswer>;
  /** The person turns a notice of the slot down (a code's «Отмена»). */
  onSlotDismiss: (notice: ISlotNotice) => Promise<void>;
  /** «Не показывать от …»: the person turns the item's source off. */
  onMuteSource: (item: ISmartItem) => void;
  onSettings: () => void;
  /** A plate is new for this long: it tells what came in two lines with its main button. */
  announceMs: number;
  /** Where the focus goes back when the card closes from the keyboard. */
  returnFocus: () => void;
  /** The card opened from the keyboard: its first action takes the focus. */
  focusOnOpen: boolean;
  /** The pointer or the focus is on the plate: news stays while it is read. */
  onHold?: (held: boolean) => void;
  /** The card is flowing out of a drop: the flight draws it until it lands. */
  arriving?: boolean;
  /** The card's field (a question's own answer, a word to a finished chat) and its dictation. */
  reply: ISmartReply;
  /** Words to the agent of a finished chat: sent there as the person's message. */
  onReply: (item: ISmartItem, text: string) => Promise<void>;
}

/** How long the plate takes to leave when nothing waits any more, ms (--sn-exit-ms in styles/smart-notice.css). */
export const SMART_EXIT_MS = 260;
/** How long the card takes to fold back into the line: the shape settles then, it does not bounce (--sn-fold-ms). */
const FOLD_MS = 600;
/** The content the shape held before keeps fading this long over the new one (hx-sn-out). */
const GHOST_MS = 240;
/** A sent answer shows its check this long at most; the plate leaves sooner, once the agent takes the answer. */
const SENT_HOLD_MS = 6000;

const ACTION: Record<TSmartKind, string> = {
  code: 'next.smart.enterCode',
  permission: 'next.smart.answer',
  question: 'next.smart.answer',
  plan: 'next.smart.answer',
  machine: 'next.smart.machine.look',
  failed: 'next.smart.open',
  limit: 'next.smart.open',
  done: 'next.smart.open',
  ask: 'next.smart.answer',
  info: 'next.smart.look',
};

/** The sound a kind rings with, so the menu can silence it; none for those that ring no sound of their own. */
const SOUND_OF: Record<TSmartKind, TNoticeSound | null> = {
  code: 'urgent',
  permission: 'urgent',
  machine: 'urgent',
  question: 'wait',
  plan: 'wait',
  done: 'done',
  failed: null,
  limit: null,
  ask: 'wait',
  info: null,
};
const MENU_EXIT_MS = 160;
const SENT_TEXT: Record<TSmartKind, string> = {
  code: 'next.smart.sent',
  permission: 'next.smart.sent',
  question: 'next.smart.sent',
  plan: 'next.smart.sent',
  machine: 'next.smart.machine.stopped',
  failed: 'next.smart.sent',
  limit: 'next.smart.sent',
  done: 'next.smart.sent',
  ask: 'next.smart.sent',
  info: 'next.smart.sent',
};

const RING_R = 16.5;
const RING_LEN = 2 * Math.PI * RING_R;

/** The kind's icon in a round chip; a code has a ring around it that runs down to its deadline. */
export const Chip = ({
  item,
  now,
  large,
  sent,
}: {
  item: ISmartItem;
  now: number;
  large?: boolean;
  sent: boolean;
}) => {
  const left =
    item.until === null ? null : Math.max(0, Math.min(1, (item.until - now) / (item.until - item.at)));
  return (
    <span className="hx-sn__chip" data-large={large || undefined} data-sent={sent || undefined}>
      <Icon name={sent ? 'check' : item.icon} size="sm" />
      {left !== null && !sent && (
        <svg className="hx-sn__ring" viewBox="0 0 36 36" aria-hidden="true">
          <circle className="hx-sn__ring-bg" cx="18" cy="18" r={RING_R} />
          <circle
            className="hx-sn__ring-fg"
            cx="18"
            cy="18"
            r={RING_R}
            strokeDasharray={RING_LEN}
            strokeDashoffset={RING_LEN * (1 - left)}
          />
        </svg>
      )}
    </span>
  );
};

const ago = (now: number, at: number): string => {
  const min = Math.floor((now - at) / 60_000);
  return min < 1 ? t('next.smart.now') : t('next.smart.minutes', { n: min });
};

/** What was sent leaves the field upwards, as a message going: a copy of its words flies off and fades. */
const flyAway = (field: HTMLInputElement | null) => {
  if (!field || field.value.trim() === '') return;
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
  const r = field.getBoundingClientRect();
  const style = getComputedStyle(field);
  const words = document.createElement('span');
  words.className = 'hx-sn-sent';
  words.textContent = field.value;
  Object.assign(words.style, {
    left: `${r.left + parseFloat(style.paddingLeft)}px`,
    top: `${r.top}px`,
    height: `${r.height}px`,
    maxWidth: `${r.width}px`,
    font: style.font,
  });
  document.body.append(words);
  const run = words.animate(
    [
      { translate: '0 0', opacity: 1, scale: '1' },
      { translate: '0 -36px', opacity: 0, scale: '0.92' },
    ],
    { duration: 420, easing: 'cubic-bezier(0.2, 0.7, 0.3, 1)', fill: 'forwards' },
  );
  void run.finished.catch(() => undefined).then(() => words.remove());
};

/** The card's field: words, or the voice, to the agent; Enter sends. While it listens it is the recording's bar. */
const ReplyField = ({
  reply,
  busy,
  placeholder,
  onSend,
  check,
}: {
  reply: ISmartReply;
  busy: boolean;
  placeholder: string;
  onSend: (text: string) => void;
  /** Stops a text before it is sent (it stays in the field). */
  check?: (text: string) => boolean;
}) => {
  const input = useRef<HTMLInputElement | null>(null);
  const rec = reply.recording;
  if (rec)
    return (
      <div className="hx-sn__field hx-sn__reply" data-rec="">
        <button
          type="button"
          className="hx-sn__round"
          aria-label={t('next.composer.cancelRecording')}
          onClick={reply.onCancelRecording}
        >
          <Icon name="x" size="sm" />
        </button>
        <span className="hx-sn__rec">
          {rec.busy ? t('next.voice.recognizing') : <RecordingTime startedAt={rec.startedAt} />}
        </span>
        <RecordingWave level={rec.level} listening={!rec.busy} />
        <button
          type="button"
          className="hx-sn__send"
          disabled={rec.busy}
          aria-label={t('next.composer.finishRecording')}
          onClick={reply.onFinishRecording}
        >
          <Icon name="square" size="sm" />
        </button>
      </div>
    );
  return (
    <form
      className="hx-sn__field hx-sn__reply"
      onSubmit={(event) => {
        event.preventDefault();
        const text = reply.text.trim();
        if (!text) return;
        if (check && !check(text)) return;
        flyAway(input.current);
        onSend(text);
        reply.setText('');
      }}
    >
      <input
        ref={input}
        className="hx-sn__input"
        value={reply.text}
        placeholder={placeholder}
        aria-label={placeholder}
        disabled={busy}
        autoComplete="off"
        enterKeyHint="send"
        onChange={(event) => reply.setText(event.target.value)}
      />
      {reply.onMic && reply.text.trim() === '' && (
        <button
          type="button"
          className="hx-sn__round"
          disabled={busy}
          aria-label={t('next.composer.voice')}
          onClick={reply.onMic}
        >
          <Icon name="mic" size="sm" />
        </button>
      )}
      <button
        type="submit"
        className="hx-sn__send"
        disabled={busy || reply.text.trim() === ''}
        aria-label={t('next.smart.send')}
      >
        <Icon name="arrow-up" size="sm" />
      </button>
    </form>
  );
};

/** The open card's body for its main item. */
const Body = ({
  item,
  detail,
  busy,
  run,
  onAnswer,
  onDecide,
  onCode,
  onCancelCode,
  onStop,
  onReply,
  onSlot,
  onSlotDismiss,
  reply,
}: {
  item: ISmartItem;
  detail: TSmartDetail;
  busy: boolean;
  run: (work: () => Promise<void>) => void;
  reply: ISmartReply;
} & Pick<
  ISmartNoticeProps,
  'onAnswer' | 'onDecide' | 'onCode' | 'onCancelCode' | 'onStop' | 'onReply' | 'onSlot' | 'onSlotDismiss'
>) => {
  const [code, setCode] = useState('');
  const [codeState, setCodeState] = useState<TCodeFieldState>(CODE_FIELD.idle);
  const [note, setNote] = useState<string | null>(null);

  if (detail.k === 'loading') return <div className="hx-sn__wait" aria-busy="true" />;
  if (detail.k === 'slot')
    return (
      <SlotBody
        notice={detail.notice}
        busy={busy}
        run={run}
        onSlot={onSlot}
        onDismiss={onSlotDismiss}
        field={(placeholder, onSend, check) => (
          <ReplyField reply={reply} busy={busy} placeholder={placeholder} onSend={onSend} check={check} />
        )}
      />
    );
  if (detail.k === 'question')
    return (
      <>
        <p className="hx-sn__q">{detail.text}</p>
        <div className="hx-sn__opts">
          {detail.options.map((label, i) => (
            <button
              key={label}
              type="button"
              className="hx-sn__opt"
              data-sn-action
              disabled={busy}
              onClick={() => run(() => onAnswer(item, { choice: i }))}
            >
              {i < 9 && <kbd className="hx-sn__kbd">{i + 1}</kbd>}
              {label}
            </button>
          ))}
        </div>
        <ReplyField
          reply={reply}
          busy={busy}
          placeholder={t('next.smart.ownAnswer')}
          onSend={(text) => run(() => onAnswer(item, { text }))}
        />
      </>
    );
  if (detail.k === 'permission')
    return (
      <>
        <pre className="hx-sn__cmd">{detail.command}</pre>
        <div className="hx-sn__row">
          <button
            type="button"
            className="hx-sn__btn"
            data-sn-action
            disabled={busy}
            onClick={() => run(() => onDecide(item, true))}
          >
            {t('next.smart.allow')}
          </button>
          <button
            type="button"
            className="hx-sn__btn"
            data-tone="soft"
            disabled={busy}
            onClick={() => run(() => onDecide(item, false))}
          >
            {t('next.smart.deny')}
          </button>
        </div>
      </>
    );
  if (detail.k === 'code')
    return (
      <>
        <p className="hx-sn__why">{detail.why}</p>
        <div className="hx-sn__code">
          <CodeField
            fromMessages={false}
            value={code}
            onChange={(value) => {
              setCode(value);
              if (codeState === CODE_FIELD.invalid) setCodeState(CODE_FIELD.idle);
            }}
            onComplete={(value) => {
              setCodeState(CODE_FIELD.busy);
              setNote(null);
              void onCode(item, value).then((sent) => {
                if (sent === SMART_CODE.ok) {
                  setCodeState(CODE_FIELD.done);
                  return;
                }
                setCode('');
                setCodeState(sent === SMART_CODE.bad ? CODE_FIELD.invalid : CODE_FIELD.idle);
                if (sent === SMART_CODE.gone) setNote(t('next.safe.code.gone'));
                if (sent === SMART_CODE.failed) setNote(t('next.safe.code.failed'));
              });
            }}
            length={detail.digits}
            label={t('next.smart.code', { title: item.title })}
            state={codeState}
          />
        </div>
        {note && <p className="hx-sn__note">{note}</p>}
        <div className="hx-sn__row">
          <button
            type="button"
            className="hx-sn__btn"
            data-tone="soft"
            disabled={busy}
            onClick={() => run(() => onCancelCode(item))}
          >
            {t('next.smart.cancel')}
          </button>
        </div>
      </>
    );
  if (detail.k === 'machine')
    return (
      <>
        <p className="hx-sn__q">{detail.text}</p>
        <progress className="hx-sn__tone" value={detail.level} max={1} aria-label={detail.text} />
        {detail.heavy.length > 0 && (
          <>
            <p className="hx-sn__why">{t('next.smart.machine.heaviest')}</p>
            <ul className="hx-sn__heavy">
              {detail.heavy.map((heavy) => (
                <li key={heavy.key}>
                  <span className="hx-sn__chip">
                    <Icon name={heavy.icon} size="sm" />
                  </span>
                  <b className="hx-sn__title">{heavy.name}</b>
                  <span className="hx-sn__text">{heavy.text}</span>
                  {heavy.stoppable && (
                    <button
                      type="button"
                      className="hx-sn__btn"
                      data-tone="soft"
                      disabled={busy}
                      onClick={() => run(() => onStop(item, heavy.key))}
                    >
                      {t('next.smart.machine.stop')}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </>
    );
  if (detail.k === 'done')
    return (
      <>
        <p className="hx-sn__summary">{detail.summary ?? item.text}</p>
        {detail.canReply && (
          <ReplyField
            reply={reply}
            busy={busy}
            placeholder={t('next.smart.reply')}
            onSend={(text) => run(() => onReply(item, text))}
          />
        )}
      </>
    );
  return <p className="hx-sn__why">{item.text}</p>;
};

/** Keeps --sn-w and --sn-h at the size of what the shape holds, so the one shape (and the plates under it) grows and
 *  shrinks to it. */
const useFitShape = (
  frame: RefObject<HTMLDivElement | null>,
  body: RefObject<HTMLDivElement | null>,
  key: string,
) => {
  useLayoutEffect(() => {
    const host = frame.current;
    const inner = body.current;
    if (!host || !inner) return;
    const fit = () => {
      host.style.setProperty('--sn-w', `${inner.offsetWidth}px`);
      host.style.setProperty('--sn-h', `${inner.offsetHeight}px`);
    };
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(inner);
    return () => observer.disconnect();
  }, [frame, body, key]);
};

/**
 * When the shape's content changes, the old content does not vanish: its own nodes, which React has just let go of,
 * are put back into a layer of ghosts that React never renders into, fade there into a blur and are removed.
 */
const useGhost = (
  ghosts: RefObject<HTMLDivElement | null>,
  body: RefObject<HTMLDivElement | null>,
  key: string,
) => {
  const last = useRef<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    const before = last.current;
    const now = body.current;
    last.current = now;
    const layer = ghosts.current;
    if (!before || before === now || before.isConnected || !layer) return;
    before.dataset.ghost = '';
    before.setAttribute('aria-hidden', 'true');
    before.inert = true;
    layer.append(before);
    window.setTimeout(() => before.remove(), GHOST_MS);
  }, [ghosts, body, key]);
};

/** What a plate of the stack shows while paging brings it to the front (pager.ts fades it in): the line it will be. */
const Peek = memo(({ item, now, sent }: { item: ISmartItem; now: number; sent: boolean }) => (
  <span className="hx-sn__peek" aria-hidden="true">
    <span className="hx-sn__pill">
      <Chip item={item} now={now} sent={sent} />
      <b className="hx-sn__title">{item.title}</b>
      <span className="hx-sn__text">{sent ? t(SENT_TEXT[item.kind]) : item.text}</span>
    </span>
  </span>
));
Peek.displayName = 'Peek';

const SmartNotice = ({
  items,
  mainKey,
  detail,
  open,
  place,
  anchor,
  covered = false,
  now,
  onOpen,
  onMain,
  onOpenChat,
  onAnswer,
  onDecide,
  onCode,
  onCancelCode,
  onStop,
  onOpenMachine,
  onHide,
  onCloseAll,
  onMute,
  onSlot,
  onSlotDismiss,
  onMuteSource,
  onSettings,
  announceMs,
  returnFocus,
  focusOnOpen,
  onHold,
  arriving = false,
  reply,
  onReply,
}: ISmartNoticeProps) => {
  const main = items.find((item) => item.key === mainKey) ?? items[0] ?? null;
  // The last item stays drawn while the plate leaves.
  const present = usePresence(main, SMART_EXIT_MS);
  const shown = present.value;
  const showing = shown !== null;
  const [entered, setEntered] = useState(false);
  const opened = usePresentFlag(open, FOLD_MS);
  const folding = opened.mounted && opened.state === PRESENCE.closing;
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<ReadonlySet<string>>(new Set());
  /** Announcements folded into the line by hand (a pull back to the edge): they do not come back as announcements. */
  const [quiet, setQuiet] = useState<ReadonlySet<string>>(new Set());
  const host = useRef<HTMLDivElement | null>(null);
  const shape = useRef<HTMLDivElement | null>(null);
  const body = useRef<HTMLDivElement | null>(null);
  const ghosts = useRef<HTMLDivElement | null>(null);
  const [menuOn, setMenuOn] = useState(false);
  const menu = usePresentFlag(menuOn && showing, MENU_EXIT_MS);
  const menuEl = useRef<HTMLDivElement | null>(null);

  const folded = !open || shown === null;
  const announcing = folded && shown !== null && !quiet.has(shown.key) && now - shown.at < announceMs;
  // The stack goes round from the front plate: the one after it is first behind, the one paged away goes to the back.
  const front = shown ? items.findIndex((item) => item.key === shown.key) : -1;
  const others = front < 0 ? [] : [...items.slice(front + 1), ...items.slice(0, front)];
  useFitShape(host, body, `${shown?.key ?? ''}:${open}:${announcing}:${detail.k}:${others.length}`);
  const bodyKey = folded ? `f:${announcing}:${shown?.key ?? ''}` : `o:${shown?.key ?? ''}`;
  useGhost(ghosts, body, bodyKey);

  const step = (dir: 1 | -1) => {
    if (!shown) return;
    const at = items.findIndex((item) => item.key === shown.key);
    const next = items[(at + dir + items.length) % items.length];
    if (next && next.key !== shown.key) {
      onMain(next.key);
      noticeEvent(NOTICE_ACTION.paged, { kind: next.kind, stack: items.length, place });
    }
  };
  const hide = (thrown = false) => {
    if (!shown) return;
    setMenuOn(false);
    onOpen(false);
    const from = thrown ? null : (body.current?.getBoundingClientRect() ?? null);
    // By hand, the plate goes as the shape flows away: the next one rises, the last one goes out at once.
    if (!thrown && others.length > 0) rise(host.current, 1);
    if (!thrown && others.length === 0 && host.current) host.current.dataset.gone = '';
    noticeEvent(WAITING_KINDS.has(shown.kind) ? NOTICE_ACTION.dropped : NOTICE_ACTION.closed, {
      kind: shown.kind,
      stack: items.length,
      place,
      ms: Date.now() - shown.at,
    });
    onHide(shown, from);
  };
  const gestures = useGestures({
    host,
    present: showing,
    frontKey: shown?.key ?? null,
    edge: place.startsWith('top') ? 'top' : 'bottom',
    open,
    off: busy,
    stacked: others.length > 0,
    onHide: () => hide(true),
    onOpen: () => onOpen(true),
    onFold: () => {
      if (open) onOpen(false);
      else if (shown) setQuiet((was) => new Set(was).add(shown.key));
    },
    onMenu: () => setMenuOn(true),
    onStep: step,
  });

  const openChat = (item: ISmartItem) => {
    noticeEvent(NOTICE_ACTION.chatOpened, { kind: item.kind, stack: items.length, place });
    onOpenChat(item);
  };

  // The metrics: each item once when it comes on the plate, the card each time it opens.
  const seenKeys = useRef<Set<string> | null>(null);
  useEffect(() => {
    const seen = (seenKeys.current ??= new Set());
    for (const item of items) {
      if (seen.has(item.key)) continue;
      seen.add(item.key);
      noticeEvent(NOTICE_ACTION.shown, { kind: item.kind, stack: items.length, place });
    }
  }, [items, place]);
  // Once per opening, not per item chosen inside the open card.
  const wasOpen = useRef(false);
  useEffect(() => {
    const on = open && shown !== null;
    if (on && !wasOpen.current && shown)
      noticeEvent(NOTICE_ACTION.opened, { kind: shown.kind, ms: Date.now() - shown.at, place });
    wasOpen.current = on;
  });

  // The menu takes the focus when it opens; a press outside it puts it away.
  useEffect(() => {
    if (!menuOn) return;
    const frame = requestAnimationFrame(() =>
      menuEl.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus(),
    );
    const away = (event: PointerEvent) => {
      if (menuEl.current && !menuEl.current.contains(event.target as Node)) setMenuOn(false);
    };
    document.addEventListener('pointerdown', away, true);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener('pointerdown', away, true);
    };
  }, [menuOn]);

  // A new plate grows out of a dot once the dot has been drawn (two frames: the first one only lays it out); a plate
  // that leaves goes back into it.
  useEffect(() => {
    if (!showing) return;
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => setEntered(true));
    });
    return () => {
      cancelAnimationFrame(frame);
      setEntered(false);
    };
  }, [showing]);

  // A plate thrown off as the last one leaves still thrown; if another comes before it is gone, it comes back.
  const shownKey = shown?.key ?? null;
  useEffect(() => {
    const el = host.current;
    if (el && shownKey !== null && el.dataset.fling !== undefined) rise(el, 1);
    if (el && shownKey !== null) delete el.dataset.gone;
  }, [shownKey]);

  // While the plate is under the pointer or holds the focus, news does not go away; it lets go when it leaves.
  const holding = useRef(false);
  const hold = (on: boolean) => {
    if (holding.current === on) return;
    holding.current = on;
    onHold?.(on);
  };
  useEffect(
    () => () => {
      if (holding.current) onHold?.(false);
    },
    [onHold],
  );

  // The middle of the bottom edge follows the field of the chat.
  useLayoutEffect(() => {
    const el = host.current;
    if (!el) return;
    if (anchor) {
      el.style.setProperty('--sn-x', `${anchor.x}px`);
      el.style.setProperty('--sn-bottom', `${anchor.bottom}px`);
    } else {
      el.style.removeProperty('--sn-x');
      el.style.removeProperty('--sn-bottom');
    }
  }, [anchor, showing]);

  // Opened from the keyboard, the card's first action takes the focus.
  useEffect(() => {
    if (!open || !focusOnOpen) return;
    const frame = requestAnimationFrame(() =>
      shape.current?.querySelector<HTMLElement>('[data-sn-action], input, button')?.focus(),
    );
    return () => cancelAnimationFrame(frame);
  }, [open, focusOnOpen, shownKey, detail.k]);

  // A press outside the open card folds it.
  useEffect(() => {
    if (!open) return;
    const away = (event: PointerEvent) => {
      if (host.current && !host.current.contains(event.target as Node)) onOpen(false);
    };
    document.addEventListener('pointerdown', away, true);
    return () => document.removeEventListener('pointerdown', away, true);
  }, [open, onOpen]);

  if (shown === null) return null;

  const run = (work: () => Promise<void>) => {
    setBusy(true);
    const item = shown;
    void work()
      .then(() => {
        noticeEvent(NOTICE_ACTION.answered, {
          kind: item.kind,
          stack: items.length,
          place,
          ms: Date.now() - item.at,
        });
        const key = shown.key;
        setSent((was) => new Set(was).add(key));
        onOpen(false);
        setTimeout(
          () =>
            setSent((was) => {
              const next = new Set(was);
              next.delete(key);
              return next;
            }),
          SENT_HOLD_MS,
        );
      })
      .catch(() => undefined)
      .finally(() => setBusy(false));
  };

  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    noteInput(NOTICE_INPUT.keys);
    if (event.key === 'Escape' && menuOn) {
      event.preventDefault();
      setMenuOn(false);
      shape.current?.querySelector<HTMLElement>('button')?.focus();
      return;
    }
    if (event.key === 'Escape' && open) {
      event.preventDefault();
      onOpen(false);
      returnFocus();
      return;
    }
    if (busy) return;
    const target = event.target as HTMLElement;
    if (target.closest('input, textarea, .hx-otp')) return;
    if (menuOn && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      const all = [...(menuEl.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];
      const at = all.indexOf(target);
      all[(at + (event.key === 'ArrowDown' ? 1 : -1) + all.length) % all.length]?.focus();
      event.preventDefault();
      return;
    }
    if (event.key === 'ContextMenu' || (event.key === 'F10' && event.shiftKey)) {
      event.preventDefault();
      setMenuOn(true);
      return;
    }
    if ((event.key === 'Delete' || event.key === 'Backspace') && event.shiftKey) {
      event.preventDefault();
      onCloseAll();
      returnFocus();
      return;
    }
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      hide();
      returnFocus();
      return;
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const dir = event.key === 'ArrowDown' ? 1 : -1;
      if (!open && others.length > 0) gestures.page(dir);
      else step(dir);
      return;
    }
    if (!open || detail.k !== 'question') return;
    const digit = Number(event.key);
    if (Number.isInteger(digit) && digit >= 1 && digit <= Math.min(9, detail.options.length)) {
      event.preventDefault();
      run(() => onAnswer(shown, { choice: digit - 1 }));
    }
  };

  const act = (work: () => void) => () => {
    setMenuOn(false);
    work();
  };
  const sound = SOUND_OF[shown.kind];

  const isSent = sent.has(shown.key);
  const v = place.startsWith('top') ? 'top' : 'bottom';
  const anchored = anchor !== null && place === 'bottom-center';
  const h = place.split('-')[1];

  return (
    <div
      ref={host}
      className="hx-sn"
      data-v={v}
      data-h={h}
      data-anchored={anchored || undefined}
      data-covered={(anchored && covered) || undefined}
      data-phase={entered && present.state === PRESENCE.open ? 'in' : 'out'}
      data-open={open || undefined}
      data-folding={folding || undefined}
      data-arrive={arriving || undefined}
      role="region"
      aria-label={t('next.smart.region')}
      aria-keyshortcuts="Meta+J Control+J"
      onKeyDown={onKey}
      onPointerDown={gestures.onPointerDown}
      onPointerMove={gestures.onPointerMove}
      onPointerUp={gestures.onPointerUp}
      onPointerCancel={gestures.onPointerCancel}
      onClickCapture={gestures.onClickCapture}
      onContextMenu={gestures.onContextMenu}
      onPointerEnter={() => hold(true)}
      onPointerLeave={(event) => {
        if (event.pointerType === 'mouse') hold(false);
      }}
      onFocus={() => hold(true)}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) hold(false);
      }}
    >
      {menu.mounted && (
        <div
          ref={menuEl}
          className="hx-sn__menu"
          role="menu"
          aria-label={shown.title}
          data-state={menu.state === PRESENCE.closing ? 'closing' : 'open'}
        >
          {shown.chatId !== null && (
            <button
              type="button"
              role="menuitem"
              className="hx-sn__item"
              onClick={act(() => openChat(shown))}
            >
              <Icon name="external-link" size="sm" />
              {t('next.smart.openChat')}
            </button>
          )}
          {shown.kind === SMART_KIND.machine && (
            <button
              type="button"
              role="menuitem"
              className="hx-sn__item"
              onClick={act(() => onOpenMachine(shown))}
            >
              <Icon name="gauge" size="sm" />
              {t('next.smart.machine.open')}
            </button>
          )}
          <button type="button" role="menuitem" className="hx-sn__item" onClick={() => hide()}>
            <Icon name="eye-off" size="sm" />
            {t('next.smart.menu.hide')}
          </button>
          {shown.source && (
            <button
              type="button"
              role="menuitem"
              className="hx-sn__item"
              onClick={act(() => onMuteSource(shown))}
            >
              <Icon name="bell-off" size="sm" />
              {t('next.smart.slot.muteSource', { name: shown.source.name })}
            </button>
          )}
          {sound !== null && (
            <button type="button" role="menuitem" className="hx-sn__item" onClick={act(() => onMute(sound))}>
              <Icon name="bell-off" size="sm" />
              {t(`next.smart.menu.mute.${sound}`)}
            </button>
          )}
          <button type="button" role="menuitem" className="hx-sn__item" onClick={act(onSettings)}>
            <Icon name="settings" size="sm" />
            {t('next.smart.menu.settings')}
          </button>
        </div>
      )}
      <div className="hx-sn__lift">
        {others.slice(0, STACK_DEPTH).map((item, i) => (
          <span
            key={item.key}
            className="hx-sn__plate"
            data-depth={i + 1}
            aria-hidden="true"
            onClick={() => {
              for (let n = 0; n <= i; n++) gestures.page(1);
            }}
          >
            <i className="hx-sn__tint" />
            {i === 0 && folded && <Peek item={item} now={now} sent={sent.has(item.key)} />}
          </span>
        ))}
        {/* Unseen at rest: the one that comes forward from behind the stack, and the one before the front plate,
            which a page back brings in from the edge (pager.ts). An <i>, so span:last-of-type stays the last seen. */}
        {others.length > STACK_DEPTH && (
          <i
            key={others[STACK_DEPTH].key}
            className="hx-sn__plate"
            data-depth={STACK_DEPTH + 1}
            aria-hidden="true"
          >
            <i className="hx-sn__tint" />
          </i>
        )}
        {others.length > 0 && (
          <i
            key={`before:${others[others.length - 1].key}`}
            className="hx-sn__plate"
            data-depth={-1}
            aria-hidden="true"
          >
            <i className="hx-sn__tint" />
            {folded && (
              <Peek
                item={others[others.length - 1]}
                now={now}
                sent={sent.has(others[others.length - 1].key)}
              />
            )}
          </i>
        )}
        <div ref={shape} className="hx-sn__shape">
          <i className="hx-sn__tint" aria-hidden="true" />
          <div ref={ghosts} className="hx-sn__ghosts" aria-hidden="true" />
          <div ref={body} className="hx-sn__body" key={bodyKey}>
            {folded && !announcing && (
              <button
                type="button"
                className="hx-sn__pill"
                aria-expanded={false}
                onClick={() => onOpen(true)}
              >
                <Chip item={shown} now={now} sent={isSent} />
                <b className="hx-sn__title">{shown.title}</b>
                <span className="hx-sn__text">{isSent ? t(SENT_TEXT[shown.kind]) : shown.text}</span>
              </button>
            )}
            {folded && announcing && (
              <div className="hx-sn__ann">
                <button
                  type="button"
                  className="hx-sn__ann-main"
                  aria-expanded={false}
                  onClick={() => onOpen(true)}
                >
                  <Chip item={shown} now={now} large sent={isSent} />
                  <span className="hx-sn__lines">
                    <b className="hx-sn__title">{shown.title}</b>
                    <span className="hx-sn__text">{shown.text}</span>
                  </span>
                </button>
                {(WAITING_KINDS.has(shown.kind) || shown.kind === SMART_KIND.machine) && (
                  <button type="button" className="hx-sn__btn" onClick={() => onOpen(true)}>
                    {t(ACTION[shown.kind])}
                  </button>
                )}
              </div>
            )}
            {!folded && (
              <div className="hx-sn__card" role="dialog" aria-label={shown.title}>
                {shown.source && <p className="hx-sn__src">{sourceLabel(shown.source)}</p>}
                <div className="hx-sn__head">
                  <Chip item={shown} now={now} large sent={isSent} />
                  <b className="hx-sn__name">{shown.title}</b>
                  <span className="hx-sn__ago">{ago(now, shown.at)}</span>
                </div>
                <Body
                  key={shown.key}
                  item={shown}
                  detail={detail}
                  busy={busy}
                  run={run}
                  onAnswer={onAnswer}
                  onDecide={onDecide}
                  onCode={onCode}
                  onCancelCode={onCancelCode}
                  onStop={onStop}
                  onReply={onReply}
                  onSlot={onSlot}
                  onSlotDismiss={onSlotDismiss}
                  reply={reply}
                />
                {shown.kind === SMART_KIND.machine && (
                  <div className="hx-sn__row">
                    <button type="button" className="hx-sn__link" onClick={() => hide()}>
                      {t('next.smart.machine.later')}
                    </button>
                    <button
                      type="button"
                      className="hx-sn__btn"
                      data-sn-action
                      onClick={() => onOpenMachine(shown)}
                    >
                      {t('next.smart.machine.open')}
                    </button>
                  </div>
                )}
                {shown.chatId !== null && (
                  <div className="hx-sn__foot">
                    <button type="button" className="hx-sn__link" onClick={() => openChat(shown)}>
                      {t('next.smart.openChat')}
                      <Icon name="arrow-up-right" size="sm" />
                    </button>
                  </div>
                )}
                {others.length > 0 && (
                  <div className="hx-sn__others-head">
                    <button type="button" className="hx-sn__link" onClick={onCloseAll}>
                      {t('next.smart.closeAll')}
                    </button>
                  </div>
                )}
                {others.length > 0 && (
                  <ul className="hx-sn__others">
                    {others.map((item) => (
                      <li key={item.key}>
                        <button type="button" className="hx-sn__other" onClick={() => onMain(item.key)}>
                          <Chip item={item} now={now} sent={sent.has(item.key)} />
                          <b className="hx-sn__title">{item.title}</b>
                          <span className="hx-sn__text">{item.text}</span>
                          <Icon name="chevron-right" size="sm" />
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>
        </div>
        {folded && (
          <button
            type="button"
            className="hx-sn__x"
            data-all={others.length > 0 || undefined}
            aria-label={others.length > 0 ? t('next.smart.closeAll') : t('next.smart.close')}
            onClick={() => (others.length > 0 ? onCloseAll() : hide())}
          >
            <Icon name="x" size="sm" />
            {others.length > 0 && <span className="hx-sn__x-all">{t('next.smart.closeAll')}</span>}
          </button>
        )}
      </div>
    </div>
  );
};

export default SmartNotice;
