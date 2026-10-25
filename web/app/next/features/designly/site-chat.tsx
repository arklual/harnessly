// Where the design's chat lives beside the site (the design's .hx-site modes): «Остров» — the field over the site,
// folded into a pill when it is empty and left; «Окно» — the chat in a window over the site that moves by its head and
// grows by its corner; «Чат» — the chat docked to the left or the right edge, its border pulled for the width;
// «Правки» — only notes on the page's blocks. The open chat (a window or an edge) is the usual chat — its feed and its
// field, the host draws it; the island is the chat folded over the site: its field, and over it the agent's last line
// with what it does now and «Стоп». The field, the pill and the chat are dragged by their grabs (site-drag.ts): where
// they would stand shows in the ghost.
import Logo from '@/next/ui/logo';
import { usePresence, usePresentFlag } from '@/next/ui/presence';
import {
  useEffect,
  useLayoutEffect,
  useRef,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react';
import {
  dragChatHead,
  dragFloatCorner,
  dragIsland,
  dragPill,
  dragSplit,
  type IFloat,
  type TDock,
  type TDropZone,
} from '@/next/features/designly/site-drag';
import { PANE_CHATS, type TPaneChat } from '@/next/features/designly/pane-chat';
import Icon from '@/next/ui/icon';
import { RoundButton } from '@/next/ui/button';
import { plural, t } from '@/next/i18n';
import '@/next/styles/island.css';

export const SITE_MODES = ['island', 'float', 'chat', 'notes'] as const;
export type TSiteMode = (typeof SITE_MODES)[number];

const MODE_ICON = {
  island: 'panel-bottom',
  float: 'picture-in-picture-2',
  chat: 'panel-left',
  notes: 'message-square-text',
} as const;

export type { IFloat };

/** The first window: on the right at the top, nearly as tall as the page. */
export const firstFloat = (section: { width: number; height: number }): IFloat => {
  const w = 360;
  return {
    x: Math.max(12, section.width - w - 40),
    y: 64,
    w,
    h: Math.max(320, Math.min(560, section.height - 140)),
  };
};

export const SiteModes = ({ mode, onMode }: { mode: TSiteMode; onMode: (mode: TSiteMode) => void }) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--seg-i', String(SITE_MODES.indexOf(mode)));
  }, [mode]);
  return (
    <div ref={ref} className="hx-site-modes" role="radiogroup" aria-label={t('next.site.modes')}>
      <span className="hx-site-modes__glider" />
      {SITE_MODES.map((id) => (
        <button
          key={id}
          type="button"
          className="hx-site-modes__item hx-tipped"
          role="radio"
          aria-checked={id === mode}
          aria-label={t(`next.site.mode.${id}`)}
          onClick={() => onMode(id)}
        >
          <Icon name={MODE_ICON[id]} size="sm" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t(`next.site.modeTip.${id}`)}
          </span>
        </button>
      ))}
    </div>
  );
};

const PANE_CHAT_ICON = {
  left: 'panel-left',
  float: 'picture-in-picture-2',
  bottom: 'panel-bottom',
} as const;

/** The switch of where the chat stands for a design in the right pane: «Слева», «Поверх», «Снизу». */
export const PaneChatModes = ({ mode, onMode }: { mode: TPaneChat; onMode: (mode: TPaneChat) => void }) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--seg-i', String(PANE_CHATS.indexOf(mode)));
  }, [mode]);
  return (
    <div ref={ref} className="hx-site-modes" role="radiogroup" aria-label={t('next.site.paneChat')}>
      <span className="hx-site-modes__glider" />
      {PANE_CHATS.map((id) => (
        <button
          key={id}
          type="button"
          className="hx-site-modes__item hx-tipped"
          role="radio"
          aria-checked={id === mode}
          aria-label={t(`next.site.paneChatMode.${id}`)}
          onClick={() => onMode(id)}
        >
          <Icon name={PANE_CHAT_ICON[id]} size="sm" />
          <span className="hx-tip" data-pos="below">
            {t(`next.site.paneChatMode.${id}`)}
          </span>
        </button>
      ))}
    </div>
  );
};

export interface ISiteChatProps {
  title: string;
  /** The chat itself (its feed and its field), drawn only while the chat is open and through its exit. */
  children?: ReactNode;
  /** In place of the title: the switch of the design's chats. */
  heading?: ReactNode;
  /** The chat on screen is giving way to the one just picked. */
  swapping?: boolean;
  mode: TSiteMode;
  float: IFloat | null;
  onFloat: (float: IFloat) => void;
  dock: TDock;
  /** The width of the docked chat. */
  width: number;
  onWidth: (width: number) => void;
  /** The chat was dragged to a place: an edge, a window, the field. */
  onZone: TDropZone;
  onHide: () => void;
  onOpenDrawer: () => void;
  /** The whole conversation on its own page: the steps, the questions, the files. */
  onOpenChat: (() => void) | null;
  /** On a phone: the site over the chat. */
  onOpenSite: () => void;
}

