// The video's shell (WP3c): bar, stage with the player, timeline, and the left panel with the chat and the comments.
// The view holds what the parts share — the format on screen, the playhead, the selected range, the source (live or MP4)
// and what a comment is about — and hands it to the player (video-player.tsx), the timeline (video-timeline.tsx) and the
// comments (video-comments.tsx). The deck's own parts serve a video too: «Стиль» is DeckStyle on /design/deck/settings,
// «Поделиться» is DeckShare in its video variant (the format the link opens in). A phone gets the player and a list of scenes instead of the timeline.
import { useEffect, useState, type ReactNode } from 'react';
import type { ISiteAccess, ISiteStage } from '@/next/features/designly/site-view';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { Skeleton } from '@/next/ui/skeleton';
import { useMedia } from '@/next/ui/use-media';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { VIDEO_FORMAT } from '@/next/features/designly/video/types';
import { useVideoView } from '@/next/features/designly/video/use-video-view';
import { goTo, ROUTE } from '@/next/routes/routes';
import { setDeck } from '@/next/stores/deck';
import { changeVideo, useVideo } from '@/next/stores/video';
import { fetchDeck, putDeckSettings } from '@/next/features/designly/deck/api';
import {
  DECK_PANEL,
  type IDeckSettings,
  type TDeckLayout,
  type TDeckPanelMode,
} from '@/next/features/designly/deck/types';
import DeckStyle, { DECK_THEMES, deckTheme, useThemeVars } from '@/next/features/designly/deck/deck-style';
import DeckShare from '@/next/features/designly/deck/deck-share';
import { shareFormatsOf } from '@/next/features/designly/video/share-view';
import VideoBar from '@/next/features/designly/video/video-bar';
import VideoComments from '@/next/features/designly/video/video-comments';
import VideoPanel, {
  VIDEO_TAB,
  type IVideoChatContext,
  type TVideoTab,
} from '@/next/features/designly/video/video-panel';
import VideoPlayer from '@/next/features/designly/video/video-player';
import VideoRender from '@/next/features/designly/video/video-render';
import { useVideoOverlays } from '@/next/features/designly/video/use-video-overlays';
import VideoTimeline from '@/next/features/designly/video/video-timeline';
import {
  clock,
  formatOf,
  renderBadge,
  renderOf,
  sceneAt,
  sourceOf,
  targetOf,
} from '@/next/features/designly/video/video-model';
import {
  VIDEO_SOURCE,
  type IVideoCommentTarget,
  type IVideoPick,
  type IVideoRange,
  type IVideoScene,
  type IVideoState,
  type TVideoFormat,
  type TVideoSource,
} from '@/next/features/designly/video/types';

export interface IVideoBoard {
  path: string;
  title: string;
}

export interface IVideoViewProps {
  machine: MachineId;
  /** The project's id: the video routes' `artifact`. */
  id: string;
  title: string;
  /** The video's boards (scenes) in the order of its pages; the video's own state comes from the store. */
  boards: readonly IVideoBoard[];
  /** The board on screen; null before the project is read. */
  path: string | null;
  onGo: (path: string) => void;
  /** The frame address of the board on screen: on the first frame it is the slot's ticket address. Null while the slot opens. */
  src: string | null;
  /** The plain address of any board in the open slot (the next scene's frame), valid once the first frame loaded `src`. */
  frameSrc: (path: string) => string | null;
  slotError: string | null;
  onReload: () => void;
  /** The site's stage: its `chat` is the panel's chat tab, `context` and `ask` talk to it. */
  stage?: ISiteStage;
  /** Set in the right pane beside a chat: the compact column, its chat is the host's. */
  pane?: { onBack: () => void };
  access?: ISiteAccess;
  maxed: boolean;
  onToggleMax: () => void;
  onOpenDrawer: () => void;
  /** What shows until the shell is built: the site view of the same project. */
  fallback: ReactNode;
  /** The video's state could not be read: why (the shell still shows the boards). */
  videoError?: string | null;
}

/** The deck board's breakpoints: ≤560 is a phone. */
export const VIDEO_PHONE_QUERY = '(max-width: 560px)';

