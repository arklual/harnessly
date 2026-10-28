// The deck's left panel, notes tab, comments and pins on made-up data, for the eye and for screenshots: ?dev=deckpanel
// (&tab=chat|notes|comments, &mode=dock|float|off, &lang=en|ru, &theme=light|dark, &empty=1 for no comments). The
// runtime's deck routes are played in the page. A phone width (≤560) shows the sheet. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/deck.css';
import '@/next/dev/deckpanel-gallery.css';

import { useEffect, useRef, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import { DeckCommentLayer } from '@/next/features/designly/deck/deck-comments';
import { DeckPanel } from '@/next/features/designly/deck/deck-panel';
import { NotesSheet } from '@/next/features/designly/deck/deck-sheets';
import { startDeckPick, useDeckPick } from '@/next/features/designly/deck/comment-ui';
import { useDeckDictation } from '@/next/features/designly/deck/dictation';
import { useDeckComments } from '@/next/features/designly/deck/use-deck-comments';
import {
  DECK_PANEL,
  DECK_TAB,
  type IDeckComment,
  type IDeckSettings,
  type IDeckSlide,
  type IDeckState,
  type TDeckPanelMode,
  type TDeckTab,
} from '@/next/features/designly/deck/types';
import type { MachineId } from '@/next/protocol/v2';
import { setDeck, useDeck } from '@/next/stores/deck';
import { usePhone } from '@/next/ui/use-media';

const MACHINE = 'gallery' as MachineId;
const ART = 'course';

const SETTINGS: IDeckSettings = {
  v: 1,
  minutes: 10,
  slides: null,
  tone: 'talk',
  script: true,
  style: null,
  theme: 'co',
  font: 'inter',
};

const slide = (n: number, title: string, notes: string, voice = false): IDeckSlide => ({
  id: `p${n}`,
  path: `s${n}.dc.html`,
  title,
  w: 1920,
  h: 1080,
  notes,
  voice,
  seconds: Math.max(10, Math.round(notes.split(' ').length / 2.2 / 5) * 5),
  version: `v${n}`,
  blank: false,
});

const SLIDES = [
  slide(1, 'Нейросети в работе', 'Привет! Это вторая часть курса про нейросети в работе.'),
  slide(
    2,
    'Агент — это модель, инструменты и инструкции',
    'Запомните одну формулу. Модель думает, инструменты дают ей руки.',
  ),
  slide(
    3,
    'Как агент ходит в ваши сервисы',
    'MCP — это способ, которым агент подключается к вашим сервисам. Каждый сервис описывает, что он умеет.',
    true,
  ),
  slide(4, 'Один раз описали — дальше одинаково', 'Теперь скиллы. Скилл — это папка с инструкцией.'),
  slide(5, 'На этой неделе', ''),
];

const comment = (
  id: string,
  page: string,
  x: number,
  y: number,
  text: string,
  resolved: boolean,
): IDeckComment => ({
  id,
  page,
  x,
  y,
  label: 'Схема',
  text,
  by: { name: 'Рома', email: 'roma@example.org' },
  at: `2026-10-27T10:00:0${id.length}Z`,
  resolved,
  resolvedBy: null,
  resolvedAt: null,
});

const stateOf = (empty: boolean): IDeckState => ({
  rev: 1,
  settings: SETTINGS,
  chrome: true,
  ffmpeg: true,
  slides: SLIDES,
  comments: empty
    ? []
    : [
        comment('a', 'p3', 74, 30, 'Схему бы покрупнее', false),
        comment('bb', 'p3', 30, 60, 'Добавить подпись к стрелке', false),
        comment('ccc', 'p1', 20, 40, 'Заголовок длинноват', true),
      ],
  guestResolved: [],
  recordings: [],
  published: { video: false, pdf: false, pdfAuto: false, pdfBuilding: false, pdfAt: null, pdfError: null },
});

/** The runtime's deck routes, played in the page. */
const playRuntime = () => {
  const real = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const answer = (body: unknown) =>
      new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
    if (url.includes('/design/access/')) return new Response('{}', { status: 404 });
    if (!url.includes('/design/deck/')) return real(input, init);
    const body = (init?.body ? JSON.parse(String(init.body)) : {}) as Record<string, unknown>;
    await new Promise((resolve) => setTimeout(resolve, 250));
    if (url.includes('/comments') && init?.method === 'POST') {
      return answer({
        id: `n${Date.now()}`,
        page: body.page,
        x: body.x,
        y: body.y,
        label: body.label,
        text: body.text,
        by: { name: 'Анна', email: 'sergey@example.org' },
        at: new Date().toISOString(),
        resolved: false,
        resolvedBy: null,
        resolvedAt: null,
      });
    }
    if (url.includes('/comments')) {
      const have = stateOf(false).comments.find((c) => c.id === body.id);
      return answer({ ...(have ?? {}), resolved: body.resolved });
    }
    if (url.includes('/notes')) {
      const have = SLIDES.find((s) => s.id === body.page);
      return answer({ slide: { ...have, notes: body.text } });
    }
    return answer({});
  };
};

const DeckPanelGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const phone = usePhone();
  const narrow = typeof window !== 'undefined' && window.matchMedia('(max-width: 560px)').matches;
  const [mode, setMode] = useState<TDeckPanelMode>(
    query.mode === 'float' || query.mode === 'off' ? query.mode : DECK_PANEL.dock,
  );
  const [tab, setTab] = useState<TDeckTab>(
    query.tab === 'notes' || query.tab === 'comments' ? query.tab : DECK_TAB.chat,
  );
  const [index, setIndex] = useState(2);
  const [sheet, setSheet] = useState(query.sheet === 'notes');
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    playRuntime();
    setDeck(ART, stateOf(query.empty === '1'));
  }, [query.empty]);
  useEffect(() => {
    const el = document.documentElement;
    el.dataset.theme = dark ? 'dark' : 'light';
    el.classList.toggle('dark', dark);
    el.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  const deck = useDeck(ART);
  const comments = useDeckComments(MACHINE, ART);
  const dict = useDeckDictation(MACHINE, ART);
  const pick = useDeckPick(ART);
  // The fake deck lands in the store from the effect above; until then there is nothing to show.
  if (!deck) return null;
  return (
    <div className="hx-root">
      <div
        ref={root}
        className="hx-dg hx-deck"
        data-screen="edit"
        data-panel={mode}
        data-pick={pick.picking ? '1' : '0'}
      >
        <div className="hx-dg__bar">
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            onClick={() => startDeckPick(ART)}
          >
            C
          </button>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            onClick={() => setMode(DECK_PANEL.dock)}
          >
            P
          </button>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            onClick={() => setSheet(true)}
          >
            N
          </button>
        </div>
        <div className="hx-dg__stage">
          <div className="hx-dg__slot">
            <div className="hx-dg__slide">{SLIDES[index].title}</div>
            <DeckCommentLayer
              artifact={ART}
              slideIndex={index}
              comments={comments}
              hit={() => Promise.resolve('Схема')}
              phone={narrow}
              onAdded={() => {
                setTab(DECK_TAB.comments);
                if (mode === DECK_PANEL.off) setMode(DECK_PANEL.dock);
              }}
            />
          </div>
        </div>
        <DeckPanel
          mode={mode}
          tab={tab}
          onMode={setMode}
          onTab={setTab}
          stage={
            <div className="hx-dg__chat">
              <textarea rows={2} placeholder="Что поменять? Можно наговорить — кнопка с микрофоном" />
            </div>
          }
          slideIndex={index}
          onGo={setIndex}
          ctx={{ slide: index + 1, label: null }}
          phone={phone}
          machine={MACHINE}
          artifact={ART}
          dict={dict}
          comments={comments}
        />
        <NotesSheet
          machine={MACHINE}
          artifact={ART}
          slide={SLIDES[index]}
          slideIndex={index}
          open={sheet && narrow}
          onClose={() => setSheet(false)}
          dict={dict}
        />
      </div>
    </div>
  );
};

export default DeckPanelGallery;
