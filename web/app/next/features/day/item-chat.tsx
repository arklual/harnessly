// A thing of the day, opened: the page unfolds from what was pressed into the thing's chat. On top stands its big
// widget: the way back, the title with its deadline, a line of what it is about, then formatted text with small widgets
// between the paragraphs (parts.tsx) and the answers the person can give with one press.
import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { openConversation } from '@/next/features/app-views/agents';
import { leadsTo, type IItemChat } from '@/next/features/day/chat-link';
import { CHIP, webUrl, type IDayCard, type IDayChip, type IDayPriority } from '@/next/features/day/feed';
import { feedIcon } from '@/next/features/day/icons';
import { Part, partsOf } from '@/next/features/day/parts';
import { cssVars } from '@/next/features/day/place';
import { plain } from '@/next/features/day/rich';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import type { TPresenceState } from '@/next/ui/presence';
import { hub } from '@/next/core/realtime/hub';

// The chat comes with the first thing opened that an agent works on: the page of the day does not carry it before.
const ChatView = lazy(() => import('@/next/bindings/chat-view'));

const SAY_MAX = 400;

interface IItemChatProps {
  card: IDayCard;
  /** The card of the list the thing was opened from: its words stay over the widget. */
  priority: IDayPriority | undefined;
  state: TPresenceState;
  /** Where the pressed element stood, for the unfolding. */
  origin: DOMRect | null;
  /** An answer is on its way: the others wait. */
  busy: boolean;
  onClose: () => void;
  onChip: (chip: IDayChip, say?: string) => void;
  /** Opens the agent's chat the thing names, here in the hub. */
  onGo: () => void;
  /**
   * The agent's chat about the thing, on this page's machine: it stands under the widget as the thing's own chat (the
   * design's dx-talk), with its field at the bottom; null while no agent has worked on it here.
   */
  chat?: IItemChat | null;
}

const ItemChat = ({
  card,
  priority,
  state,
  origin,
  busy,
  onClose,
  onChip,
  onGo,
  chat = null,
}: IItemChatProps) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const backRef = useRef<HTMLButtonElement>(null);
  const [saying, setSaying] = useState<IDayChip | null>(null);

  // The unfolding starts from the pressed element's box: its distance to each edge of the page.
  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const box = root.getBoundingClientRect();
    const from = origin ?? box;
    cssVars({
      '--ct': `${Math.max(0, from.top - box.top)}px`,
      '--cl': `${Math.max(0, from.left - box.left)}px`,
      '--cr': `${Math.max(0, box.right - from.right)}px`,
      '--cb': `${Math.max(0, box.bottom - from.bottom)}px`,
    })(root);
    backRef.current?.focus({ preventScroll: true });
  }, [origin]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return;
      onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const title = priority?.title ?? card.title;
  const when = priority?.due ?? card.whenText;
  const lead = card.lead ? plain(card.lead).trim() : (priority?.sub ?? '');
  // What leads to the chat that stands right under the widget has nothing to add there.
  const chips = (card.chips ?? []).filter(
    (chip) => !(chat && chip.kind === CHIP.open && leadsTo({ url: chip.url }, chat)),
  );
  const go = chat && leadsTo(card.go, chat) ? null : (card.go ?? null);

  const press = (chip: IDayChip) => {
    if (chip.kind === CHIP.say) setSaying(chip);
    else onChip(chip);
  };

  const widget = (
    <article className="hx-day-w">
      <header className="hx-day-w__bar">
        <button ref={backRef} type="button" className="hx-day-w__back" onClick={onClose}>
          <Icon name="arrow-left" size="sm" />
          {t('next.day.back')}
        </button>
      </header>
      <div className="hx-day-w__t">
        <b>{title}</b>
        {when && <time data-soon={priority?.soon === true ? '' : undefined}>{when}</time>}
      </div>
      {lead !== '' && <p className="hx-day-w__lead">{lead}</p>}
      <div className="hx-day-w__parts">
        {partsOf(card).map((part, i) => (
          <Part key={i} part={part} />
        ))}
        {go && (
          <section className="hx-day-p" data-k="file">
            <div className="hx-day-fl">
              <span className="hx-day-fl__ic">
                <Icon name={feedIcon(go.icon, 'file-text')} />
              </span>
              <span className="hx-day-fl__m">
                <b>{go.title}</b>
                {go.meta && <span>{go.meta}</span>}
              </span>
              {go.chat !== undefined && (
                <button type="button" className="hx-day-fl__b" onClick={onGo}>
                  {go.button ?? t('next.day.open')}
                  <Icon name="arrow-right" size="sm" />
                </button>
              )}
              {go.chat === undefined && webUrl(go.url) && (
                <a className="hx-day-fl__b" href={webUrl(go.url)} target="_blank" rel="noopener noreferrer">
                  {go.button ?? t('next.day.open')}
                  <Icon name="arrow-up-right" size="sm" />
                </a>
              )}
            </div>
          </section>
        )}
      </div>
      {chips.length > 0 && saying === null && (
        <footer className="hx-day-w__f">
          {chips.map((chip, i) => (
            <button
              key={i}
              type="button"
              className="hx-day-chip"
              data-main={i === 0 ? '' : undefined}
              disabled={busy}
              onClick={() => press(chip)}
            >
              {chip.label}
            </button>
          ))}
        </footer>
      )}
      {saying !== null && (
        <form
          className="hx-day-w__say"
          onSubmit={(event) => {
            event.preventDefault();
            const text = new FormData(event.currentTarget).get('say');
            if (typeof text !== 'string' || text.trim() === '') return;
            onChip(saying, text.trim());
            setSaying(null);
          }}
        >
          <input
            name="say"
            className="hx-day-w__inp"
            aria-label={saying.label}
            placeholder={saying.label}
            defaultValue={saying.say ?? ''}
            maxLength={SAY_MAX}
            autoFocus
            onKeyDown={(event) => {
              if (event.key !== 'Escape') return;
              event.preventDefault();
              event.stopPropagation();
              setSaying(null);
            }}
          />
          <button type="submit" className="hx-day-chip" data-main="" disabled={busy}>
            {t('next.day.say')}
          </button>
          <button type="button" className="hx-day-chip" onClick={() => setSaying(null)}>
            {t('next.day.cancel')}
          </button>
        </form>
      )}
    </article>
  );

  if (chat !== null)
    return (
      <div
        ref={rootRef}
        className="hx-day-item"
        data-state={state}
        data-chat=""
        role="dialog"
        aria-label={title}
      >
        <Suspense fallback={<div className="hx-day-item__in">{widget}</div>}>
          <ChatView
            key={chat.conv}
            conv={chat.conv}
            machine={chat.machine}
            lead={widget}
            onRetryLoad={() => hub.client(chat.machine)?.reconnectNow()}
            onShowBackground={() => openConversation(chat.conv)}
          />
        </Suspense>
      </div>
    );

  return (
    <div ref={rootRef} className="hx-day-item" data-state={state} role="dialog" aria-label={title}>
      <div className="hx-day-item__in">{widget}</div>
    </div>
  );
};

export default ItemChat;
