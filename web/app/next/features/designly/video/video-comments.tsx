// The comments of a video: the list (the team's and the guests', merged) and the composer with its «what I am
// commenting» chip: «Сцена 3 · 0:12–0:15 · Заголовок · 9:16». Adds and resolves pessimistically.
import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { formatVideoAnnotation } from '@/next/features/designly/video/chat-context';
import {
  commentStart,
  spanText,
  targetParts,
  type IVideoCommentItem,
} from '@/next/features/designly/video/comments-model';
import { useVideoComments } from '@/next/features/designly/video/use-video-comments';
import type { ISiteStage } from '@/next/features/designly/site-view';
import type { IVideoCommentTarget, IVideoScene, IVideoState } from '@/next/features/designly/video/types';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { toast } from '@/next/features/designly/sdk/toast';

export interface IVideoCommentsProps {
  machine: MachineId;
  id: string;
  video: IVideoState;
  /** The person may write and resolve comments (role comment and above). */
  canComment: boolean;
  /** What a new comment is about; null: no composer open. */
  target: IVideoCommentTarget | null;
  onTarget: (target: IVideoCommentTarget | null) => void;
  /** Jumps the player to a comment's time, ms of the whole video. */
  onSeek: (time: number) => void;
  /** The chat a comment is sent to («В чат»). */
  stage?: ISiteStage;
  /** «Агенту» on a comment: its `<annotation kind="video">` text for the chat's context. */
  onSendToChat?: (annotation: string) => void;
}

/** The composer leaves in 160 ms. */
const COMPOSER_OUT_MS = 160;

const annotationOf = (id: string, video: IVideoState, item: IVideoCommentItem): string | null => {
  const scene: IVideoScene = video.scenes[item.scene];
  const source = video.comments.find((c) => c.id === item.key)?.video ?? null;
  if (!item.time) return null;
  return formatVideoAnnotation({
    n: item.n,
    project: id,
    scene,
    sceneNo: item.scene + 1,
    format: item.time.format,
    t0: item.time.t0,
    t1: item.time.t1,
    block: item.block,
    html: '',
    frame: source ? source.frame : item.frame,
    comment: item.text,
  });
};

const Row = ({
  item,
  scene,
  canComment,
  busy,
  onGo,
  onResolve,
  onAgent,
}: {
  item: IVideoCommentItem;
  scene: IVideoScene;
  canComment: boolean;
  busy: boolean;
  onGo: (item: IVideoCommentItem) => void;
  onResolve: (item: IVideoCommentItem) => void;
  onAgent: ((item: IVideoCommentItem) => void) | null;
}) => (
  <div className="hx-video-cm" data-done={item.resolved ? '1' : '0'} onClick={() => onGo(item)}>
    <span className="hx-video-cm__n">{item.n}</span>
    <div className="hx-video-cm__b">
      <small>
        {t('next.video.comments.who', { who: item.who ?? t('next.video.comments.guest') })}
        {' · '}
        {item.time
          ? t('next.video.comments.where', {
              n: item.scene + 1,
              time: spanText(
                scene.start + item.time.t0,
                item.time.t1 === null ? null : scene.start + item.time.t1,
              ),
            })
          : t('next.video.comments.board', { n: item.scene + 1 })}
        {item.time?.changed && <em>{` · ${t('next.video.timeline.sceneChanged')}`}</em>}
      </small>
      {item.text}
    </div>
    {onAgent && (
      <button
        className="hx-btn"
        data-variant="ghost"
        data-size="sm"
        data-shape="icon"
        type="button"
        aria-label={t('next.video.comments.toAgent')}
        title={t('next.video.comments.toAgent')}
        onClick={(event) => {
          event.stopPropagation();
          onAgent(item);
        }}
      >
        <Icon name="send" size="sm" />
      </button>
    )}
    {canComment && (
      <button
        className="hx-btn"
        data-variant="ghost"
        data-size="sm"
        data-shape="icon"
        type="button"
        aria-label={t('next.video.comments.resolve')}
        aria-pressed={item.resolved}
        disabled={busy}
        onClick={(event) => {
          event.stopPropagation();
          onResolve(item);
        }}
      >
        <Icon name="check" size="sm" />
      </button>
    )}
  </div>
);