/** The chat's fade, as long as .hx-site__chat's opacity transition in site.css. */
const CHAT_EXIT_MS = 300;

export const SiteChat = ({
  title,
  children,
  heading,
  swapping = false,
  mode,
  float,
  onFloat,
  dock,
  width,
  onWidth,
  onZone,
  onHide,
  onOpenDrawer,
  onOpenChat,
  onOpenSite,
}: ISiteChatProps) => {
  const shown = mode === 'chat' || mode === 'float';
  // The feed and its field are there while the chat is open and while it fades away, not under the folded island: one
  // field takes the words at a time, and a hidden one would take the files dropped on the page.
  const body = usePresentFlag(shown, CHAT_EXIT_MS);

  const moveBy = (event: ReactPointerEvent<HTMLElement>) => {
    if (mode === 'island' || mode === 'notes') return;
    dragChatHead(event, { mode, dock, float, onFloat, onZone });
  };
  const growBy = (event: ReactPointerEvent<HTMLElement>) => {
    if (float) dragFloatCorner(event, float, onFloat);
  };

  return (
    <>
      <aside className="hx-site__chat" aria-hidden={!shown}>
        <div className="hx-site__chat-head" onPointerDown={moveBy}>
          <button
            type="button"
            className="hx-head-btn hx-site__menu"
            aria-label={t('next.header.openMenu')}
            onClick={onOpenDrawer}
          >
            <Icon name="menu" />
          </button>
          {heading ?? <span className="hx-site__chat-title">{title}</span>}
          <span className="hx-spacer" />
          <button
            type="button"
            className="hx-head-btn hx-site__open"
            aria-label={t('next.site.openSite')}
            onClick={onOpenSite}
          >
            <Icon name="app-window" />
          </button>
          {onOpenChat && (
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-label={t('next.site.openChat')}
              onClick={onOpenChat}
            >
              <Icon name="message-square" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.site.openChat')}
              </span>
            </button>
          )}
          <button
            type="button"
            className="hx-head-btn hx-tipped hx-site__chat-x"
            aria-label={t('next.site.hideChat')}
            onClick={onHide}
          >
            <Icon name="x" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.site.hideChatTip')}
            </span>
          </button>
        </div>
        <div className="hx-site__conv" data-state={body.state} data-swap={swapping ? 'out' : undefined}>
          {body.mounted && children}
        </div>
        <span className="hx-site__fres" aria-hidden="true" onPointerDown={growBy} />
      </aside>
      <div
        className="hx-site__split"
        aria-hidden="true"
        onPointerDown={(event) => dragSplit(event, { w: width, dock, onWidth, onClose: onHide })}
      >
        <span />
      </div>
      <div className="hx-site__ghost" aria-hidden="true">
        <span className="hx-site__ghost-label" />
      </div>
    </>
  );
};

export const ISLAND_STATUS = { run: 'run', wait: 'wait', done: 'done' } as const;

/** The agent's state over the folded chat's field. */
export type TIslandStatus =
  | { k: typeof ISLAND_STATUS.run; text: string }
  /** It asks something the island cannot answer: the chat opens to answer it. */
  | { k: typeof ISLAND_STATUS.wait; text: string }
  | { k: typeof ISLAND_STATUS.done; text: string }
  | null;

export interface ISiteIslandProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  /** There are words or notes, and no turn is running. */
  canSend: boolean;
  placeholder: string;
  /** The field is not taken yet: the conversation is being made. */
  sending: boolean;
  status: TIslandStatus;
  /** What the agent said last (IslandLast): the folded chat shows it over its status. */
  last?: ReactNode;
  onStop: () => void;
  onDismiss: () => void;
  /** Opens the chat beside the site (the line, «Ответить в чате»). */
  onExpand: () => void;
  /** Folded into the pill. */
  min: boolean;
  onMin: (min: boolean) => void;
  mode: TSiteMode;
  /** The field was dragged to a place, or pulled down to fold. */
  onZone: TDropZone;
  recording: boolean;
  onMic?: () => void;
  /** The notes going with the words, over the field. */
  extra?: ReactNode;
  /** How many notes go with the words: the folded pill says so. */
  notes: number;
  /** A click on the page picks a block for a note; null: the page cannot be picked from (no bridge yet). */
  pick: boolean | null;
  onPick: () => void;
}

const FIELD_MAX = 132;
/** The status's fold, as long as .hx-island__status's transition in island.css. */
const STATUS_EXIT_MS = 420;
const BLUR_MS = 140;

