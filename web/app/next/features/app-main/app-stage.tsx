// An app opened as the main thing in Sinopia's stage: the same section as an open design (site-view.tsx) — the chat
// docked at an edge, in a window, folded into the field over the page or alone on a phone, moved by the same grabs and
// kept the same way — with the app's view where Sinopia has the design's page. The strip over the view names the app's
// home and the object open in it (a document, a board); the right pane of the chat opens beside it as beside a design.
import { useLayoutEffect, useRef, type ReactNode } from 'react';
import { AppMenu, type IAppMenuItem } from '@/next/features/app-main/app-main';
import { ChatPlacement } from '@/next/features/designly/site-chat';
import type { ISiteStage } from '@/next/features/designly/site-view';
import Icon from '@/next/ui/icon';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';
import '@/next/styles/designly.css';
import '@/next/styles/site.css';
import '@/next/styles/site-tools.css';
import '@/next/styles/app-main.css';

/** The stage's key among the designs' (their chats, their layouts): the app's home, or one of its objects. */
export const appStageDesign = (app: string, object: string | null): string =>
  object ? `app:${app}/${object}` : `app:${app}`;

export interface IAppStageProps {
  /** The app's name, the home's tab. */
  title: string;
  icon: string;
  /** The object open in the app; null on the app's home. */
  object: { title: string } | null;
  /** Back to the app's home from an open object. */
  onHome: () => void;
  stage: ISiteStage;
  /** The app's view. */
  frame: ReactNode;
  /** The inspector under the view, when it is open. */
  inspector: ReactNode;
  menu: readonly IAppMenuItem[];
  onShare: () => void;
  /** The right pane of the chat (browser, files, terminal), as beside a design. */
  toolPane: { open: boolean; onToggle: () => void } | null;
  onOpenDrawer: () => void;
}

const AppStage = ({
  title,
  icon,
  object,
  onHome,
  stage,
  frame,
  inspector,
  menu,
  onShare,
  toolPane,
  onOpenDrawer,
}: IAppStageProps) => {
  const sectionRef = useRef<HTMLElement>(null);
  const phone = usePhone();
  const size = () => {
    const rect = sectionRef.current?.getBoundingClientRect();
    return { width: rect?.width ?? 0, height: rect?.height ?? 0 };
  };

  // The chat's width and the window's place go to the CSS as on a design's page.
  useLayoutEffect(() => {
    sectionRef.current?.style.setProperty('--site-chat', `${stage.chatW}px`);
  }, [stage.chatW]);
  const { float } = stage;
  useLayoutEffect(() => {
    const el = sectionRef.current;
    if (!el || !float) return;
    el.style.setProperty('--fx', `${float.x}px`);
    el.style.setProperty('--fy', `${float.y}px`);
    el.style.setProperty('--fw', `${float.w}px`);
    el.style.setProperty('--fh', `${float.h}px`);
  }, [float]);

  const homeOn = object === null;
  return (
    <section
      className="hx-site hx-appstage"
      ref={sectionRef}
      data-mode={stage.mode}
      data-variant="site"
      data-dock={phone ? 'left' : stage.dock}
      data-min={stage.min}
      data-run={stage.run}
      data-pick={false}
      data-full={false}
      data-dev={false}
      data-sview={stage.sview && stage.mode === 'chat'}
    >
      {stage.chat}
      <div className="hx-site__view">
        <div className="hx-browser">
          <div className="hx-strip">
            <button
              type="button"
              className="hx-head-btn hx-site__menu"
              aria-label={t('next.header.openMenu')}
              onClick={onOpenDrawer}
            >
              <Icon name="menu" />
            </button>
            <button
              type="button"
              className="hx-tochat hx-site__back"
              aria-label={t('next.site.toChat')}
              onClick={() => stage.onSview(false)}
            >
              <Icon name="message-square" size="sm" />
              <span>{t('next.pane.chat')}</span>
            </button>
            <div className="hx-strip__tabs" role="tablist" aria-label={title}>
              <div
                className="hx-tab"
                role="tab"
                tabIndex={0}
                aria-selected={homeOn}
                data-kind="page"
                data-state="open"
                title={title}
                onClick={() => !homeOn && onHome()}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !homeOn) onHome();
                }}
              >
                <Icon name="house" size="sm" className="hx-tab__ic" />
                <span className="hx-tab__name">{title}</span>
              </div>
              {object && (
                <div
                  className="hx-tab"
                  role="tab"
                  tabIndex={0}
                  aria-selected
                  data-kind="page"
                  data-state="open"
                  title={object.title}
                >
                  <Icon name={icon} size="sm" className="hx-tab__ic" />
                  <span className="hx-tab__name">{object.title}</span>
                  <button
                    type="button"
                    className="hx-tab__x"
                    aria-label={t('next.site.closeTab')}
                    onClick={(event) => {
                      event.stopPropagation();
                      onHome();
                    }}
                  >
                    <Icon name="x" size="sm" />
                  </button>
                </div>
              )}
            </div>
            {/* Where the chat stands is picked in the open chat's head; with the chat folded, here. */}
            {stage.mode !== 'chat' && stage.mode !== 'float' && (
              <ChatPlacement mode={stage.mode} onMode={(mode) => stage.onMode(mode, size())} />
            )}
            <button
              type="button"
              className="hx-head-btn hx-site__swap"
              aria-label={stage.mode === 'chat' ? t('next.site.siteFull') : t('next.site.chatFull')}
              onClick={() => stage.onMode(stage.mode === 'chat' ? 'island' : 'chat', size())}
            >
              <Icon name={stage.mode === 'chat' ? 'maximize-2' : 'message-square'} />
            </button>
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-label={t('next.appmain.share')}
              onClick={onShare}
            >
              <Icon name="share" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.appmain.share')}
              </span>
            </button>
            {toolPane && (
              <button
                type="button"
                className="hx-head-btn hx-tipped"
                aria-pressed={toolPane.open}
                aria-label={t('next.header.pane')}
                onClick={toolPane.onToggle}
              >
                <Icon name="panel-right" />
                <span className="hx-tip" data-pos="below" data-align="end">
                  {t('next.header.pane')}
                </span>
              </button>
            )}
            <AppMenu items={menu} />
          </div>
          <div className="hx-appstage__body">{frame}</div>
          {inspector}
        </div>
      </div>
      {stage.island}
      {stage.overlay}
    </section>
  );
};

export default AppStage;
