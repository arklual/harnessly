// Designly in the right pane on made-up data, for the eye and for screenshots: ?dev=panedl with the
// installation's next-ui feature. &view=library shows the library alone, &view=design (the default) an open design;
// &states=0 gives the page no settings, &pages=1 only one page, &pages=many eight of them (the pager's list gets its search), &loading=1 keeps the page loading, &theme=light|dark and &lang=en|ru pick the look.
// &nav=1 gives the page an app's own bars, a header that sticks to the top and a tab bar fixed to the bottom, to see
// that the pane's controls leave them free on a phone. &bridge=1 puts the slot's real bridge into the page (its script is
// asked for at /__tk/bridge.js of the gallery, which a test answers with the built dist/preview-bridge.js): the pins
// then follow their blocks. &lib=1 puts the library's board script in instead (/__lib/client.js, which a test answers
// with internal/design/texts/client.js), as a real design page has it: picking, looks and pins come from it. The page has a row of chips that scrolls sideways, a list that scrolls in its own box and a
// sidebar «Фильтры» that slides over the page, to see a pin cut, followed and hidden.
// The chat's field on the left is the real composer: a block picked on the page lands in
// it as a chip. The page is a blob document that speaks the slot's bridge, so picking and settings work as on a
// machine. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/styles/composer.css';
import '@/next/styles/pane.css';
import '@/next/dev/panedl-gallery.css';

