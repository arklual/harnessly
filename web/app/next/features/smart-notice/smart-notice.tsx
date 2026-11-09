// The smart notice: a black plate that shows up only while something waits for the person (a question of an agent,
// a permission, a plan, a one-time code) and for a few seconds when a turn ends or breaks. Folded it is one line —
// the chat and what waits — with plates peeking from under it while more waits; open it is a card where the thing is
// done in place: an option or words for a question, allow or deny, the code's cells. One black shape grows from a strip
// into the line and into the card and back: opening springs, closing eases out. Where it stands is the person's
// choice (stores/notices.ts); the middle of the bottom edge is over the field of the chat on screen. One
// implementation for every width: on the phone it is as wide as the screen less a margin.
import '@/next/styles/smart-notice.css';

import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { t } from '@/next/i18n';
import CodeField, { CODE_FIELD, type TCodeFieldState } from '@/next/ui/code-field';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence, usePresentFlag } from '@/next/ui/presence';
import type { TNoticePlace, TNoticeSound } from '@/next/stores/notices';
import useGestures from './use-gestures';

export const SMART_KIND = {
  code: 'code',
  permission: 'permission',
  question: 'question',
  plan: 'plan',
  machine: 'machine',
  failed: 'failed',
  limit: 'limit',
  done: 'done',
} as const;
export type TSmartKind = (typeof SMART_KIND)[keyof typeof SMART_KIND];

/** Kinds that wait for the person: they stay until done and cannot be taken away. */
export const WAITING_KINDS: ReadonlySet<TSmartKind> = new Set([
  SMART_KIND.code,
  SMART_KIND.permission,
  SMART_KIND.question,
  SMART_KIND.plan,
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
}

/** What the open card shows for its main item, read by the binding. */
export type TSmartDetail =
  | { k: 'plain' }
  | { k: 'loading' }
  | { k: 'question'; text: string; options: readonly string[] }
  | { k: 'permission'; command: string }
  | { k: 'code'; why: string; digits: number }
  | { k: 'machine'; text: string; level: number; heavy: readonly ISmartHeavy[] };

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
  onHide: (item: ISmartItem) => void;
  onMute: (sound: TNoticeSound) => void;
  onSettings: () => void;
  /** A plate is new for this long: it tells what came in two lines with its main button. */
  announceMs: number;
  /** Where the focus goes back when the card closes from the keyboard. */
  returnFocus: () => void;
  /** The card opened from the keyboard: its first action takes the focus. */
  focusOnOpen: boolean;
}

/** How long the plate takes to leave when nothing waits any more, ms (styles/smart-notice.css has the same). */
export const SMART_EXIT_MS = 240;
/** How long the card takes to fold back into the line: the shape eases out then, it does not spring. */
const FOLD_MS = 300;
/** A sent answer shows its check this long at most; the plate leaves sooner, once the agent takes the answer. */
const SENT_HOLD_MS = 6000;
/** The folded plate shows this many plates under the front one. */
const PEEKS = 2;

const ACTION: Record<TSmartKind, string> = {
  code: 'next.smart.enterCode',
  permission: 'next.smart.answer',
  question: 'next.smart.answer',
  plan: 'next.smart.answer',
  machine: 'next.smart.machine.look',
  failed: 'next.smart.open',
  limit: 'next.smart.open',
  done: 'next.smart.open',
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
};

const RING_R = 16.5;
const RING_LEN = 2 * Math.PI * RING_R;