const POP = { style: 'style', share: 'share', render: 'render' } as const;
type TPop = (typeof POP)[keyof typeof POP];
const POP_EXIT_MS = 160;
const FOCUS_DELAY_MS = 60;

const LAYOUT_KEYS = {
  cover: 'next.deck.style.layoutCover',
  statement: 'next.deck.style.layoutStatement',
  list: 'next.deck.style.layoutList',
  cards: 'next.deck.style.layoutCards',
} as const satisfies Record<TDeckLayout, string>;

const focusComposer = () =>
  setTimeout(
    () => document.querySelector<HTMLTextAreaElement>('.hx-composer__input')?.focus({ preventScroll: true }),
    FOCUS_DELAY_MS,
  );

interface ISceneListProps {
  scenes: readonly IVideoScene[];
  time: number;
  onPick: (scene: IVideoScene) => void;
}

/** The phone's scenes: a tap puts the playhead at the scene's start. */
const SceneList = ({ scenes, time, onPick }: ISceneListProps) => {
  const now = sceneAt(scenes, time);
  return (
    <ol className="hx-video-scenes" aria-label={t('next.video.shell.scenes')}>
      {scenes.map((scene, i) => (
        <li key={scene.id}>
          <button
            type="button"
            data-on={scene.id === now?.id ? '1' : '0'}
            aria-current={scene.id === now?.id || undefined}
            onClick={() => onPick(scene)}
          >
            <span>{t('next.video.shell.sceneN', { n: i + 1 })}</span>
            <b>{scene.title}</b>
            <em>{clock(scene.dur)}</em>
          </button>
        </li>
      ))}
    </ol>
  );
};

const Empty = ({ children }: { children: ReactNode }) => <div className="hx-video__empty">{children}</div>;

const chatContextOf = (video: IVideoState, time: number, sent: string | null): IVideoChatContext | null => {
  if (sent !== null)
    return { label: t('next.video.shell.sentChip'), text: sent, selected: true, block: true };
  const scene = sceneAt(video.scenes, time);
  if (scene === null) return null;
  const n = video.scenes.indexOf(scene) + 1;
  return {
    label: t('next.video.shell.chatContext', { n, time: clock(time) }),
    text: `scene ${n}, ${clock(time)}`,
    selected: false,
    block: false,
  };
};