/** The agent's last line in the folded chat; a press opens the chat. */
export const IslandLast = ({ text, onExpand }: { text: string | null; onExpand: () => void }) =>
  text ? (
    <button type="button" className="hx-island__last" onClick={onExpand}>
      {text}
    </button>
  ) : null;

export const SiteIsland = (props: ISiteIslandProps) => {
  const { value, onChange, onSend, status, min, onMin, mode } = props;
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  // The status folds away with what it said: its words stay while it leaves.
  const { value: held } = usePresence(status, STATUS_EXIT_MS);

  useLayoutEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.style.removeProperty('height');
    if (value) el.style.setProperty('height', `${Math.min(FIELD_MAX, el.scrollHeight)}px`);
  }, [value]);

  // «/» outside a field brings the folded field back.
  useEffect(() => {
    if (!min) return;
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as Element | null;
      if (event.key !== '/' || target?.closest('input, textarea, [contenteditable="true"]')) return;
      event.preventDefault();
      onMin(false);
      inputRef.current?.focus({ preventScroll: true });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [min, onMin]);

  const expand = () => {
    onMin(false);
    inputRef.current?.focus({ preventScroll: true });
  };

  // A press is the gesture's; a click that no pointer made is the keyboard's.
  const onPillClick = (event: ReactMouseEvent) => {
    if (event.detail === 0) expand();
  };

  const onBlur = () => {
    if (mode !== 'island') return;
    window.setTimeout(() => {
      const active = document.activeElement;
      if (inputRef.current?.value.trim() || props.recording || rootRef.current?.contains(active)) return;
      onMin(true);
    }, BLUR_MS);
  };

  return (
    <div className="hx-island" ref={rootRef}>
      <div
        className="hx-island__status"
        data-shown={status !== null}
        data-done={held?.k === ISLAND_STATUS.done}
      >
        <div className="hx-island__status-in">
          {props.last}
          <div className="hx-island__now">
            <span className="hx-island__ic">
              <Icon name="loader-circle" size="sm" className="hx-island__spin" />
              <Icon name="circle-check" size="sm" className="hx-island__ok" />
            </span>
            <span className="hx-island__step">{held?.text}</span>
            <span className="hx-spacer" />
            {held?.k === ISLAND_STATUS.run && (
              <button type="button" className="hx-island__act" onClick={props.onStop}>
                {t('next.site.stop')}
              </button>
            )}
            {held?.k === ISLAND_STATUS.wait && (
              <button type="button" className="hx-island__act" onClick={props.onExpand}>
                {t('next.site.answerInChat')}
              </button>
            )}
            {held?.k === ISLAND_STATUS.done && (
              <button
                type="button"
                className="hx-island__x"
                aria-label={t('next.site.dismiss')}
                onClick={props.onDismiss}
              >
                <Icon name="x" size="sm" />
              </button>
            )}
          </div>
        </div>
      </div>
      <div className="hx-island__box">
        <span
          className="hx-island__grab"
          title={t('next.site.grabTip')}
          onPointerDown={(event) => dragIsland(event, props.onZone)}
        />
        {props.extra}
        <div className="hx-island__row">
          <textarea
            ref={inputRef}
            className="hx-island__input"
            rows={1}
            placeholder={props.placeholder}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            onBlur={onBlur}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                onSend();
              }
              if (event.key === 'Escape' && mode === 'island' && !value.trim()) {
                event.preventDefault();
                inputRef.current?.blur();
                onMin(true);
              }
            }}
          />
          {props.pick !== null && (
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-pressed={props.pick}
              aria-label={t('next.notes.pick')}
              onClick={props.onPick}
            >
              <Icon name="square-dashed-mouse-pointer" />
              <span className="hx-tip" data-align="end">
                {t('next.notes.pickTip')}
                <kbd>V</kbd>
              </span>
            </button>
          )}
          {props.onMic && (
            <button
              type="button"
              className="hx-head-btn hx-tipped hx-island__mic"
              data-rec={props.recording}
              aria-label={t('next.composer.voice')}
              onClick={props.onMic}
            >
              <Icon name="mic" />
              <span className="hx-tip" data-align="end">
                {t('next.composer.voice')}
              </span>
            </button>
          )}
          <RoundButton
            icon="arrow-up"
            label={t('next.site.send')}
            variant="accent"
            size="sm"
            busy={props.sending}
            disabled={!props.canSend}
            onClick={onSend}
          />
        </div>
      </div>
      <button
        type="button"
        className="hx-island__mini"
        tabIndex={min ? 0 : -1}
        onPointerDown={(event) => dragPill(event, expand)}
        onClick={onPillClick}
      >
        <Logo className="hx-island__orb" />
        {props.notes > 0 ? plural(props.notes, 'next.site.askWith') : t('next.site.ask')}
        <kbd>/</kbd>
      </button>
    </div>
  );
};
