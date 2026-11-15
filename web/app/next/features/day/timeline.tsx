// The calendar strip of the day's page: the days in a row by the real clock. The «now» line stands on the present
// minute, what has passed fades, the strip opens on the present hour (in the evening, on the next day). Meetings at one
// time go in a stack, promises hang under them at their deadlines. A click in a free place puts the person's own entry
// there, and a meeting the agent proposes is dashed until the person says yes or no. Both are kept on this page only:
// the feed has nothing to write to a calendar with yet.
import {
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from 'react';
import {
  FLAG_WHO,
  SLOT,
  type IDay,
  type IDayFlag,
  type IDayMaterial,
  type IDayPrep,
  type IDaySlot,
  type TItemId,
  webUrl,
} from '@/next/features/day/feed';
import HoverCard, { CARD_PLACE } from '@/next/features/day/hover-card';
import { feedIcon } from '@/next/features/day/icons';
import { cssVars } from '@/next/features/day/place';
import {
  isFree,
  openingDay,
  OWN_MS,
  percentOf,
  slotKey,
  snapStart,
  stripDay,
  type IStripDay,
  type IStripEvent,
  type IStripFlag,
} from '@/next/features/day/strip';
import { capitalize, type IDayClock } from '@/next/features/day/time';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';

/** How far from the box's left edge the «now» line stands when the strip opens. */
const OPEN_AT_PX = 140;
/** The day the head names is the one under this point of the box. */
const HEAD_AT_PX = 90;
const LEAVE_MS = 160;
const OWN_TITLE_MAX = 40;

const OFFER = { held: 'held', leaving: 'leaving', gone: 'gone' } as const;
type TOffer = (typeof OFFER)[keyof typeof OFFER];

interface IOwnEntry {
  id: number;
  day: number;
  start: number;
  end: number;
  title: string;
}

interface ISpot {
  day: number;
  start: number;
}

type TOpenItem = (id: TItemId, from: HTMLElement) => void;

const onKeyPress = (run: (el: HTMLElement) => void) => (event: KeyboardEvent<HTMLElement>) => {
  if (event.key !== 'Enter' && event.key !== ' ') return;
  if (event.target !== event.currentTarget) return;
  event.preventDefault();
  run(event.currentTarget);
};

const unquote = (text: string) => text.replace(/^«|»$/g, '');

const flagIcon = (f: IStripFlag): string => {
  if (f.who === FLAG_WHO.agent) return 'sparkles';
  if (!f.reply) return 'user';
  const mail = f.flags.every((x) => x.icon === 'mail');
  return mail ? 'mail' : 'messages-square';
};

const flagWho = (f: IStripFlag, first: IDayFlag): string => {
  if (f.who === FLAG_WHO.agent)
    return t(first.kind === 'run' ? 'next.day.cal.agentDoes' : 'next.day.cal.agentPromised');
  return t(f.reply ? 'next.day.cal.waitsYou' : 'next.day.cal.youPromised');
};

/** «Today, friday», «Tomorrow, saturday», then the weekday alone. */
const dayLabel = (n: number, weekday: string): string => {
  if (n === 0) return t('next.day.cal.today', { weekday });
  if (n === 1) return t('next.day.cal.tomorrow', { weekday });
  return capitalize(weekday);
};

const Materials = ({ items }: { items: IDayMaterial[] }) => (
  <>
    {items.map((m, i) =>
      webUrl(m.url) ? (
        <a key={i} className="hx-day-mat" href={webUrl(m.url)} target="_blank" rel="noopener noreferrer">
          <Icon name={feedIcon(m.icon, 'link')} size="sm" />
          {m.label}
        </a>
      ) : (
        <span key={i} className="hx-day-mat">
          <Icon name={feedIcon(m.icon, 'link')} size="sm" />
          {m.label}
        </span>
      ),
    )}
  </>
);

/** What the agent gathered for a meeting or an answer. */
const Prep = ({ prep }: { prep: IDayPrep | undefined }) => {
  if (!prep) return null;
  return (
    <>
      {prep.quote && <p className="hx-day-pop__quote">«{unquote(prep.quote)}»</p>}
      {prep.cols && prep.cols.length > 0 && (
        <div className="hx-day-pop__cols">
          {prep.cols.map((col, i) => (
            <div key={i} className="hx-day-pop__col" data-k={col.tone ?? 'good'}>
              <span>{col.title}</span>
              <ul>
                {col.lines.map((line, j) => (
                  <li key={j}>{line}</li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
      {prep.ctx?.map((text, i) => (
        <p key={i} className="hx-day-pop__ctx">
          {text}
        </p>
      ))}
      {prep.note && (
        <p className="hx-day-pop__ctx">
          <span className="hx-day-pop__lbl">{prep.note.label}</span> {prep.note.text}
        </p>
      )}
      {prep.draft && (
        <p className="hx-day-pop__ctx">
          <span className="hx-day-pop__lbl">{t('next.day.cal.draft')}</span> {prep.draft}
        </p>
      )}
    </>
  );
};

interface IFootProps {
  mats: IDayMaterial[] | undefined;
  url: string | undefined;
  itemId: TItemId | undefined;
  onMore: (from: HTMLElement) => void;
}

const Foot = ({ mats, url, itemId, onMore }: IFootProps) => {
  const links: IDayMaterial[] = [
    ...(mats ?? []),
    ...(url ? [{ icon: 'link', label: t('next.day.cal.join'), url }] : []),
  ];
  if (links.length === 0 && itemId === undefined) return null;
  return (
    <div className="hx-day-pop__foot">
      <div className="hx-day-pop__mats">
        <Materials items={links} />
      </div>
      {itemId !== undefined && (
        <button type="button" className="hx-day-more" onClick={(event) => onMore(event.currentTarget)}>
          {t('next.day.more')}
          <Icon name="arrow-right" size="sm" />
        </button>
      )}
    </div>
  );
};

const Head = ({ title, meta }: { title: string; meta: string }) => (
  <div className="hx-day-pop__head">
    <b>{title}</b>
    {meta !== '' && <span>{meta}</span>}
  </div>
);

interface ITimelineProps {
  day: IDay;
  clock: IDayClock;
  /** The present moment; the page ticks it. */
  now: number;
  /** Where each slot is drawn, by its key: a link in the feed's text finds its slot here. */
  slotEls: Map<string, HTMLElement>;
  onItem: TOpenItem;
}

const Timeline = ({ day, clock, now, slotEls, onItem }: ITimelineProps) => {
  const days = useMemo(
    () => [day.calendar, ...(day.calendar.days ?? [])].map((c, n) => stripDay(c, n)),
    [day.calendar],
  );
  const boxRef = useRef<HTMLDivElement>(null);
  const opened = useRef(false);
  const seq = useRef(0);
  const settled = useRef(false);
  // The day the head names: the one the strip opens on, then the one the person has scrolled to.
  const [shown, setShown] = useState(() => openingDay(days, now));
  const [own, setOwn] = useState<IOwnEntry[]>([]);
  const [offers, setOffers] = useState<Record<string, TOffer>>({});
  const [draft, setDraft] = useState<ISpot | null>(null);
  const [ghost, setGhost] = useState<ISpot | null>(null);

  const slotMeta = (slot: IDaySlot): string => {
    if (slot.meta) return slot.meta;
    const span = `${clock.hm(Date.parse(slot.start))}–${clock.hm(Date.parse(slot.end))}`;
    if (slot.kind === SLOT.focus) return t('next.day.cal.focusMeta', { span });
    return slot.place ? `${span} · ${slot.place}` : span;
  };

  const flagMeta = (flag: IDayFlag, reply: boolean): string => {
    if (flag.meta) return flag.meta;
    const where = flag.where ?? '';
    return [
      flag.due ? t('next.day.cal.until', { time: clock.hm(Date.parse(flag.due)) }) : '',
      reply && where !== '' ? t('next.day.cal.waits', { where }) : where,
    ]
      .filter((x) => x !== '')
      .join(' · ');
  };

  // The strip opens on the present hour; in the evening and at night, on the next day. Once per page, not per reload.
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (!box || opened.current || box.clientWidth === 0) return;
    opened.current = true;
    const bodies = box.querySelectorAll<HTMLElement>(':scope > .hx-day-k__body');
    const start = openingDay(days, now);
    const body = bodies[start];
    const today = days[0];
    if (!body || !today) return;
    if (start > 0) {
      box.scrollLeft = body.offsetLeft - 8;
      return;
    }
    const at = (percentOf(today, Math.max(now, today.from)) / 100) * body.offsetWidth;
    box.scrollLeft = Math.max(0, body.offsetLeft + at - OPEN_AT_PX);
  }, [days, now]);

  // A promise's pill hangs left of its line; one without a deadline stands right of «now». Those that would run over
  // each other move apart in order.
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (!box) return;
    for (const body of box.querySelectorAll<HTMLElement>(':scope > .hx-day-k__body')) {
      const flags = [...body.querySelectorAll<HTMLElement>('.hx-day-flag')]
        .map((el) => ({ el, base: (Number(el.dataset.l) / 100) * body.offsetWidth }))
        .sort((a, b) => a.base - b.base);
      let edge = -Infinity;
      for (const { el, base } of flags) {
        const width = el.querySelector<HTMLElement>('.hx-day-flag__p')?.offsetWidth ?? 0;
        let x = base;
        if (el.dataset.nodue !== undefined) x += width + 12;
        if (x - width - 4 < edge + 8) x = edge + 8 + width + 4;
        el.style.setProperty('--x', `${Math.round(x)}px`);
        edge = x - 4;
      }
    }
  }, [days, now]);

  const onScroll = () => {
    const box = boxRef.current;
    if (!box) return;
    const at = box.getBoundingClientRect().left + HEAD_AT_PX;
    let current = 0;
    box.querySelectorAll<HTMLElement>(':scope > .hx-day-k__body').forEach((body, n) => {
      if (body.getBoundingClientRect().left <= at) current = n;
    });
    setShown(current);
  };

  const takenOf = (d: IStripDay) => [
    ...d.events.filter((e) => e.slots.some((s) => s.offer !== true || offers[slotKey(s)] !== OFFER.gone)),
    ...own.filter((o) => o.day === d.n),
  ];

  /** The free place under the pointer, or null: over a thing, on a day off, where something already stands. */
  const spotAt = (d: IStripDay, event: MouseEvent<HTMLElement> | PointerEvent<HTMLElement>): ISpot | null => {
    if (d.off || !(event.target instanceof Element)) return null;
    if (event.target.closest('.hx-day-e, .hx-day-flag, .hx-day-k__h span')) return null;
    const rect = event.currentTarget.getBoundingClientRect();
    const at = d.from + ((event.clientX - rect.left) / rect.width) * (d.to - d.from);
    const start = snapStart(d, at);
    return isFree(takenOf(d), start, start + OWN_MS) ? { day: d.n, start } : null;
  };

  const sameSpot = (a: ISpot | null, b: ISpot | null) => a?.day === b?.day && a?.start === b?.start;

  const decide = (key: string, yes: boolean) => {
    if (yes) {
      setOffers((had) => ({ ...had, [key]: OFFER.held }));
      return;
    }
    setOffers((had) => ({ ...had, [key]: OFFER.leaving }));
    setTimeout(() => setOffers((had) => ({ ...had, [key]: OFFER.gone })), LEAVE_MS);
  };

  // Enter saves and Esc drops; the blur that follows either must not save a second time.
  const endDraft = (title: string | null) => {
    if (draft === null || settled.current) return;
    settled.current = true;
    const text = title?.trim() ?? '';
    if (text !== '') {
      seq.current += 1;
      const id = seq.current;
      setOwn((had) => [...had, { id, ...draft, end: draft.start + OWN_MS, title: text }]);
    }
    setDraft(null);
  };
  const startDraft = (spot: ISpot) => {
    settled.current = false;
    setDraft(spot);
  };

  const register = (slots: IDaySlot[]) => (el: HTMLElement | null) => {
    for (const slot of slots) {
      if (el) slotEls.set(slotKey(slot), el);
      else slotEls.delete(slotKey(slot));
    }
  };

  const drawEvent = (d: IStripDay, e: IStripEvent): ReactNode => {
    const first = e.slots[0];
    if (!first) return null;
    const stack = e.slots.length > 1;
    const decided = first.offer === true ? offers[slotKey(first)] : undefined;
    if (decided === OFFER.gone) return null;
    const offered = first.offer === true && decided !== OFFER.held;
    const focus = first.kind === SLOT.focus;
    const place = cssVars({ '--l': `${e.left}%`, '--w': `${e.width}%` });
    const open = (from: HTMLElement) => {
      if (!stack && !offered && first.itemId !== undefined) onItem(first.itemId, from);
    };
    const card = (close: () => void): ReactNode => {
      if (offered)
        return (
          <>
            <Head title={t('next.day.cal.offer')} meta={slotMeta(first)} />
            <p className="hx-day-pop__ctx">{first.why ?? first.title}</p>
            <div className="hx-day-pop__acts">
              <button
                type="button"
                className="hx-day-chip"
                data-main=""
                onClick={() => {
                  close();
                  decide(slotKey(first), true);
                }}
              >
                {t('next.day.cal.offerYes')}
              </button>
              <button
                type="button"
                className="hx-day-chip"
                onClick={() => {
                  close();
                  decide(slotKey(first), false);
                }}
              >
                {t('next.day.cal.offerNo')}
              </button>
            </div>
          </>
        );
      if (stack)
        return e.slots.map((slot) => {
          const more = slot.prep !== undefined || slot.url !== undefined;
          const pick = (from: HTMLElement) => {
            if (slot.itemId === undefined) return;
            close();
            onItem(slot.itemId, from);
          };
          return (
            <HoverCard
              key={slotKey(slot)}
              place={CARD_PLACE.beside}
              disabled={!more}
              card={() => (
                <>
                  <Prep prep={slot.prep} />
                  <Foot mats={slot.prep?.mats} url={slot.url} itemId={undefined} onMore={pick} />
                </>
              )}
            >
              {() => (
                <div
                  className="hx-day-pop__item"
                  data-pick={slot.itemId !== undefined ? '' : undefined}
                  role={slot.itemId !== undefined ? 'button' : undefined}
                  tabIndex={0}
                  onClick={(event) => pick(event.currentTarget)}
                  onKeyDown={onKeyPress(pick)}
                >
                  <Head title={slot.title} meta={slotMeta(slot)} />
                </div>
              )}
            </HoverCard>
          );
        });
      const prep = first.prep ?? (focus ? { ctx: [t('next.day.cal.focusNote')] } : undefined);
      return (
        <>
          <Head title={first.title} meta={slotMeta(first)} />
          <Prep prep={prep} />
          <Foot
            mats={first.prep?.mats}
            url={first.url}
            itemId={first.itemId}
            onMore={(from) => {
              close();
              open(from);
            }}
          />
        </>
      );
    };
    const wide = !stack && !offered && (first.prep?.cols?.length ?? 0) > 0;
    return (
      <HoverCard
        key={e.key}
        place={stack ? CARD_PLACE.over : CARD_PLACE.under}
        className={[wide ? 'hx-day-pop--wide' : '', stack ? 'hx-day-pop--list' : '']
          .filter(Boolean)
          .join(' ')}
        card={card}
      >
        {(toggle) => {
          const press = (from: HTMLElement) => {
            if (!stack && !offered && first.itemId !== undefined) open(from);
            else toggle();
          };
          return (
            <div
              ref={(el) => {
                place(el);
                register(e.slots)(el);
              }}
              className="hx-day-e"
              data-k={focus ? SLOT.focus : SLOT.meet}
              data-short={e.short ? '' : undefined}
              data-stack={stack ? '' : undefined}
              data-offer={offered ? (decided ?? '') : undefined}
              data-in={decided === OFFER.held ? '' : undefined}
              data-past={e.end <= now ? '' : undefined}
              role="button"
              tabIndex={0}
              onClick={(event) => press(event.currentTarget)}
              onKeyDown={onKeyPress(press)}
            >
              <div className="hx-day-e__in">
                {!focus && <Icon name={offered ? 'sparkles' : 'users'} size="sm" className="hx-day-e__ic" />}
                <b>{first.short ?? first.title}</b>
                {stack && <i className="hx-day-e__more">+{e.slots.length - 1}</i>}
              </div>
            </div>
          );
        }}
      </HoverCard>
    );
  };

  const drawFlag = (d: IStripDay, f: IStripFlag): ReactNode => {
    const first = f.flags[0];
    if (!first) return null;
    const pile = f.flags.length > 1;
    const icon = flagIcon(f);
    const who = flagWho(f, first);
    const left = percentOf(d, f.due ?? Math.min(Math.max(now, d.from), d.to));
    const rich = !pile && (f.reply || first.prep !== undefined);
    const open = (from: HTMLElement) => {
      if (!pile && first.itemId !== undefined) onItem(first.itemId, from);
    };
    const flag = (toggle?: () => void) => (
      <div
        ref={cssVars({ '--l': `${left}%` })}
        className="hx-day-flag"
        data-l={left}
        data-who={f.who}
        data-kind={f.reply ? 'reply' : undefined}
        data-nodue={f.due === null ? '' : undefined}
        data-past={f.due !== null && f.due < now ? '' : undefined}
        role="button"
        tabIndex={0}
        onClick={(event) => (pile || first.itemId === undefined ? toggle?.() : open(event.currentTarget))}
        onKeyDown={onKeyPress((el) => (pile || first.itemId === undefined ? toggle?.() : open(el)))}
      >
        <span className="hx-day-flag__p">
          <Icon name={icon} size="sm" />
          <span>{first.short ?? first.title}</span>
          {pile && <i className="hx-day-flag__n">+{f.flags.length - 1}</i>}
        </span>
      </div>
    );
    if (!pile && !rich) {
      // A short promise is explained by the platform's hint; an answer and a pile have a card with their content.
      const what = first.tip ?? first.title;
      const until =
        first.due && !first.tip
          ? ` · ${t('next.day.cal.until', { time: clock.hm(Date.parse(first.due)) })}`
          : '';
      return (
        <Tooltip key={f.key} label={`${who}: ${what}${until}`}>
          {flag()}
        </Tooltip>
      );
    }
    return (
      <HoverCard
        key={f.key}
        className={pile ? 'hx-day-pop--list' : undefined}
        card={(close) =>
          pile ? (
            <>
              <Head title={who} meta="" />
              {f.flags.map((x, i) => (
                <div
                  key={i}
                  className="hx-day-pop__item"
                  data-pick={x.itemId !== undefined ? '' : undefined}
                  role={x.itemId !== undefined ? 'button' : undefined}
                  tabIndex={0}
                  onClick={(event) => {
                    if (x.itemId === undefined) return;
                    close();
                    onItem(x.itemId, event.currentTarget);
                  }}
                  onKeyDown={onKeyPress((el) => {
                    if (x.itemId === undefined) return;
                    close();
                    onItem(x.itemId, el);
                  })}
                >
                  <Head title={x.title} meta={flagMeta(x, f.reply)} />
                </div>
              ))}
            </>
          ) : (
            <>
              <Head title={first.title} meta={flagMeta(first, f.reply)} />
              <Prep prep={first.prep} />
              <Foot
                mats={first.prep?.mats}
                url={undefined}
                itemId={first.itemId}
                onMore={(from) => {
                  close();
                  open(from);
                }}
              />
            </>
          )
        }
      >
        {(toggle) => flag(toggle)}
      </HoverCard>
    );
  };

  const drawDay = (d: IStripDay): ReactNode => {
    const tag = d.n > 0 ? <em>{clock.shortDate(d.from)}</em> : null;
    if (d.off)
      return (
        <div key={d.n} className="hx-day-k__body" data-off="">
          <div className="hx-day-k__h">
            <span>{tag}</span>
          </div>
          <p className="hx-day-k__off">{t('next.day.cal.dayOff')}</p>
        </div>
      );
    const drafting = draft !== null && draft.day === d.n ? draft : null;
    const hover = ghost !== null && ghost.day === d.n && draft === null ? ghost : null;
    return (
      <div
        key={d.n}
        className="hx-day-k__body"
        onPointerMove={(event) => {
          if (event.pointerType === 'touch') return;
          const spot = spotAt(d, event);
          if (!sameSpot(spot, ghost)) setGhost(spot);
        }}
        onPointerLeave={() => setGhost(null)}
        onClick={(event) => {
          if (event.target instanceof Element && event.target.closest('input')) return;
          const spot = spotAt(d, event);
          if (spot !== null) startDraft(spot);
        }}
      >
        {d.hours.map((h, i) => (
          <div key={h.at} ref={cssVars({ '--l': `${h.left}%` })} className="hx-day-k__h">
            <span>
              {i === 0 && tag}
              {clock.hm(h.at)}
            </span>
          </div>
        ))}
        {d.events.map((e) => drawEvent(d, e))}
        {own
          .filter((o) => o.day === d.n)
          .map((o) => (
            <Tooltip key={o.id} label={`${clock.hm(o.start)}–${clock.hm(o.end)}`}>
              <div
                ref={cssVars({
                  '--l': `${percentOf(d, o.start)}%`,
                  '--w': `${percentOf(d, o.end) - percentOf(d, o.start)}%`,
                })}
                className="hx-day-e"
                data-k={SLOT.meet}
                data-short=""
                data-in=""
                data-past={o.end <= now ? '' : undefined}
                tabIndex={0}
              >
                <div className="hx-day-e__in">
                  <Icon name="user" size="sm" className="hx-day-e__ic" />
                  <b>{o.title}</b>
                </div>
              </div>
            </Tooltip>
          ))}
        {drafting && (
          <div
            ref={cssVars({
              '--l': `${percentOf(d, drafting.start)}%`,
              '--w': `${percentOf(d, drafting.start + OWN_MS) - percentOf(d, drafting.start)}%`,
            })}
            className="hx-day-e"
            data-k={SLOT.meet}
            data-draft=""
          >
            <div className="hx-day-e__in">
              <Icon name="user" size="sm" className="hx-day-e__ic" />
              <input
                className="hx-day-e__inp"
                aria-label={t('next.day.cal.ownLabel', { time: clock.hm(drafting.start) })}
                placeholder={t('next.day.cal.ownPlaceholder')}
                maxLength={OWN_TITLE_MAX}
                autoFocus
                onKeyDown={(event) => {
                  event.stopPropagation();
                  if (event.key === 'Escape') endDraft(null);
                  if (event.key === 'Enter') endDraft(event.currentTarget.value);
                }}
                onBlur={(event) => endDraft(event.currentTarget.value)}
              />
            </div>
          </div>
        )}
        {hover && (
          <div ref={cssVars({ '--l': `${percentOf(d, hover.start)}%` })} className="hx-day-k__ghost">
            <Icon name="plus" size="sm" />
            {clock.hm(hover.start)}
          </div>
        )}
        {d.flags.map((f) => drawFlag(d, f))}
        {d.n === 0 && now >= d.from && now < d.to && (
          <div
            ref={cssVars({ '--l': `${percentOf(d, now)}%` })}
            className="hx-day-k__now"
            data-t={clock.hm(now)}
          />
        )}
      </div>
    );
  };

  const head = days[shown] ?? days[0];
  const weekday = head ? clock.weekday(head.from) : '';
  const label = dayLabel(shown, weekday);
  return (
    <section id="hx-day-cal" className="hx-day-cal" aria-label={t('next.day.cal.label')}>
      <div className="hx-day-cal__head">
        <b key={`d${shown}`}>{label}</b>
        {head && <span key={`t${shown}`}>{clock.longDate(head.from)}</span>}
        <HoverCard
          className="hx-day-pop--how"
          card={() => (
            <>
              <b>{t('next.day.how.title')}</b>
              {(
                [
                  ['users', 'meetings'],
                  ['sparkles', 'promises'],
                  ['layout-grid', 'stack'],
                  ['plus', 'own'],
                  ['arrow-right', 'open'],
                ] as const
              ).map(([icon, key]) => (
                <span key={key} className="hx-day-how__row">
                  <Icon name={icon} size="sm" />
                  <span>
                    <em>{t(`next.day.how.${key}`)}</em> {t(`next.day.how.${key}Text`)}
                  </span>
                </span>
              ))}
              <span className="hx-day-how__src">{t('next.day.how.scroll')}</span>
            </>
          )}
        >
          {(toggle) => (
            <button type="button" className="hx-day-how" onClick={toggle}>
              <Icon name="info" size="sm" />
              {t('next.day.how.title')}
            </button>
          )}
        </HoverCard>
      </div>
      <div ref={boxRef} className="hx-day-k" onScroll={onScroll}>
        {days.map(drawDay)}
      </div>
    </section>
  );
};

export default Timeline;
