// The page of the day on the start page's stage, drawn by the host itself from the feed an app's view names
// (`view.feed`): the digest, the calendar strip, the priorities, and a thing opened into its chat. The greeting of the
// day goes to the shell's own greeting over the field (onGreet), not here. The feed's site signs the person in in a
// window of its own; the page asks for the day again when that window closes.
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { openConversation } from '@/next/features/app-views/agents';
import { cardChatOf, chatOf, itemChatOf } from '@/next/features/day/chat-link';
import { sendMessage } from '@/next/commands';
import type { MachineId } from '@/next/protocol/v2';
import {
  ACTION,
  actOnItem,
  CHIP,
  FEED_STATUS,
  FeedError,
  isLogin,
  loadCard,
  loadDay,
  loginUrl,
  SECRET_NEED,
  secretNeed,
  sameItem,
  SLOT,
  type IActResult,
  type IDay,
  type IDayCard,
  type IDayChip,
  type IDayRow,
  type TItemId,
  type TRichPart,
  type TSecretNeed,
  webUrl,
} from '@/next/features/day/feed';
import { DayListen, DayTalk, useDayVoice, type IDayVoice } from '@/next/features/day/day-voice';
import { dayPhrases, type IDayPhrase } from '@/next/features/day/digest-phrases';
import ItemChat from '@/next/features/day/item-chat';
import Priorities from '@/next/features/day/priorities';
import { RichLine, type IRichActions } from '@/next/features/day/rich';
import { slotKey } from '@/next/features/day/strip';
import Timeline from '@/next/features/day/timeline';
import {
  capitalize,
  dayClock,
  leftText,
  PART,
  type IDayClock,
  type TPartOfDay,
} from '@/next/features/day/time';
import { DIGEST_ITEM } from '@/next/features/voice-live/digest-line';
import { DIGEST_PHRASE, DIGEST_READING, DIGEST_VOICE } from '@/next/features/voice-live/digest-listen';
import { t } from '@/next/i18n';
import { goTo, ROUTE } from '@/next/routes/routes';
import { usePresence } from '@/next/ui/presence';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import { toast } from '@/next/ui/toast';
import { useStableFn } from '@/next/ui/use-stable-fn';

/** The day is asked for again this often while the page is in sight and no thing is open. */
const POLL_MS = 5 * 60 * 1000;
/** The «now» line and what has passed follow the clock this closely. */
const TICK_MS = 30_000;
const ITEM_EXIT_MS = 340;
const LOGIN_WATCH_MS = 500;
const FLASH_MS = 1200;
/** The air left over a block the page scrolls to. */
const PAGE_GAP_PX = 8;
/** Stands for the meeting's name inside a translated line, so the link goes where the language puts the name. */
const MEETING_MARK = '\u0001';

const LOAD = {
  loading: 'loading',
  ready: 'ready',
  login: 'login',
  secret: 'secret',
  failed: 'failed',
} as const;
type TLoad =
  | { k: typeof LOAD.loading }
  | { k: typeof LOAD.ready; day: IDay }
  | { k: typeof LOAD.login }
  | { k: typeof LOAD.secret; need: TSecretNeed }
  | { k: typeof LOAD.failed };

interface IOpenItem {
  card: IDayCard;
  origin: DOMRect | null;
}

/** What a refusal of the feed puts on the page in place of the day; null for a failure told another way. */
const standsFor = (error: unknown, feed: string): TLoad | null => {
  if (isLogin(error, feed)) return { k: LOAD.login };
  const need = secretNeed(error);
  return need === null ? null : { k: LOAD.secret, need };
};

export interface IDayStageProps {
  /** The app whose view names the feed: its card is where the machine's token for the feed is put. */
  app: { id: string; name: string };
  /** The https address of the day's feed, or its path through the machine's runtime (`feedPath`). */
  feed: string;
  /** The greeting of the day for the shell's own greeting; an empty title gives the host's question back. */
  onGreet: (title: string, sub: ReactNode) => void;
  /** The clock; a test stands it still. */
  clockNow?: () => number;
  /** The page's machine: an agent's chat of a thing that runs on it opens inside the thing. */
  machine?: MachineId | null;
}

const smooth = (): ScrollBehavior =>
  window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth';

