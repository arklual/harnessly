// «По ссылке»: the deck as a guest sees it. Slides (the stage, a pager, pins of comments), full screen (black, a capsule
// with «Выйти», ‹ n/N › and «Комментарий»), or the recording with its chapters. «Как откроется» of the link decides where
// it starts. The guest page and the owner's «Как видит гость» both draw this; the owner's has «Назад» and no comments.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import DeckVideo, { type IDeckVideoChapter } from '@/next/features/designly/deck/deck-video';
import { readDeckMessage, readPickedPoint } from '@/next/features/designly/deck/bridge';
import type { IChapterOfSlide } from '@/next/features/designly/deck/video-chapters';
import { DECK_OPENS, type TDeckOpens } from '@/next/features/designly/deck/types';
import { GUEST_KEY_CODE, readBridgeMessage } from '@/next/features/designly/page-bridge';
import { useTouch } from '@/next/features/designly/guest/guest-full';
import Icon from '@/next/ui/icon';
import { Button } from '@/next/ui/button';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

export interface IDeckGuestSlide {
  id: string;
  title: string;
  /** The board's path in the project: the key the gateway knows it by. */
  path: string;
  /** The frame's address. */
  src: string;
  w: number;
  h: number;
}

/** A comment's place on a slide, in percent of the slide. */
export interface IDeckGuestPin {
  id: string;
  slide: number;
  x: number;
  y: number;
}

export interface IDeckGuestComments {
  /** Asks for the guest's name once; false if they did not give one. */
  ensureName: () => Promise<boolean>;
  pins: readonly IDeckGuestPin[];
  /** Sends a comment about a place of a slide; true when it was taken. */
  send: (slide: number, at: { x: number; y: number }, label: string, text: string) => Promise<boolean>;
}

export interface IDeckGuestProps {
  title: string;
  slides: readonly IDeckGuestSlide[];
  /** The published recording; null when there is none. */
  video: { src: string; chapters: readonly IChapterOfSlide[] } | null;
  /** The published PDF's address, only where the owner allowed it; null otherwise. */
  pdf: string | null;
  start: TDeckOpens;
  /** Null for a link that only shows. */
  comments: IDeckGuestComments | null;
  /** The owner looking as a guest: «Назад» is there (a guest has no way back). */
  onBack?: () => void;
  /** The board on screen (its path), null when none is (a paused video, a hidden tab): for counting views. */
  onShown?: (path: string | null) => void;
}

const FULL = { off: 'off', on: 'on', left: 'left' } as const;
const MODE = { slides: 'slides', video: 'video' } as const;
type TMode = (typeof MODE)[keyof typeof MODE];
const OUT_MS = 200;
const POP_W = 320;
const POP_H = 220;
const SIZE_FALLBACK = { w: 1920, h: 1080 } as const;

/** A key pressed while the guest writes is the field's. */
const typingIn = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));

interface IDraft {
  slide: number;
  /** Percent of the slide. */
  x: number;
  y: number;
  /** Pixels in the page: where the card opens. */
  px: number;
  py: number;
  label: string;
  /** The board's px, to anchor the comment to. */
  text: string;
}

