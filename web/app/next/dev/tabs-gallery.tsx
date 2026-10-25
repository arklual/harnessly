// The strip of tabs over the window on made-up data (`?dev=tabs`, only where the installation has next-ui), for
// screenshots: the real TabStrip with its menu, drag and tooltip, and a window under it.
//
//   ?dev=tabs                      six tabs, the first one busy, one waiting for the person
//   &set=long|short|many           long titles, only short ones, or enough tabs to scroll the strip
//   &theme=light|dark&lang=en|ru
import { useEffect, useState } from 'react';
import AppFrame from '@/next/features/shell/app-frame';
import TabStrip, { PACE, type IStripTab, type TPace } from '@/next/features/shell/tab-strip';
import { moveTab, type ITabsState } from '@/next/stores/tabs';
import { pinLocale, type TLang } from '@/next/i18n';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/sidebar.css';
import '@/next/styles/header.css';
import '@/next/styles/tabs.css';

const params = new URLSearchParams(window.location.search);
const SET = params.get('set') ?? 'default';
const LANG: TLang = params.get('lang') === 'en' ? 'en' : 'ru';
pinLocale(LANG);

const tab = (
  id: string,
  title: string,
  icon = 'message-square',
  busy: TPace | null = null,
  attention = false,
  note?: string,
): IStripTab => ({
  id,
  title,
  icon,
  busy,
  attention,
  ...(note ? { note } : {}),
});

const SETS: Record<string, readonly IStripTab[]> = {
  default: [
    tab('a', 'Доступ к dev-машине Acme Admin', 'loader-circle', PACE.working),
    tab('b', 'Вкладки сверху: правки', 'message-square'),
    tab('c', 'Designly: афиша фестиваля', 'palette'),
    tab('d', 'Главная', 'house'),
    tab('e', 'Нужно разрешение на запуск', 'shield-question', null, true),
    tab('g', 'Фоновая работа ещё идёт', 'loader-circle', PACE.background, false, 'Ждёт: команда npm test'),
    tab('f', 'Настройки', 'settings'),
  ],
  long: [
    tab('a', 'Доступ к dev-машине Acme Admin', 'loader-circle', PACE.working),
    tab(
      'b',
      'Разобрать, почему падает сборка Angular 22 при SSR на стенде и что с этим делать дальше',
      'message-square',
    ),
    tab('c', 'Designly: афиша фестиваля «Осенний джем» в трёх форматах для соцсетей', 'palette'),
  ],
  short: [tab('a', 'Главная', 'house'), tab('b', 'Чат', 'message-square'), tab('c', 'Apps', 'app-window')],
  many: Array.from({ length: 16 }, (_, i) =>
    tab(
      `m${i}`,
      `Задача номер ${i + 1}: разбор и правки`,
      i === 2 ? 'loader-circle' : 'message-square',
      i === 2 ? PACE.working : null,
    ),
  ),
};

const Gallery = () => {
  const [tabs, setTabs] = useState<readonly IStripTab[]>(SETS[SET] ?? SETS.default);
  const [active, setActive] = useState(tabs[0].id);
  const [log, setLog] = useState('');
  useEffect(() => {
    const root = document.documentElement;
    const theme = params.get('theme') === 'light' ? 'light' : 'dark';
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, []);
  // The gallery keeps the order the way the store does, so the drop is the same move.
  const move = (id: string, index: number) => {
    const state: ITabsState = {
      tabs: tabs.map((x) => ({ id: x.id, path: '/', title: null, scroll: null, view: null })),
      active,
    };
    const next = moveTab(state, id, index).tabs.map((x) => x.id);
    setTabs(next.map((key) => tabs.find((x) => x.id === key) as IStripTab));
    setLog(`move ${id} -> ${index}`);
  };
  const close = (id: string) => {
    const left = tabs.filter((x) => x.id !== id);
    setTabs(left.length > 0 ? left : [tab('z', 'Главная', 'house')]);
    if (active === id) setActive((left[0] ?? { id: 'z' }).id);
    setLog(`close ${id}`);
  };
  const closeOthers = (id: string) => {
    setTabs(tabs.filter((x) => x.id === id));
    setActive(id);
    setLog(`others ${id}`);
  };
  const closeRight = (id: string) => {
    const at = tabs.findIndex((x) => x.id === id);
    setTabs(tabs.slice(0, at + 1));
    if (!tabs.slice(0, at + 1).some((x) => x.id === active)) setActive(id);
    setLog(`right ${id}`);
  };
  return (
    <AppFrame
      rail={false}
      drawerOpen={false}
      onDrawerChange={() => undefined}
      sidebar={null}
      tabs={
        <TabStrip
          tabs={tabs}
          active={active}
          onActivate={setActive}
          onClose={close}
          onCloseOthers={closeOthers}
          onCloseRight={closeRight}
          onNew={() => setTabs((list) => [...list, tab(`n${list.length}`, 'Новая вкладка', 'house')])}
          onMove={move}
        />
      }
    >
      <div className="hx-lg-body" data-log={log}>
        <b data-testid="log">{log}</b>
        <p data-testid="order">{tabs.map((x) => x.id).join(',')}</p>
      </div>
    </AppFrame>
  );
};

const TabsGallery = () => <Gallery />;

export default TabsGallery;