/** Brings a block of the page to the top of the page's own scroll, and moves nothing else. */
const scrollDown = (el: HTMLElement) => {
  const page = el.closest<HTMLElement>('.hx-day__scroll');
  if (!page) return;
  const top =
    page.scrollTop + el.getBoundingClientRect().top - page.getBoundingClientRect().top - PAGE_GAP_PX;
  page.scrollTo({ top: Math.max(0, top), behavior: smooth() });
};

/** Brings a block into sight inside the page's own scroll when it is out of it, and moves nothing else. */
const scrollInto = (el: HTMLElement) => {
  const page = el.closest<HTMLElement>('.hx-day__scroll');
  if (!page) return;
  const at = el.getBoundingClientRect();
  const box = page.getBoundingClientRect();
  if (at.top >= box.top + PAGE_GAP_PX && at.bottom <= box.bottom - PAGE_GAP_PX) return;
  const top = page.scrollTop + at.top - box.top - (box.height - at.height) / 2;
  page.scrollTo({ top: Math.max(0, top), behavior: smooth() });
};

const Rows = ({
  title,
  rows,
  onItem,
}: {
  title: string;
  rows: IDayRow[] | undefined;
  onItem: IRichActions['onItem'];
}) => {
  if (!rows || rows.length === 0) return null;
  return (
    <div className="hx-day-tm__c">
      <h4>{title}</h4>
      {rows.map((row, i) => {
        const id = row.itemId;
        const body = (
          <>
            <time>{row.time ?? ''}</time>
            <span>
              <b>{row.title}</b>
              {row.sub && <span>{row.sub}</span>}
            </span>
          </>
        );
        return id === undefined ? (
          <div key={i} className="hx-day-tm__r">
            {body}
          </div>
        ) : (
          <button
            key={i}
            type="button"
            className="hx-day-tm__r"
            onClick={(event) => onItem(id, event.currentTarget)}
          >
            {body}
          </button>
        );
      })}
    </div>
  );
};

/** The line the digest shows now: in the evening, tomorrow's; else today's. */
const digestLine = (day: IDay, part: TPartOfDay): TRichPart[] => {
  const line = part === PART.pm && day.tomorrow ? day.tomorrow : day.digest;
  if (line?.rich) return line.rich;
  return line?.text ? [line.text] : [];
};

/** The digest's words, phrase by phrase: the one the voice says is lit. */
const Phrases = ({
  phrases,
  cur,
  actions,
}: {
  phrases: readonly IDayPhrase[];
  cur: number;
  actions: IRichActions;
}) => (
  <p>
    {phrases.map((ph, i) => (
      <span key={i} className={DIGEST_PHRASE} data-cur={i === cur || undefined}>
        <RichLine parts={ph.parts} tone="digest" {...actions} />
      </span>
    ))}
  </p>
);

/** The day in a few words: in the morning, today; in the evening, what tomorrow brings. */
const Digest = ({
  day,
  clock,
  now,
  actions,
  phrases,
  dv,
  onTalkOpen,
}: {
  day: IDay;
  clock: IDayClock;
  now: number;
  actions: IRichActions;
  phrases: readonly IDayPhrase[];
  /** The voice that reads the digest; null where the page has no machine to call through. */
  dv: IDayVoice | null;
  onTalkOpen: (anchor: string) => void;
}) => {
  const part = clock.part(now);
  const tomorrow = day.tomorrow;
  const reading = dv !== null && dv.voice.state !== DIGEST_VOICE.off;
  const voiceAttr = { [DIGEST_READING]: reading || undefined };
  const cur = reading && dv ? dv.voice.index : -1;
  const head = (title: ReactNode) => (
    <div className="hx-day-sum__h">
      <span className="hx-day-sum__t">{title}</span>
      {dv && phrases.length > 0 && <DayListen dv={dv} />}
    </div>
  );
  const talk = dv && <DayTalk dv={dv} onOpen={onTalkOpen} />;
  if (part === PART.pm && tomorrow) {
    return (
      <section className="hx-day-sum" {...voiceAttr}>
        {head(tomorrow.title ?? t('next.day.tomorrow'))}
        {phrases.length > 0 && <Phrases phrases={phrases} cur={cur} actions={actions} />}
        {talk}
        <div className="hx-day-tm">
          <Rows title={t('next.day.meetings')} rows={tomorrow.meetings} onItem={actions.onItem} />
          <Rows title={t('next.day.promises')} rows={tomorrow.promises} onItem={actions.onItem} />
        </div>
      </section>
    );
  }
  if (phrases.length === 0) return null;
  return (
    <section className="hx-day-sum" {...voiceAttr}>
      {head(t(`next.day.digest.${part}`, { weekday: capitalize(clock.weekday(now)) }))}
      <Phrases phrases={phrases} cur={cur} actions={actions} />
      {talk}
    </section>
  );
};

