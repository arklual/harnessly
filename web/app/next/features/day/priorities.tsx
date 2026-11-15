// «Priorities by importance»: the day's things to do in the feed's order, a card each: what to do, by when, why, and what
// opening it gives. A thing the feed marks done is ticked before the person's eyes once, then folds into a line under
// «Done» while the cards below rise to its place.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { IDayPriority, TItemId } from '@/next/features/day/feed';
import { DIGEST_ITEM } from '@/next/features/voice-live/digest-line';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

const SEEN_KEY = 'hl.day.done';
/** A done thing stays a card this long before its tick is drawn, and as long again before it folds. */
const DONE_STEP_MS = 1500;
const DONE_STAGGER_MS = 400;
const RISE_MS = 520;

const DONE = { card: 'card', ticked: 'ticked', folded: 'folded' } as const;
type TDone = (typeof DONE)[keyof typeof DONE];

const readSeen = (): Set<string> => {
  try {
    const raw: unknown = JSON.parse(sessionStorage.getItem(SEEN_KEY) ?? '[]');
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === 'string') : []);
  } catch {
    return new Set();
  }
};

const writeSeen = (seen: Set<string>) => {
  try {
    sessionStorage.setItem(SEEN_KEY, JSON.stringify([...seen]));
  } catch {
    // Without storage the tick is simply drawn again on the next visit.
  }
};

const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

interface IPrioritiesProps {
  items: IDayPriority[];
  onItem: (id: TItemId, from: HTMLElement) => void;
  /** The thing the digest's voice talks about now (its id as a string): outlined. */
  voiceItem?: string | null;
}

const Priorities = ({ items, onItem, voiceItem = null }: IPrioritiesProps) => {
  const listRef = useRef<HTMLDivElement>(null);
  const tops = useRef<Map<Element, number> | null>(null);
  // One the person has already seen done lies folded from the start; so does every done thing when motion is off.
  const [seenBefore] = useState(readSeen);
  const [still] = useState(reducedMotion);
  // How far each thing marked done has gone before the person's eyes since.
  const [progress, setProgress] = useState<Record<string, TDone>>({});
  const stageOf = (id: string): TDone =>
    progress[id] ?? (still || seenBefore.has(id) ? DONE.folded : DONE.card);
  const doneIds = items
    .filter((p) => p.done === true)
    .map((p) => String(p.itemId))
    .join('\n');

  useEffect(() => {
    const ids = doneIds === '' ? [] : doneIds.split('\n');
    const seen = readSeen();
    const fresh = ids.filter((id) => !seen.has(id));
    if (still) {
      fresh.forEach((id) => seen.add(id));
      writeSeen(seen);
      return;
    }
    const timers: ReturnType<typeof setTimeout>[] = [];
    fresh.forEach((id, i) => {
      timers.push(
        setTimeout(
          () => {
            setProgress((had) => (had[id] === DONE.folded ? had : { ...had, [id]: DONE.ticked }));
            timers.push(
              setTimeout(() => {
                const list = listRef.current;
                if (list)
                  tops.current = new Map(
                    [...list.querySelectorAll('.hx-day-pr')].map((el) => [
                      el,
                      el.getBoundingClientRect().top,
                    ]),
                  );
                setProgress((had) => ({ ...had, [id]: DONE.folded }));
                // Seen once it has folded: a page left halfway draws the tick again.
                const now = readSeen();
                now.add(id);
                writeSeen(now);
              }, DONE_STEP_MS),
            );
          },
          DONE_STEP_MS + i * DONE_STAGGER_MS,
        ),
      );
    });
    return () => timers.forEach(clearTimeout);
  }, [doneIds, still]);

  // The fold moves the cards: each travels from where it stood to where it stands now.
  useLayoutEffect(() => {
    const before = tops.current;
    if (before === null) return;
    tops.current = null;
    before.forEach((top, el) => {
      const dy = top - el.getBoundingClientRect().top;
      if (dy === 0 || !(el instanceof HTMLElement)) return;
      el.animate([{ translate: `0 ${dy}px` }, { translate: '0 0' }], {
        duration: RISE_MS,
        easing: 'cubic-bezier(.2,.8,.2,1)',
      });
    });
  }, [progress]);

  const anyFolded = items.some((p) => p.done === true && stageOf(String(p.itemId)) === DONE.folded);
  return (
    <>
      <div id="hx-day-pri" className="hx-day-h">
        {t('next.day.priorities')}
      </div>
      <div ref={listRef} className="hx-day-prs">
        {items.length === 0 && <p className="hx-day-pr__sub">{t('next.day.noPriorities')}</p>}
        {items.map((p) => {
          const done = p.done === true ? stageOf(String(p.itemId)) : undefined;
          const open = (from: HTMLElement) => onItem(p.itemId, from);
          return (
            <article
              key={String(p.itemId)}
              className={`hx-day-pr ${DIGEST_ITEM}`}
              data-digest-voice={voiceItem === String(p.itemId) || undefined}
              data-done={done === DONE.ticked || done === DONE.folded ? '' : undefined}
              data-folded={done === DONE.folded ? '' : undefined}
              role="button"
              tabIndex={0}
              onClick={(event) => open(event.currentTarget)}
              onKeyDown={(event) => {
                if (event.key !== 'Enter' && event.key !== ' ') return;
                event.preventDefault();
                open(event.currentTarget);
              }}
            >
              <div className="hx-day-pr__t">
                {p.done === true && (
                  <span className="hx-day-pr__ok" aria-hidden="true">
                    <svg viewBox="0 0 16 16" width="16" height="16">
                      <path d="M3.5 8.4l3 3 6-6.6" />
                    </svg>
                  </span>
                )}
                <b>{p.title}</b>
                {p.due && <time data-soon={p.soon === true ? '' : undefined}>{p.due}</time>}
                {p.done === true && <em className="hx-day-pr__donet">{p.doneAt ?? t('next.day.done')}</em>}
              </div>
              {p.sub && <p className="hx-day-pr__sub">{p.sub}</p>}
              {p.points && p.points.length > 0 && (
                <ul className="hx-day-pr__pts">
                  {p.points.map((point, i) => (
                    <li key={i}>{point}</li>
                  ))}
                </ul>
              )}
              <span className="hx-day-pr__cta">
                <span>{p.cta ?? t('next.day.open')}</span>
                <Icon name="arrow-right" size="sm" />
              </span>
            </article>
          );
        })}
        {anyFolded && <div className="hx-day-h hx-day-prs__done">{t('next.day.doneHead')}</div>}
      </div>
    </>
  );
};

export default Priorities;
