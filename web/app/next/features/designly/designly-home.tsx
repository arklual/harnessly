// Designly's start (the design's .hx-dl): «Что нарисуем?» with the field on top, what to make under it, and the
// library below — the machine's designs as cards with their pictures. Scrolled, the field goes up and the library's
// row sticks. A card opens the design; a brief sent starts a project and its agent, and its card draws while it works.
import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import Icon from '@/next/ui/icon';
import DsPicker from '@/next/features/designly/ds-picker';
import { RecordingTime, RecordingWave } from '@/next/features/composer/composer';
import ModelPicker, { type IModelPickerProps } from '@/next/features/composer/model-picker';
import type { Recording } from '@/next/features/composer/types';
import { RoundButton } from '@/next/ui/button';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import Swap from '@/next/ui/swap';
import { usePresence } from '@/next/ui/presence';
import { HSCROLL_DIR, useHScroll } from '@/next/ui/use-h-scroll';
import type { IDesignSystem } from '@/next/features/designly/api';
import {
  DESIGN_SCOPE,
  DESIGN_SCOPES,
  inScope,
  initialOf,
  type TDesignScope,
} from '@/next/features/designly/owners';
import type { DesignOwner } from '@/next/protocol/v2';
import { plural, t } from '@/next/i18n';
import { formatAgoFull } from '@/next/i18n/format';
import '@/next/styles/designly.css';

export const DESIGN_KIND = { site: 'site', slides: 'slides', ds: 'ds' } as const;
export type TDesignKind = (typeof DESIGN_KIND)[keyof typeof DESIGN_KIND];

const KIND_ICON: Record<TDesignKind, string> = {
  [DESIGN_KIND.site]: 'app-window',
  [DESIGN_KIND.slides]: 'presentation',
  [DESIGN_KIND.ds]: 'palette',
};
const KINDS = [DESIGN_KIND.site, DESIGN_KIND.slides, DESIGN_KIND.ds] as const;

export interface IDesignCard {
  id: string;
  title: string;
  boards: number;
  updatedAt: number;
  /** The picture of its main board; null while there is none. */
  image: string | null;
  kind: TDesignKind;
  pinned: boolean;
  /** An agent is drawing it right now. */
  drawing: boolean;
  /** Who made it; null for a project older than owners. */
  owner: DesignOwner | null;
}

/** The start of a project: the brief's field, the kinds, the model and the design system. */
export interface IDesignlyCompose {
  /** The design systems a new project can take; null while they load. */
  systems: readonly IDesignSystem[] | null;
  /** Sending a brief: resolves once the project and its agent exist (the card is in the list then). */
  onCreate: (brief: string, kind: TDesignKind, designSystem: string | null) => Promise<void>;
  /** The brief in the field; the screen keeps it so dictation can write into it. */
  brief: string;
  onBrief: (value: string) => void;
  /** The agent, model and depth that draw, as in the chat; null while the machine's agents are unknown. */
  picker: IModelPickerProps | null;
  /** Dictation into the brief; without onMic the microphone is not offered. */
  voice: IDesignlyVoice;
}

export interface IDesignlyHomeProps {
  /** null while the library loads; an error string when it did not. */
  cards: readonly IDesignCard[] | null;
  error: string | null;
  now: number;
  /** Pins or unpins a card; resolves once the machine kept it. */
  onPin: (id: string, pinned: boolean) => Promise<void>;
  onOpen: (id: string) => void;
  onRetry: () => void;
  /** Where a project is started; null is the library alone (the right pane's: its chat is the field). */
  compose: IDesignlyCompose | null;
}

export interface IDesignlyVoice {
  recording: Recording | null;
  onMic?: () => void;
  onCancel: () => void;
  onFinish: () => void;
}

/** The recording bar's exit, as long as hx-recbar-out in composer.css. */
const REC_EXIT_MS = 180;

/** The cards move to their new places over this long, as in the design (dlFlip). */
const FLIP_MS = 620;
const FLIP_EASE = 'cubic-bezier(.2, .8, .2, 1)';
/** A pin's rects are dropped this long after the machine answered: a reorder later is not this pin's. */
const FLIP_KEEP_MS = 1000;

/** The address of a picture asked again: a new query keeps the browser from answering with the failure it remembers. */
const askedAgain = (image: string, round: number) =>
  round === 0 ? image : `${image}${image.includes('?') ? '&' : '?'}r=${round}`;

/** The card's picture through a custom property set on the element, not an inline style attribute. A picture that
 *  does not come leaves a «Повторить» on its place, which asks it again. */