const DayStage = ({ app, feed, onGreet, clockNow = Date.now, machine = null }: IDayStageProps) => {
  const [load, setLoad] = useState<TLoad>({ k: LOAD.loading });
  const [now, setNow] = useState(clockNow);
  const [item, setItem] = useState<IOpenItem | null>(null);
  const [busy, setBusy] = useState(false);
  const shown = usePresence(item, ITEM_EXIT_MS);
  const rootRef = useRef<HTMLDivElement>(null);
  const [slotEls] = useState(() => new Map<string, HTMLElement>());
  const opening = useRef(0);
  const loading = useRef(false);
  const loginWatch = useRef<ReturnType<typeof setInterval> | null>(null);
  const greet = useStableFn(onGreet);

  const reload = useStableFn(async () => {
    if (loading.current) return;
    loading.current = true;
    try {
      const day = await loadDay(feed);
      setLoad({ k: LOAD.ready, day });
    } catch (error) {
      const stands = standsFor(error, feed);
      if (stands) setLoad(stands);
      // A day already on the page stays: the next ask may well succeed.
      else setLoad((had) => (had.k === LOAD.ready ? had : { k: LOAD.failed }));
    } finally {
      loading.current = false;
    }
  });

  const itemOpen = item !== null;
  useEffect(() => {
    void reload();
  }, [feed, reload]);
  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden && !itemOpen) void reload();
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [itemOpen, reload]);
  useEffect(() => {
    const timer = setInterval(() => setNow(clockNow()), TICK_MS);
    return () => clearInterval(timer);
  }, [clockNow]);
  useEffect(
    () => () => {
      if (loginWatch.current !== null) clearInterval(loginWatch.current);
    },
    [],
  );

  const day = load.k === LOAD.ready ? load.day : null;
  const clock = useMemo(() => dayClock(day?.tz), [day?.tz]);

  const openItem = useStableFn(async (id: TItemId, from: HTMLElement) => {
    const origin = from.isConnected ? from.getBoundingClientRect() : null;
    opening.current += 1;
    const turn = opening.current;
    try {
      const card = await loadCard(feed, id);
      if (turn === opening.current) setItem({ card, origin });
    } catch (error) {
      const stands = standsFor(error, feed);
      if (stands) setLoad(stands);
      else toast(t('next.day.openFailed'), { tone: 'error' });
    }
  });

  const actions = useMemo<IRichActions>(
    () => ({
      onItem: (id, from) => void openItem(id, from),
      onSlot: (key) => {
        const el = slotEls.get(key);
        const slots = day ? [day.calendar, ...(day.calendar.days ?? [])].flatMap((c) => c.slots ?? []) : [];
        const slot = slots.find((s) => slotKey(s) === key);
        if (slot?.itemId !== undefined && rootRef.current) {
          void openItem(slot.itemId, el ?? rootRef.current);
          return;
        }
        if (!el) return;
        // Only the strip moves sideways and only the page of the day moves down: scrollIntoView would move every
        // ancestor, the stage and the window with it, and a phone's screen would stay shifted.
        const strip = el.closest<HTMLElement>('.hx-day-k');
        if (strip) {
          const at = el.getBoundingClientRect();
          const box = strip.getBoundingClientRect();
          strip.scrollTo({
            left: strip.scrollLeft + at.left - box.left - (box.width - at.width) / 2,
            behavior: smooth(),
          });
          scrollDown(strip);
        }
        el.focus({ preventScroll: true });
      },
      onJump: (to) => {
        const el = rootRef.current?.querySelector<HTMLElement>(`#hx-day-${to}`);
        if (!el) return;
        scrollDown(el);
        el.dataset.flash = '';
        setTimeout(() => delete el.dataset.flash, FLASH_MS);
      },
    }),
    [day, openItem, slotEls],
  );

  // The digest read out by voice: the phrases are the words the digest shows now.
  const digestPart = day ? clock.part(now) : null;
  const phrases = useMemo(
    () => (day && digestPart !== null ? dayPhrases(digestLine(day, digestPart)) : []),
    [day, digestPart],
  );
  const dayVoice = useDayVoice(phrases, machine ?? '');
  const talked = dayVoice.voice.item;
  useEffect(() => {
    if (talked === null) return;
    const el = rootRef.current?.querySelector<HTMLElement>(`.${DIGEST_ITEM}[data-digest-voice='true']`);
    if (el) scrollInto(el);
  }, [talked]);
  const openTalked = (anchor: string) => {
    const id = phrases.find((ph) => ph.anchor === anchor)?.item;
    if (id === undefined || id === null || !rootRef.current) return;
    const el = rootRef.current.querySelector<HTMLElement>(`.${DIGEST_ITEM}[data-digest-voice='true']`);
    void openItem(id, el ?? rootRef.current);
  };

  // The greeting of the day: the part of the day and the person's name, under it the feed's headline with its links.
  const part = day ? clock.part(now) : null;
  const next = day
    ? (day.calendar.slots ?? [])
        .filter((s) => s.kind === SLOT.meet && s.offer !== true && Date.parse(s.start) > now)
        .sort((a, b) => Date.parse(a.start) - Date.parse(b.start))[0]
    : undefined;
  const nextLeft = next ? leftText(Date.parse(next.start) - now) : '';
  useEffect(() => {
    if (!day || part === null) {
      greet('', '');
      return;
    }
    const title = day.name
      ? t(`next.day.hello.${part}Name`, { name: day.name })
      : t(`next.day.hello.${part}`);
    const headline = day.headline;
    if (headline?.rich) {
      greet(title, <RichLine parts={headline.rich} tone="hello" {...actions} />);
      return;
    }
    const quoted = headline?.text ? /^(.*?)«([^»]+)»(.*)$/.exec(headline.text) : null;
    const words: TRichPart[] = [];
    if (quoted && headline?.itemId !== undefined)
      words.push(quoted[1] ?? '', { t: quoted[2] ?? '', item: headline.itemId }, quoted[3] ?? '');
    else if (headline?.text) words.push(headline.text.replace(/[«»]/g, ''));
    if (next) {
      // «25 min until «Standup»»: the words around the meeting's name are the language's own.
      const [before = '', after = ''] = t('next.day.hello.next', {
        meeting: MEETING_MARK,
        left: nextLeft,
      }).split(MEETING_MARK);
      if (words.length > 0) words.push(', ');
      words.push(before, { t: `«${next.title}»`, slot: slotKey(next) }, after);
    }
    greet(title, words.length > 0 ? <RichLine parts={words} tone="hello" {...actions} /> : '');
  }, [day, part, next, nextLeft, actions, greet]);
  useEffect(() => () => greet('', ''), [greet]);

  const signIn = () => {
    const popup = window.open(loginUrl(feed), 'hl-day-login', 'width=520,height=680');
    if (!popup) {
      toast(t('next.day.login.blocked'), { tone: 'error' });
      return;
    }
    if (loginWatch.current !== null) clearInterval(loginWatch.current);
    loginWatch.current = setInterval(() => {
      if (!popup.closed) return;
      if (loginWatch.current !== null) clearInterval(loginWatch.current);
      loginWatch.current = null;
      void reload();
    }, LOGIN_WATCH_MS);
  };

  const openChat = (named: IActResult & { chat?: string }) => {
    const chat = chatOf(named);
    if (chat !== null) {
      openConversation(chat);
      return;
    }
    // A chat this page cannot open (another machine's, another hub's) opens at its address, beside the day.
    const url = webUrl(named.run?.url ?? named.url);
    if (url) window.open(url, '_blank', 'noopener');
  };

  const onChip = async (chip: IDayChip, say?: string) => {
    if (item === null || busy) return;
    const id = item.card.itemId;
    if (chip.kind === CHIP.open) {
      const url = webUrl(chip.url);
      if (url) window.open(url, '_blank', 'noopener');
      return;
    }
    // A thing whose agent's chat stands open under it: what the person says about it goes into that chat.
    const here = machine === null ? null : cardChatOf(item.card, machine);
    const words = say ?? chip.say ?? chip.label;
    if (here !== null && (chip.kind === CHIP.ask || chip.kind === CHIP.say)) {
      sendMessage(here.machine, { conversationId: here.conv, text: words });
      return;
    }
    setBusy(true);
    try {
      let result: IActResult = {};
      let told = t('next.day.told.taken');
      if (chip.kind === CHIP.send) {
        result = await actOnItem(feed, id, ACTION.send);
        told = t('next.day.told.sent');
      } else if (chip.kind === CHIP.ask || chip.kind === CHIP.say) {
        result = await actOnItem(feed, id, ACTION.ask, { say: words });
      } else if (chip.kind === CHIP.take) {
        result = await actOnItem(feed, id, ACTION.take);
      } else {
        result = await actOnItem(feed, id, ACTION.feedback, { verdict: chip.kind });
        told = t(chip.kind === CHIP.done ? 'next.day.told.done' : 'next.day.told.notMine');
        setItem(null);
      }
      toast(told, { tone: 'success' });
      // The chat the agent took the thing into opens under its widget, in place; one of another machine at its address.
      const inPlace = machine === null ? null : itemChatOf(result, machine);
      if (inPlace !== null && chip.kind !== CHIP.done && chip.kind !== CHIP.notMine)
        setItem((was) =>
          was !== null && sameItem(was.card.itemId, id)
            ? { ...was, card: { ...was.card, run: { chat: inPlace.conv } } }
            : was,
        );
      else openChat(result);
      void reload();
    } catch (error) {
      const stands = standsFor(error, feed);
      if (stands) {
        setItem(null);
        setLoad(stands);
      } else if (error instanceof FeedError && error.status === FEED_STATUS.forbidden)
        toast(t('next.day.told.forbidden'), { tone: 'error' });
      else toast(t('next.day.told.failed'), { tone: 'error' });
    } finally {
      setBusy(false);
    }
  };

  const close = useStableFn(() => setItem(null));
  const open = shown.value;

  return (
    <div ref={rootRef} className="hx-day" data-item={open ? shown.state : undefined}>
      {load.k === LOAD.loading && (
        <Delayed>
          <div className="hx-day__sk" aria-busy="true">
            <Skeleton className="hx-day__sk-line" />
            <Skeleton className="hx-day__sk-strip" />
            <Skeleton className="hx-day__sk-card" />
            <Skeleton className="hx-day__sk-card" />
          </div>
        </Delayed>
      )}
      {load.k === LOAD.login && (
        <div className="hx-day__note">
          <p>{t('next.day.login.text')}</p>
          <button type="button" className="hx-day-chip" data-main="" onClick={signIn}>
            {t('next.day.login.button')}
          </button>
        </div>
      )}
      {load.k === LOAD.secret && (
        <div className="hx-day__note">
          <p>
            {t(load.need === SECRET_NEED.refused ? 'next.day.secret.refused' : 'next.day.secret.missing', {
              app: app.name,
            })}
          </p>
          <div className="hx-day__note-acts">
            <button
              type="button"
              className="hx-day-chip"
              data-main=""
              onClick={() => goTo({ kind: ROUTE.appAbout, id: app.id })}
            >
              {t('next.day.secret.open')}
            </button>
            <button type="button" className="hx-day-chip" onClick={() => void reload()}>
              {t('next.day.retry')}
            </button>
          </div>
        </div>
      )}
      {load.k === LOAD.failed && (
        <div className="hx-day__note">
          <p>{t('next.day.failed')}</p>
          <button type="button" className="hx-day-chip" onClick={() => void reload()}>
            {t('next.day.retry')}
          </button>
        </div>
      )}
      {day && (
        <div className="hx-day__scroll" data-home-scroll="" inert={open !== null}>
          <Digest
            day={day}
            clock={clock}
            now={now}
            actions={actions}
            phrases={phrases}
            dv={machine === null ? null : dayVoice}
            onTalkOpen={openTalked}
          />
          <Timeline day={day} clock={clock} now={now} slotEls={slotEls} onItem={actions.onItem} />
          <Priorities
            items={day.priorities ?? []}
            onItem={actions.onItem}
            voiceItem={machine === null ? null : dayVoice.voice.item}
          />
        </div>
      )}
      {open && (
        <ItemChat
          key={String(open.card.itemId)}
          card={open.card}
          priority={day?.priorities?.find((p) => sameItem(p.itemId, open.card.itemId))}
          state={shown.state}
          origin={open.origin}
          busy={busy}
          onClose={close}
          onChip={(chip, say) => void onChip(chip, say)}
          onGo={() => open.card.go && openChat(open.card.go)}
          chat={machine === null ? null : cardChatOf(open.card, machine)}
        />
      )}
    </div>
  );
};

export default DayStage;
