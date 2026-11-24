// The stack of what waits, in the chat's right column, as the notifications of a phone (iOS, One UI): folded it is the
// newest card with the edges of the others under it; a tap spreads it into the list, grouped by chat, with «Свернуть»
// and «Очистить всё». A card opens in place — what is asked, whole, and the answer right there when there is one —
// and never leaves its spot for the middle of the screen. A swipe aside or «Убрать» takes a card off the stack (the
// chat still waits; the notification centre keeps it). Everything moves on transform and opacity only: the cards
// slide and fade, the ones around a card that came or went spring to their new places (useFlip), and one that is
// answered elsewhere leaves with the same flight as a swipe.
import { memo, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import { formatAgo } from '@/next/i18n/format';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresentFlag } from '@/next/ui/presence';
import { AUTO_OPEN_DELAY_MS, AUTO_OPEN_MS, takeAutoOpen } from '@/next/features/notifications/auto-open';
import { LEAVE_MS, useFlip, useSwipe } from '@/next/features/notifications/motion';
import { useDropArriving, type IDrop } from '@/next/stores/smart-drops';
import { snap } from './fly';
import { Chip, SMART_KIND } from './smart-notice';

/** How long the list takes to fold back into the stack, ms (styles/notice-stack.css). */
export const FOLD_MS = 240;
/** A card's details close in this long (styles/notice-stack.css). */
const DETAIL_MS = 260;
/** Edges of the cards under the top one in a folded stack. */
const UNDER = 2;
const MINUTE_MS = 60_000;
/** The cards that come in one after another when the stack spreads; the rest come with the last of them. */
const STAGGER_STEPS = 5;

/** A card asked back into the smart notice: its key, where it stands and a copy of what it shows. */
export type TStackBack = (key: string, from: DOMRect | null, view: HTMLElement | null) => void;

export interface INoticeStackProps {
  drops: readonly IDrop[];
  now: number;
  /** The heading: «Ждут ответа», «Машина». */
  label: string;
  /** The heading's icon, as the other blocks of the column have (the island's titles); none for a heading that is hidden. */
  icon?: string;
  /** «Ответить» or «Подробнее» where the answer is not in the card: the smart notice takes it back, open. */
  onBack: TStackBack;
  onOpenChat?: (chatId: string) => void;
  onDismiss: (keys: readonly string[]) => void;
  /** The answer in place (a question's options, a permission's buttons; a machine's cause and what weighs on it);
   *  null where there is none. */
  renderAnswer?: (drop: IDrop, open: boolean, toggle: () => void) => ReactNode;
  /** Whether a card is answered in place (and so opens in place on a press); a card that is not opens its chat. */
  inPlace?: (drop: IDrop) => boolean;
}

interface IShown {
  drop: IDrop;
  /** Answered elsewhere: it flies off, then goes. */
  gone: boolean;
}

const NO_KEYS: ReadonlySet<string> = new Set();

/** The drops with the ones that just went kept for their flight off; the same list while nothing changed. Each stays
 *  for its own flight, not cut short by another's. One the person took off (a swipe, «Убрать») has flown already when
 *  it goes: it is not kept, nor flown a second time. */
const useLeaving = (drops: readonly IDrop[], flown: ReadonlySet<string>): readonly IShown[] => {
  const [shown, setShown] = useState<readonly IShown[]>(() => drops.map((drop) => ({ drop, gone: false })));
  const [was, setWas] = useState(drops);
  if (was !== drops) {
    setWas(drops);
    const keys = new Set(drops.map((d) => d.item.key));
    const next: IShown[] = drops.map((drop) => ({ drop, gone: false }));
    // A card that went stays where it stood, so its flight starts from its place.
    shown.forEach((s, i) => {
      const key = s.drop.item.key;
      if (keys.has(key) || flown.has(key)) return;
      next.splice(Math.min(i, next.length), 0, s.gone ? s : { drop: s.drop, gone: true });
    });
    setShown(next);
  }
  const goneKeys = shown
    .filter((s) => s.gone)
    .map((s) => s.drop.item.key)
    .join('\n');
  const since = useRef(new Map<string, number>());
  useEffect(() => {
    const keys = goneKeys === '' ? [] : goneKeys.split('\n');
    const at = since.current;
    const now = Date.now();
    for (const key of keys) if (!at.has(key)) at.set(key, now);
    for (const key of [...at.keys()]) if (!keys.includes(key)) at.delete(key);
    if (keys.length === 0) return;
    const first = Math.min(...keys.map((key) => at.get(key) ?? now));
    const timer = window.setTimeout(
      () => {
        const end = Date.now();
        setShown((all) => all.filter((s) => !s.gone || end - (at.get(s.drop.item.key) ?? 0) < LEAVE_MS));
      },
      Math.max(0, first + LEAVE_MS - now),
    );
    return () => window.clearTimeout(timer);
  }, [goneKeys]);
  return shown;
};