/** The kind's icon in a round chip; a code has a ring around it that runs down to its deadline. */
const Chip = ({
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
}: {
  item: ISmartItem;
  detail: TSmartDetail;
  busy: boolean;
  run: (work: () => Promise<void>) => void;
} & Pick<ISmartNoticeProps, 'onAnswer' | 'onDecide' | 'onCode' | 'onCancelCode' | 'onStop'>) => {
  const [words, setWords] = useState('');
  const [code, setCode] = useState('');
  const [codeState, setCodeState] = useState<TCodeFieldState>(CODE_FIELD.idle);
  const [note, setNote] = useState<string | null>(null);

  if (detail.k === 'loading') return <div className="hx-sn__wait" aria-busy="true" />;
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
        <form
          className="hx-sn__field"
          onSubmit={(event) => {
            event.preventDefault();
            const text = words.trim();
            if (text) run(() => onAnswer(item, { text }));
          }}
        >
          <input
            className="hx-sn__input"
            value={words}
            placeholder={t('next.smart.ownAnswer')}
            aria-label={t('next.smart.ownAnswer')}
            disabled={busy}
            onChange={(event) => setWords(event.target.value)}
          />
          <button
            type="submit"
            className="hx-sn__send"
            disabled={busy || words.trim() === ''}
            aria-label={t('next.smart.send')}
          >
            <Icon name="arrow-up" size="sm" />
          </button>
        </form>
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

const SmartNotice = ({
  items,
  mainKey,
  detail,
  open,
  place,
  anchor,
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
  onMute,
  onSettings,
  announceMs,
  returnFocus,
  focusOnOpen,
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
  const host = useRef<HTMLDivElement | null>(null);
  const shape = useRef<HTMLDivElement | null>(null);
  const body = useRef<HTMLDivElement | null>(null);
  const [menuOn, setMenuOn] = useState(false);
  const menu = usePresentFlag(menuOn && showing, MENU_EXIT_MS);
  const menuEl = useRef<HTMLDivElement | null>(null);

  const folded = !open || shown === null;
  const announcing = folded && shown !== null && now - shown.at < announceMs;
  const others = shown ? items.filter((item) => item.key !== shown.key) : [];
  useFitShape(host, body, `${shown?.key ?? ''}:${open}:${announcing}:${detail.k}:${others.length}`);

  const step = (dir: 1 | -1) => {
    if (!shown) return;
    const at = items.findIndex((item) => item.key === shown.key);
    const next = items[(at + dir + items.length) % items.length];
    if (next && next.key !== shown.key) onMain(next.key);
  };
  const hide = () => {
    if (!shown) return;
    setMenuOn(false);
    onOpen(false);
    onHide(shown);
  };
  const gestures = useGestures({
    host,
    present: showing,
    edge: place.startsWith('top') ? 'top' : 'bottom',
    open,
    off: busy,
    stacked: others.length > 0,
    onHide: hide,
    onOpen: () => onOpen(true),
    onFold: () => onOpen(false),
    onMenu: () => setMenuOn(true),
    onStep: step,
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

  // A new plate grows from the strip on the next frame; a plate that leaves shrinks back into it.
  useEffect(() => {
    if (!showing) return;
    const frame = requestAnimationFrame(() => setEntered(true));
    return () => {
      cancelAnimationFrame(frame);
      setEntered(false);
    };
  }, [showing]);

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
  const shownKey = shown?.key ?? null;
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
    void work()
      .then(() => {
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
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      hide();
      returnFocus();
      return;
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      step(event.key === 'ArrowDown' ? 1 : -1);
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
  const h = place.split('-')[1];

  return (
    <div
      ref={host}
      className="hx-sn"
      data-v={v}
      data-h={h}
      data-anchored={(anchor !== null && place === 'bottom-center') || undefined}
      data-phase={entered && present.state === PRESENCE.open ? 'in' : 'out'}
      data-open={open || undefined}
      data-folding={folding || undefined}
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
              onClick={act(() => onOpenChat(shown))}
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
          <button type="button" role="menuitem" className="hx-sn__item" onClick={act(hide)}>
            <Icon name="eye-off" size="sm" />
            {t('next.smart.menu.hide')}
          </button>
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
      {folded &&
        others
          .slice(0, PEEKS)
          .map((item, i) => (
            <span key={item.key} className="hx-sn__plate" data-depth={i + 1} aria-hidden="true" />
          ))}
      <div ref={shape} className="hx-sn__shape">
        <div ref={body} className="hx-sn__body" key={folded ? `f:${announcing}` : `o:${shown.key}`}>
          {folded && !announcing && (
            <button type="button" className="hx-sn__pill" aria-expanded={false} onClick={() => onOpen(true)}>
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
              <button
                type="button"
                className="hx-sn__btn"
                onClick={() =>
                  WAITING_KINDS.has(shown.kind) || shown.kind === SMART_KIND.machine
                    ? onOpen(true)
                    : onOpenChat(shown)
                }
              >
                {t(ACTION[shown.kind])}
              </button>
            </div>
          )}
          {!folded && (
            <div className="hx-sn__card" role="dialog" aria-label={shown.title}>
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
              />
              {shown.kind === SMART_KIND.machine && (
                <div className="hx-sn__row">
                  <button type="button" className="hx-sn__link" onClick={hide}>
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
                  <button type="button" className="hx-sn__link" onClick={() => onOpenChat(shown)}>
                    {t('next.smart.openChat')}
                    <Icon name="arrow-up-right" size="sm" />
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
    </div>
  );
};

export default SmartNotice;
