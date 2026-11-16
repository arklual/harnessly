// An app opened as the main thing in Sinopia's stage: the same section as an open design (site-view.tsx) — the chat
// docked at an edge, in a window, folded into the field over the page or alone on a phone, moved by the same grabs and
// kept the same way — with the app's view where Sinopia has the design's page. The strip over the view names the app's
// home and the object open in it (a document, a board); the right pane of the chat opens beside it as beside a design.
import { useLayoutEffect, useRef, type ReactNode } from 'react';
import { AppMenu, type IAppMenuItem } from '@/next/features/app-main/app-menu';
import { isSpaceApp } from '@/next/features/app-views/service-health';
import { ChatPlacement } from '@/next/features/designly/site-chat';
import type { ISiteStage } from '@/next/features/designly/site-view';
import Icon from '@/next/ui/icon';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';
import '@/next/styles/designly.css';
import '@/next/styles/site.css';
import '@/next/styles/site-tools.css';
import '@/next/styles/app-main.css';

/** The one stage of the person's spaces (boards, documents): one chat, one field and one draft over all of them. */
export const SPACES_STAGE = 'app:spaces';

/**
 * The stage's key among the designs' (their chats, their layouts): the app's home, or one of its objects. The spaces
 * share one: going from a board to a document or to a list, the island, its draft and its chat stay.
 */
export const appStageDesign = (app: string, object: string | null): string => {
  if (isSpaceApp(app)) return SPACES_STAGE;
  return object ? `app:${app}/${object}` : `app:${app}`;
};

export interface IAppStageProps {
  /** What is open (a merge request, a document, the app's start): never the app's own name. */
  title: string;
  /** After the title: the place of what is open (project and number). */
  crumb?: string;
  /** Back to where the person came from (the chat, the list, the app's start); absent: no back button. */
  onBack?: () => void;
  backLabel?: string;
  /** A screen opened from a chat folds back into it (the chat's right pane): not a way back, so its own icon and word. */
  onCollapse?: () => void;
  /** Before the title: the app's own control (the switch between looking and editing). */
  lead?: ReactNode;
  stage: ISiteStage;
  /** The app's view. */
  frame: ReactNode;
  /** The inspector under the view, when it is open. */
  inspector: ReactNode;
  menu: readonly IAppMenuItem[];
  /**
   * The app draws its own bar (a space, or a view with chrome none): no strip of the host above it on a wide screen, and
   * the host's actions reach the view through hl.host. A phone keeps the strip: its menu and «to the chat» live there.
   */
  bare?: boolean;
  /** Absent: the app has no link to share. */
  onShare?: () => void;
  /** The right pane of the chat (browser, files, terminal), as beside a design. */
  toolPane: { open: boolean; onToggle: () => void } | null;
  onOpenDrawer: () => void;
  /** A space (boards, documents): the chat is called over it from the field, so the strip has no chat buttons. */
  space?: boolean;
}

const AppStage = ({
  title,
  crumb,
  onBack,
  backLabel,
  onCollapse,
  lead,
  stage,
  frame,
  inspector,
  menu,
  bare = false,
  onShare,
  toolPane,
  onOpenDrawer,
  space = false,
}: IAppStageProps) => {
  const sectionRef = useRef<HTMLElement>(null);
  const phone = usePhone();
  const noStrip = bare && !phone;
  const size = () => {
    const rect = sectionRef.current?.getBoundingClientRect();
    return { width: rect?.width ?? 0, height: rect?.height ?? 0 };
  };

  // The chat's width and the window's place go to the CSS as on a design's page.
  useLayoutEffect(() => {
    sectionRef.current?.style.setProperty('--site-chat', `${stage.chatW}px`);
  }, [stage.chatW]);
  const { float, bindSection } = stage;
  // The stage counts a space's window from this section.
  useLayoutEffect(() => {
    bindSection?.(sectionRef.current);
    return () => bindSection?.(null);
  }, [bindSection]);
  useLayoutEffect(() => {
    const el = sectionRef.current;
    if (!el || !float) return;
    el.style.setProperty('--fx', `${float.x}px`);
    el.style.setProperty('--fy', `${float.y}px`);
    el.style.setProperty('--fw', `${float.w}px`);
    el.style.setProperty('--fh', `${float.h}px`);
  }, [float]);

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
      data-space={space}
      data-bare={noStrip}
    >
      {stage.chat}
      <div className="hx-site__view">
        <div className="hx-browser">
          {!noStrip && (
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
              {/* What is open, not which app it is. */}
              <div className="hx-strip__tabs">
                {lead}
                {onCollapse && (
                  <button
                    type="button"
                    className="hx-head-btn hx-tipped"
                    aria-label={t('next.appmain.collapse')}
                    onClick={onCollapse}
                  >
                    <Icon name="minimize-2" />
                    <span className="hx-tip" data-pos="below" data-align="start">
                      {t('next.appmain.collapse')}
                    </span>
                  </button>
                )}
                {onBack && !onCollapse && (
                  <button
                    type="button"
                    className="hx-head-btn"
                    aria-label={backLabel ?? t('next.appmain.back')}
                    onClick={onBack}
                  >
                    <Icon name="arrow-left" />
                  </button>
                )}
                <div className="hx-appmain__addr">
                  <h1 className="hx-appmain__title">{title}</h1>
                  {crumb && (
                    <>
                      <span aria-hidden="true">/</span>
                      <em className="hx-appmain__crumb">{crumb}</em>
                    </>
                  )}
                </div>
              </div>
              {/* Where the chat stands is picked in the open chat's head; with the chat folded, here. */}
              {!space && stage.mode !== 'chat' && stage.mode !== 'float' && (
                <ChatPlacement mode={stage.mode} onMode={(mode) => stage.onMode(mode, size())} />
              )}
              {!space && (
                <button
                  type="button"
                  className="hx-head-btn hx-site__swap"
                  aria-label={stage.mode === 'chat' ? t('next.site.siteFull') : t('next.site.chatFull')}
                  onClick={() => stage.onMode(stage.mode === 'chat' ? 'island' : 'chat', size())}
                >
                  <Icon name={stage.mode === 'chat' ? 'maximize-2' : 'message-square'} />
                </button>
              )}
              {onShare && (
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
              )}
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
          )}
          <div className="hx-appstage__body">{frame}</div>
          {inspector}
        </div>
      </div>
      {stage.island}
      {stage.spaceTurn}
      {stage.overlay}
    </section>
  );
};

export default AppStage;