const DeckGuest = ({ title, slides, video, pdf, start, comments, onBack, onShown }: IDeckGuestProps) => {
  const touch = useTouch();
  const root = useRef<HTMLDivElement>(null);
  const slot = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const [index, setIndex] = useState(0);
  const [mode, setMode] = useState<TMode>(start === DECK_OPENS.video && video ? MODE.video : MODE.slides);
  const [full, setFull] = useState<(typeof FULL)[keyof typeof FULL]>(
    start === DECK_OPENS.full ? FULL.on : FULL.off,
  );
  const [picking, setPicking] = useState(false);
  const [draft, setDraft] = useState<IDraft | null>(null);
  const [menu, setMenu] = useState(false);
  const [sending, setSending] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [ready, setReady] = useState(false);
  const [step, setStep] = useState(0);
  const [steps, setSteps] = useState(0);
  const wantLast = useRef(false);
  const [videoSlide, setVideoSlide] = useState<string | null>(null);

  const total = slides.length;
  const slide: IDeckGuestSlide | undefined = slides[index];
  const inSlides = mode === MODE.slides;
  const isFull = full === FULL.on && inSlides;
  const canComment = comments !== null && inSlides;

  const videoView = usePresence(mode === MODE.video && video ? video : null, OUT_MS);
  const capsule = usePresence(isFull ? true : null, OUT_MS);
  const menuView = usePresence(menu ? true : null, OUT_MS);
  const cardView = usePresence(draft, OUT_MS);

  // The board's own window is the only one listened to; its sandbox gives it no origin, so it is addressed as `*`.
  const post = (message: Record<string, unknown>) => frame.current?.contentWindow?.postMessage(message, '*');

  const go = (to: number) => {
    const next = Math.max(0, Math.min(total - 1, to));
    if (next === index) return;
    setIndex(next);
    setLoaded(false);
    setReady(false);
    setStep(0);
    setSteps(0);
    setDraft(null);
    setPicking(false);
  };
  const forward = () => {
    if (step < steps) setStep(step + 1);
    else go(index + 1);
  };
  const back = () => {
    if (step > 0) setStep(step - 1);
    else if (index > 0) {
      wantLast.current = true;
      go(index - 1);
    }
  };

  useEffect(() => {
    if (ready) post({ type: 'tk:deck-step', n: step });
  }, [step, ready]);
  // Turns the board's deck mode on and tells it what the page is doing; again whenever its script says it is there.
  const sync = () => {
    post({ type: 'tk:deck', on: true, mode: 'guest' });
    post({ type: 'tk:pick', on: picking });
    post({ type: 'tk:hold', on: comments !== null });
  };
  useEffect(() => {
    if (loaded) post({ type: 'tk:pick', on: picking });
  }, [picking, loaded]);

  // What is on screen, for counting views.
  const shown = inSlides ? (slide?.path ?? null) : videoSlide;
  useEffect(() => {
    onShown?.(shown);
  }, [shown, onShown]);

  const place = (message: {
    point: [number, number] | null;
    rect: { x: number; y: number; width: number; height: number };
    label: string;
  }) => {
    if (!slide || !slot.current || !root.current) return;
    const w = slide.w || SIZE_FALLBACK.w;
    const h = slide.h || SIZE_FALLBACK.h;
    const [bx, by] = message.point ?? [
      message.rect.x + message.rect.width / 2,
      message.rect.y + message.rect.height / 2,
    ];
    const x = Math.min(100, Math.max(0, (bx / w) * 100));
    const y = Math.min(100, Math.max(0, (by / h) * 100));
    const s = slot.current.getBoundingClientRect();
    const r = root.current.getBoundingClientRect();
    setPicking(false);
    setDraft({
      slide: index,
      x,
      y,
      px: Math.max(8, Math.min(r.width - POP_W - 8, s.left - r.left + (s.width * x) / 100 + 12)),
      py: Math.max(8, Math.min(r.height - POP_H - 8, s.top - r.top + (s.height * y) / 100 + 12)),
      label: message.label,
      text: '',
    });
  };

  const onMessage = useEffectEvent((event: MessageEvent) => {
    if (event.source === null || event.source !== frame.current?.contentWindow) return;
    const deck = readDeckMessage(event.data);
    if (deck) {
      if (deck.k === 'ready') {
        setReady(true);
        setSteps(deck.steps);
        if (wantLast.current) {
          wantLast.current = false;
          setStep(deck.steps);
        }
      }
      return;
    }
    const bridge = readBridgeMessage(event.data);
    if (!bridge) return;
    if (bridge.k === 'lib-ready') sync();
    else if (bridge.k === 'key')
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: bridge.key, code: GUEST_KEY_CODE[bridge.key] }),
      );
    else if (bridge.k === 'pick-off') setPicking(false);
    else if (bridge.k === 'picked' && comments)
      place({
        point: readPickedPoint(event.data),
        rect: bridge.picked.rect,
        label: bridge.picked.text || bridge.picked.component || '',
      });
  });
  useEffect(() => {
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, []);

  const startPick = () => {
    if (!comments || !inSlides) return;
    if (picking) {
      setPicking(false);
      return;
    }
    void comments.ensureName().then((named) => {
      if (named) setPicking(true);
    });
  };

  const askFull = () => {
    const el = document.documentElement;
    if (!document.fullscreenElement && typeof el.requestFullscreen === 'function')
      el.requestFullscreen().catch(() => undefined);
  };
  const leaveFull = () => {
    if (document.fullscreenElement && typeof document.exitFullscreen === 'function')
      document.exitFullscreen().catch(() => undefined);
  };
  const enterFull = () => {
    setMode(MODE.slides);
    setFull(FULL.on);
    if (!touch) askFull();
  };
  const exitFull = () => {
    setFull(FULL.left);
    leaveFull();
  };
  // The browser's own Esc leaves its full screen without a key for the page: the page leaves with it.
  const fullNow = useRef(isFull);
  useEffect(() => {
    fullNow.current = isFull;
  });
  useEffect(() => {
    const change = () => {
      if (!document.fullscreenElement && fullNow.current) setFull(FULL.left);
    };
    document.addEventListener('fullscreenchange', change);
    return () => document.removeEventListener('fullscreenchange', change);
  }, []);

  const onKey = useEffectEvent((event: KeyboardEvent) => {
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey || typingIn(event.target))
      return;
    if (draft) {
      if (event.code === 'Escape') setDraft(null);
      return;
    }
    if (event.code === 'Escape') {
      if (menu) setMenu(false);
      else if (picking) setPicking(false);
      else if (isFull) exitFull();
    } else if (event.code === 'KeyF') {
      if (isFull) exitFull();
      else enterFull();
    } else if (event.code === 'KeyC') startPick();
    else if (inSlides && (event.code === 'ArrowRight' || event.code === 'PageDown')) forward();
    else if (inSlides && (event.code === 'ArrowLeft' || event.code === 'PageUp')) back();
  });
  useEffect(() => {
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // The slide is drawn at the board's size and scaled to the slot.
  const w = slide?.w || SIZE_FALLBACK.w;
  const h = slide?.h || SIZE_FALLBACK.h;
  useEffect(() => {
    const el = slot.current;
    if (!el) return;
    el.style.setProperty('--hx-gw', `${w}px`);
    el.style.setProperty('--hx-gh', `${h}px`);
    const fit = () => el.style.setProperty('--hx-gk', String(el.clientWidth / w));
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(el);
    return () => watch.disconnect();
  }, [w, h, mode]);

  // The card takes the keyboard when it opens.
  const focusOnOpen = (el: HTMLTextAreaElement | null) => el?.focus();

  // The card opens where the place is.
  const card = useRef<HTMLElement>(null);
  const shownCard = cardView.value;
  useEffect(() => {
    if (!shownCard || !card.current) return;
    card.current.style.setProperty('--hx-px', `${shownCard.px}px`);
    card.current.style.setProperty('--hx-py', `${shownCard.py}px`);
  }, [shownCard]);

  // A click outside the menu closes it.
  useEffect(() => {
    if (!menu) return;
    const close = (event: PointerEvent) => {
      if (
        !(event.target instanceof Element) ||
        !event.target.closest('.hx-deck-guest__menu, [data-menu-trigger]')
      )
        setMenu(false);
    };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, [menu]);

  const send = () => {
    if (!draft || !comments || draft.text.trim() === '' || sending) return;
    setSending(true);
    comments
      .send(draft.slide, { x: draft.x, y: draft.y }, draft.label, draft.text.trim())
      .then((taken) => {
        if (taken) setDraft(null);
      })
      .finally(() => setSending(false));
  };

  const chapters: IDeckVideoChapter[] =
    video?.chapters.map((c) => {
      const s = slides[c.slide];
      return { id: `${s.id}@${c.t}`, title: c.title, t: c.t, frame: s.src, w: s.w, h: s.h };
    }) ?? [];

  const pins = comments?.pins.filter((p) => p.slide === index) ?? [];
  const lmode = mode;
  const bar = (
    <header className="hx-deck-guest__bar">
      {onBack && (
        <Button
          variant="ghost"
          size="sm"
          className="hx-deck-guest__back"
          onClick={onBack}
          aria-label={t('next.deck.guest.back')}
        >
          <Icon name="arrow-left" />
        </Button>
      )}
      <div className="hx-deck-guest__addr">
        <b>{title}</b>
        <span>/</span>
        <em>{t('next.deck.bar.crumbLink', { n: index + 1, total })}</em>
      </div>
      <span className="hx-deck-guest__grow" />
      {video && (
        <div className="hx-deck-guest__seg" role="radiogroup" aria-label={t('next.deck.share.how')}>
          <button
            type="button"
            role="radio"
            aria-checked={inSlides}
            data-on={inSlides ? '1' : '0'}
            onClick={() => setMode(MODE.slides)}
          >
            {t('next.deck.bar.slides')}
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={!inSlides}
            data-on={inSlides ? '0' : '1'}
            onClick={() => {
              setMode(MODE.video);
              setFull(FULL.off);
              setPicking(false);
            }}
          >
            {t('next.deck.bar.video')}
          </button>
        </div>
      )}
      {comments && (
        <Button
          variant="ghost"
          size="sm"
          className="hx-deck-guest__cm"
          onClick={startPick}
          aria-label={t('next.deck.bar.comment')}
          disabled={!inSlides}
        >
          <Icon name="message-square-plus" size="sm" />
          <span className="hx-deck-guest__lbl">{t('next.deck.bar.comment')}</span>
          <span className="hx-deck-guest__kbd">C</span>
        </Button>
      )}
      <Button variant="primary" size="sm" onClick={enterFull} aria-label={t('next.deck.bar.full')}>
        <Icon name="maximize-2" size="sm" />
        <span className="hx-deck-guest__lbl">{t('next.deck.bar.full')}</span>
        <span className="hx-deck-guest__kbd">F</span>
      </Button>
      {pdf !== null && (
        <Button
          variant="ghost"
          size="sm"
          data-menu-trigger="1"
          onClick={() => setMenu(!menu)}
          aria-label={t('next.deck.bar.more')}
          aria-expanded={menu}
        >
          <Icon name="ellipsis" />
        </Button>
      )}
    </header>
  );

  return (
    <div
      ref={root}
      className="hx-deck-guest"
      data-screen="link"
      data-lmode={lmode}
      data-full={isFull ? '1' : '0'}
      data-pick={picking ? '1' : '0'}
      data-touch={touch ? '1' : '0'}
    >
      {bar}

      <div className="hx-deck-guest__stage">
        <div
          ref={slot}
          className="hx-deck-guest__slot"
          aria-label={slide ? t('next.deck.stage.slide', { n: index + 1 }) : undefined}
        >
          {slide && (
            <iframe
              key={slide.id}
              ref={frame}
              className="hx-deck-guest__frame"
              data-ready={loaded ? '1' : '0'}
              src={slide.src}
              title={slide.title}
              onLoad={() => {
                setLoaded(true);
                sync();
              }}
            />
          )}
          {pins.map((pin) => (
            <GuestPin key={pin.id} pin={pin} n={(comments?.pins.indexOf(pin) ?? 0) + 1} />
          ))}
        </div>
        <div className="hx-deck-guest__pager">
          <Button
            variant="ghost"
            size="sm"
            disabled={index === 0 && step === 0}
            onClick={back}
            aria-label={t('next.deck.pager.prev')}
          >
            <Icon name="chevron-left" />
          </Button>
          <span>{t('next.deck.pager.count', { n: index + 1, total })}</span>
          <Button
            variant="ghost"
            size="sm"
            disabled={index === total - 1 && step >= steps}
            onClick={forward}
            aria-label={t('next.deck.pager.next')}
          >
            <Icon name="chevron-right" />
          </Button>
        </div>
      </div>

      {videoView.value && (
        <div className="hx-deck-guest__video" data-closing={videoView.state === 'closing' ? '1' : '0'}>
          <DeckVideo
            src={videoView.value.src}
            chapters={chapters}
            onPlaying={(chapter) =>
              setVideoSlide(
                chapter === null ? null : (slides[video?.chapters[chapter]?.slide ?? -1]?.path ?? null),
              )
            }
          />
        </div>
      )}

      {canComment && picking && (
        <div className="hx-deck-guest__pickbar" role="status">
          {t('next.deck.comments.pickBar')}
          <button type="button" onClick={() => setPicking(false)}>
            {t('next.deck.comments.cancel')}
          </button>
        </div>
      )}

      {shownCard && (
        <section
          ref={card}
          className="hx-deck-guest__cpop"
          data-closing={cardView.state === 'closing' ? '1' : '0'}
          role="dialog"
          aria-label={t('next.deck.comments.new')}
        >
          <div className="hx-deck-guest__chip">
            <Icon name="locate" size="sm" />
            {t('next.deck.comments.chip', {
              n: shownCard.slide + 1,
              label: shownCard.label || t('next.deck.comments.fallback'),
            })}
          </div>
          <textarea
            ref={focusOnOpen}
            value={draft?.text ?? shownCard.text}
            placeholder={t('next.deck.comments.placeholder')}
            aria-label={t('next.deck.comments.text')}
            onChange={(event) => draft && setDraft({ ...draft, text: event.target.value })}
          />
          <div className="hx-deck-guest__cpop-row">
            <Button size="sm" onClick={() => setDraft(null)}>
              {t('next.deck.comments.cancel')}
            </Button>
            <Button
              variant="primary"
              size="sm"
              busy={sending}
              disabled={(draft?.text.trim() ?? '') === ''}
              onClick={send}
            >
              {t('next.deck.comments.send')}
            </Button>
          </div>
        </section>
      )}

      {menuView.value && pdf !== null && (
        <div
          className="hx-deck-guest__menu"
          data-closing={menuView.state === 'closing' ? '1' : '0'}
          role="menu"
        >
          <a
            className="hx-deck-guest__item"
            role="menuitem"
            href={pdf}
            target="_blank"
            rel="noopener"
            download
            onClick={() => setMenu(false)}
          >
            <Icon name="download" />
            <span>
              {t('next.deck.menu.pdf')}
              <small>{t('next.deck.menu.pdfHint')}</small>
            </span>
          </a>
        </div>
      )}

      {capsule.value && (
        <div className="hx-deck-guest__cap" data-closing={capsule.state === 'closing' ? '1' : '0'}>
          <button type="button" onClick={exitFull}>
            <Icon name="minimize-2" size="sm" />
            <span className="hx-deck-guest__lbl">{t('next.deck.guest.exit')}</span>
            <span className="hx-deck-guest__kbd">Esc</span>
          </button>
          <i className="sep" />
          <button type="button" onClick={back} aria-label={t('next.deck.guest.back')}>
            <Icon name="chevron-left" size="sm" />
          </button>
          <span className="hx-deck-guest__time">
            <small>{t('next.deck.pager.count', { n: index + 1, total })}</small>
          </span>
          <button type="button" onClick={forward} aria-label={t('next.deck.guest.forward')}>
            <Icon name="chevron-right" size="sm" />
          </button>
          {comments && (
            <>
              <i className="sep" />
              <button type="button" onClick={startPick} data-on={picking ? '1' : '0'}>
                <Icon name="message-square-plus" size="sm" />
                <span className="hx-deck-guest__lbl">{t('next.deck.guest.comment')}</span>
                <span className="hx-deck-guest__kbd">C</span>
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
};

/** A comment's pin on the slide: the number in a drop at its place. */
const GuestPin = ({ pin, n }: { pin: IDeckGuestPin; n: number }) => {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.style.setProperty('--hx-pin-x', `${pin.x}%`);
    ref.current?.style.setProperty('--hx-pin-y', `${pin.y}%`);
  }, [pin.x, pin.y]);
  return (
    <div ref={ref} className="hx-deck-guest__pin">
      {n}
    </div>
  );
};

export default DeckGuest;