const Composer = ({
  video,
  target,
  closing,
  onSend,
  onCancel,
}: {
  video: IVideoState;
  target: IVideoCommentTarget;
  closing: boolean;
  onSend: (text: string) => Promise<boolean>;
  onCancel: () => void;
}) => {
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  const parts = targetParts(video.scenes, target);
  useEffect(() => field.current?.focus({ preventScroll: true }), []);

  const send = async () => {
    const body = text.trim();
    if (sending || closing || body === '') return;
    setSending(true);
    if (!(await onSend(body))) setSending(false);
  };
  const onKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onCancel();
    } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void send();
    }
  };

  return (
    <section
      className="hx-video-cmp"
      data-closing={closing ? '1' : '0'}
      aria-label={t('next.video.comments.new')}
    >
      {parts && (
        <div className="hx-video-cmp__chip">
          <Icon name="locate" size="sm" />
          {t('next.video.comments.chip', {
            n: parts.scene,
            time: parts.time,
            name: parts.name,
            format: parts.format,
          })}
        </div>
      )}
      <textarea
        ref={field}
        value={text}
        placeholder={t('next.video.comments.placeholder')}
        aria-label={t('next.video.comments.text')}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={onKey}
      />
      <div className="hx-video-cmp__row">
        <button className="hx-btn" data-size="sm" type="button" onClick={onCancel}>
          {t('next.video.comments.cancel')}
        </button>
        <button
          className="hx-btn"
          data-variant="primary"
          data-size="sm"
          type="button"
          disabled={sending || text.trim() === '' || parts === null}
          onClick={() => void send()}
        >
          {t('next.video.comments.send')}
        </button>
      </div>
    </section>
  );
};

const VideoComments = ({
  machine,
  id,
  video,
  canComment,
  target,
  onTarget,
  onSeek,
  onSendToChat,
}: IVideoCommentsProps) => {
  const comments = useVideoComments(machine, id, video);
  const [busy, setBusy] = useState<string | null>(null);
  const composer = usePresence(canComment ? target : null, COMPOSER_OUT_MS);

  const resolve = async (item: IVideoCommentItem) => {
    setBusy(item.key);
    await comments.resolve(item);
    setBusy(null);
  };
  const go = (item: IVideoCommentItem) => {
    const start = commentStart(video.scenes, item);
    if (start !== null) onSeek(start);
  };
  const agent = onSendToChat
    ? (item: IVideoCommentItem) => {
        const text = annotationOf(id, video, item);
        if (text !== null) onSendToChat(text);
      }
    : null;

  const send = async (text: string): Promise<boolean> => {
    const c = composer.value;
    if (!c) return false;
    const ok = await comments.add({
      page: c.page,
      t0: c.t0,
      t1: c.t1,
      format: c.format,
      x: c.pick ? c.pick.x : null,
      y: c.pick ? c.pick.y : null,
      block: c.pick?.block ?? '',
      label: c.pick?.block ?? '',
      text,
    });
    if (!ok) return false;
    toast(t('next.video.comments.added'), { tone: 'success' });
    onTarget(null);
    return true;
  };

  return (
    <div className="hx-video-cms" role="tabpanel">
      {composer.value && (
        <Composer
          key={`${composer.value.page}:${composer.value.t0}:${composer.value.t1}`}
          video={video}
          target={composer.value}
          closing={composer.state === 'closing'}
          onSend={send}
          onCancel={() => onTarget(null)}
        />
      )}
      {comments.items.map((item) => (
        <Row
          key={item.key}
          item={item}
          scene={video.scenes[item.scene]}
          canComment={canComment}
          busy={busy === item.key}
          onGo={go}
          onResolve={resolve}
          onAgent={agent}
        />
      ))}
      {comments.items.length === 0 && !composer.value && (
        <div className="hx-video-cm-empty">
          {t('next.video.comments.empty')}
          <br />
          {t('next.video.comments.emptyHint')}
        </div>
      )}
    </div>
  );
};

export default VideoComments;