const CardShot = ({ image }: { image: string | null }) => {
  const ref = useRef<HTMLSpanElement>(null);
  const [round, setRound] = useState(0);
  const [broken, setBroken] = useState<string | null>(null);
  const src = image ? askedAgain(image, round) : null;
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (src) el.style.setProperty('--img', `url("${src}")`);
    else el.style.removeProperty('--img');
  }, [src]);
  // The picture is a background, which has no error of its own: the same address is asked for as an image.
  useEffect(() => {
    if (!src) return;
    const probe = new Image();
    probe.onerror = () => setBroken(src);
    probe.src = src;
    return () => {
      probe.onerror = null;
    };
  }, [src]);
  return (
    <>
      <span ref={ref} className="hx-dl-card__img" />
      {src !== null && broken === src && (
        <button
          type="button"
          className="hx-dl-card__retry"
          aria-label={t('next.load.picture')}
          title={t('next.load.picture')}
          onClick={(event) => {
            event.stopPropagation();
            setBroken(null);
            setRound((n) => n + 1);
          }}
        >
          <Icon name="rotate-ccw" size="sm" />
        </button>
      )}
    </>
  );
};

/** What an empty list says: nothing matched the search, else nothing is in the tab. */
const emptyKey = (scope: TDesignScope, searching: boolean) => {
  if (searching) return 'next.designly.notFound';
  if (scope === DESIGN_SCOPE.mine) return 'next.designly.emptyMine';
  if (scope === DESIGN_SCOPE.shared) return 'next.designly.emptyShared';
  return 'next.designly.empty';
};

/** Six cards of the grid's size, so the cards take their places without the page moving. */
const LibrarySkeleton = () => (
  <div className="hx-dl__grid hx-dl__grid--sk" role="status" aria-label={t('next.designly.loading')}>
    {[0, 1, 2, 3, 4, 5].map((i) => (
      <div key={i} className="hx-dl-sk" data-i={i}>
        <span className="hx-sk hx-dl-sk__shot" />
        <span className="hx-sk hx-dl-sk__title" />
        <span className="hx-sk hx-dl-sk__sub" />
      </div>
    ))}
  </div>
);

interface IHeroProps extends IDesignlyCompose {
  /** The field takes focus when the library's «Новый» scrolls back to it. */
  inputRef: RefObject<HTMLTextAreaElement | null>;
}

