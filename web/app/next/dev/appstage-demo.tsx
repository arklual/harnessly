// An app's object open in Sinopia's stage on made-up data, for the shell gallery (?dev=shell&route=doc|board): the real
// AppStage, SiteChat and SiteIsland, the chat at `&place=chat|right|float|island`, and a drawn document or board where
// the app's view stands. Never part of a production path.
import { useState } from 'react';
import AppStage from '@/next/features/app-main/app-stage';
import { firstFloat, SiteChat, SiteIsland, type TSiteMode } from '@/next/features/designly/site-chat';
import type { IFloat, TDock } from '@/next/features/designly/site-drag';
import type { ISiteStage } from '@/next/features/designly/site-view';
import '@/next/styles/chat.css';
import '@/next/dev/appstage-demo.css';

export type TDemoKind = 'doc' | 'board';
export type TDemoPlace = 'chat' | 'right' | 'float' | 'island';

const APP = {
  doc: { title: 'Документы', icon: 'file-text', object: 'План выката приложений' },
  board: { title: 'Доски', icon: 'shapes', object: 'Схема сервиса оплаты' },
} as const;

const FEED = [
  { me: true, text: 'Собери план выката документов и досок как приложений' },
  {
    me: false,
    text: 'Готово: документ «План выката приложений» открыт справа. Разделы — порядок выката, проверки и откат.',
  },
  { me: true, text: 'Добавь таблицу рисков' },
  { me: false, text: 'Добавил раздел «Риски» с таблицей: вероятность, влияние, что делаем.' },
];

const DemoChat = () => (
  <div className="hx-demo-chat">
    <div className="hx-demo-chat__feed">
      {FEED.map((m) => (
        <p key={m.text} className="hx-demo-chat__msg" data-me={m.me}>
          {m.text}
        </p>
      ))}
    </div>
    <div className="hx-demo-chat__field">Напишите, что поправить…</div>
  </div>
);

const DemoDoc = () => (
  <article className="hx-demo-doc">
    <h1>План выката приложений</h1>
    <p className="hx-demo-doc__meta">Обновлён сегодня · 4 раздела</p>
    <h2>Порядок</h2>
    <ol>
      <li>Хаб с адресами сервисов в установке.</li>
      <li>Сервисы с видом приложения, переключатель выключен.</li>
      <li>Включение у одной организации, потом у всех.</li>
    </ol>
    <h2>Риски</h2>
    <table>
      <thead>
        <tr>
          <th>Риск</th>
          <th>Влияние</th>
          <th>Что делаем</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td>Сервис недоступен</td>
          <td>Высокое</td>
          <td>Экран «недоступно», ссылки живут</td>
        </tr>
        <tr>
          <td>Старые ссылки</td>
          <td>Среднее</td>
          <td>Ключ ?k= работает как раньше</td>
        </tr>
      </tbody>
    </table>
  </article>
);

const DemoBoard = () => (
  <div className="hx-demo-board" role="img" aria-label="Схема сервиса оплаты">
    <div className="hx-demo-board__tools">
      {['↖', '▭', '◯', '◇', '→', 'T', '✎'].map((tool) => (
        <span key={tool}>{tool}</span>
      ))}
    </div>
    <svg viewBox="0 0 640 360" className="hx-demo-board__svg">
      <rect x="40" y="60" width="150" height="70" rx="10" />
      <text x="115" y="100">
        Витрина
      </text>
      <rect x="250" y="60" width="150" height="70" rx="10" />
      <text x="325" y="100">
        Гейтвей
      </text>
      <rect x="460" y="60" width="150" height="70" rx="10" />
      <text x="535" y="100">
        Платежи
      </text>
      <rect x="250" y="230" width="150" height="70" rx="10" />
      <text x="325" y="270">
        Заказы
      </text>
      <path d="M190 95 H250 M400 95 H460 M325 130 V230" />
    </svg>
  </div>
);

export const AppStageDemo = ({
  kind,
  place,
  onOpenDrawer,
  toolPane,
}: {
  kind: TDemoKind;
  place: TDemoPlace;
  onOpenDrawer: () => void;
  toolPane: { open: boolean; onToggle: () => void };
}) => {
  const app = APP[kind];
  const [mode, setMode] = useState<TSiteMode>(place === 'right' ? 'chat' : place);
  const [dock, setDock] = useState<TDock>(place === 'right' ? 'right' : 'left');
  const [chatW, setChatW] = useState(372);
  const [float, setFloat] = useState<IFloat | null>(null);
  const [home, setHome] = useState(false);
  const [text, setText] = useState('');
  const onMode = (next: TSiteMode, section: { width: number; height: number }) => {
    if (next === 'float' && !float) setFloat(firstFloat(section));
    setMode(next);
  };
  const stage: ISiteStage = {
    conv: null,
    mode,
    onMode,
    min: false,
    run: 'idle',
    float: float ?? (mode === 'float' ? firstFloat({ width: 1100, height: 760 }) : null),
    dock,
    chatW,
    chat: (
      <SiteChat
        title={app.object}
        mode={mode}
        float={float ?? firstFloat({ width: 1100, height: 760 })}
        onFloat={setFloat}
        dock={dock}
        width={chatW}
        onWidth={setChatW}
        onZone={(zone) => {
          if (zone === 'left' || zone === 'right') {
            setDock(zone);
            setMode('chat');
          } else if (zone === 'float' || zone === 'island') setMode(zone);
        }}
        onHide={() => setMode('island')}
        onOpenDrawer={onOpenDrawer}
        onMode={onMode}
        more={[]}
        onOpenSite={() => undefined}
      >
        <DemoChat />
      </SiteChat>
    ),
    island:
      mode === 'island' || mode === 'notes' ? (
        <SiteIsland
          value={text}
          onChange={setText}
          onSend={() => setText('')}
          canSend={text.trim() !== ''}
          placeholder="Что поправить в документе?"
          sending={false}
          status={null}
          last={null}
          onStop={() => undefined}
          onDismiss={() => undefined}
          onExpand={() => setMode('chat')}
          min={false}
          onMin={() => undefined}
          mode={mode}
          onZone={() => undefined}
          recording={false}
          notes={0}
          pick={false}
          onPick={() => undefined}
        />
      ) : null,
    overlay: null,
    pins: null,
    pick: false,
    onPicked: () => undefined,
    onPickEnd: () => undefined,
    onBridge: () => undefined,
    styles: [],
    onStyled: () => undefined,
    anchors: [],
    picking: null,
    onPick: () => undefined,
    sview: false,
    onSview: () => undefined,
    onAttach: () => undefined,
  };
  return (
    <AppStage
      title={app.title}
      icon={app.icon}
      object={home ? null : { title: app.object }}
      onHome={() => setHome(true)}
      stage={stage}
      frame={kind === 'doc' ? <DemoDoc /> : <DemoBoard />}
      inspector={null}
      menu={[{ id: 'about', icon: 'info', label: 'О приложении', onSelect: () => undefined }]}
      onShare={() => undefined}
      toolPane={toolPane}
      onOpenDrawer={onOpenDrawer}
    />
  );
};
