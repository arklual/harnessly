// The deck and the video in the two modes (looking and editing) on made-up data, for the eye and for screenshots:
// ?dev=viewmode&kind=deck|video&edit=1 (&lang=en|ru, &theme=dark). The runtime's routes are not played: the stores are
// seeded and the page is a blob. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/deck.css';
import '@/next/styles/video-player.css';
import '@/next/styles/video-overlays.css';
import '@/next/styles/video-timeline.css';
import '@/next/styles/video-shell.css';
import { useEffect, useMemo, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale, t } from '@/next/i18n';
import { MODE_KIND, modeStateKey } from '@/next/features/designly/design-mode';
import type { MachineId } from '@/next/protocol/v2';
import DeckView from '@/next/features/designly/deck/deck-view';
import VideoView from '@/next/features/designly/video/video-view';
import type { ISiteMode } from '@/next/features/designly/site-view';
import { videoState } from '@/next/features/designly/video/video-fixture';
import type { IDeckState } from '@/next/features/designly/deck/types';
import { setDeck } from '@/next/stores/deck';
import { setVideo } from '@/next/stores/video';

const ID = 'gallery-view';
const boards = [1, 2, 3].map((n) => ({ path: `s${n}.dc.html`, title: `Слайд ${n}` }));

const deckState = (): IDeckState => ({
  rev: 1,
  settings: {
    v: 1,
    minutes: 10,
    slides: null,
    tone: 'talk',
    script: true,
    style: null,
    theme: 'co',
    font: 'inter',
  },
  chrome: true,
  ffmpeg: false,
  slides: boards.map((b, i) => ({
    id: `P${i + 1}`,
    path: b.path,
    title: b.title,
    w: 1920,
    h: 1080,
    notes: `Заметки: ${b.title}`,
    voice: false,
    seconds: 20,
    version: `v${i}`,
    blank: false,
  })),
  comments: [],
  guestResolved: [],
  recordings: [],
  published: { video: false, pdf: false, pdfAuto: false, pdfBuilding: false, pdfAt: null, pdfError: null },
});

const page = (title: string) =>
  `<body style="margin:0;display:grid;place-items:center;height:100vh;background:#b8f06b;font:700 64px sans-serif">${title}</body>`;

const ViewModeGallery = () => {
  const { query } = useRouter();
  const lang = query.lang === 'en' ? 'en' : 'ru';
  pinLocale(lang);
  const dark = query.theme === 'dark';
  const video = query.kind === 'video';
  const [edit, setEdit] = useState(query.edit === '1');
  const [path, setPath] = useState(boards[0].path);
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    setDeck(ID, deckState());
    setVideo(ID, videoState());
  }, [dark]);
  const src = useMemo(() => URL.createObjectURL(new Blob([page(path)], { type: 'text/html' })), [path]);
  const word = video ? MODE_KIND.video : MODE_KIND.slides;
  const mode: ISiteMode = {
    state: t(modeStateKey(word, edit)),
    editing: edit,
    action: { onClick: () => setEdit(!edit), ask: false },
  };
  const common = {
    machine: 'm1' as MachineId,
    id: ID,
    title: 'Презентация для партнёров',
    boards,
    path,
    onGo: setPath,
    src,
    frameSrc: () => src,
    slotError: null,
    onReload: () => undefined,
    mode,
    viewing: !edit,
    maxed: false,
    onToggleMax: null,
    onOpenDrawer: () => undefined,
    fallback: null,
  };
  return (
    <div className="hx-root" style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      {video ? <VideoView {...common} /> : <DeckView {...common} mayEdit />}
    </div>
  );
};

export default ViewModeGallery;
