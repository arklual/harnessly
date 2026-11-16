// The widgets under the chat's right column on made-up data, for the eye and for screenshots: ?dev=widgets.
// The chat's header with its buttons (the terminal, the pane, «Виджеты») over a feed, and the stack beside it.
// ?mode=col|pop|shut the column (where there is room), the popover at the button (where there is not) or none;
// ?theme=light|dark&lang=en|ru the look; ?folded=1 the second card folded; ?empty=1 no widgets yet. Never part of a
// production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/styles/header.css';
import '@/next/styles/notice-stack.css';
import '@/next/styles/smart-notice.css';

import { useEffect, useRef, useState, type ReactNode } from 'react';
import { PortalContext } from '@/next/ui/portal';
import { useRouter } from '@/platform/router';
import { pinLocale, t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { BackgroundButton, TerminalButton } from '@/next/features/header/header-buttons';
import SideWidgets from '@/next/features/widgets/side-widgets';
import type { ISideWidget } from '@/next/features/widgets/model';

const LOOK: Record<string, { title: string; icon: string }> = {
  limits: { title: 'next.widgets.limits.title', icon: 'gauge' },
  machine: { title: 'next.widgets.machine.title', icon: 'server' },
  subagents: { title: 'next.widgets.subagents.title', icon: 'workflow' },
  pipeline: { title: 'next.widgets.pipeline.title', icon: 'git-merge' },
  note: { title: 'next.widgets.note.title', icon: 'pencil' },
  clock: { title: 'next.widgets.clock.title', icon: 'clock' },
};

const Meter = ({ left }: { left: number }) => (
  <svg className="hx-wgm" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true">
    <rect className="hx-wgm__track" x="0" y="0" width="100" height="4" rx="2" />
    <rect
      className="hx-wgm__fill"
      data-level={left < 10 ? 'out' : left < 30 ? 'low' : undefined}
      x="0"
      y="0"
      width={left}
      height="4"
      rx="2"
    />
  </svg>
);

const Win = ({ name, left, shown }: { name: string; left: number; shown?: number }) => (
  <span className="hx-wgl__win">
    <small>{name}</small>
    <Meter left={left} />
    <b>{shown ?? left}%</b>
  </span>
);

const BODY: Record<string, () => ReactNode> = {
  limits: () => (
    <div className="hx-wgl">
      <div className="hx-wgl__row">
        <span className="hx-wgl__name">Claude</span>
        <Win name="5 ч" left={64} />
        <Win name="Нед." left={22} />
      </div>
      <div className="hx-wgl__row">
        <span className="hx-wgl__name">Codex</span>
        <Win name="Нед." left={75} />
      </div>
    </div>
  ),
  machine: () => (
    <div className="hx-wgl">
      <span className="hx-wgl__name">dev-department</span>
      <Win name="ЦП" left={58} shown={42} />
      <Win name="ОЗУ" left={29} shown={71} />
      <Win name="Диск" left={66} shown={34} />
    </div>
  ),
  subagents: () => (
    <div className="hx-wgr">
      {['Ресерч коннекторов', 'Ревью вёрстки шапки', 'Прогон eval на build-1'].map((x, i) => (
        <button key={x} type="button" className="hx-wgr__row">
          <span className="hx-wgr__dot" data-state={i === 1 ? 'wait' : 'run'} />
          <span className="hx-wgr__title">{x}</span>
        </button>
      ))}
    </div>
  ),
  pipeline: () => (
    <div className="hx-wgr">
      <a className="hx-wgr__row" href="#">
        <span className="hx-wgr__dot" data-state="running" />
        <span className="hx-wgr__title">Виджеты: вид шапки</span>
        <span className="hx-wgr__id">!812</span>
      </a>
      <a className="hx-wgr__row" href="#">
        <span className="hx-wgr__dot" data-state="success" />
        <span className="hx-wgr__title">Лимиты из /api/oauth/usage</span>
        <span className="hx-wgr__id">!809</span>
      </a>
    </div>
  ),
  note: () => <textarea className="hx-wgn" defaultValue="Проверить поповер на 1280 и тёмную тему" />,
  clock: () => (
    <div className="hx-wgc">
      <span className="hx-wgc__big">07:42</span>
      <span className="hx-wgc__row">
        {[5, 15, 25].map((m) => (
          <button key={m} type="button" className="hx-wgc__btn">
            {m} мин
          </button>
        ))}
      </span>
    </div>
  ),
};

const FEED = [
  'Сделай кнопку «Виджеты» как соседние и поправь вид стопки.',
  'Смотрю шапку: у кнопки своя круглая подложка и пилюля, поэтому она выглядит нажатой. Привожу к виду терминала и панели, затем снимаю стопку в обеих темах на 1280, 1440 и 1920.',
  'Хорошо, жду скриншоты.',
];

const WidgetsGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const mode = query.mode === 'pop' ? 'pop' : query.mode === 'shut' ? 'shut' : 'col';
  const portalRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(mode !== 'shut');
  const [items, setItems] = useState<ISideWidget[]>(() =>
    query.empty
      ? []
      : ['limits', 'subagents', 'machine', 'pipeline', 'note', 'clock'].map((k, i) => ({
          id: k,
          kind: k as ISideWidget['kind'],
          folded: query.folded !== undefined && i === 1,
        })),
  );

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);

  return (
    <PortalContext value={portalRef}>
      <div className="hx-root" style={{ height: '100dvh', display: 'flex', background: 'var(--hx-bg-app)' }}>
        <div className="hx-main" style={{ flex: 1, marginLeft: 10 }}>
          <header className="hx-header">
            <span className="hx-header__title">
              <span className="hx-header__name">Виджеты под правой колонкой</span>
            </span>
            <span style={{ flex: 1 }} />
            <div className="hx-header__end">
              <TerminalButton terminal={{ open: false, onToggle: () => {} }} />
              <button type="button" className="hx-head-btn hx-tipped" aria-label={t('next.header.pane')}>
                <Icon name="panel-right" size="sm" />
              </button>
              <BackgroundButton
                background={{
                  running: 0,
                  total: 0,
                  recap: false,
                  widgets: true,
                  docked: mode === 'col',
                  open,
                  onToggle: () => setOpen((o) => !o),
                }}
              />
            </div>
          </header>
          <div className="hx-stage" data-phase="chat">
            <div style={{ maxWidth: 720, margin: '24px auto', padding: '0 24px', display: 'grid', gap: 16 }}>
              {FEED.map((x, i) => (
                <p
                  key={x}
                  style={{
                    margin: 0,
                    justifySelf: i % 2 ? 'start' : 'end',
                    maxWidth: 560,
                    padding: i % 2 ? 0 : '10px 14px',
                    borderRadius: 18,
                    background: i % 2 ? 'none' : 'var(--hx-bg-subtle)',
                    lineHeight: 1.6,
                  }}
                >
                  {x}
                </p>
              ))}
            </div>
            {mode !== 'shut' && (
              <div className="hx-side-below">
                <div className="hx-wgs-wrap" data-shown={open} data-pop={mode === 'pop' || undefined}>
                  <SideWidgets
                    widgets={items}
                    lookOf={(w) => ({
                      title: t(LOOK[w.kind]?.title ?? ''),
                      icon: LOOK[w.kind]?.icon ?? 'square',
                    })}
                    renderBody={(w) => BODY[w.kind]?.() ?? null}
                    gallery={[]}
                    onMove={() => {}}
                    onFold={(id, folded) =>
                      setItems((s) => s.map((x) => (x.id === id ? { ...x, folded } : x)))
                    }
                    onRemove={(id) => setItems((s) => s.filter((x) => x.id !== id))}
                  />
                </div>
              </div>
            )}
          </div>
        </div>
        <div ref={portalRef} />
      </div>
    </PortalContext>
  );
};

export default WidgetsGallery;