interface ICardProps {
  drop: IDrop;
  /** The minute for «when», or the second for a code's ring: a card does not redraw every second for nothing. */
  now: number;
  open: boolean;
  gone: boolean;
  /** Taken off by the person and flown: drawn gone until the stack lets it go. */
  flown: boolean;
  index: number;
  onToggle: (key: string) => void;
  onBack: TStackBack;
  onOpenChat?: (chatId: string) => void;
  /** Its flight off is over (a swipe, «Убрать»): it goes from the stack at once. */
  onFlown: (key: string) => void;
  renderAnswer?: INoticeStackProps['renderAnswer'];
  inPlace?: INoticeStackProps['inPlace'];
}

/** A drop the notice flies into: hidden until the flight lands, then shown as it is, without an entrance of its own. */
const useArrival = (key: string): { arriving: boolean; landed: boolean } => {
  const arriving = useDropArriving(key);
  const [came, setCame] = useState<string | null>(arriving ? key : null);
  if (arriving && came !== key) setCame(key);
  return { arriving, landed: !arriving && came === key };
};

const Card = memo(
  ({
    drop,
    now,
    open,
    gone,
    flown,
    index,
    onToggle,
    onBack,
    onOpenChat,
    onFlown,
    renderAnswer,
    inPlace,
  }: ICardProps) => {
    const el = useRef<HTMLLIElement | null>(null);
    const [leaving, setLeaving] = useState(false);
    const more = usePresentFlag(open, DETAIL_MS);
    const { item, what } = drop;
    const key = item.key;
    const arrival = useArrival(key);
    const away = useCallback(() => onFlown(key), [onFlown, key]);
    useSwipe(el, !leaving && !gone && !flown, away);
    useEffect(() => {
      if (!leaving) return;
      const timer = window.setTimeout(away, LEAVE_MS);
      return () => window.clearTimeout(timer);
    }, [leaving, away]);
    const toggle = () => onToggle(key);
    const back = () => onBack(key, el.current?.getBoundingClientRect() ?? null, snap(el.current));
    const machine = item.kind === SMART_KIND.machine;
    // A wait of a chat (a permission, a question, a menu, a plan) shows what it asks and the answer right in the card,
    // from the first draw and with no fold: the row leads to the chat, the body answers, and the kind of the
    // wait is said once, by the body. Any other card opens in place as before.
    const gate = renderAnswer !== undefined && inPlace?.(drop) === true;
    const answer = gate
      ? renderAnswer(drop, true, toggle)
      : more.mounted && renderAnswer
        ? renderAnswer(drop, open, toggle)
        : null;
    // Where the stack knows what is answered in place (the column's), a card's row leads to its chat at a press.
    const direct = inPlace !== undefined && !machine && item.chatId !== null && onOpenChat !== undefined;
    return (
      <li
        ref={el}
        className="hx-ns__card"
        data-flip={key}
        data-drop-key={key}
        data-kind={item.kind}
        data-gate={gate || undefined}
        data-open={open || undefined}
        data-leaving={flown ? 'flown' : leaving || gone || undefined}
        data-arriving={arrival.arriving || undefined}
        data-landed={arrival.landed || undefined}
        data-i={Math.min(index, STAGGER_STEPS)}
      >
        <div className="hx-ns__line">
          <button
            type="button"
            className="hx-ns__hit"
            aria-expanded={direct ? undefined : open}
            onClick={direct ? () => onOpenChat?.(item.chatId as string) : toggle}
          >
            <Chip item={item} now={now} sent={false} />
            <span className="hx-ns__lines">
              <span className="hx-ns__t1">
                <b className="hx-ns__title">{item.title}</b>
                <span className="hx-ns__when">{formatAgo(item.at, now)}</span>
              </span>
              {!(gate && answer !== null) && <span className="hx-ns__text">{item.text}</span>}
            </span>
          </button>
          <button
            type="button"
            className="hx-ns__x"
            aria-label={t('next.smart.stack.remove')}
            title={t('next.smart.stack.remove')}
            onClick={() => setLeaving(true)}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
        {gate && answer !== null && (
          <div className="hx-ns__body" data-no-swipe>
            {answer}
          </div>
        )}
        <div
          className="hx-ns__fold"
          data-state={more.mounted && more.state === PRESENCE.closing ? 'closing' : undefined}
        >
          <div className="hx-ns__inner" data-no-swipe>
            {more.mounted && (
              <div className="hx-ns__more">
                {what !== null && what !== item.text && <p className="hx-ns__what">{what}</p>}
                {item.summary && <p className="hx-ns__what">{item.summary}</p>}
                {!gate && answer}
                <div className="hx-ns__acts">
                  {answer === null && (
                    <button type="button" className="hx-ns__btn" data-main onClick={back}>
                      {t(machine ? 'next.smart.stack.details' : 'next.smart.answer')}
                    </button>
                  )}
                  {item.chatId !== null && onOpenChat && (
                    <button
                      type="button"
                      className="hx-ns__btn"
                      onClick={() => onOpenChat(item.chatId as string)}
                    >
                      {t('next.smart.stack.openChat')}
                    </button>
                  )}
                </div>
              </div>
            )}
          </div>
        </div>
      </li>
    );
  },
);
Card.displayName = 'StackCard';

