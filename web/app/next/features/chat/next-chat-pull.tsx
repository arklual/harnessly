// The phone's way to the next chat, as in Telegram: the chat read to its end and pulled further up shows
// the next chat that is at work or has something unread, by the sidebar's order — its name and the gist of its last
// turn in a card whose frame fills as the finger goes. Pulled far enough, a check and a light tap of the vibration;
// let go there, the chat opens; let go before, the card goes back down. No such chat: nothing at all.
import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { PHONE_QUERY } from '@/next/features/shell/gestures';
import { byPrompt } from '@/next/features/sidebar/group';
import { CHAT_STATUS, type ISidebarChat, type TChatStatus } from '@/next/features/sidebar/types';

/** How far the finger pulls past the end for the card to be ready, px. */
const ARM_PX = 120;
/** Movement before the pull counts (or a scroll is left alone), px. */
const SLOP_PX = 8;
/** The card's way out, back down or into the next chat, ms. */
const EXIT_MS = 240;
/** The feed counts as read to its end this close to it, px. */
const END_PX = 4;

/** What keeps the finger for itself: a field, a slider. A strip that scrolls sideways does not: the pull is vertical. */
const OWNS_TOUCH = 'input, textarea, [contenteditable="true"], [role="slider"], [data-nogesture]';

/** A chat is active when its agent is at work, waits for the person, or has a turn not read yet. */
const QUIET: ReadonlySet<TChatStatus> = new Set([CHAT_STATUS.none, CHAT_STATUS.limited]);

/** The next active chat after the open one, in the sidebar's order (pinned first, then by the person's last
 *  message), going round to the top; null when there is none. */
export const nextActiveChat = (
  chats: readonly ISidebarChat[],
  currentId: string | null,
): ISidebarChat | null => {
  const order = [...chats].sort((a, b) => Number(b.pinned) - Number(a.pinned) || byPrompt(a, b));
  const at = order.findIndex((chat) => chat.id === currentId);
  for (let i = 1; i <= order.length; i++) {
    const chat = order[(at + i + order.length) % order.length];
    if (chat.id !== currentId && !QUIET.has(chat.status)) return chat;
  }
  return null;
};

export interface INextChatPullProps {
  enabled: boolean;
  chats: readonly ISidebarChat[] | null;
  currentId: string | null;
  /** The gist of the chat's last turn, a line or two; null when there is none. */
  contextOf: (id: string) => string | null;
  onOpen: (id: string) => void;
}

interface IShown {
  chat: ISidebarChat;
  context: string | null;
  bottom: number;
  phase: 'pull' | 'back' | 'go';
}

