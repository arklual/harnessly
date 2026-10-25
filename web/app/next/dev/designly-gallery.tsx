// The card of a block on a design's page, on made-up data, for the eye and for screenshots: ?dev=designly with
// the installation's next-ui feature. &look=0 is a block the page gave no settings for, &tune=0 folds the settings,
// &theme=light|dark and &lang=en|ru pick the look; &chrome=0 hides the toolbar. &view=island shows the folded chat over
// the site instead, &status=run|wait|done|none its state (the open chat is the usual one, see ?dev=chat). Never part of
// a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/notes.css';
import '@/next/styles/chat.css';
import '@/next/styles/site.css';

import { useEffect, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import { NO_EDIT, type IStyleEdit } from '@/next/features/designly/element-style';
import type { IPicked } from '@/next/features/designly/page-bridge';
import { NotePopover, type INoteDraft } from '@/next/features/designly/site-notes';
import {
  IslandLast,
  ISLAND_STATUS,
  SiteIsland,
  type TIslandStatus,
} from '@/next/features/designly/site-chat';

const PICKED: IPicked = {
  id: 7,
  look: { background: '#7b61ff', radius: 12, opacity: 1 },
  selector: 'main > section.hero > a.cta',
  tag: 'a',
  text: 'Создать ссылки для рассылки и афиш',
  rect: { x: 0, y: 0, width: 240, height: 44 },
  component: 'HeroCta',
  html: '<a class="cta">Создать ссылки для рассылки и афиш</a>',
};

const STATUS_TEXT = {
  run: 'Правлю шапку на «Главной»',
  wait: 'Агент ждёт вашего ответа',
  done: 'Готово',
} as const;

const statusOf = (k: unknown): TIslandStatus => {
  if (k === ISLAND_STATUS.wait) return { k: ISLAND_STATUS.wait, text: STATUS_TEXT.wait };
  if (k === ISLAND_STATUS.done) return { k: ISLAND_STATUS.done, text: STATUS_TEXT.done };
  if (k === 'none') return null;
  return { k: ISLAND_STATUS.run, text: STATUS_TEXT.run };
};

/** The folded chat over a site: the agent's last line, what it does now, «Стоп», and the field. */
const IslandView = ({ status }: { status: TIslandStatus }) => {
  const [value, setValue] = useState('');
  return (
    <section className="hx-site hx-gallery-dl__site" data-mode="island" data-min="false" data-run="idle">
      <SiteIsland
        value={value}
        onChange={setValue}
        onSend={() => setValue('')}
        canSend={value.trim() !== ''}
        placeholder="Опишите правку или выделите блок"
        sending={false}
        status={status}
        last={<IslandLast text="Поменял кнопку в шапке на синюю и убрал лишний отступ" onExpand={() => {}} />}
        onStop={() => {}}
        onDismiss={() => {}}
        onExpand={() => {}}
        min={false}
        onMin={() => {}}
        mode="island"
        onZone={() => {}}
        recording={false}
        notes={0}
        pick={false}
        onPick={() => {}}
      />
    </section>
  );
};

const DesignlyGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  const lang = query.lang === 'en' ? 'en' : 'ru';
  pinLocale(lang);
  const hasLook = query.look !== '0';
  const [text, setText] = useState(typeof query.text === 'string' ? query.text : '');
  const [style, setStyle] = useState<IStyleEdit>(NO_EDIT);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  const draft: INoteDraft = {
    picked: { ...PICKED, look: hasLook ? PICKED.look : null },
    at: { x: 40, y: 56, right: false },
    text,
    style,
  };
  if (query.view === 'island')
    return (
      <div className="hx-root hx-gallery-dl">
        <IslandView status={statusOf(query.status)} />
      </div>
    );
  return (
    <div className="hx-root hx-gallery-dl">
      <div className="hx-gallery-dl__page">
        <a className="hx-gallery-dl__cta">{PICKED.text}</a>
        <NotePopover
          n={1}
          draft={draft}
          text={text}
          onText={setText}
          mic={{ recording: false, onToggle: () => {} }}
          needsText={false}
          onOk={() => {}}
          onCancel={() => {}}
          onDelete={() => {}}
          onStyle={setStyle}
          gone={false}
        />
      </div>
    </div>
  );
};

export default DesignlyGallery;