const VideoView = (props: IVideoViewProps) => {
  const { machine, id, title, path, onGo, src, frameSrc, slotError, onReload, stage, pane, access } = props;
  const { videoError = null } = props;
  const video = useVideo(id);
  const phone = useMedia(VIDEO_PHONE_QUERY);
  const embed = pane !== undefined;

  const [chosen, setChosen] = useState<TVideoFormat | null>(null);
  const [want, setWant] = useState<TVideoSource>(VIDEO_SOURCE.live);
  const [time, setTime] = useState(0);
  const [range, setRange] = useState<IVideoRange | null>(null);
  const [loop, setLoop] = useState(false);
  const [target, setTarget] = useState<IVideoCommentTarget | null>(null);
  const [sent, setSent] = useState<string | null>(null);
  const [panel, setPanel] = useState<TDeckPanelMode>(phone ? DECK_PANEL.off : DECK_PANEL.dock);
  const [tab, setTab] = useState<TVideoTab>(VIDEO_TAB.chat);
  // A phone hides the panel, a wide window brings it back docked.
  const [wasPhone, setWasPhone] = useState(phone);
  if (wasPhone !== phone) {
    setWasPhone(phone);
    setPanel(phone ? DECK_PANEL.off : DECK_PANEL.dock);
  }
  const [pop, setPop] = useState<TPop | null>(null);
  const shownPop = usePresence(pop, POP_EXIT_MS);
  const [playing, setPlaying] = useState(false);
  const overlays = useVideoOverlays(machine, id);
  const view = useVideoView({
    machine,
    id,
    time,
    playing,
    format: video ? formatOf(video, chosen) : VIDEO_FORMAT.landscape,
    onSeek: setTime,
    onFormat: setChosen,
    onRange: setRange,
    onLoop: setLoop,
  });
  const shownOffer = usePresence(view.offer, POP_EXIT_MS);
  const themeRef = useThemeVars<HTMLDivElement>(
    video ? deckTheme(video.deck).vars : DECK_THEMES[0].vars,
    video?.deck.font,
  );

  // The deck's share popover reads the deck's state; the video's project has one too (the deck routes serve it).
  const sharing = pop === POP.share;
  useEffect(() => {
    if (!sharing) return;
    const abort = new AbortController();
    fetchDeck(machine, id, abort.signal).then(
      (deck) => setDeck(id, deck),
      () => undefined,
    );
    return () => abort.abort();
  }, [sharing, machine, id]);

  if (video === undefined) {
    // Never an empty screen: the video is on its way (a bar stands in) or its read failed (the words and a way on).
    return (
      <div ref={themeRef} className="hx-deck hx-video" data-screen="edit" data-panel="off">
        {videoError === null ? (
          <Empty>
            <Skeleton className="hx-video__wait" />
            <span className="hx-video__wait-text">{t('next.video.shell.loading')}</span>
          </Empty>
        ) : (
          <Empty>
            <p>{t('next.video.shell.failed')}</p>
            <small>{videoError}</small>
            <button type="button" className="hx-btn" data-variant="ghost" data-size="sm" onClick={onReload}>
              {t('next.video.shell.reload')}
            </button>
          </Empty>
        )}
      </div>
    );
  }

  const format = formatOf(video, chosen);
  const source = sourceOf(video, format, want);
  const info = renderOf(video, format);
  const scenes = video.scenes;
  const now = sceneAt(scenes, time);
  const crumb = now ? t('next.video.shell.crumb', { n: scenes.indexOf(now) + 1, time: clock(time) }) : '';
  const panelMode = embed ? DECK_PANEL.off : panel;

  const togglePop = (which: TPop) => setPop((cur) => (cur === which ? null : which));
  const openPanel = (on: TVideoTab) => {
    setTab(on);
    setPanel((cur) => (cur === DECK_PANEL.off ? DECK_PANEL.dock : cur));
  };
  const back = () => {
    if (pane) pane.onBack();
    else goTo({ kind: ROUTE.designly });
  };
  const comment = (r: IVideoRange | null, pick: IVideoPick | null) => {
    const next = targetOf(video, format, time, r, pick);
    if (next === null) return;
    setTarget(next);
    openPanel(VIDEO_TAB.comments);
  };
  const sendToChat = (annotation: string) => {
    setSent(annotation);
    if (embed) {
      stage?.context?.({
        label: t('next.video.shell.sentChip'),
        text: annotation,
        selected: true,
        block: true,
      });
      focusComposer();
      return;
    }
    openPanel(VIDEO_TAB.chat);
    focusComposer();
  };
  const seek = (ms: number) => setTime(ms);
  const pickScene = (scene: IVideoScene) => {
    setTime(scene.start);
    onGo(scene.path);
  };
  const saveSettings = async (change: Partial<IDeckSettings>) => {
    const settings = await putDeckSettings(machine, id, change);
    changeVideo(id, (v) => ({ ...v, deck: settings }));
  };
  const ask = stage?.ask;
  const pickLayout = (layout: TDeckLayout) => {
    if (!now) return;
    ask?.(t('next.video.shell.layoutAsk', { n: scenes.indexOf(now) + 1, layout: t(LAYOUT_KEYS[layout]) }));
  };
  const shareFormats = shareFormatsOf(video.settings.formats);
  const onShare = access ? () => (access.manage ? togglePop(POP.share) : access.onOpen()) : undefined;

  const stageBody = (): ReactNode => {
    if (scenes.length === 0) return <Empty>{t('next.video.shell.noScenes')}</Empty>;
    if (slotError !== null) {
      return (
        <Empty>
          <p>{slotError}</p>
          <button type="button" className="hx-btn" data-variant="ghost" data-size="sm" onClick={onReload}>
            {t('next.video.shell.reload')}
          </button>
        </Empty>
      );
    }
    return (
      <VideoPlayer
        machine={machine}
        id={id}
        video={video}
        format={format}
        src={src}
        path={path}
        frameSrc={frameSrc}
        source={source}
        onSource={setWant}
        time={time}
        onTime={setTime}
        range={phone ? null : range}
        onRange={phone ? () => undefined : setRange}
        loop={loop}
        onComment={() => comment(range, null)}
        onPick={(pick) => comment(null, pick)}
        command={view.command}
        onPlaying={setPlaying}
        overlays={overlays}
      />
    );
  };

  return (
    <div
      ref={themeRef}
      className="hx-deck hx-video"
      data-screen="edit"
      data-panel={panelMode}
      data-embed={embed ? '1' : '0'}
      data-phone={phone ? '1' : '0'}
      data-path={path ?? undefined}
      onPointerDownCapture={view.touch}
      onKeyDownCapture={view.touch}
    >
      <VideoBar
        title={title}
        crumb={crumb}
        onBack={back}
        onPanel={
          embed
            ? undefined
            : () => setPanel((cur) => (cur === DECK_PANEL.off ? DECK_PANEL.dock : DECK_PANEL.off))
        }
        panelOn={panel !== DECK_PANEL.off}
        formats={video.settings.formats}
        format={format}
        onFormat={setChosen}
        source={source}
        onSource={setWant}
        mp4={info?.published === true}
        onComment={() => comment(range, null)}
        onStyle={() => togglePop(POP.style)}
        styleOn={pop === POP.style}
        onRender={() => togglePop(POP.render)}
        renderOn={pop === POP.render}
        badge={renderBadge(info)}
        onShare={onShare}
        shareOn={pop === POP.share}
      />

      {!embed && (
        <VideoPanel
          mode={panelMode}
          tab={tab}
          onMode={setPanel}
          onTab={setTab}
          phone={phone}
          chat={stage?.deckChat ?? null}
          comments={
            <VideoComments
              machine={machine}
              id={id}
              video={video}
              canComment
              target={target}
              onTarget={setTarget}
              onSeek={seek}
              stage={stage}
              onSendToChat={sendToChat}
            />
          }
          context={chatContextOf(video, time, sent)}
          site={stage}
        />
      )}

      <div className="hx-video__main">
        {shownOffer.value && (
          <div className="hx-video__offer" data-state={shownOffer.state}>
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={view.takeOffer}
            >
              {t('next.video.shell.offerShow')}
              {shownOffer.value.t !== undefined ? ` ${clock(shownOffer.value.t)}` : ''}
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              aria-label={t('next.video.shell.offerDrop')}
              onClick={view.dropOffer}
            >
              ×
            </button>
          </div>
        )}
        <div className="hx-video__stage" data-format={format}>
          {stageBody()}
        </div>
        {scenes.length > 0 && phone && <SceneList scenes={scenes} time={time} onPick={pickScene} />}
        {scenes.length > 0 && !phone && (
          <div className="hx-video__timeline">
            <VideoTimeline
              machine={machine}
              id={id}
              video={video}
              format={format}
              time={time}
              onSeek={seek}
              range={range}
              onRange={setRange}
              loop={loop}
              onLoop={setLoop}
              onComment={(r) => comment(r, null)}
              onSendToChat={sendToChat}
              overlays={overlays}
            />
          </div>
        )}
      </div>

      {shownPop.value === POP.style && (
        <DeckStyle
          state={shownPop.state}
          settings={video.deck}
          onSettings={saveSettings}
          layout={null}
          onLayout={pickLayout}
        />
      )}
      {shownPop.value === POP.render && (
        <VideoRender machine={machine} id={id} video={video} state={shownPop.state} />
      )}
      {shownPop.value === POP.share && shareFormats !== null && (
        <DeckShare
          machine={machine}
          project={id}
          title={title}
          closing={shownPop.state === PRESENCE.closing}
          onAsGuest={() => setPop(null)}
          video={{ formats: shareFormats }}
        />
      )}
    </div>
  );
};

export default VideoView;