const NextChatPull = ({ enabled, chats, currentId, contextOf, onOpen }: INextChatPullProps) => {
  const [shown, setShown] = useState<IShown | null>(null);
  const card = useRef<HTMLDivElement>(null);
  const frame = useRef<SVGRectElement>(null);
  const progress = useRef(0);
  const armed = useRef(false);

  const paint = useEffectEvent(() => {
    const el = card.current;
    if (!el) return;
    const p = progress.current;
    el.style.setProperty('--ncp', p.toFixed(3));
    if (shown) el.style.setProperty('--ncb', `${shown.bottom}px`);
    el.dataset.armed = armed.current ? 'true' : 'false';
    frame.current?.style.setProperty('stroke-dashoffset', (1 - p).toFixed(3));
  });

  useLayoutEffect(() => paint(), [shown]);

  const begin = useEffectEvent((): Omit<IShown, 'phase' | 'bottom'> | null => {
    if (!chats) return null;
    const chat = nextActiveChat(chats, currentId);
    if (!chat) return null;
    // The gist is plain text: the marks of markdown (headings, emphasis, code) are left out.
    const context =
      contextOf(chat.id)
        ?.replace(/[#*_`>]+/g, '')
        .replace(/\s+/g, ' ')
        .trim() || null;
    return { chat, context };
  });
  const go = useEffectEvent((id: string) => onOpen(id));

  useEffect(() => {
    if (!enabled) return;
    const phone = window.matchMedia(PHONE_QUERY);
    let pull: {
      startX: number;
      startY: number;
      active: boolean;
      next: Omit<IShown, 'phase' | 'bottom'>;
      raf: number;
    } | null = null;
    let timer = 0;

    const onStart = (event: TouchEvent) => {
      if (!phone.matches || pull || event.touches.length !== 1) return;
      const target = event.target instanceof Element ? event.target : null;
      const feed = target?.closest<HTMLElement>('.hx-stage .hx-feed');
      if (!feed || target?.closest(OWNS_TOUCH)) return;
      if (feed.scrollTop + feed.clientHeight < feed.scrollHeight - END_PX) return;
      const next = begin();
      if (!next) return;
      const touch = event.touches[0];
      pull = { startX: touch.clientX, startY: touch.clientY, active: false, next, raf: 0 };
    };

    const onMove = (event: TouchEvent) => {
      if (!pull) return;
      const touch = event.touches[0];
      const up = pull.startY - touch.clientY;
      const dx = touch.clientX - pull.startX;
      if (!pull.active) {
        if (Math.abs(up) < SLOP_PX && Math.abs(dx) < SLOP_PX) return;
        if (up < SLOP_PX || Math.abs(dx) * 1.2 > up) {
          pull = null;
          return;
        }
        pull.active = true;
        pull.startY = touch.clientY;
        progress.current = 0;
        armed.current = false;
        window.clearTimeout(timer);
        const dock = document.querySelector('.hx-stage .hx-dock');
        const top = dock ? dock.getBoundingClientRect().top : window.innerHeight;
        setShown({ ...pull.next, bottom: Math.max(16, window.innerHeight - top + 12), phase: 'pull' });
      }
      if (event.cancelable) event.preventDefault();
      const p = Math.max(0, Math.min(1, (pull.startY - touch.clientY) / ARM_PX));
      progress.current = p;
      const ready = p >= 1;
      if (ready !== armed.current) {
        armed.current = ready;
        if (ready) navigator.vibrate?.(12);
      }
      if (!pull.raf)
        pull.raf = requestAnimationFrame(() => {
          if (pull) pull.raf = 0;
          paint();
        });
    };

    // A touch the system took (a call, a gesture of the OS) never opens the chat.
    const finish = (ok: boolean) => {
      if (!pull) return;
      const current = pull;
      pull = null;
      if (current.raf) cancelAnimationFrame(current.raf);
      if (!current.active) return;
      const ready = ok && armed.current;
      setShown((s) => (s ? { ...s, phase: ready ? 'go' : 'back' } : s));
      if (ready) go(current.next.chat.id);
      timer = window.setTimeout(() => setShown(null), EXIT_MS);
    };
    const onEnd = () => finish(true);
    const onCancel = () => finish(false);

    document.addEventListener('touchstart', onStart, { passive: true });
    document.addEventListener('touchmove', onMove, { passive: false });
    document.addEventListener('touchend', onEnd);
    document.addEventListener('touchcancel', onCancel);
    return () => {
      document.removeEventListener('touchstart', onStart);
      document.removeEventListener('touchmove', onMove);
      document.removeEventListener('touchend', onEnd);
      document.removeEventListener('touchcancel', onCancel);
      window.clearTimeout(timer);
      if (pull?.raf) cancelAnimationFrame(pull.raf);
    };
  }, [enabled]);

  if (!shown) return null;
  return (
    <div
      ref={card}
      className="hx-nextchat"
      data-phase={shown.phase}
      role="status"
      aria-label={t('next.chat.nextPull', { title: shown.chat.title })}
    >
      <svg className="hx-nextchat__frame" aria-hidden="true">
        <rect ref={frame} x="1" y="1" rx="15" pathLength={1} />
      </svg>
      <span className="hx-nextchat__check" aria-hidden="true">
        <Icon name="check" size="sm" />
      </span>
      <div className="hx-nextchat__title">{shown.chat.title}</div>
      {shown.context && <div className="hx-nextchat__context">{shown.context}</div>}
    </div>
  );
};

export default NextChatPull;