/** «Что нарисуем?»: the field with its model, design system and microphone, and the kinds under it. */
const Hero = ({ systems, onCreate, brief, onBrief, picker, voice, inputRef }: IHeroProps) => {
  const [kind, setKind] = useState<TDesignKind>(DESIGN_KIND.site);
  const [ds, setDs] = useState<string | null>(null);
  const { value: bar, state: barState } = usePresence(voice.recording, REC_EXIT_MS);
  const [sending, setSending] = useState(false);
  const startsRef = useRef<HTMLDivElement>(null);
  const hsStep = useHScroll(startsRef);

  const send = () => {
    const text = brief.trim();
    if (sending) return;
    // An empty field is not an error: the button takes the person back to it, as the design does.
    if (!text) {
      inputRef.current?.focus();
      return;
    }
    setSending(true);
    void onCreate(text, kind, kind === DESIGN_KIND.ds ? null : ds)
      // A refusal is said by the caller; the brief stays in the field to send again.
      .then(
        () => onBrief(''),
        () => undefined,
      )
      .finally(() => setSending(false));
  };
  const onKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      send();
    }
  };

  return (
    <div className="hx-dl__hero">
      <div className="hx-dl__title">{t('next.designly.title')}</div>
      <div className="hx-dl__sub">{t('next.designly.sub')}</div>
      <div className="hx-dl__composer">
        <textarea
          ref={inputRef}
          className="hx-dl__input"
          rows={2}
          value={brief}
          placeholder={t(`next.designly.hint.${kind}`)}
          disabled={sending}
          onChange={(event) => onBrief(event.target.value)}
          onKeyDown={onKey}
        />
        {bar && (
          <div className="hx-dl__bar hx-recbar" data-state={barState}>
            <RoundButton
              icon="x"
              label={t('next.composer.cancelRecording')}
              variant="ghost"
              size="sm"
              onClick={voice.onCancel}
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
              className="hx-recbar__stop"
              busy={bar.busy}
              disabled={bar.busy}
              onClick={voice.onFinish}
            />
          </div>
        )}
        <div className="hx-dl__bar" hidden={bar !== null}>
          {picker && (
            <div className="hx-dl__model">
              <ModelPicker {...picker} />
            </div>
          )}
          <span className="hx-spacer" />
          <DsPicker systems={systems ?? []} value={ds} shown={kind !== DESIGN_KIND.ds} onChange={setDs} />
          {voice.onMic && <RoundButton icon="mic" label={t('next.composer.voice')} onClick={voice.onMic} />}
          <button
            type="button"
            className="hx-round"
            data-variant="accent"
            aria-label={t('next.designly.send')}
            aria-busy={sending || undefined}
            disabled={sending}
            onClick={send}
          >
            <Icon name={sending ? 'loader-circle' : 'arrow-up'} />
          </button>
        </div>
      </div>
      <div className="hx-hs">
        <button
          type="button"
          className="hx-hs__arrow"
          data-dir="prev"
          aria-label={t('next.designly.hsPrev')}
          tabIndex={-1}
          onClick={() => hsStep(HSCROLL_DIR.prev)}
        >
          <Icon name="chevron-left" size="sm" />
        </button>
        <button
          type="button"
          className="hx-hs__arrow"
          data-dir="next"
          aria-label={t('next.designly.hsNext')}
          tabIndex={-1}
          onClick={() => hsStep(HSCROLL_DIR.next)}
        >
          <Icon name="chevron-right" size="sm" />
        </button>
        <div
          ref={startsRef}
          className="hx-dl__starts hx-hs__track"
          role="radiogroup"
          aria-label={t('next.designly.kind')}
        >
          {KINDS.map((k) => (
            <button
              key={k}
              type="button"
              className="hx-dl-start"
              role="radio"
              aria-checked={k === kind}
              onClick={() => {
                setKind(k);
                inputRef.current?.focus();
              }}
            >
              <span className="hx-dl-start__ic">
                <Icon name={KIND_ICON[k]} />
              </span>
              <span className="hx-dl-start__text">
                <span className="hx-dl-start__name">{t(`next.designly.kinds.${k}`)}</span>
                <span className="hx-dl-start__desc">{t(`next.designly.kindsDesc.${k}`)}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};

const DesignlyHome = ({ cards, error, now, onPin, onOpen, onRetry, compose }: IDesignlyHomeProps) => {
  const [pinning, setPinning] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState<TDesignScope>(DESIGN_SCOPE.all);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const flipFrom = useRef<ReadonlyMap<string, DOMRect> | null>(null);
  const rootRef = useRef<HTMLElement>(null);

  // Scrolled: --p is how far the field went (0…1), data-stuck says the library's row is at the top. Once a frame,
  // reads first, then writes; --p goes on the hero only, so the cards are not recomputed.
  useEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    let frame = 0;
    let last = '';
    const measure = () => {
      frame = 0;
      const hero = el.querySelector<HTMLElement>('.hx-dl__hero');
      const bar = el.querySelector<HTMLElement>('.hx-dl__toolbar');
      if (!bar) return;
      const top = el.scrollTop;
      const stuck = top > 0 && bar.getBoundingClientRect().top - el.getBoundingClientRect().top <= 0.5;
      // The library alone (the pane's) has no field to move away.
      const p = hero ? Math.min(1, Math.max(0, top / Math.max(1, hero.offsetHeight))).toFixed(3) : '0';
      if (hero && p !== last) {
        last = p;
        hero.style.setProperty('--p', p);
      }
      if (el.dataset.stuck !== String(stuck)) el.dataset.stuck = String(stuck);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    measure();
    return () => {
      el.removeEventListener('scroll', onScroll);
      cancelAnimationFrame(frame);
    };
  }, []);

  const toTop = () => {
    rootRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
    window.setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 420);
  };

  const q = query.trim().toLowerCase();
  // Pinned first, then the latest change.
  const shown =
    cards
      ?.filter((c) => inScope(scope, c.owner) && (!q || c.title.toLowerCase().includes(q)))
      .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt - a.updatedAt) ?? null;

  // Pinning: the cards were measured before the machine kept the pin; when the new order is on screen, each one
  // travels from its old place (FLIP: all reads first, then the animations). Not with reduced motion.
  const order = shown?.map((c) => c.id).join('|') ?? '';
  useLayoutEffect(() => {
    const from = flipFrom.current;
    const grid = gridRef.current;
    if (!from || !grid) return;
    flipFrom.current = null;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const cardsNow = [...grid.children].filter((el): el is HTMLElement => el instanceof HTMLElement);
    const rects = cardsNow.map((el) => el.getBoundingClientRect());
    cardsNow.forEach((el, i) => {
      const before = from.get(el.dataset.id ?? '');
      const now = rects[i];
      if (!before || !now) return;
      const dx = before.left - now.left;
      const dy = before.top - now.top;
      if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return;
      el.animate([{ translate: `${dx}px ${dy}px` }, { translate: '0 0' }], {
        duration: FLIP_MS,
        easing: FLIP_EASE,
      });
    });
  }, [order]);

  const pin = (card: IDesignCard) => {
    const grid = gridRef.current;
    const rects = new Map<string, DOMRect>();
    for (const el of grid?.children ?? []) {
      if (el instanceof HTMLElement && el.dataset.id) rects.set(el.dataset.id, el.getBoundingClientRect());
    }
    flipFrom.current = rects;
    setPinning(card.id);
    void onPin(card.id, !card.pinned)
      .catch(() => {
        flipFrom.current = null;
      })
      .finally(() => {
        setPinning(null);
        window.setTimeout(() => {
          flipFrom.current = null;
        }, FLIP_KEEP_MS);
      });
  };

  return (
    <section className="hx-dl" ref={rootRef} data-library={compose === null}>
      {compose && <Hero {...compose} inputRef={inputRef} />}

      <div className="hx-dl__body">
        <div className="hx-dl__toolbar">
          <div className="hx-dl__heading">{t('next.designly.library')}</div>
          <div className="hx-dl__tabs" role="tablist" aria-label={t('next.designly.scope')}>
            {DESIGN_SCOPES.map((k) => (
              <button
                key={k}
                type="button"
                role="tab"
                className="hx-dl__tab"
                aria-selected={k === scope}
                onClick={() => setScope(k)}
              >
                {t(`next.designly.scopes.${k}`)}
              </button>
            ))}
          </div>
          <span className="hx-spacer" />
          <label className="hx-dl__search">
            <Icon name="search" size="sm" />
            <input
              type="search"
              placeholder={t('next.designly.search')}
              autoComplete="off"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          {compose && (
            <button type="button" className="hx-dl__new" tabIndex={-1} onClick={toTop}>
              <Icon name="plus" size="sm" />
              {t('next.designly.new')}
            </button>
          )}
        </div>

        {cards === null && error !== null && (
          <LoadFailed
            size={LOAD_FAILED_SIZE.page}
            title={t('next.designly.failed')}
            reason={error}
            onRetry={onRetry}
          />
        )}
        {(cards !== null || error === null) && (
          <Swap ready={shown !== null} skeleton={<LibrarySkeleton />}>
            {shown && shown.length > 0 && (
              <div className="hx-dl__grid" ref={gridRef}>
                {shown.map((card, i) => (
                  <div
                    key={card.id}
                    data-id={card.id}
                    className="hx-dl-card"
                    data-state={card.drawing ? 'gen' : 'ready'}
                    data-pinned={card.pinned}
                    data-i={Math.min(i, 8)}
                    tabIndex={0}
                    role="button"
                    aria-label={card.title}
                    onClick={() => onOpen(card.id)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' && event.target === event.currentTarget) onOpen(card.id);
                    }}
                  >
                    <div className="hx-dl-card__shot">
                      <CardShot image={card.image} />
                      <span className="hx-dl-card__alt" />
                      <button
                        type="button"
                        className="hx-dl-card__pin hx-tipped"
                        aria-pressed={card.pinned}
                        aria-busy={pinning === card.id || undefined}
                        aria-label={card.pinned ? t('next.designly.unpin') : t('next.designly.pin')}
                        disabled={pinning === card.id}
                        onClick={(event) => {
                          event.stopPropagation();
                          pin(card);
                        }}
                      >
                        <Icon name="pin" size="sm" />
                        <span className="hx-tip" data-pos="below" data-align="end">
                          {card.pinned ? t('next.designly.unpin') : t('next.designly.pin')}
                        </span>
                      </button>
                      <span className="hx-dl-card__gen">
                        <Icon name="loader-circle" size="sm" />
                        {t('next.designly.drawing')}
                      </span>
                    </div>
                    <div className="hx-dl-card__meta">
                      <span className="hx-dl-card__text">
                        <span className="hx-dl-card__title">{card.title}</span>
                        <span className="hx-dl-card__sub">
                          <Icon name={KIND_ICON[card.kind]} size="sm" />
                          {card.drawing
                            ? t('next.designly.drawingShort')
                            : `${plural(card.boards, `next.designly.units.${card.kind}`)} · ${formatAgoFull(card.updatedAt, now)}`}
                        </span>
                      </span>
                      {card.owner && (
                        <span
                          className="hx-dl-card__who"
                          role="img"
                          aria-label={t('next.designly.owner', { name: card.owner.name })}
                          title={t('next.designly.owner', { name: card.owner.name })}
                        >
                          {initialOf(card.owner.name)}
                        </span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Swap>
        )}
        {shown && shown.length === 0 && <div className="hx-dl__empty">{t(emptyKey(scope, q !== ''))}</div>}
      </div>
    </section>
  );
};

export default DesignlyHome;