/** The cards by chat, in the order their newest came: a chat's cards stand together under its name. */
const byChat = (shown: readonly IShown[]): IShown[][] => {
  const groups = new Map<string, IShown[]>();
  for (const s of shown) {
    const key = s.drop.item.chatId ?? s.drop.item.key;
    const group = groups.get(key);
    if (group) group.push(s);
    else groups.set(key, [s]);
  }
  return [...groups.values()];
};

const NoticeStack = ({
  drops,
  now,
  label,
  icon,
  onBack,
  onOpenChat,
  onDismiss,
  renderAnswer,
  inPlace,
}: INoticeStackProps) => {
  // Taken off by the person: flown already, so it is drawn gone at once, then leaves the list without a second flight.
  const [flown, setFlown] = useState<ReadonlySet<string>>(NO_KEYS);
  const shown = useLeaving(drops, flown);
  const [expanded, setExpanded] = useState(false);
  const [openKey, setOpenKey] = useState<string | null>(null);
  const list = useRef<HTMLUListElement | null>(null);
  const live = shown.filter((s) => !s.gone);
  const pile = live.length > 1;
  // The list folds back with its own animation only when the person folds it. One card has no stack to fold into,
  // and a second one coming makes the stack at once: the card does not fold away first.
  const byHand = usePresentFlag(expanded, FOLD_MS);
  const spread = !pile || byHand.mounted;
  const folding = pile && byHand.mounted && byHand.state === PRESENCE.closing;
  // The stack that took the place of a lone card stands there already: no fade in.
  const [was, setWas] = useState({ spread, pile, calm: false });
  if (was.spread !== spread || was.pile !== pile)
    setWas({ spread, pile, calm: spread ? false : !was.pile || was.calm });
  useFlip(list, spread);
  // A stack of one left folds by itself, so the next one that comes starts folded.
  if (expanded && live.length <= 1) setExpanded(false);
  const minute = Math.floor(now / MINUTE_MS) * MINUTE_MS;
  // A card opened by itself (below) folds by itself; one the person opened, folded or pointed at is theirs.
  const [autoKey, setAutoKey] = useState<string | null>(null);
  const toggle = useCallback(
    (key: string) => {
      setAutoKey(null);
      setOpenKey((was) => (was === key ? null : key));
    },
    [setAutoKey],
  );
  const mine = () => setAutoKey(null);
  // An urgent card (a machine about to fail its work) opens in place for a few seconds and folds again, seldom
  // (features/notifications/auto-open.ts): what is wrong is read without a press.
  const urgent = spread ? live.find((s) => s.drop.item.urgent)?.drop.item : undefined;
  const urgentKey = urgent?.key ?? null;
  const urgentId = urgent?.machine?.id ?? urgentKey;
  useEffect(() => {
    if (urgentKey === null || urgentId === null) return;
    // Once it has landed in its place: a card that opens while it still arrives reads as a jump.
    const timer = window.setTimeout(() => {
      if (document.visibilityState !== 'visible' || !takeAutoOpen(urgentId, true, Date.now())) return;
      setAutoKey(urgentKey);
      setOpenKey(urgentKey);
    }, AUTO_OPEN_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [urgentKey, urgentId]);
  useEffect(() => {
    if (autoKey === null) return;
    const timer = window.setTimeout(() => {
      setAutoKey(null);
      setOpenKey((was) => (was === autoKey ? null : was));
    }, AUTO_OPEN_MS);
    return () => window.clearTimeout(timer);
  }, [autoKey]);
  const onFlown = useCallback((key: string) => setFlown((was) => new Set(was).add(key)), []);
  // Told once the card is drawn gone, so the list that comes back without it never shows it again.
  const flownOut = [...flown].filter((key) => drops.some((d) => d.item.key === key)).join('\n');
  useEffect(() => {
    if (flownOut !== '') onDismiss(flownOut.split('\n'));
  }, [flownOut, onDismiss]);
  // What is not there any more need not be remembered as flown.
  const [flownFor, setFlownFor] = useState(drops);
  if (flownFor !== drops) {
    setFlownFor(drops);
    const keys = new Set(drops.map((d) => d.item.key));
    if ([...flown].some((key) => !keys.has(key)))
      setFlown(new Set([...flown].filter((key) => keys.has(key))));
  }
  const topKey = live[0]?.drop.item.key ?? '';
  const topArrival = useArrival(topKey);
  if (shown.length === 0) return null;

  const top = live[0] ?? shown[0];
  const tick = (drop: IDrop) => (drop.item.until !== null ? now : minute);
  let index = 0;
  const cards = byChat(shown).map((group) => {
    const head = group[0].drop.item;
    const many = group.filter((s) => !s.gone).length > 1;
    return (
      <li key={`g:${head.chatId ?? head.key}`} className="hx-ns__group">
        {many && (
          <span className="hx-ns__gname" data-flip={`gn:${head.chatId ?? head.key}`}>
            {head.title}
            <span className="hx-ns__gcount">{group.length}</span>
          </span>
        )}
        <ul className="hx-ns__cards">
          {group.map((s) => (
            <Card
              key={s.drop.item.key}
              drop={s.drop}
              now={tick(s.drop)}
              open={openKey === s.drop.item.key && !s.gone}
              gone={s.gone}
              flown={flown.has(s.drop.item.key)}
              index={index++}
              onToggle={toggle}
              onBack={onBack}
              onOpenChat={onOpenChat}
              onFlown={onFlown}
              renderAnswer={renderAnswer}
              inPlace={inPlace}
            />
          ))}
        </ul>
      </li>
    );
  });

  return (
    <section
      className="hx-ns"
      data-state={spread ? 'spread' : 'folded'}
      aria-label={label}
      onPointerEnter={mine}
      onFocus={mine}
    >
      <div className="hx-ns__head">
        {icon && <Icon name={icon} size="sm" className="hx-ns__hicon" />}
        <span className="hx-ns__label">{label}</span>
        {live.length > 1 && <span className="hx-ns__count">{live.length}</span>}
        <span className="hx-ns__tools">
          {expanded && pile && (
            <button type="button" className="hx-ns__link" onClick={() => setExpanded(false)}>
              {t('next.smart.stack.collapse')}
            </button>
          )}
          {/* «Очистить всё» is for a group: one card has its own «Убрать». */}
          {live.length > 1 && (
            <button
              type="button"
              className="hx-ns__link"
              onClick={() => onDismiss(live.map((s) => s.drop.item.key))}
            >
              {t('next.smart.stack.clearAll')}
            </button>
          )}
        </span>
      </div>
      {spread ? (
        <ul ref={list} className="hx-ns__list" data-folding={folding || undefined}>
          {cards}
        </ul>
      ) : (
        <button
          type="button"
          className="hx-ns__pile"
          data-calm={was.calm || undefined}
          aria-label={t('next.smart.stack.expand', { n: live.length })}
          onClick={() => setExpanded(true)}
        >
          <span
            className="hx-ns__face"
            data-drop-key={top.drop.item.key}
            data-arriving={(topArrival.arriving && top.drop.item.key === topKey) || undefined}
            data-landed={(topArrival.landed && top.drop.item.key === topKey) || undefined}
          >
            <Chip item={top.drop.item} now={tick(top.drop)} sent={false} />
            <span className="hx-ns__lines">
              <span className="hx-ns__t1">
                <b className="hx-ns__title">{top.drop.item.title}</b>
                <span className="hx-ns__when">{formatAgo(top.drop.item.at, minute)}</span>
                <span className="hx-ns__rest">{t('next.smart.stack.more', { n: live.length - 1 })}</span>
              </span>
              <span className="hx-ns__text">{top.drop.item.text}</span>
            </span>
          </span>
          {live.slice(1, UNDER + 1).map((s, i) => (
            <span key={s.drop.item.key} className="hx-ns__under" data-depth={i + 1} aria-hidden="true" />
          ))}
        </button>
      )}
    </section>
  );
};

export default NoticeStack;