import { useEffect, useMemo, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import ToolPane from '@/next/features/pane/tool-pane';
import DesignlyHome, { type IDesignCard } from '@/next/features/designly/designly-home';
import SiteView, { type ISitePage, type ISiteTab } from '@/next/features/designly/site-view';
import Composer from '@/next/features/composer/composer';
import { noteQuote } from '@/next/features/designly/site-notes';
import { usePaneChatStage } from '@/next/bindings/pane-stage';
import { patchDesignNotes, useDesignNotes } from '@/next/stores/design-notes';
import type { ConvId, MachineId } from '@/next/protocol/v2';

const CONV = 'gallery-chat' as ConvId;
const MACHINE = 'gallery' as MachineId;
const DESIGN = 'ostrovok';

const PAGES: ISitePage[] = [
  { path: 'home.html', name: 'Главная' },
  { path: 'offer.html', name: 'Выдача' },
];

const MANY_PAGES: ISitePage[] = [
  ...PAGES,
  ...['Поиск', 'Отель', 'Бронь', 'Оплата', 'Профиль', 'Помощь'].map((name, i) => ({
    path: `screens/${i + 1}.html`,
    name,
  })),
];

/** The bridge of the slot as the made-up page speaks it: picking, settings and the canvas's keys, no following of pins. */
const fakeBridge = (states: boolean) => `<script>
  var picking = false, seq = 0, ids = new WeakMap();
  var send = function (m) { parent.postMessage(m, '*'); };
  var tweaks = ${
    states
      ? `[
    { name: 'theme', label: 'Тема', section: 'Тема', editor: 'enum', value: 'light', options: [{ value: 'light', text: 'Светлая' }, { value: 'dark', text: 'Тёмная' }] },
    { name: 'screen', label: 'screen', section: 'Состояния', editor: 'enum', value: 'empty', options: [
      { value: 'empty', text: 'Главная: пусто' }, { value: 'bcn', text: 'Главная: Барселона с малышом' },
      { value: 'age', text: 'Гости: возраст ребёнка' }, { value: 'tokyo', text: 'Главная: Токио' }, { value: 'offer', text: 'Выдача' } ] }
  ]`
      : '[]'
  };
  addEventListener('message', function (e) {
    var m = e.data || {};
    if (m.type === 'tk:ping') send({ type: 'tk:hello' });
    if (m.type === 'tk:tweaks-host') send({ type: 'tk:tweaks', controls: tweaks });
    if (m.type === 'tk:pick') picking = !!m.on;
    if (m.type === 'tk:canvas') canvas = !!m.on;
  });
  // The canvas's keys, wheel and middle drag, as the slot's bridge tells them (lib/hl-preview/bridge-core.ts).
  var canvas = false, spaceDown = false, mid = null;
  addEventListener('keydown', function (e) {
    if (!canvas || e.code !== 'Space' || /INPUT|TEXTAREA/.test(e.target.tagName)) return;
    e.preventDefault();
    if (!e.repeat && !spaceDown) { spaceDown = true; send({ type: 'tk:space', down: true }); }
  }, true);
  addEventListener('keyup', function (e) {
    if (e.code === 'Space' && spaceDown) { spaceDown = false; send({ type: 'tk:space', down: false }); }
  }, true);
  addEventListener('wheel', function (e) {
    if (e.ctrlKey || e.metaKey) { e.preventDefault(); send({ type: 'tk:wheel', x: e.clientX, y: e.clientY, dy: e.deltaY }); return; }
  }, { passive: false });
  addEventListener('pointerdown', function (e) {
    if (!canvas || e.button !== 1) return;
    e.preventDefault(); mid = { x: e.screenX, y: e.screenY };
  }, true);
  addEventListener('pointermove', function (e) {
    if (!mid) return;
    send({ type: 'tk:pan', dx: e.screenX - mid.x, dy: e.screenY - mid.y, by: 'drag' });
    mid = { x: e.screenX, y: e.screenY };
  }, true);
  addEventListener('pointerup', function () { mid = null; }, true);
  document.addEventListener('click', function (e) {
    if (!picking) return;
    e.preventDefault(); e.stopPropagation();
    var el = e.target, r = el.getBoundingClientRect();
    if (!ids.has(el)) ids.set(el, ++seq);
    picking = false;
    send({ type: 'tk:picked', pick: ids.get(el), selector: el.tagName.toLowerCase() + (el.className ? '.' + el.className : ''), tag: el.tagName.toLowerCase(),
      text: (el.textContent || '').trim().slice(0, 80), rect: { x: r.x, y: r.y, width: r.width, height: r.height }, outerHTML: el.outerHTML.slice(0, 300),
      look: { background: '#2742e8', radius: 12, opacity: 1 } });
    send({ type: 'tk:pick', on: false });
  }, true);
  send({ type: 'tk:hello' });
</script>`;

/** What speaks for the page: the made-up bridge, the slot's real bridge or the library's board script. */
const PAGE_SCRIPT = { fake: 'fake', slot: 'slot', lib: 'lib' } as const;
type TPageScript = (typeof PAGE_SCRIPT)[keyof typeof PAGE_SCRIPT];

const scriptTag = (script: TPageScript) => {
  const origin = window.location.origin;
  if (script === PAGE_SCRIPT.slot)
    return `<script src="${origin}/__tk/bridge.js" data-parent="${origin}"></script>`;
  if (script === PAGE_SCRIPT.lib)
    return `<script src="${origin}/__lib/client.js" data-artifact="ostrovok"></script>`;
  return '';
};

/** The board: a phone's screen with the blocks the person can pick, speaking the bridge as the slot's does. */
const pageSource = (
  states: boolean,
  dark: boolean,
  nav: boolean,
  script: TPageScript,
) => `<!doctype html><meta charset="utf-8">
${scriptTag(script)}
<style>
  body { margin: 0; font: 16px/1.4 system-ui, sans-serif; background: ${dark ? '#17171c' : '#f4f2ee'}; color: ${dark ? '#f2f2f5' : '#1b1b21'}; }
  .hero { margin: 0; padding: 40px 28px 32px; background: #b8f06a; color: #101010; }
  .hero h1 { margin: 0 0 6px; color: #2742e8; font-size: 30px; }
  .hero h2 { margin: 0 0 20px; font-size: 28px; line-height: 1.1; }
  .card { background: #fff; border-radius: 16px; padding: 14px 16px; margin: 0 0 10px; color: #222; }
  .card small { display: block; color: #777; }
  .cta { display: block; width: 100%; box-sizing: border-box; padding: 16px; border: 0; border-radius: 14px; background: #2742e8; color: #fff; font: 700 20px system-ui; }
  section { padding: 24px 28px; }
  .top { position: sticky; top: 0; z-index: 1; display: flex; align-items: center; justify-content: space-between; padding: 14px 20px; background: #101010; color: #fff; font-weight: 700; }
  .tabs { position: fixed; left: 0; right: 0; bottom: 0; display: flex; justify-content: space-around; padding: 10px 0 14px; background: #101010; color: #bbb; font-size: 12px; }
  .tabs b { color: #fff; }
  .top ~ section { padding-bottom: 88px; }
  .chips { display: flex; gap: 8px; overflow-x: auto; padding: 12px 20px; scrollbar-width: none; }
  .chip { flex: none; padding: 8px 14px; border-radius: 999px; background: #fff; color: #222; font-weight: 600; }
  .chip.on { background: #222; color: #fff; }
  .list { height: 220px; overflow-y: auto; margin: 0 20px; padding: 10px; border-radius: 18px; background: rgba(0,0,0,.06); }
  .list .card { height: 64px; }
  .side { position: fixed; top: 0; bottom: 0; right: 0; width: 70%; z-index: 5; padding: 24px; box-sizing: border-box; background: #fff; color: #222; box-shadow: -8px 0 24px rgba(0,0,0,.2); transform: translateX(110%); transition: transform 240ms ease; }
  .side.open { transform: none; }
  .filters { margin: 12px 20px; padding: 10px 14px; border: 0; border-radius: 12px; background: #222; color: #fff; font: 600 15px system-ui; }
  .more .card { height: 90px; }
</style>
${
  nav
    ? `<div class="top"><span>Островок</span><span>Войти</span></div>
<nav class="tabs"><b>Поиск</b><span>Избранное</span><span>Поездки</span><span>Профиль</span></nav>`
    : ''
}
<div class="hero"><h1>Островок!</h1><h2>Найдём отель<br>под вашу поездку</h2>
  <div class="card"><small>Направление</small><b>Барселона</b></div>
  <div class="card"><small>Даты</small><b>12–15 ноя</b></div>
  <button class="cta">Найти</button></div>
<section><h3>Для семьи с малышом</h3><p>Барселона, 12–15 ноября · ребёнку 1 год</p>
  <div class="card"><b>Отель у Рамбла-де-Каталунья</b><small>Эшампле · 6 мин до пл. Каталонии</small></div></section>
<div class="chips"><span class="chip on">Все 64</span><span class="chip">Номера 22</span><span class="chip">Ванная 8</span><span class="chip">Завтрак 11</span><span class="chip">Бассейн 4</span><span class="chip">Парковка 9</span><span class="chip">Терраса 3</span><span class="chip">Кухня 5</span><span class="chip">Вид на море 7</span><span class="chip">Тишина 12</span></div>
<button class="filters" onclick="document.querySelector('.side').classList.toggle('open')">Фильтры</button>
<div class="list">${Array.from({ length: 8 }, (_, i) => `<div class="card"><b>Номер ${i + 1}</b><small>${2 + (i % 3)} гостя · ${7900 + i * 650} ₽</small></div>`).join('')}</div>
<section class="more">${Array.from({ length: 6 }, (_, i) => `<div class="card"><b>Отель ${i + 2}</b><small>${i + 3} мин до моря</small></div>`).join('')}</section>
<aside class="side"><h3>Фильтры</h3><p>Цена, удобства, рейтинг</p></aside>
${script === PAGE_SCRIPT.fake ? fakeBridge(states) : ''}`;

const shot = (a: string, b: string) =>
  `data:image/svg+xml;utf8,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="520" height="320"><rect width="520" height="320" fill="${a}"/><rect x="40" y="48" width="280" height="30" rx="8" fill="${b}"/><rect x="40" y="96" width="200" height="18" rx="8" fill="${b}" opacity=".6"/><rect x="40" y="200" width="440" height="72" rx="16" fill="${b}" opacity=".35"/></svg>`,
  )}`;

const cards = (now: number): IDesignCard[] => [
  {
    id: 'ostrovok',
    title: 'Досье отеля — хакатон Островка',
    boards: 2,
    updatedAt: now - 60_000,
    image: shot('#1b1b21', '#7b61ff'),
    kind: 'site',
    pinned: true,
    drawing: false,
    owner: null,
  },
  {
    id: 'harnessly',
    title: 'harnessly',
    boards: 3,
    updatedAt: now - 3_600_000,
    image: shot('#f4f2ee', '#2742e8'),
    kind: 'site',
    pinned: false,
    drawing: false,
    owner: null,
  },
  {
    id: 'analytics',
    title: 'Новая аналитика',
    boards: 6,
    updatedAt: now - 86_400_000,
    image: shot('#0f1a24', '#2fb67c'),
    kind: 'site',
    pinned: false,
    drawing: false,
    owner: null,
  },
  {
    id: 'deck',
    title: 'Питч-дек',
    boards: 8,
    updatedAt: now - 2 * 86_400_000,
    image: shot('#f3eefc', '#7b61ff'),
    kind: 'slides',
    pinned: false,
    drawing: true,
    owner: null,
  },
];

const FIRST: ISiteTab = { id: 't1', hist: [PAGES[0].path], hi: 0 };

/** The viewer as the pane gives it, on the made-up page. */
const Viewer = ({
  states,
  dark,
  single,
  many,
  loading,
  nav,
  script,
}: {
  states: boolean;
  dark: boolean;
  single: boolean;
  many: boolean;
  loading: boolean;
  nav: boolean;
  script: TPageScript;
}) => {
  const [tab, setTab] = useState(FIRST);
  const [reloadKey, setReloadKey] = useState(0);
  const all = many ? MANY_PAGES : PAGES;
  const pages = single ? all.slice(0, 1) : all;
  // A blob page is of the gallery's own origin, which the view can post to (a data: page has the origin «null»).
  const src = useMemo(
    () => URL.createObjectURL(new Blob([pageSource(states, dark, nav, script)], { type: 'text/html' })),
    [states, dark, nav, script],
  );
  const path = tab.hist[tab.hi];
  const { stage, chat } = usePaneChatStage({
    machine: MACHINE,
    design: DESIGN,
    fromChat: CONV,
    dir: '/home/coder/design/ostrovok',
    touched: [],
    title: 'Досье отеля — хакатон Островка',
    pageUrl: `/d/${DESIGN}/${path}`,
    port: 5555,
    onOpenDrawer: () => {},
  });
  return (
    <SiteView
      title="Досье отеля — хакатон Островка"
      pages={pages}
      tabs={[tab]}
      tab={tab.id}
      src={loading ? null : src}
      error={null}
      reloadKey={reloadKey}
      onPickTab={() => {}}
      onCloseTab={() => {}}
      onNewTab={() => {}}
      onNavigate={(next) =>
        setTab((x) => ({ ...x, hist: [...x.hist.slice(0, x.hi + 1), next], hi: x.hi + 1 }))
      }
      onHistory={(step) =>
        setTab((x) => ({ ...x, hi: Math.max(0, Math.min(x.hist.length - 1, x.hi + step)) }))
      }
      onReload={() => setReloadKey((n) => n + 1)}
      onOpenOutside={() => {}}
      onCopyLink={() => {}}
      onRetry={() => setReloadKey((n) => n + 1)}
      onOpenDrawer={() => {}}
      onToggleMax={() => {}}
      maxed={false}
      stage={stage}
      pane={{ onShare: () => {}, chat }}
      memoryKey={new URLSearchParams(window.location.search).get('design') ?? DESIGN}
    />
  );
};

/** The chat's field: the real composer, its chips are the notes made in the pane. */
const ChatField = () => {
  const [text, setText] = useState('');
  const notes = useDesignNotes(CONV);
  return (
    <div className="hx-gallery-pd__chat">
      <div className="hx-gallery-pd__msg">
        Выделите блок на макете справа: заметка попадёт сюда, в поле чата, и уйдёт агенту вместе с сообщением.
      </div>
      <Composer
        value={text}
        onChange={setText}
        onSubmit={() => setText('')}
        mode="idle"
        onStop={() => Promise.resolve()}
        picker={null}
        attachments={[]}
        onFiles={() => {}}
        onRemoveAttachment={() => {}}
        onRetryAttachment={() => {}}
        quotes={notes.map((note, i) => noteQuote(note, i + 1))}
        onRemoveQuote={(id) => patchDesignNotes(CONV, (have) => have.filter((n) => n.id !== id))}
        recording={null}
        onCancelRecording={() => {}}
        onFinishRecording={() => {}}
        maxText={100000}
      />
    </div>
  );
};

const pageScriptOf = (slot: boolean, lib: boolean): TPageScript => {
  if (slot) return PAGE_SCRIPT.slot;
  if (lib) return PAGE_SCRIPT.lib;
  return PAGE_SCRIPT.fake;
};

const PanedlGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  const lang = query.lang === 'en' ? 'en' : 'ru';
  pinLocale(lang);
  const library = query.view === 'library';
  const [now] = useState(() => Date.now());
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  return (
    <div className="hx-root hx-gallery-pd">
      <ChatField />
      <div className="hx-gallery-pd__pane">
        <ToolPane
          tabs={[{ id: 'd', icon: 'palette', name: 'Designly' }]}
          tab="d"
          ownBar
          maxed={false}
          onPick={() => {}}
          onCloseTab={() => {}}
          onToggleMax={() => {}}
          onClose={() => {}}
        >
          {library ? (
            <DesignlyHome
              cards={cards(now)}
              error={null}
              now={now}
              onPin={() => Promise.resolve()}
              onOpen={() => {}}
              onRetry={() => {}}
              compose={null}
            />
          ) : (
            <Viewer
              states={query.states !== '0'}
              dark={dark}
              single={query.pages === '1'}
              many={query.pages === 'many'}
              loading={query.loading === '1'}
              nav={query.nav === '1'}
              script={pageScriptOf(query.bridge === '1', query.lib === '1')}
            />
          )}
        </ToolPane>
      </div>
    </div>
  );
};

export default PanedlGallery;
